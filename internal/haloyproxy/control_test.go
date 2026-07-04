package haloyproxy

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/haloydev/haloy/internal/proxy"
	"github.com/haloydev/haloy/internal/proxywire"
)

// newTestControl starts a control server on a unix socket in a short temp dir
// (unix socket paths have a ~104 byte limit on macOS) and returns an HTTP
// client dialing it.
func newTestControl(t *testing.T) (*controlServer, *proxy.Proxy, *http.Client) {
	t.Helper()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	certManager, err := proxy.NewCertManager(t.TempDir(), logger)
	if err != nil {
		t.Fatal(err)
	}
	proxyServer := proxy.New(logger, certManager)
	control := newControlServer(proxyServer, certManager, logger)

	dir, err := os.MkdirTemp("", "haloy-ctl")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })

	socketPath := filepath.Join(dir, "control.sock")
	if err := control.Start(socketPath); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		control.Shutdown(ctx)
	})

	httpc := &http.Client{
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
				var d net.Dialer
				return d.DialContext(ctx, "unix", socketPath)
			},
		},
	}
	return control, proxyServer, httpc
}

func putConfig(t *testing.T, httpc *http.Client, snap *proxywire.Snapshot) *http.Response {
	t.Helper()
	body, err := json.Marshal(snap)
	if err != nil {
		t.Fatal(err)
	}
	req, err := http.NewRequest(http.MethodPut, "http://proxy/v1/config", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	resp, err := httpc.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

func getStatus(t *testing.T, httpc *http.Client) proxywire.Status {
	t.Helper()
	resp, err := httpc.Get("http://proxy/v1/status")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status endpoint returned %d", resp.StatusCode)
	}
	var status proxywire.Status
	if err := json.NewDecoder(resp.Body).Decode(&status); err != nil {
		t.Fatal(err)
	}
	return status
}

func TestControlAPI_ConfigPushAndStatus(t *testing.T) {
	_, proxyServer, httpc := newTestControl(t)

	if status := getStatus(t, httpc); status.LoadedFrom != "empty" || status.Routes != 0 {
		t.Fatalf("initial status = %+v, want empty config", status)
	}

	snap := &proxywire.Snapshot{
		SchemaVersion: proxywire.SchemaVersion,
		GeneratedAt:   time.Now().UTC(),
		APIDomain:     "api.example.com",
		APIBackend:    &proxywire.Backend{IP: "127.0.0.1", Port: "9922"},
		Routes: []proxywire.Route{
			{Canonical: "app.example.com", Backends: []proxywire.Backend{{IP: "10.0.0.2", Port: "8080"}}},
		},
	}

	resp := putConfig(t, httpc, snap)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("config push returned %d", resp.StatusCode)
	}
	var ack map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&ack); err != nil {
		t.Fatal(err)
	}
	if ack["config_hash"] != snap.Hash() {
		t.Fatalf("ack hash = %q, want %q", ack["config_hash"], snap.Hash())
	}

	// The route table must actually be applied to the proxy.
	cfg := proxyServer.GetConfig()
	if route := cfg.FindRoute("app.example.com"); route == nil {
		t.Fatal("pushed route not applied to proxy")
	}
	if cfg.APIDomain() != "api.example.com" {
		t.Fatalf("APIDomain = %q", cfg.APIDomain())
	}

	status := getStatus(t, httpc)
	if status.LoadedFrom != "socket" || status.Routes != 1 || status.ConfigHash != snap.Hash() {
		t.Fatalf("status after push = %+v", status)
	}
}

func TestControlAPI_RejectsNewerSchema(t *testing.T) {
	_, proxyServer, httpc := newTestControl(t)

	// Apply a good config first; a rejected push must not clobber it.
	good := &proxywire.Snapshot{
		SchemaVersion: proxywire.SchemaVersion,
		Routes: []proxywire.Route{
			{Canonical: "app.example.com", Backends: []proxywire.Backend{{IP: "10.0.0.2", Port: "8080"}}},
		},
	}
	resp := putConfig(t, httpc, good)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("good push returned %d", resp.StatusCode)
	}

	tooNew := &proxywire.Snapshot{SchemaVersion: proxywire.SchemaVersion + 1}
	resp = putConfig(t, httpc, tooNew)
	resp.Body.Close()
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("newer-schema push returned %d, want %d", resp.StatusCode, http.StatusConflict)
	}

	if proxyServer.GetConfig().FindRoute("app.example.com") == nil {
		t.Fatal("rejected push must keep the previous config")
	}
	if status := getStatus(t, httpc); status.ConfigHash != good.Hash() {
		t.Fatalf("status hash changed after rejected push: %+v", status)
	}
}

func TestControlAPI_RejectsInvalidConfig(t *testing.T) {
	_, _, httpc := newTestControl(t)

	// "app.example.com" is both canonical and an alias: RouteBuilder rejects it.
	invalid := &proxywire.Snapshot{
		SchemaVersion: proxywire.SchemaVersion,
		Routes: []proxywire.Route{
			{Canonical: "app.example.com"},
			{Canonical: "other.example.com", Aliases: []string{"app.example.com"}},
		},
	}
	resp := putConfig(t, httpc, invalid)
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("invalid push returned %d, want %d", resp.StatusCode, http.StatusBadRequest)
	}
}

func TestControlAPI_CertsReload(t *testing.T) {
	_, _, httpc := newTestControl(t)

	resp, err := httpc.Post("http://proxy/v1/certs/reload", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("certs reload returned %d", resp.StatusCode)
	}
	var ack map[string]int
	if err := json.NewDecoder(resp.Body).Decode(&ack); err != nil {
		t.Fatal(err)
	}
	if got, ok := ack["certs_loaded"]; !ok || got != 0 {
		t.Fatalf("certs_loaded = %v, want 0 for empty cert dir", ack)
	}
}
