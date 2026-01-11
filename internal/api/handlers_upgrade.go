package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"time"

	"github.com/haloydev/haloy/internal/apitypes"
	"github.com/haloydev/haloy/internal/constants"
	"github.com/haloydev/haloy/internal/logging"
)

type gitHubRelease struct {
	TagName string `json:"tag_name"`
}

// handleUpgrade handles the first phase of upgrade: updating haloyadm binary using self-update
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

		if currentVersion == latestVersion {
			encodeJSON(w, http.StatusOK, apitypes.UpgradeResponse{
				Status:          "completed",
				PreviousVersion: currentVersion,
				TargetVersion:   latestVersion,
				Message:         "Already running the latest version",
			})
			return
		}

		logger.Info("Starting upgrade via haloyadm self-update", "from", currentVersion, "to", latestVersion)

		// Run haloyadm self-update
		if err := runHaloyadmSelfUpdate(ctx, logger); err != nil {
			logger.Error("haloyadm self-update failed", "error", err)
			encodeJSON(w, http.StatusInternalServerError, apitypes.UpgradeResponse{
				Status:          "failed",
				PreviousVersion: currentVersion,
				TargetVersion:   latestVersion,
				Message:         fmt.Sprintf("Self-update failed: %v", err),
			})
			return
		}

		logger.Info("Successfully updated haloyadm binary", "version", latestVersion)

		encodeJSON(w, http.StatusOK, apitypes.UpgradeResponse{
			Status:          "updating",
			PreviousVersion: currentVersion,
			TargetVersion:   latestVersion,
			Message:         "haloyadm binary updated. Call /v1/upgrade/restart to complete the upgrade.",
		})
	}
}

// handleUpgradeRestart handles the second phase: restarting services with the new version
func (s *APIServer) handleUpgradeRestart() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		logger := logging.NewLogger(s.logLevel, s.logBroker)

		logger.Info("Starting service restart for upgrade")

		// Spawn a detached process to run haloyadm restart
		// We need to do this because haloyadm restart will stop this container
		go func() {
			// Small delay to ensure the HTTP response is sent
			time.Sleep(500 * time.Millisecond)

			logger.Info("Executing haloyadm restart")

			// Run haloyadm restart
			cmd := exec.Command("haloyadm", "restart", "--no-logs")
			cmd.Stdout = os.Stdout
			cmd.Stderr = os.Stderr

			if err := cmd.Start(); err != nil {
				logger.Error("Failed to start haloyadm restart", "error", err)
				return
			}

			// Don't wait for completion - the container will be replaced
			logger.Info("haloyadm restart started, container will be replaced")
		}()

		encodeJSON(w, http.StatusAccepted, apitypes.UpgradeResponse{
			Status:  "restarting",
			Message: "Services are restarting. Poll /v1/version to check when upgrade is complete.",
		})
	}
}

// runHaloyadmSelfUpdate executes the haloyadm self-update command
func runHaloyadmSelfUpdate(ctx context.Context, logger *slog.Logger) error {
	cmd := exec.CommandContext(ctx, "haloyadm", "self-update")

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	if err != nil {
		// Log the output for debugging
		if stdout.Len() > 0 {
			logger.Info("haloyadm self-update stdout", "output", stdout.String())
		}
		if stderr.Len() > 0 {
			logger.Error("haloyadm self-update stderr", "output", stderr.String())
		}
		return fmt.Errorf("haloyadm self-update failed: %w", err)
	}

	// Log success output
	if stdout.Len() > 0 {
		logger.Info("haloyadm self-update completed", "output", stdout.String())
	}

	return nil
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
