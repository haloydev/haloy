package proxyclient

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/haloydev/haloy/internal/constants"
	"github.com/haloydev/haloy/internal/proxywire"
)

// fakeControl is a minimal stand-in for the haloy-proxy control API.
type fakeControl struct {
	mu         sync.Mutex
	configHash string
	pushes     int
}

func (f *fakeControl) hash() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.configHash
}

func (f *fakeControl) pushCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.pushes
}

func startFakeControl(t *testing.T, dataDir string) *fakeControl {
	t.Helper()
	fc := &fakeControl{}

	mux := http.NewServeMux()
	mux.HandleFunc("PUT /v1/config", func(w http.ResponseWriter, r *http.Request) {
		var snap proxywire.Snapshot
		if err := json.NewDecoder(r.Body).Decode(&snap); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		fc.mu.Lock()
		fc.configHash = snap.Hash()
		fc.pushes++
		fc.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"config_hash": snap.Hash()})
	})
	mux.HandleFunc("GET /v1/status", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(proxywire.Status{
			Version:       "test",
			SchemaVersion: proxywire.SchemaVersion,
			ConfigHash:    fc.hash(),
			LoadedFrom:    "socket",
		})
	})
	mux.HandleFunc("POST /v1/certs/reload", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]int{"certs_loaded": 1})
	})

	socketPath := filepath.Join(dataDir, constants.ProxyDir, constants.ProxySocketFileName)
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatal(err)
	}
	server := &http.Server{Handler: mux}
	go server.Serve(listener)
	t.Cleanup(func() { server.Close() })

	return fc
}

func TestPushDeliversToControlServer(t *testing.T) {
	dataDir := tempDataDir(t)
	fc := startFakeControl(t, dataDir)
	client := New(dataDir, testLogger())
	snap := testSnapshot()

	if err := client.Push(context.Background(), snap); err != nil {
		t.Fatalf("Push() error = %v", err)
	}
	if fc.hash() != snap.Hash() {
		t.Fatalf("server hash = %q, want %q", fc.hash(), snap.Hash())
	}

	// The snapshot file is written on every push.
	loaded, err := proxywire.ReadSnapshotFile(filepath.Join(dataDir, constants.ProxyDir, constants.ProxySnapshotFileName))
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Hash() != snap.Hash() {
		t.Fatal("snapshot file content mismatch")
	}
}

func TestReconcileRepushesWhenProxyOutOfSync(t *testing.T) {
	dataDir := tempDataDir(t)
	client := New(dataDir, testLogger())
	snap := testSnapshot()

	// Proxy down: push fails over the socket but is durably recorded.
	if err := client.Push(context.Background(), snap); err == nil {
		t.Fatal("expected push to fail while proxy is down")
	}

	// Proxy comes back (e.g. after an upgrade) with a different config.
	fc := startFakeControl(t, dataDir)

	client.reconcile(context.Background())

	if fc.pushCount() != 1 {
		t.Fatalf("pushes = %d, want 1 re-push from reconcile", fc.pushCount())
	}
	if fc.hash() != snap.Hash() {
		t.Fatalf("server hash = %q, want %q after reconcile", fc.hash(), snap.Hash())
	}

	// A second reconcile with matching hashes must not push again.
	client.reconcile(context.Background())
	if fc.pushCount() != 1 {
		t.Fatalf("pushes = %d, reconcile must be a no-op when in sync", fc.pushCount())
	}
}

func TestWaitReadyAndReloadCerts(t *testing.T) {
	dataDir := tempDataDir(t)
	startFakeControl(t, dataDir)
	client := New(dataDir, testLogger())

	if err := client.WaitReady(context.Background(), 2*time.Second); err != nil {
		t.Fatalf("WaitReady() error = %v", err)
	}
	if err := client.ReloadCerts(context.Background()); err != nil {
		t.Fatalf("ReloadCerts() error = %v", err)
	}
}
