package configloader

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/haloydev/haloy/internal/config"
)

func TestResolveValueSourceReturnsLiteralCopyWithoutConfigLookup(t *testing.T) {
	source := &config.ValueSource{Value: "literal-token"}

	resolved, err := ResolveValueSource(context.Background(), source, nil, "yaml", filepath.Join(t.TempDir(), "missing.yaml"))
	if err != nil {
		t.Fatalf("ResolveValueSource() unexpected error = %v", err)
	}

	if resolved == source {
		t.Fatal("ResolveValueSource() returned original pointer, want copy")
	}
	if resolved.Value != "literal-token" || resolved.From != nil {
		t.Fatalf("resolved source = %#v, want literal token", resolved)
	}
}

func TestResolveValueSourceResolvesEnv(t *testing.T) {
	t.Setenv("HALOY_TEST_TOKEN", "resolved-token")

	source := &config.ValueSource{
		From: &config.SourceReference{Env: "HALOY_TEST_TOKEN"},
	}

	resolved, err := ResolveValueSource(context.Background(), source, nil, "yaml", writeResolveValueSourceTestConfig(t))
	if err != nil {
		t.Fatalf("ResolveValueSource() unexpected error = %v", err)
	}

	if resolved.Value != "resolved-token" || resolved.From != nil {
		t.Fatalf("resolved source = %#v, want resolved env token", resolved)
	}
	if source.Value != "" || source.From == nil {
		t.Fatalf("source was mutated = %#v", source)
	}
}

func TestResolveValueSourceFailsForSecretWithoutProviders(t *testing.T) {
	source := &config.ValueSource{
		From: &config.SourceReference{Secret: "onepassword:server-secrets:api-token"},
	}

	_, err := ResolveValueSource(context.Background(), source, nil, "yaml", writeResolveValueSourceTestConfig(t))
	if err == nil {
		t.Fatal("expected missing secret provider error")
	}

	for _, want := range []string{"failed to group sources", "from.secret", "secret_providers"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("expected error to contain %q, got: %v", want, err)
		}
	}
}

func writeResolveValueSourceTestConfig(t *testing.T) string {
	t.Helper()

	configPath := filepath.Join(t.TempDir(), "haloy.yaml")
	if err := os.WriteFile(configPath, []byte("name: test\nserver: haloy.example.com\n"), 0o644); err != nil {
		t.Fatalf("failed to write config file: %v", err)
	}
	return configPath
}
