package proxywire

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func testSnapshot() *Snapshot {
	return &Snapshot{
		SchemaVersion: SchemaVersion,
		GeneratedAt:   time.Now().UTC(),
		APIDomain:     "api.example.com",
		APIBackend:    &Backend{IP: "127.0.0.1", Port: "9922"},
		Routes: []Route{
			{
				Canonical: "app.example.com",
				Aliases:   []string{"www.app.example.com"},
				Backends: []Backend{
					{IP: "10.0.0.2", Port: "8080"},
					{IP: "10.0.0.3", Port: "8080"},
				},
			},
			{
				Canonical: "failed.example.com",
			},
		},
	}
}

func TestSnapshotJSONRoundTrip(t *testing.T) {
	original := testSnapshot()

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var decoded Snapshot
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if decoded.Hash() != original.Hash() {
		t.Fatal("expected hash to survive JSON round trip")
	}
	if decoded.APIDomain != original.APIDomain {
		t.Fatalf("api domain mismatch: %q != %q", decoded.APIDomain, original.APIDomain)
	}
	if decoded.APIBackend == nil || *decoded.APIBackend != *original.APIBackend {
		t.Fatalf("api backend mismatch: %+v", decoded.APIBackend)
	}
	if len(decoded.Routes) != len(original.Routes) {
		t.Fatalf("expected %d routes, got %d", len(original.Routes), len(decoded.Routes))
	}
}

func TestSnapshotUnknownFieldsIgnored(t *testing.T) {
	// A newer haloyd may add fields; older proxies must decode without error.
	data := []byte(`{
		"schema_version": 1,
		"api_domain": "api.example.com",
		"some_future_field": {"nested": true},
		"routes": [
			{"canonical": "app.example.com", "future_route_field": 42, "backends": [{"ip": "10.0.0.2", "port": "8080", "weight": 7}]}
		]
	}`)

	var s Snapshot
	if err := json.Unmarshal(data, &s); err != nil {
		t.Fatalf("expected unknown fields to be ignored, got: %v", err)
	}
	if len(s.Routes) != 1 || s.Routes[0].Canonical != "app.example.com" {
		t.Fatalf("unexpected routes: %+v", s.Routes)
	}
}

func TestSnapshotHashStableAcrossOrdering(t *testing.T) {
	a := testSnapshot()

	b := testSnapshot()
	// Reverse route and backend order; hash must not change.
	b.Routes[0], b.Routes[1] = b.Routes[1], b.Routes[0]
	for i := range b.Routes {
		backends := b.Routes[i].Backends
		for j, k := 0, len(backends)-1; j < k; j, k = j+1, k-1 {
			backends[j], backends[k] = backends[k], backends[j]
		}
	}
	b.GeneratedAt = a.GeneratedAt.Add(time.Hour)

	if a.Hash() != b.Hash() {
		t.Fatal("expected hash to be independent of route/backend ordering and GeneratedAt")
	}
}

func TestSnapshotHashChangesWithContent(t *testing.T) {
	a := testSnapshot()
	b := testSnapshot()
	b.Routes[0].Backends[0].Port = "9090"

	if a.Hash() == b.Hash() {
		t.Fatal("expected hash to change when a backend changes")
	}
}

func TestCheckSchemaVersion(t *testing.T) {
	s := testSnapshot()
	if err := s.CheckSchemaVersion(); err != nil {
		t.Fatalf("expected current schema version to be accepted, got: %v", err)
	}

	s.SchemaVersion = SchemaVersion + 1
	err := s.CheckSchemaVersion()
	if !errors.Is(err, ErrSchemaTooNew) {
		t.Fatalf("expected ErrSchemaTooNew, got: %v", err)
	}
}

func TestNormalizeProxyGeneration(t *testing.T) {
	if got := NormalizeProxyGeneration(0); got != LegacyProxyGeneration {
		t.Fatalf("NormalizeProxyGeneration(0) = %d, want %d", got, LegacyProxyGeneration)
	}
	if got := NormalizeProxyGeneration(7); got != 7 {
		t.Fatalf("NormalizeProxyGeneration(7) = %d, want 7", got)
	}
}

func TestIsProxyCompatible(t *testing.T) {
	if !IsProxyCompatible(ProxyGeneration, SchemaVersion) {
		t.Fatal("current proxy metadata should be compatible")
	}
	if !IsProxyCompatible(0, SchemaVersion) {
		t.Fatal("legacy generation should normalize to the initial generation")
	}
	if IsProxyCompatible(ProxyGeneration, SchemaVersion-1) {
		t.Fatal("older proxy schema should be incompatible")
	}
}

func TestSnapshotFileRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "snapshot.json")
	original := testSnapshot()

	if err := WriteSnapshotFile(path, original); err != nil {
		t.Fatalf("write: %v", err)
	}

	// No temp file should remain after a successful write.
	if _, err := os.Stat(path + ".tmp"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected temp file to be gone, got: %v", err)
	}

	loaded, err := ReadSnapshotFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if loaded.Hash() != original.Hash() {
		t.Fatal("expected loaded snapshot to hash identically")
	}
}

func TestReadSnapshotFileMissing(t *testing.T) {
	_, err := ReadSnapshotFile(filepath.Join(t.TempDir(), "missing.json"))
	if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected os.ErrNotExist, got: %v", err)
	}
}

func TestReadSnapshotFileCorrupt(t *testing.T) {
	path := filepath.Join(t.TempDir(), "snapshot.json")
	if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadSnapshotFile(path); err == nil {
		t.Fatal("expected error for corrupt snapshot file")
	}
}
