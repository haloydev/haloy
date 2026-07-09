package haloy

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/haloydev/haloy/internal/config"
)

func writeTestFile(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("failed to create dir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte("FROM scratch\n"), 0o644); err != nil {
		t.Fatalf("failed to create file %s: %v", path, err)
	}
}

func TestResolveBuildPaths(t *testing.T) {
	t.Run("defaults resolve to Dockerfile in config dir", func(t *testing.T) {
		configDir := t.TempDir()
		writeTestFile(t, filepath.Join(configDir, "Dockerfile"))

		paths, err := resolveBuildPaths(configDir, nil)
		if err != nil {
			t.Fatalf("resolveBuildPaths returned error: %v", err)
		}
		if paths.ContextDir != configDir {
			t.Errorf("ContextDir = %q, want %q", paths.ContextDir, configDir)
		}
		if want := filepath.Join(configDir, "Dockerfile"); paths.Dockerfile != want {
			t.Errorf("Dockerfile = %q, want %q", paths.Dockerfile, want)
		}
	})

	t.Run("dockerfile resolves relative to context", func(t *testing.T) {
		configDir := t.TempDir()
		writeTestFile(t, filepath.Join(configDir, "platform", "docker", "Dockerfile"))

		paths, err := resolveBuildPaths(configDir, &config.BuildConfig{
			Context:    "platform",
			Dockerfile: "docker/Dockerfile",
		})
		if err != nil {
			t.Fatalf("resolveBuildPaths returned error: %v", err)
		}
		if want := filepath.Join(configDir, "platform"); paths.ContextDir != want {
			t.Errorf("ContextDir = %q, want %q", paths.ContextDir, want)
		}
		if want := filepath.Join(configDir, "platform", "docker", "Dockerfile"); paths.Dockerfile != want {
			t.Errorf("Dockerfile = %q, want %q", paths.Dockerfile, want)
		}
	})

	t.Run("dockerfile outside context via parent path", func(t *testing.T) {
		configDir := t.TempDir()
		writeTestFile(t, filepath.Join(configDir, "Dockerfile"))
		if err := os.MkdirAll(filepath.Join(configDir, "app"), 0o755); err != nil {
			t.Fatalf("failed to create context dir: %v", err)
		}

		paths, err := resolveBuildPaths(configDir, &config.BuildConfig{
			Context:    "app",
			Dockerfile: "../Dockerfile",
		})
		if err != nil {
			t.Fatalf("resolveBuildPaths returned error: %v", err)
		}
		if want := filepath.Join(configDir, "Dockerfile"); paths.Dockerfile != want {
			t.Errorf("Dockerfile = %q, want %q", paths.Dockerfile, want)
		}
	})

	t.Run("absolute paths pass through", func(t *testing.T) {
		configDir := t.TempDir()
		otherDir := t.TempDir()
		dockerfile := filepath.Join(otherDir, "custom.Dockerfile")
		writeTestFile(t, dockerfile)

		paths, err := resolveBuildPaths(configDir, &config.BuildConfig{
			Context:    otherDir,
			Dockerfile: dockerfile,
		})
		if err != nil {
			t.Fatalf("resolveBuildPaths returned error: %v", err)
		}
		if paths.ContextDir != otherDir {
			t.Errorf("ContextDir = %q, want %q", paths.ContextDir, otherDir)
		}
		if paths.Dockerfile != dockerfile {
			t.Errorf("Dockerfile = %q, want %q", paths.Dockerfile, dockerfile)
		}
	})

	t.Run("missing dockerfile error names resolved path and bases", func(t *testing.T) {
		configDir := t.TempDir()
		if err := os.MkdirAll(filepath.Join(configDir, "platform"), 0o755); err != nil {
			t.Fatalf("failed to create context dir: %v", err)
		}

		_, err := resolveBuildPaths(configDir, &config.BuildConfig{
			Context:    "platform",
			Dockerfile: "platform/Dockerfile",
		})
		if err == nil {
			t.Fatal("expected error for missing dockerfile, got nil")
		}
		msg := err.Error()
		resolved := filepath.Join(configDir, "platform", "platform", "Dockerfile")
		for _, want := range []string{`dockerfile "platform/Dockerfile" not found`, resolved, filepath.Join(configDir, "platform"), configDir} {
			if !strings.Contains(msg, want) {
				t.Errorf("error %q does not contain %q", msg, want)
			}
		}
	})

	t.Run("missing context error names resolved path and config dir", func(t *testing.T) {
		configDir := t.TempDir()

		_, err := resolveBuildPaths(configDir, &config.BuildConfig{Context: "nope"})
		if err == nil {
			t.Fatal("expected error for missing context, got nil")
		}
		msg := err.Error()
		for _, want := range []string{`build context "nope" not found`, filepath.Join(configDir, "nope"), configDir} {
			if !strings.Contains(msg, want) {
				t.Errorf("error %q does not contain %q", msg, want)
			}
		}
	})

	t.Run("context that is a file errors", func(t *testing.T) {
		configDir := t.TempDir()
		writeTestFile(t, filepath.Join(configDir, "notadir"))

		_, err := resolveBuildPaths(configDir, &config.BuildConfig{Context: "notadir"})
		if err == nil || !strings.Contains(err.Error(), "is not a directory") {
			t.Fatalf("expected not-a-directory error, got: %v", err)
		}
	})

	t.Run("dockerfile that is a directory errors", func(t *testing.T) {
		configDir := t.TempDir()
		if err := os.MkdirAll(filepath.Join(configDir, "Dockerfile"), 0o755); err != nil {
			t.Fatalf("failed to create dir: %v", err)
		}

		_, err := resolveBuildPaths(configDir, nil)
		if err == nil || !strings.Contains(err.Error(), "is a directory, not a file") {
			t.Fatalf("expected dockerfile-is-a-directory error, got: %v", err)
		}
	})
}

func TestValidateBuildPaths(t *testing.T) {
	configDir := t.TempDir()

	t.Run("skips targets that do not build", func(t *testing.T) {
		if err := validateBuildPaths(configDir, config.TargetConfig{}); err != nil {
			t.Errorf("expected nil for target without image, got: %v", err)
		}

		noBuild := config.TargetConfig{Image: &config.Image{Repository: "myapp"}}
		if err := validateBuildPaths(configDir, noBuild); err != nil {
			t.Errorf("expected nil for non-building image, got: %v", err)
		}
	})

	t.Run("errors when build paths are missing", func(t *testing.T) {
		target := config.TargetConfig{Image: &config.Image{
			Repository:  "myapp",
			BuildConfig: &config.BuildConfig{Dockerfile: "missing/Dockerfile"},
		}}
		if err := validateBuildPaths(configDir, target); err == nil {
			t.Error("expected error for missing dockerfile, got nil")
		}
	})
}

func TestBuildImage_PreflightFailsBeforeDockerRuns(t *testing.T) {
	origRunner := runCLICommandInDir
	t.Cleanup(func() { runCLICommandInDir = origRunner })

	dockerInvoked := false
	runCLICommandInDir = func(ctx context.Context, workDir, name string, args ...string) error {
		dockerInvoked = true
		return nil
	}

	configDir := t.TempDir()
	image := &config.Image{
		Repository:  "myapp",
		Tag:         "latest",
		BuildConfig: &config.BuildConfig{Context: "platform", Dockerfile: "platform/Dockerfile"},
	}

	err := BuildImage(context.Background(), image.ImageRef(), image, configDir)
	if err == nil {
		t.Fatal("expected preflight error, got nil")
	}
	if dockerInvoked {
		t.Error("docker was invoked despite failed preflight")
	}
}

func TestBuildImage_PassesAbsolutePathsToDocker(t *testing.T) {
	origRunner := runCLICommandInDir
	t.Cleanup(func() { runCLICommandInDir = origRunner })

	var capturedArgs []string
	runCLICommandInDir = func(ctx context.Context, workDir, name string, args ...string) error {
		capturedArgs = args
		return nil
	}

	configDir := t.TempDir()
	writeTestFile(t, filepath.Join(configDir, "platform", "docker", "Dockerfile"))

	image := &config.Image{
		Repository:  "myapp",
		Tag:         "latest",
		BuildConfig: &config.BuildConfig{Context: "platform", Dockerfile: "docker/Dockerfile"},
	}

	if err := BuildImage(context.Background(), image.ImageRef(), image, configDir); err != nil {
		t.Fatalf("BuildImage returned error: %v", err)
	}

	wantDockerfile := filepath.Join(configDir, "platform", "docker", "Dockerfile")
	foundDockerfile := false
	for i, arg := range capturedArgs {
		if arg == "-f" && i+1 < len(capturedArgs) {
			foundDockerfile = true
			if capturedArgs[i+1] != wantDockerfile {
				t.Errorf("-f arg = %q, want %q", capturedArgs[i+1], wantDockerfile)
			}
		}
	}
	if !foundDockerfile {
		t.Errorf("no -f flag in docker args: %v", capturedArgs)
	}

	wantContext := filepath.Join(configDir, "platform")
	if got := capturedArgs[len(capturedArgs)-1]; got != wantContext {
		t.Errorf("context arg = %q, want %q", got, wantContext)
	}
}
