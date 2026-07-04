package proxyclient

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/haloydev/haloy/internal/constants"
	"github.com/haloydev/haloy/internal/proxywire"
)

// tempDataDir returns a short-lived data dir outside t.TempDir() because unix
// socket paths (dataDir/proxy/haloy-proxy.sock) must stay under ~104 bytes.
func tempDataDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "haloy-pc")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	if err := os.MkdirAll(filepath.Join(dir, constants.ProxyDir), constants.ModeDirPrivate); err != nil {
		t.Fatal(err)
	}
	return dir
}

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func testSnapshot() *proxywire.Snapshot {
	return &proxywire.Snapshot{
		SchemaVersion: proxywire.SchemaVersion,
		GeneratedAt:   time.Now().UTC(),
		APIDomain:     "api.example.com",
		Routes: []proxywire.Route{
			{Canonical: "app.example.com", Backends: []proxywire.Backend{{IP: "10.0.0.2", Port: "8080"}}},
		},
	}
}

func TestPushWithProxyDownWritesFileAndReturnsUnreachable(t *testing.T) {
	dataDir := tempDataDir(t)
	client := New(dataDir, testLogger())
	snap := testSnapshot()

	err := client.Push(context.Background(), snap)
	if !errors.Is(err, ErrUnreachable) {
		t.Fatalf("Push() error = %v, want ErrUnreachable", err)
	}

	// The snapshot file must exist regardless: it is the proxy's boot config.
	loaded, err := proxywire.ReadSnapshotFile(filepath.Join(dataDir, constants.ProxyDir, constants.ProxySnapshotFileName))
	if err != nil {
		t.Fatalf("snapshot file not written: %v", err)
	}
	if loaded.Hash() != snap.Hash() {
		t.Fatal("snapshot file content mismatch")
	}
}

func TestPushRejectsInvalidSnapshotWithoutWritingFile(t *testing.T) {
	dataDir := tempDataDir(t)
	client := New(dataDir, testLogger())

	invalid := testSnapshot()
	invalid.Routes = append(invalid.Routes, proxywire.Route{
		Canonical: "other.example.com",
		Aliases:   []string{"app.example.com"}, // collides with canonical of first route
	})

	err := client.Push(context.Background(), invalid)
	if err == nil || errors.Is(err, ErrUnreachable) {
		t.Fatalf("Push() error = %v, want validation error", err)
	}

	snapshotPath := filepath.Join(dataDir, constants.ProxyDir, constants.ProxySnapshotFileName)
	if _, err := os.Stat(snapshotPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("invalid snapshot must not be persisted, stat err = %v", err)
	}
}

func TestStatusAndWaitReadyWithProxyDown(t *testing.T) {
	client := New(tempDataDir(t), testLogger())

	if _, err := client.Status(context.Background()); !errors.Is(err, ErrUnreachable) {
		t.Fatalf("Status() error = %v, want ErrUnreachable", err)
	}

	if err := client.WaitReady(context.Background(), 100*time.Millisecond); err == nil {
		t.Fatal("WaitReady() should fail when the proxy is down")
	}
}
