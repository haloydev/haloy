package haloyproxy

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/haloydev/haloy/internal/constants"
	"github.com/haloydev/haloy/internal/proxy"
	"github.com/haloydev/haloy/internal/proxyclient"
	"github.com/haloydev/haloy/internal/proxywire"
)

// TestClientAgainstRealControlServer wires the haloyd-side client to a real
// control server, verifying the two packages agree on socket path, wire
// format and endpoints.
func TestClientAgainstRealControlServer(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	// Short base dir: the unix socket path must stay under ~104 bytes.
	dataDir, err := os.MkdirTemp("", "haloy-int")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dataDir) })
	proxyDir := filepath.Join(dataDir, constants.ProxyDir)
	if err := os.MkdirAll(proxyDir, constants.ModeDirPrivate); err != nil {
		t.Fatal(err)
	}

	certManager, err := proxy.NewCertManager(filepath.Join(dataDir, constants.CertStorageDir), logger)
	if err != nil {
		t.Fatal(err)
	}
	proxyServer := proxy.New(logger, certManager)
	control := newControlServer(proxyServer, certManager, logger)
	if err := control.Start(filepath.Join(proxyDir, constants.ProxySocketFileName)); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		control.Shutdown(ctx)
	})

	client := proxyclient.New(dataDir, logger)
	ctx := context.Background()

	if err := client.WaitReady(ctx, 2*time.Second); err != nil {
		t.Fatalf("WaitReady() error = %v", err)
	}

	snap := &proxywire.Snapshot{
		SchemaVersion: proxywire.SchemaVersion,
		GeneratedAt:   time.Now().UTC(),
		APIDomain:     "api.example.com",
		APIBackend:    &proxywire.Backend{IP: constants.HaloydAPIHost, Port: constants.HaloydAPIPort},
		Routes: []proxywire.Route{
			{Canonical: "app.example.com", Backends: []proxywire.Backend{{IP: "10.0.0.2", Port: "8080"}}},
		},
	}
	if err := client.Push(ctx, snap); err != nil {
		t.Fatalf("Push() error = %v", err)
	}

	if proxyServer.GetConfig().FindRoute("app.example.com") == nil {
		t.Fatal("route not applied to the proxy")
	}

	status, err := client.Status(ctx)
	if err != nil {
		t.Fatalf("Status() error = %v", err)
	}
	if status.ConfigHash != snap.Hash() || status.LoadedFrom != "socket" || status.Routes != 1 {
		t.Fatalf("status = %+v", status)
	}

	if err := client.ReloadCerts(ctx); err != nil {
		t.Fatalf("ReloadCerts() error = %v", err)
	}
}
