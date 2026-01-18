package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"time"

	"github.com/haloydev/haloy/internal/apitypes"
	"github.com/haloydev/haloy/internal/constants"
	"github.com/haloydev/haloy/internal/helpers"
	"github.com/haloydev/haloy/internal/logging"
)

type gitHubRelease struct {
	TagName string `json:"tag_name"`
}

// handleUpgrade handles the upgrade: downloads and installs the new haloyd binary
func (s *APIServer) handleUpgrade() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		logger := logging.NewLogger(s.logLevel, s.logBroker)
		ctx := r.Context()

		currentVersion := constants.Version

		latestVersion, err := fetchLatestVersion(ctx)
		if err != nil {
			logger.Error("Failed to fetch latest version from GitHub", "error", err)
			encodeJSON(w, http.StatusInternalServerError, apitypes.UpgradeResponse{
				Status:          "failed",
				PreviousVersion: currentVersion,
				Message:         fmt.Sprintf("Failed to fetch latest version: %v", err),
			})
			return
		}

		// Normalize versions for comparison (strip 'v' prefix if present)
		normalizedCurrent := helpers.NormalizeVersion(currentVersion)
		normalizedLatest := helpers.NormalizeVersion(latestVersion)

		if normalizedCurrent == normalizedLatest {
			encodeJSON(w, http.StatusOK, apitypes.UpgradeResponse{
				Status:          "completed",
				PreviousVersion: currentVersion,
				TargetVersion:   latestVersion,
				Message:         "Already running the latest version",
			})
			return
		}

		logger.Info("Starting upgrade", "from", currentVersion, "to", latestVersion)

		// Download and install the new binary
		if err := downloadAndInstallBinary(ctx, latestVersion, logger); err != nil {
			logger.Error("Binary upgrade failed", "error", err)
			encodeJSON(w, http.StatusInternalServerError, apitypes.UpgradeResponse{
				Status:          "failed",
				PreviousVersion: currentVersion,
				TargetVersion:   latestVersion,
				Message:         fmt.Sprintf("Upgrade failed: %v", err),
			})
			return
		}

		logger.Info("Successfully updated haloyd binary", "version", latestVersion)

		encodeJSON(w, http.StatusOK, apitypes.UpgradeResponse{
			Status:          "updating",
			PreviousVersion: currentVersion,
			TargetVersion:   latestVersion,
			Message:         "haloyd binary updated. Restart the service to complete: systemctl restart haloyd",
		})
	}
}

// handleUpgradeRestart handles the restart phase of upgrade
// Since haloyd runs natively via systemd, restarting requires systemctl
func (s *APIServer) handleUpgradeRestart() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		logger := logging.NewLogger(s.logLevel, s.logBroker)

		logger.Info("Restart requested - attempting systemctl restart")

		// Try to restart via systemctl in a goroutine so we can return the response first
		go func() {
			// Small delay to ensure the HTTP response is sent
			time.Sleep(500 * time.Millisecond)

			cmd := exec.Command("systemctl", "restart", "haloyd")
			if err := cmd.Run(); err != nil {
				logger.Error("Failed to restart haloyd via systemctl", "error", err)
				return
			}
			logger.Info("systemctl restart haloyd initiated")
		}()

		encodeJSON(w, http.StatusAccepted, apitypes.UpgradeResponse{
			Status:  "restarting",
			Message: "Service restart initiated. Poll /v1/version to check when upgrade is complete.",
		})
	}
}

// downloadAndInstallBinary downloads and installs the new haloyd binary
func downloadAndInstallBinary(ctx context.Context, version string, logger interface {
	Info(msg string, args ...any)
	Error(msg string, args ...any)
},
) error {
	platform := runtime.GOOS
	arch := runtime.GOARCH

	binaryName := fmt.Sprintf("haloyd-%s-%s", platform, arch)
	downloadURL := fmt.Sprintf("https://github.com/haloydev/haloy/releases/download/%s/%s", version, binaryName)

	logger.Info("Downloading new binary", "url", downloadURL)

	// Create temp file for download
	tmpFile, err := os.CreateTemp("", "haloyd-upgrade-*")
	if err != nil {
		return fmt.Errorf("failed to create temp file: %w", err)
	}
	tmpPath := tmpFile.Name()
	defer os.Remove(tmpPath)

	// Download binary
	req, err := http.NewRequestWithContext(ctx, "GET", downloadURL, nil)
	if err != nil {
		tmpFile.Close()
		return fmt.Errorf("failed to create download request: %w", err)
	}

	client := &http.Client{Timeout: 5 * time.Minute}
	resp, err := client.Do(req)
	if err != nil {
		tmpFile.Close()
		return fmt.Errorf("failed to download: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		tmpFile.Close()
		return fmt.Errorf("download failed with status %d", resp.StatusCode)
	}

	_, err = io.Copy(tmpFile, resp.Body)
	if err != nil {
		tmpFile.Close()
		return fmt.Errorf("failed to write downloaded file: %w", err)
	}
	tmpFile.Close()

	// Make executable
	if err := os.Chmod(tmpPath, 0o755); err != nil {
		return fmt.Errorf("failed to make binary executable: %w", err)
	}

	// Verify the binary works
	logger.Info("Verifying downloaded binary")
	verifyCmd := exec.CommandContext(ctx, tmpPath, "version")
	if _, err := verifyCmd.Output(); err != nil {
		return fmt.Errorf("downloaded binary verification failed: %w", err)
	}

	// Get current executable path
	execPath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("failed to determine executable path: %w", err)
	}

	// Create backup
	backupPath := execPath + ".backup"
	logger.Info("Backing up current binary", "path", backupPath)
	if err := copyFile(execPath, backupPath); err != nil {
		return fmt.Errorf("failed to backup current binary: %w", err)
	}

	// Install new binary
	logger.Info("Installing new binary")
	if err := copyFile(tmpPath, execPath); err != nil {
		// Try to restore backup on failure
		if restoreErr := copyFile(backupPath, execPath); restoreErr != nil {
			return fmt.Errorf("installation failed and could not restore backup: %w (restore error: %v)", err, restoreErr)
		}
		return fmt.Errorf("installation failed (backup restored): %w", err)
	}

	// Remove backup on success
	os.Remove(backupPath)

	return nil
}

// copyFile copies a file from src to dst
func copyFile(src, dst string) error {
	sourceFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer sourceFile.Close()

	sourceInfo, err := sourceFile.Stat()
	if err != nil {
		return err
	}

	destFile, err := os.OpenFile(dst, os.O_RDWR|os.O_CREATE|os.O_TRUNC, sourceInfo.Mode())
	if err != nil {
		return err
	}
	defer destFile.Close()

	_, err = io.Copy(destFile, sourceFile)
	return err
}

// fetchLatestVersion fetches the latest release version from GitHub.
// It first tries to get the latest stable release, and if none exists,
// falls back to the most recent release (including prereleases/betas).
func fetchLatestVersion(ctx context.Context) (string, error) {
	client := &http.Client{Timeout: 30 * time.Second}

	// First, try to get the latest stable release
	version, err := fetchReleaseVersion(ctx, client, "https://api.github.com/repos/haloydev/haloy/releases/latest")
	if err == nil && version != "" {
		return version, nil
	}

	// If no stable release found (404), try to get the most recent release including prereleases
	version, err = fetchLatestPrerelease(ctx, client)
	if err != nil {
		return "", err
	}

	if version == "" {
		return "", fmt.Errorf("no releases found on GitHub")
	}

	return version, nil
}

// fetchReleaseVersion fetches version from a specific GitHub releases endpoint
func fetchReleaseVersion(ctx context.Context, client *http.Client, url string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to fetch release: %w", err)
	}
	defer resp.Body.Close()

	// Return empty string for 404 (no stable release exists)
	if resp.StatusCode == http.StatusNotFound {
		return "", nil
	}

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("GitHub API returned status %d: %s", resp.StatusCode, string(body))
	}

	var release gitHubRelease
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return "", fmt.Errorf("failed to decode GitHub response: %w", err)
	}

	return release.TagName, nil
}

// fetchLatestPrerelease fetches the most recent release including prereleases
func fetchLatestPrerelease(ctx context.Context, client *http.Client) (string, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", "https://api.github.com/repos/haloydev/haloy/releases", nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to fetch releases: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("GitHub API returned status %d: %s", resp.StatusCode, string(body))
	}

	var releases []gitHubRelease
	if err := json.NewDecoder(resp.Body).Decode(&releases); err != nil {
		return "", fmt.Errorf("failed to decode GitHub response: %w", err)
	}

	if len(releases) == 0 {
		return "", nil
	}

	// Return the first (most recent) release
	return releases[0].TagName, nil
}
