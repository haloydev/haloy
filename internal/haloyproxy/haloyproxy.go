// Package haloyproxy wires up the standalone haloy-proxy daemon: the reverse
// proxy data plane (ports 80/443) plus a control API on a unix domain socket
// that haloyd pushes routing snapshots to. It boots from the snapshot file
// haloyd writes, so it serves last-known-good routes while haloyd is down.
package haloyproxy

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/haloydev/haloy/internal/config"
	"github.com/haloydev/haloy/internal/constants"
	"github.com/haloydev/haloy/internal/logging"
	"github.com/haloydev/haloy/internal/proxy"
	"github.com/haloydev/haloy/internal/proxywire"
)

const shutdownTimeout = 30 * time.Second

// Run starts the proxy daemon and blocks until it receives SIGINT/SIGTERM or
// a listener fails.
func Run(debug bool) error {
	logLevel := slog.LevelInfo
	if debug {
		logLevel = slog.LevelDebug
	}
	logger := logging.NewLogger(logLevel, nil)

	logger.Info("haloy-proxy started", "version", constants.Version, "debug", debug)

	dataDir, err := config.DataDir()
	if err != nil {
		return fmt.Errorf("resolve data directory: %w", err)
	}
	proxyDir := filepath.Join(dataDir, constants.ProxyDir)
	if err := os.MkdirAll(proxyDir, constants.ModeDirPrivate); err != nil {
		return fmt.Errorf("create proxy directory: %w", err)
	}

	certManager, err := proxy.NewCertManager(filepath.Join(dataDir, constants.CertStorageDir), logger)
	if err != nil {
		return fmt.Errorf("create certificate manager: %w", err)
	}

	proxyServer := proxy.New(logger, certManager)
	control := newControlServer(proxyServer, certManager, logger)

	// Boot from the last snapshot haloyd wrote, if any. A missing or broken
	// snapshot is not fatal: binding 80/443 with existing certificates and a
	// working ACME challenge path beats crash-looping.
	snapshotPath := filepath.Join(proxyDir, constants.ProxySnapshotFileName)
	if snap, err := proxywire.ReadSnapshotFile(snapshotPath); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			logger.Info("No routing snapshot yet (fresh install); starting with empty routes",
				"path", snapshotPath)
		} else {
			logger.Error("Failed to read routing snapshot; starting with empty routes",
				"path", snapshotPath, "error", err)
		}
	} else if cfg, err := proxy.ConfigFromSnapshot(snap); err != nil {
		logger.Error("Invalid routing snapshot; starting with empty routes",
			"path", snapshotPath, "error", err)
	} else {
		proxyServer.UpdateConfig(cfg)
		control.recordApplied(snap, "snapshot-file")
		logger.Info("Routing snapshot applied",
			"routes", len(snap.Routes),
			"age", time.Since(snap.GeneratedAt).Round(time.Second).String())
	}

	if err := proxyServer.Start(":80", ":443"); err != nil {
		return fmt.Errorf("start proxy: %w", err)
	}
	logger.Info("Proxy started", "http", ":80", "https", ":443")

	socketPath := filepath.Join(proxyDir, constants.ProxySocketFileName)
	if err := control.Start(socketPath); err != nil {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer cancel()
		proxyServer.Shutdown(shutdownCtx)
		return err
	}

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	var runErr error
	select {
	case sig := <-sigChan:
		logger.Info("Received shutdown signal", "signal", sig.String())
	case err := <-proxyServer.Err():
		logger.Error("Proxy listener failed", "error", err)
		runErr = err
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()

	if err := control.Shutdown(shutdownCtx); err != nil {
		logger.Error("Control API shutdown failed", "error", err)
	}
	if err := proxyServer.Shutdown(shutdownCtx); err != nil {
		logger.Error("Proxy shutdown failed", "error", err)
	}

	logger.Info("haloy-proxy stopped")
	return runErr
}
