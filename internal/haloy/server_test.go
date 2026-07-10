package haloy

import (
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/haloydev/haloy/internal/apitypes"
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

func captureVersionOutput(t *testing.T, fn func()) (string, string) {
	t.Helper()
	oldStdout, oldStderr := os.Stdout, os.Stderr
	stdoutR, stdoutW, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	stderrR, stderrW, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout, os.Stderr = stdoutW, stderrW
	defer func() { os.Stdout, os.Stderr = oldStdout, oldStderr }()

	fn()
	stdoutW.Close()
	stderrW.Close()
	stdout, err := io.ReadAll(stdoutR)
	if err != nil {
		t.Fatal(err)
	}
	stderr, err := io.ReadAll(stderrR)
	if err != nil {
		t.Fatal(err)
	}
	return string(stdout), string(stderr)
}

func TestPrintServerVersionUsesProductFirstOutput(t *testing.T) {
	compatible := true
	version := &apitypes.VersionResponse{
		Version:                    "v1.2.3",
		ProxyVersion:               "v1.2.1",
		ProxyGeneration:            1,
		RequiredProxyGeneration:    1,
		ProxySchemaVersion:         1,
		RequiredProxySchemaVersion: 1,
		ProxyCompatible:            &compatible,
	}

	stdout, _ := captureVersionOutput(t, func() { printServerVersion(version, "", false) })
	if !strings.Contains(stdout, "Haloy server version: v1.2.3") || !strings.Contains(stdout, "haloy-proxy: compatible") {
		t.Fatalf("unexpected default output:\n%s", stdout)
	}
	if strings.Contains(stdout, "v1.2.1") || strings.Contains(stdout, "generation") {
		t.Fatalf("default output exposed component details:\n%s", stdout)
	}

	stdout, _ = captureVersionOutput(t, func() { printServerVersion(version, "", true) })
	for _, want := range []string{"haloyd build version: v1.2.3", "haloy-proxy build version: v1.2.1", "generation: 1", "schema: 1"} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("component output missing %q:\n%s", want, stdout)
		}
	}
}

func TestPrintServerVersionWarnsWhenProxyIsIncompatible(t *testing.T) {
	compatible := false
	_, stderr := captureVersionOutput(t, func() {
		printServerVersion(&apitypes.VersionResponse{Version: "v1", ProxyVersion: "v0", ProxyCompatible: &compatible}, "", false)
	})
	if !strings.Contains(stderr, "haloy-proxy: incompatible") {
		t.Fatalf("expected incompatibility warning, got:\n%s", stderr)
	}
}
