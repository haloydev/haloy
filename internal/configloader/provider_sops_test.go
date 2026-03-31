package configloader

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/haloydev/haloy/internal/config"
)

func TestFetchFromSOPS_YAMLAndJSONNestedFlatten(t *testing.T) {
	tests := []struct {
		name     string
		cfg      config.SOPSSourceConfig
		payload  string
		expected map[string]string
	}{
		{
			name: "yaml inferred by extension",
			cfg:  config.SOPSSourceConfig{File: "secrets.yaml"},
			payload: `db:
  host: localhost
  port: 5432
`,
			expected: map[string]string{"db.host": "localhost", "db.port": "5432"},
		},
		{
			name:     "json format override",
			cfg:      config.SOPSSourceConfig{File: "secrets.txt", Format: "json"},
			payload:  `{"db":{"host":"localhost","port":5432}}`,
			expected: map[string]string{"db.host": "localhost", "db.port": "5432"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			withSOPSProviderTestDoubles(t, []byte("encrypted"), []byte(tt.payload), nil)

			got, err := fetchFromSOPS(context.Background(), tt.cfg)
			if err != nil {
				t.Fatalf("fetchFromSOPS returned error: %v", err)
			}

			for key, expected := range tt.expected {
				if got[key] != expected {
					t.Fatalf("key %s: got %q, expected %q", key, got[key], expected)
				}
			}
		})
	}
}

func TestFetchFromSOPS_ArraysAndNestedArrays(t *testing.T) {
	withSOPSProviderTestDoubles(t, []byte("encrypted"), []byte(`users:
  - name: alice
    roles: [admin, dev]
`), nil)

	got, err := fetchFromSOPS(context.Background(), config.SOPSSourceConfig{File: "secrets.yml"})
	if err != nil {
		t.Fatalf("fetchFromSOPS returned error: %v", err)
	}

	expected := map[string]string{
		"users.0.name":    "alice",
		"users.0.roles.0": "admin",
		"users.0.roles.1": "dev",
	}

	for key, value := range expected {
		if got[key] != value {
			t.Fatalf("key %s: got %q, expected %q", key, got[key], value)
		}
	}
}

func TestFetchFromSOPS_DotenvOverrideBehavior(t *testing.T) {
	withSOPSProviderTestDoubles(t, []byte("encrypted"), []byte("A=first\nA=second\nB=value\n"), nil)

	got, err := fetchFromSOPS(context.Background(), config.SOPSSourceConfig{File: ".env"})
	if err != nil {
		t.Fatalf("fetchFromSOPS returned error: %v", err)
	}

	if got["A"] != "second" {
		t.Fatalf("expected dotenv duplicate key override to keep last value, got %q", got["A"])
	}
	if got["B"] != "value" {
		t.Fatalf("expected dotenv key B=value, got %q", got["B"])
	}
}

func TestFetchFromSOPS_UnsupportedFormatUnknownExtensionAndBinaryRejected(t *testing.T) {
	t.Run("unsupported format override", func(t *testing.T) {
		withSOPSProviderTestDoubles(t, []byte("encrypted"), []byte("x: 1"), nil)
		_, err := fetchFromSOPS(context.Background(), config.SOPSSourceConfig{File: "secrets.yaml", Format: "xml"})
		if err == nil {
			t.Fatal("expected unsupported format error")
		}
		if !strings.Contains(err.Error(), "stage=validate") || !strings.Contains(err.Error(), "unsupported sops format 'xml'") {
			t.Fatalf("unexpected error: %q", err.Error())
		}
	})

	t.Run("unknown extension without format", func(t *testing.T) {
		withSOPSProviderTestDoubles(t, []byte("encrypted"), []byte("x: 1"), nil)
		_, err := fetchFromSOPS(context.Background(), config.SOPSSourceConfig{File: "secrets.unknown"})
		if err == nil {
			t.Fatal("expected unknown extension error")
		}
		if !strings.Contains(err.Error(), "could not infer sops format") {
			t.Fatalf("unexpected error: %q", err.Error())
		}
	})

	t.Run("binary rejected", func(t *testing.T) {
		withSOPSProviderTestDoubles(t, []byte("encrypted"), []byte("irrelevant"), nil)
		_, err := fetchFromSOPS(context.Background(), config.SOPSSourceConfig{File: "secrets.bin", Format: "binary"})
		if err == nil {
			t.Fatal("expected binary format rejection")
		}
		if !strings.Contains(err.Error(), "not supported for key-based secret resolution") {
			t.Fatalf("unexpected error: %q", err.Error())
		}
	})
}

func TestFetchFromSOPS_CollisionsAndKeyWithDotRejection(t *testing.T) {
	t.Run("object key containing dot rejected", func(t *testing.T) {
		withSOPSProviderTestDoubles(t, []byte("encrypted"), []byte("app.config: value\n"), nil)

		_, err := fetchFromSOPS(context.Background(), config.SOPSSourceConfig{File: "secrets.yaml"})
		if err == nil {
			t.Fatal("expected key-with-dot parse rejection")
		}
		if !strings.Contains(err.Error(), "contains '.' which is not supported") {
			t.Fatalf("unexpected error: %q", err.Error())
		}
	})

	t.Run("collision rejected", func(t *testing.T) {
		out := map[string]string{"users.0.name": "alice"}
		err := flattenValue("bob", []string{"users", "0", "name"}, out)
		if err == nil {
			t.Fatal("expected collision error")
		}
		if !strings.Contains(err.Error(), "flattened key collision") {
			t.Fatalf("unexpected error: %q", err.Error())
		}
	})
}

func TestFetchFromSOPS_ScalarConversion(t *testing.T) {
	withSOPSProviderTestDoubles(t, []byte("encrypted"), []byte(`
flag: true
count: 42
ratio: 1.5
nothing: null
`), nil)

	got, err := fetchFromSOPS(context.Background(), config.SOPSSourceConfig{File: "secrets.yml"})
	if err != nil {
		t.Fatalf("fetchFromSOPS returned error: %v", err)
	}

	expected := map[string]string{
		"flag":    "true",
		"count":   "42",
		"ratio":   "1.5",
		"nothing": "",
	}
	for key, value := range expected {
		if got[key] != value {
			t.Fatalf("key %s: got %q, expected %q", key, got[key], value)
		}
	}
}

func TestFetchFromSOPS_MissingFileRequiredDiagnostic(t *testing.T) {
	t.Run("missing required file field", func(t *testing.T) {
		withSOPSProviderTestDoubles(t, []byte("encrypted"), []byte("x: 1"), nil)
		_, err := fetchFromSOPS(context.Background(), config.SOPSSourceConfig{})
		if err == nil {
			t.Fatal("expected required file validation error")
		}
		for _, want := range []string{"provider=sops", "stage=validate"} {
			if !strings.Contains(err.Error(), want) {
				t.Fatalf("expected error to contain %q, got %q", want, err.Error())
			}
		}
	})

	t.Run("missing file path read error", func(t *testing.T) {
		withSOPSProviderTestDoubles(t, nil, nil, errors.New("open failed"))
		_, err := fetchFromSOPS(context.Background(), config.SOPSSourceConfig{File: "missing.yaml"})
		if err == nil {
			t.Fatal("expected read error")
		}
		for _, want := range []string{"provider=sops", "stage=read", "path=missing.yaml"} {
			if !strings.Contains(err.Error(), want) {
				t.Fatalf("expected error to contain %q, got %q", want, err.Error())
			}
		}
	})
}

func TestFetchFromSOPS_StageSpecificContextualErrors(t *testing.T) {
	t.Run("decrypt stage", func(t *testing.T) {
		withSOPSProviderTestDoubles(t, []byte("encrypted"), nil, nil)
		originalDecrypt := sopsDecryptDataFn
		defer func() { sopsDecryptDataFn = originalDecrypt }()
		sopsDecryptDataFn = func(_ []byte, _ string) ([]byte, error) {
			return nil, errors.New("decrypt boom")
		}

		_, err := fetchFromSOPS(context.Background(), config.SOPSSourceConfig{File: "secrets.yaml"})
		if err == nil {
			t.Fatal("expected decrypt error")
		}
		if !strings.Contains(err.Error(), "stage=decrypt") {
			t.Fatalf("expected decrypt stage, got %q", err.Error())
		}
	})

	t.Run("parse stage", func(t *testing.T) {
		withSOPSProviderTestDoubles(t, []byte("encrypted"), []byte("invalid: ["), nil)
		_, err := fetchFromSOPS(context.Background(), config.SOPSSourceConfig{File: "secrets.yaml"})
		if err == nil {
			t.Fatal("expected parse error")
		}
		if !strings.Contains(err.Error(), "stage=parse") {
			t.Fatalf("expected parse stage, got %q", err.Error())
		}
	})
}

func TestFetchFromSOPS_UsesResolvedFilePathFromConfig(t *testing.T) {
	baseDir := t.TempDir()
	var readPath string

	originalRead := sopsReadFileFn
	originalDecrypt := sopsDecryptDataFn
	defer func() {
		sopsReadFileFn = originalRead
		sopsDecryptDataFn = originalDecrypt
	}()

	sopsReadFileFn = func(path string) ([]byte, error) {
		readPath = path
		return []byte("encrypted"), nil
	}
	sopsDecryptDataFn = func(_ []byte, _ string) ([]byte, error) {
		return []byte("key: value\n"), nil
	}

	resolved := filepath.Clean(filepath.Join(baseDir, "secrets/enc.yaml"))
	_, err := fetchFromSOPS(context.Background(), config.SOPSSourceConfig{File: resolved})
	if err != nil {
		t.Fatalf("fetchFromSOPS returned error: %v", err)
	}

	expected := filepath.Clean(filepath.Join(baseDir, "secrets/enc.yaml"))
	if filepath.Clean(readPath) != expected {
		t.Fatalf("expected read path %q, got %q", expected, readPath)
	}
}

func withSOPSProviderTestDoubles(t *testing.T, encrypted []byte, decrypted []byte, readErr error) {
	t.Helper()

	originalRead := sopsReadFileFn
	originalDecrypt := sopsDecryptDataFn
	t.Cleanup(func() {
		sopsReadFileFn = originalRead
		sopsDecryptDataFn = originalDecrypt
	})

	sopsReadFileFn = func(_ string) ([]byte, error) {
		if readErr != nil {
			return nil, readErr
		}
		return encrypted, nil
	}
	sopsDecryptDataFn = func(_ []byte, _ string) ([]byte, error) {
		return decrypted, nil
	}
}
