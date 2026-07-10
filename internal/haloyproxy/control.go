package haloyproxy

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/haloydev/haloy/internal/constants"
	"github.com/haloydev/haloy/internal/proxy"
	"github.com/haloydev/haloy/internal/proxywire"
)

// maxSnapshotBodySize bounds config pushes; even large installs are far below this.
const maxSnapshotBodySize = 16 << 20 // 16 MiB

// controlServer serves the local control API on a unix domain socket. haloyd
// is the only client; the socket's file permissions are the auth boundary.
type controlServer struct {
	proxy       *proxy.Proxy
	certManager *proxy.CertManager
	logger      *slog.Logger

	httpServer *http.Server
	socketPath string

	// mu serializes config updates and guards the status fields below.
	mu           sync.Mutex
	configHash   string
	loadedFrom   string // "socket" | "snapshot-file" | "empty"
	lastUpdateAt time.Time
	routeCount   int
}

func newControlServer(p *proxy.Proxy, certManager *proxy.CertManager, logger *slog.Logger) *controlServer {
	c := &controlServer{
		proxy:       p,
		certManager: certManager,
		logger:      logger,
		loadedFrom:  "empty",
	}

	mux := http.NewServeMux()
	mux.HandleFunc("PUT /v1/config", c.handleConfig)
	mux.HandleFunc("POST /v1/certs/reload", c.handleCertsReload)
	mux.HandleFunc("GET /v1/status", c.handleStatus)

	c.httpServer = &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}
	return c
}

// recordApplied records where the current config came from, for the status
// endpoint. Used at startup when the snapshot file is applied before the
// control socket exists.
func (c *controlServer) recordApplied(snap *proxywire.Snapshot, loadedFrom string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.configHash = snap.Hash()
	c.loadedFrom = loadedFrom
	c.lastUpdateAt = time.Now()
	c.routeCount = len(snap.Routes)
}

// Start binds the unix socket and serves the control API. A stale socket file
// from a previous run is removed first.
func (c *controlServer) Start(socketPath string) error {
	if err := os.Remove(socketPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove stale control socket: %w", err)
	}

	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		return fmt.Errorf("bind control socket: %w", err)
	}
	if err := os.Chmod(socketPath, constants.ModeFileSecret); err != nil {
		listener.Close()
		return fmt.Errorf("chmod control socket: %w", err)
	}
	c.socketPath = socketPath

	go func() {
		if err := c.httpServer.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			c.logger.Error("Control API server failed", "error", err)
		}
	}()

	c.logger.Info("Control API listening", "socket", socketPath)
	return nil
}

// Shutdown stops the control API and removes the socket file.
func (c *controlServer) Shutdown(ctx context.Context) error {
	err := c.httpServer.Shutdown(ctx)
	if c.socketPath != "" {
		os.Remove(c.socketPath)
	}
	return err
}

func (c *controlServer) handleConfig(w http.ResponseWriter, r *http.Request) {
	var snap proxywire.Snapshot
	body := http.MaxBytesReader(w, r.Body, maxSnapshotBodySize)
	if err := json.NewDecoder(body).Decode(&snap); err != nil {
		writeJSONError(w, http.StatusBadRequest, fmt.Sprintf("decode snapshot: %v", err))
		return
	}

	cfg, err := proxy.ConfigFromSnapshot(&snap)
	if err != nil {
		status := http.StatusBadRequest
		if errors.Is(err, proxywire.ErrSchemaTooNew) {
			// Keep the current config; haloyd must not downgrade us blindly.
			status = http.StatusConflict
		}
		c.logger.Warn("Rejected config push", "error", err)
		writeJSONError(w, status, err.Error())
		return
	}

	c.mu.Lock()
	c.proxy.UpdateConfig(cfg)
	c.configHash = snap.Hash()
	c.loadedFrom = "socket"
	c.lastUpdateAt = time.Now()
	c.routeCount = len(snap.Routes)
	hash := c.configHash
	c.mu.Unlock()

	writeJSON(w, http.StatusOK, map[string]string{"config_hash": hash})
}

func (c *controlServer) handleCertsReload(w http.ResponseWriter, r *http.Request) {
	if err := c.certManager.ReloadCertificates(); err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]int{"certs_loaded": c.certManager.CertCount()})
}

func (c *controlServer) handleStatus(w http.ResponseWriter, r *http.Request) {
	c.mu.Lock()
	status := proxywire.Status{
		Version:       constants.Version,
		Generation:    proxywire.ProxyGeneration,
		SchemaVersion: proxywire.SchemaVersion,
		ConfigHash:    c.configHash,
		Routes:        c.routeCount,
		LoadedFrom:    c.loadedFrom,
		LastUpdateAt:  c.lastUpdateAt,
		CertsLoaded:   c.certManager.CertCount(),
	}
	c.mu.Unlock()

	writeJSON(w, http.StatusOK, status)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func writeJSONError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}
