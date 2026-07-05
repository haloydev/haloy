package haloy

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/haloydev/haloy/internal/apitypes"
	"github.com/haloydev/haloy/internal/config"
)

func TestGetBuilderWorkDir(t *testing.T) {
	// Create a temporary directory structure for testing
	tempDir, err := os.MkdirTemp("", "haloy-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Create a subdirectory
	subDir := filepath.Join(tempDir, "subdir")
	if err := os.Mkdir(subDir, 0o755); err != nil {
		t.Fatalf("failed to create subdir: %v", err)
	}

	// Create a config file
	configFile := filepath.Join(tempDir, "haloy.yaml")
	if err := os.WriteFile(configFile, []byte(""), 0o644); err != nil {
		t.Fatalf("failed to create config file: %v", err)
	}

	// Create a config file in subdirectory
	subConfigFile := filepath.Join(subDir, "haloy.yaml")
	if err := os.WriteFile(subConfigFile, []byte(""), 0o644); err != nil {
		t.Fatalf("failed to create sub config file: %v", err)
	}

	tests := []struct {
		name       string
		configPath string
		expected   string
	}{
		{
			name:       "empty path returns current dir",
			configPath: "",
			expected:   ".",
		},
		{
			name:       "dot returns current dir",
			configPath: ".",
			expected:   ".",
		},
		{
			name:       "file path returns parent directory",
			configPath: configFile,
			expected:   tempDir,
		},
		{
			name:       "directory path returns the directory itself",
			configPath: subDir,
			expected:   subDir,
		},
		{
			name:       "nested file path returns parent directory",
			configPath: subConfigFile,
			expected:   subDir,
		},
		{
			name:       "relative file path returns parent",
			configPath: "some/path/haloy.yaml",
			expected:   "some/path",
		},
		{
			name:       "simple filename returns current dir",
			configPath: "haloy.yaml",
			expected:   ".",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := getBuilderWorkDir(tt.configPath)
			if result != tt.expected {
				t.Errorf("getBuilderWorkDir(%q) = %q, want %q", tt.configPath, result, tt.expected)
			}
		})
	}
}

func TestUploadImage_TempFilePattern(t *testing.T) {
	tests := []struct {
		name     string
		imageRef string
	}{
		{"simple image", "nginx:latest"},
		{"image with org slash", "myorg/myapp:latest"},
		{"deeply nested ref", "registry.io/org/app:v1"},
		{"no tag", "myapp"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sanitized := strings.NewReplacer("/", "-", ":", "-").Replace(tt.imageRef)
			pattern := fmt.Sprintf("haloy-upload-%s-*.tar", sanitized)
			f, err := os.CreateTemp("", pattern)
			if err != nil {
				t.Errorf("os.CreateTemp failed for image ref %q: %v", tt.imageRef, err)
				return
			}
			os.Remove(f.Name())
			f.Close()
		})
	}
}

func TestExtractDigestFromPath(t *testing.T) {
	tests := []struct {
		name      string
		layerPath string
		want      string
	}{
		{
			name:      "OCI format: blobs/sha256/<hash>",
			layerPath: "blobs/sha256/abc123def456",
			want:      "sha256:abc123def456",
		},
		{
			name:      "older buildkit format: blobs/sha256/<hash>/layer.tar",
			layerPath: "blobs/sha256/abc123def456/layer.tar",
			want:      "sha256:abc123def456",
		},
		{
			name:      "legacy format: sha256:<hash>/layer.tar",
			layerPath: "sha256:abc123def456/layer.tar",
			want:      "sha256:abc123def456",
		},
		{
			name:      "simple format: <hash>/layer.tar",
			layerPath: "abc123def456/layer.tar",
			want:      "sha256:abc123def456",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractDigestFromPath(tt.layerPath)
			if got != tt.want {
				t.Fatalf("extractDigestFromPath(%q) = %q, want %q", tt.layerPath, got, tt.want)
			}
		})
	}
}

func TestHasContentAddressedLayers(t *testing.T) {
	tests := []struct {
		name   string
		layers []string
		want   bool
	}{
		{
			name:   "OCI blobs format",
			layers: []string{"blobs/sha256/abc123", "blobs/sha256/def456"},
			want:   true,
		},
		{
			name:   "older buildkit format",
			layers: []string{"blobs/sha256/abc123/layer.tar"},
			want:   true,
		},
		{
			name:   "sha256-prefixed directories",
			layers: []string{"sha256:abc123/layer.tar"},
			want:   true,
		},
		{
			name:   "legacy chain ID directories",
			layers: []string{"abc123def456/layer.tar"},
			want:   false,
		},
		{
			name:   "mixed formats with one legacy layer",
			layers: []string{"blobs/sha256/abc123", "def456/layer.tar"},
			want:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			manifest := apitypes.ImageManifestEntry{Layers: tt.layers}
			if got := hasContentAddressedLayers(manifest); got != tt.want {
				t.Fatalf("hasContentAddressedLayers(%v) = %v, want %v", tt.layers, got, tt.want)
			}
		})
	}
}

func TestBuildImage_BuildArgsArePassedWithoutLiteralQuotes(t *testing.T) {
	origRunner := runCLICommandInDir
	t.Cleanup(func() { runCLICommandInDir = origRunner })

	var capturedName string
	var capturedArgs []string
	runCLICommandInDir = func(ctx context.Context, workDir, name string, args ...string) error {
		capturedName = name
		capturedArgs = args
		return nil
	}

	image := &config.Image{
		Repository: "myapp",
		Tag:        "latest",
		BuildConfig: &config.BuildConfig{
			Args: []config.BuildArg{
				{Name: "VITE_POCKETBASE_URL", ValueSource: config.ValueSource{Value: "https://pb.example.com"}},
				{Name: "LABEL", ValueSource: config.ValueSource{Value: "my value"}},
				{Name: "ENV_ONLY"},
			},
		},
	}

	if err := BuildImage(context.Background(), image.ImageRef(), image, ""); err != nil {
		t.Fatalf("BuildImage returned error: %v", err)
	}

	if capturedName != "docker" {
		t.Errorf("expected docker command, got %q", capturedName)
	}

	want := map[string]string{
		"VITE_POCKETBASE_URL": "VITE_POCKETBASE_URL=https://pb.example.com",
		"LABEL":               "LABEL=my value",
	}
	seen := map[string]bool{}
	envOnlySeen := false

	for i := 0; i < len(capturedArgs); i++ {
		if capturedArgs[i] != "--build-arg" {
			continue
		}
		if i+1 >= len(capturedArgs) {
			t.Fatalf("--build-arg at end of args with no value: %v", capturedArgs)
		}
		got := capturedArgs[i+1]

		if strings.Contains(got, `"`) {
			t.Errorf("build-arg contains literal double quote: %q", got)
		}

		if got == "ENV_ONLY" {
			envOnlySeen = true
			continue
		}

		name, _, ok := strings.Cut(got, "=")
		if !ok {
			t.Errorf("build-arg missing '=': %q", got)
			continue
		}
		expected, known := want[name]
		if !known {
			t.Errorf("unexpected build-arg %q", got)
			continue
		}
		if got != expected {
			t.Errorf("build-arg %s = %q, want %q", name, got, expected)
		}
		seen[name] = true
	}

	for name := range want {
		if !seen[name] {
			t.Errorf("missing build-arg for %s in %v", name, capturedArgs)
		}
	}
	if !envOnlySeen {
		t.Errorf("missing name-only build-arg ENV_ONLY in %v", capturedArgs)
	}
}

func TestFormatDiskSpaceEstimateMessage_FullUpload(t *testing.T) {
	msg := formatDiskSpaceEstimateMessage(
		apitypes.ImageDiskSpaceCheckRequest{
			UploadSizeBytes: 512 * 1024 * 1024,
		},
		apitypes.ImageDiskSpaceCheckResponse{
			RequiredBytes:  3*1024*1024*1024 + 128*1024*1024,
			AvailableBytes: 10 * 1024 * 1024 * 1024,
		},
	)

	want := "Server disk space estimate: need 3.1 GiB, have 10.0 GiB free (includes temporary image tar, Docker load, and 2.0 GiB reserve)"
	if msg != want {
		t.Fatalf("message = %q, want %q", msg, want)
	}
}

func TestFormatDiskSpaceEstimateMessage_LayeredUploadAllLayersCached(t *testing.T) {
	msg := formatDiskSpaceEstimateMessage(
		apitypes.ImageDiskSpaceCheckRequest{
			AssembledImageSizeBytes: 576 * 1024 * 1024,
		},
		apitypes.ImageDiskSpaceCheckResponse{
			RequiredBytes:  3*1024*1024*1024 + 128*1024*1024,
			AvailableBytes: 10 * 1024 * 1024 * 1024,
		},
	)

	want := "Server disk space estimate: need 3.1 GiB, have 10.0 GiB free (includes assembled temp image tar, Docker load, 2.0 GiB reserve)"
	if msg != want {
		t.Fatalf("message = %q, want %q", msg, want)
	}
}

func TestFormatDiskSpaceEstimateMessage_LayeredUploadWithMissingLayers(t *testing.T) {
	msg := formatDiskSpaceEstimateMessage(
		apitypes.ImageDiskSpaceCheckRequest{
			LayerUploadBytes:        128 * 1024 * 1024,
			AssembledImageSizeBytes: 576 * 1024 * 1024,
		},
		apitypes.ImageDiskSpaceCheckResponse{
			RequiredBytes:  3*1024*1024*1024 + 256*1024*1024,
			AvailableBytes: 10 * 1024 * 1024 * 1024,
		},
	)

	want := "Server disk space estimate: need 3.2 GiB, have 10.0 GiB free (includes missing layer upload, assembled temp image tar, Docker load, 2.0 GiB reserve)"
	if msg != want {
		t.Fatalf("message = %q, want %q", msg, want)
	}
}
