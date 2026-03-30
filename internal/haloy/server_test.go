package haloy

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/haloydev/haloy/internal/constants"
)

func writeTestConfig(t *testing.T, content string) string {
	t.Helper()

	configPath := filepath.Join(t.TempDir(), "haloy.yaml")
	if err := os.WriteFile(configPath, []byte(strings.TrimSpace(content)+"\n"), 0o644); err != nil {
		t.Fatalf("failed to write config file: %v", err)
	}

	return configPath
}

func runRootCommand(t *testing.T, args ...string) error {
	t.Helper()

	cmd := NewRootCmd()
	cmd.SetArgs(args)

	return cmd.Execute()
}

func TestServerVersionSingleTargetWithoutSelectors(t *testing.T) {
	t.Setenv(constants.EnvVarConfigDir, t.TempDir())
	t.Setenv(constants.EnvVarAPIToken, "test-token")

	srv := newVersionServer(http.StatusOK)
	defer srv.Close()

	configPath := writeTestConfig(t, `
name: simple-test-app
server: `+srv.URL+`
`)

	if err := runRootCommand(t, "server", "version", "-c", configPath); err != nil {
		t.Fatalf("expected single-target config without selectors to succeed, got: %v", err)
	}
}

func TestServerVersionMultiTargetDefaultsToAllTargets(t *testing.T) {
	t.Setenv(constants.EnvVarConfigDir, t.TempDir())
	t.Setenv(constants.EnvVarAPIToken, "test-token")

	srv := newVersionServer(http.StatusOK)
	defer srv.Close()

	configPath := writeTestConfig(t, `
targets:
  prod:
    server: `+srv.URL+`
  staging:
    server: `+srv.URL+`
`)

	if err := runRootCommand(t, "server", "version", "-c", configPath); err != nil {
		t.Fatalf("expected multi-target config without selectors to succeed, got: %v", err)
	}
}

func TestServerVersionExplicitAllStillFailsForSingleTarget(t *testing.T) {
	t.Setenv(constants.EnvVarConfigDir, t.TempDir())
	t.Setenv(constants.EnvVarAPIToken, "test-token")

	srv := newVersionServer(http.StatusOK)
	defer srv.Close()

	configPath := writeTestConfig(t, `
name: simple-test-app
server: `+srv.URL+`
`)

	err := runRootCommand(t, "server", "version", "-c", configPath, "--all")
	if err == nil {
		t.Fatal("expected explicit --all on a single-target config to fail")
	}

	want := "the --targets and --all flags are not applicable for a single-target configuration file"
	if !strings.Contains(err.Error(), want) {
		t.Fatalf("expected error to contain %q, got: %v", want, err)
	}
}
