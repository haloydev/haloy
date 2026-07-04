// Package proxyclient is haloyd's client for the haloy-proxy control API.
// Every push first writes the snapshot file (the proxy's durable
// last-known-good boot config), then delivers the snapshot over the control
// socket. A background reconcile loop re-pushes after the proxy restarts or
// a push fails, so the two processes converge without haloyd ever crashing
// on an unreachable proxy.
package proxyclient

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/haloydev/haloy/internal/constants"
	"github.com/haloydev/haloy/internal/proxy"
	"github.com/haloydev/haloy/internal/proxywire"
)

// ErrUnreachable wraps transport-level failures talking to the proxy control
// socket (proxy not running or restarting). The snapshot file is already
// written when this is returned, so callers should log and continue; the
// reconcile loop delivers the config once the proxy is back.
var ErrUnreachable = errors.New("haloy-proxy control socket unreachable")

const (
	requestTimeout    = 10 * time.Second
	reconcileInterval = 5 * time.Second
)

// Client pushes routing snapshots to haloy-proxy.
type Client struct {
	proxyDir     string
	snapshotPath string
	httpc        *http.Client
	logger       *slog.Logger

	// mu serializes pushes (updater vs health updater) and file writes.
	mu   sync.Mutex
	last *proxywire.Snapshot

	// reachable tracks reachability transitions so the reconcile loop logs
	// once per outage instead of every tick.
	reachableMu sync.Mutex
	unreachable bool
}

// New creates a client for the control socket under dataDir.
func New(dataDir string, logger *slog.Logger) *Client {
	proxyDir := filepath.Join(dataDir, constants.ProxyDir)
	socketPath := filepath.Join(proxyDir, constants.ProxySocketFileName)

	return &Client{
		proxyDir:     proxyDir,
		snapshotPath: filepath.Join(proxyDir, constants.ProxySnapshotFileName),
		logger:       logger,
		httpc: &http.Client{
			Timeout: requestTimeout,
			Transport: &http.Transport{
				DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
					var d net.Dialer
					return d.DialContext(ctx, "unix", socketPath)
				},
			},
		},
	}
}

// Push durably records the snapshot and delivers it to the proxy.
//
// The snapshot is validated, written to the snapshot file first (so a proxy
// restart converges even if the socket push fails), then PUT to the control
// socket. A transport failure is returned wrapped in ErrUnreachable; any
// other error means the config was not accepted and must be treated as real.
func (c *Client) Push(ctx context.Context, snap *proxywire.Snapshot) error {
	// Validate before persisting: an invalid snapshot must never become the
	// proxy's boot config.
	if _, err := proxy.ConfigFromSnapshot(snap); err != nil {
		return fmt.Errorf("invalid snapshot: %w", err)
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	if err := os.MkdirAll(c.proxyDir, constants.ModeDirPrivate); err != nil {
		return fmt.Errorf("create proxy directory: %w", err)
	}
	if err := proxywire.WriteSnapshotFile(c.snapshotPath, snap); err != nil {
		return err
	}
	c.last = snap

	return c.pushLocked(ctx, snap)
}

// pushLocked PUTs the snapshot to the control socket. Callers hold c.mu.
func (c *Client) pushLocked(ctx context.Context, snap *proxywire.Snapshot) error {
	body, err := json.Marshal(snap)
	if err != nil {
		return fmt.Errorf("marshal snapshot: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPut, "http://haloy-proxy/v1/config", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpc.Do(req)
	if err != nil {
		c.setUnreachable(err)
		return fmt.Errorf("%w: %v", ErrUnreachable, err)
	}
	defer resp.Body.Close()
	c.setReachable()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("proxy rejected config push: %s: %s", resp.Status, readErrorBody(resp.Body))
	}

	c.logger.Debug("Proxy config pushed", "routes", len(snap.Routes), "config_hash", snap.Hash())
	return nil
}

// ReloadCerts tells the proxy to reload certificates from disk.
func (c *Client) ReloadCerts(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://haloy-proxy/v1/certs/reload", nil)
	if err != nil {
		return err
	}
	resp, err := c.httpc.Do(req)
	if err != nil {
		c.setUnreachable(err)
		return fmt.Errorf("%w: %v", ErrUnreachable, err)
	}
	defer resp.Body.Close()
	c.setReachable()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("proxy certificate reload failed: %s: %s", resp.Status, readErrorBody(resp.Body))
	}
	return nil
}

// Status fetches the proxy's current status.
func (c *Client) Status(ctx context.Context) (*proxywire.Status, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://haloy-proxy/v1/status", nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.httpc.Do(req)
	if err != nil {
		c.setUnreachable(err)
		return nil, fmt.Errorf("%w: %v", ErrUnreachable, err)
	}
	defer resp.Body.Close()
	c.setReachable()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("proxy status failed: %s: %s", resp.Status, readErrorBody(resp.Body))
	}

	var status proxywire.Status
	if err := json.NewDecoder(resp.Body).Decode(&status); err != nil {
		return nil, fmt.Errorf("decode proxy status: %w", err)
	}
	return &status, nil
}

// WaitReady polls the proxy until it answers status requests, so ACME
// challenges have a live route to the challenge server before certificate
// issuance starts.
func (c *Client) WaitReady(ctx context.Context, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for {
		if _, err := c.Status(ctx); err == nil {
			return nil
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("haloy-proxy not ready after %s: %w", timeout, ctx.Err())
		case <-ticker.C:
		}
	}
}

// Start launches the background reconcile loop. On every tick, if the proxy
// is reachable and its config hash differs from the last pushed snapshot
// (failed push, proxy restart, schema recovery), the snapshot is re-pushed.
// The loop stops when ctx is cancelled.
func (c *Client) Start(ctx context.Context) {
	go func() {
		ticker := time.NewTicker(reconcileInterval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				c.reconcile(ctx)
			}
		}
	}()
}

func (c *Client) reconcile(ctx context.Context) {
	c.mu.Lock()
	snap := c.last
	c.mu.Unlock()
	if snap == nil {
		return
	}

	status, err := c.Status(ctx)
	if err != nil {
		// Logged once by setUnreachable; keep ticking until the proxy is back.
		return
	}
	if status.ConfigHash == snap.Hash() {
		return
	}

	c.logger.Info("Proxy config out of sync, re-pushing",
		"proxy_config_hash", status.ConfigHash,
		"loaded_from", status.LoadedFrom)

	c.mu.Lock()
	defer c.mu.Unlock()
	// Re-check: another push may have landed while we were unlocked.
	if c.last != snap {
		return
	}
	if err := c.pushLocked(ctx, snap); err != nil {
		c.logger.Warn("Proxy config re-push failed", "error", err)
	}
}

func (c *Client) setUnreachable(err error) {
	c.reachableMu.Lock()
	defer c.reachableMu.Unlock()
	if !c.unreachable {
		c.unreachable = true
		c.logger.Warn("haloy-proxy is unreachable; traffic is served with its last applied config. "+
			"If haloy-proxy is not installed, run the server upgrade script.",
			"error", err)
	}
}

func (c *Client) setReachable() {
	c.reachableMu.Lock()
	defer c.reachableMu.Unlock()
	if c.unreachable {
		c.unreachable = false
		c.logger.Info("haloy-proxy is reachable again")
	}
}

func readErrorBody(r io.Reader) string {
	data, err := io.ReadAll(io.LimitReader(r, 4096))
	if err != nil || len(data) == 0 {
		return "<no body>"
	}
	return string(bytes.TrimSpace(data))
}
