package haloydcli

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"time"

	"github.com/haloydev/haloy/internal/constants"
	"github.com/haloydev/haloy/internal/github"
	"github.com/haloydev/haloy/internal/helpers"
	"github.com/haloydev/haloy/internal/ui"
	"github.com/spf13/cobra"
)

func newUpgradeCmd() *cobra.Command {
	var checkOnly bool
	var force bool

	cmd := &cobra.Command{
		Use:   "upgrade",
		Short: "Upgrade haloyd to the latest version",
		Long: `Upgrade haloyd to the latest version from GitHub releases.

This command will:
  - Check for the latest version on GitHub
  - Download the new binary for your platform
  - Backup the existing binary
  - Install the new version

After upgrading, restart haloyd with: systemctl restart haloyd

Examples:
  # Upgrade to latest version
  haloyd upgrade

  # Check if upgrade is available without installing
  haloyd upgrade --check

  # Force upgrade even if already on latest
  haloyd upgrade --force`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			return runUpgrade(ctx, checkOnly, force)
		},
	}

	cmd.Flags().BoolVar(&checkOnly, "check", false, "Only check if an upgrade is available, don't install")
	cmd.Flags().BoolVar(&force, "force", false, "Force upgrade even if already on the latest version")

	return cmd
}

func runUpgrade(ctx context.Context, checkOnly, force bool) error {
	currentVersion := constants.Version
	ui.Info("Current version: %s", currentVersion)

	ui.Info("Checking for updates...")
	latestVersion, err := github.FetchLatestVersion(ctx)
	if err != nil {
		return fmt.Errorf("failed to check for updates: %w", err)
	}

	ui.Info("Latest version: %s", latestVersion)

	normalizedCurrent := helpers.NormalizeVersion(currentVersion)
	normalizedLatest := helpers.NormalizeVersion(latestVersion)

	if normalizedCurrent == normalizedLatest && !force {
		ui.Success("Already running the latest version!")
		return nil
	}

	if checkOnly {
		if normalizedCurrent != normalizedLatest {
			ui.Info("Update available: %s -> %s", currentVersion, latestVersion)
		}
		return nil
	}

	execPath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("failed to determine executable path: %w", err)
	}

	execPath, err = resolveSymlinks(execPath)
	if err != nil {
		return fmt.Errorf("failed to resolve executable path: %w", err)
	}

	ui.Info("Upgrading haloyd...")
	ui.Info("Binary path: %s", execPath)

	if err := downloadAndInstall(ctx, execPath, latestVersion); err != nil {
		return err
	}

	ui.Success("Successfully upgraded haloyd to %s!", latestVersion)
	ui.Info("Restart haloyd with: systemctl restart haloyd")

	return nil
}

func downloadAndInstall(ctx context.Context, currentPath, version string) error {
	platform := runtime.GOOS
	arch := runtime.GOARCH

	binaryName := fmt.Sprintf("haloyd-%s-%s", platform, arch)
	downloadURL := fmt.Sprintf("https://github.com/haloydev/haloy/releases/download/%s/%s", version, binaryName)

	ui.Info("Downloading %s...", binaryName)

	tmpFile, err := os.CreateTemp("", "haloyd-upgrade-*")
	if err != nil {
		return fmt.Errorf("failed to create temp file: %w", err)
	}
	tmpPath := tmpFile.Name()
	defer os.Remove(tmpPath)

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

	if err := os.Chmod(tmpPath, 0o755); err != nil {
		return fmt.Errorf("failed to make binary executable: %w", err)
	}

	ui.Info("Verifying download...")
	verifyCmd := exec.CommandContext(ctx, tmpPath, "version")
	if output, err := verifyCmd.Output(); err != nil {
		return fmt.Errorf("downloaded binary verification failed: %w", err)
	} else {
		ui.Info("Downloaded version: %s", string(output))
	}

	backupPath := currentPath + ".backup"
	ui.Info("Backing up current binary to %s", backupPath)
	if err := copyFile(currentPath, backupPath); err != nil {
		return fmt.Errorf("failed to backup current binary: %w", err)
	}

	ui.Info("Installing new binary...")
	if err := copyFile(tmpPath, currentPath); err != nil {
		ui.Warn("Installation failed, restoring backup...")
		if restoreErr := copyFile(backupPath, currentPath); restoreErr != nil {
			return fmt.Errorf("installation failed and could not restore backup: %w (restore error: %v)", err, restoreErr)
		}
		return fmt.Errorf("installation failed (backup restored): %w", err)
	}

	os.Remove(backupPath)

	return nil
}

func resolveSymlinks(path string) (string, error) {
	return filepath.EvalSymlinks(path)
}

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
