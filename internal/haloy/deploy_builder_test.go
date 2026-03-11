package haloy

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/haloydev/haloy/internal/apitypes"
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
