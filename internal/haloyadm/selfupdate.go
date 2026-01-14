package haloyadm

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"time"

	"github.com/haloydev/haloy/internal/constants"
	"github.com/haloydev/haloy/internal/github"
	"github.com/haloydev/haloy/internal/helpers"
	"github.com/haloydev/haloy/internal/ui"
	"github.com/spf13/cobra"
)

func SelfUpdateCmd() *cobra.Command {
	var (
		checkOnly bool
		force     bool
	)

	cmd := &cobra.Command{
		Use:   "self-update",
		Short: "Update haloyadm to the latest version",
		Long: `Update haloyadm to the latest version from GitHub releases.

This command will:
  - Check for the latest version on GitHub
  - Download the new binary for your platform
  - Backup the existing binary
  - Install the new version

Examples:
  # Update to latest version
  haloyadm self-update

  # Check if update is available without installing
  haloyadm self-update --check

  # Force update even if already on latest
  haloyadm self-update --force`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			return runSelfUpdate(ctx, checkOnly, force)
		},
	}

	cmd.Flags().BoolVar(&checkOnly, "check", false, "Only check if an update is available, don't install")
	cmd.Flags().BoolVar(&force, "force", false, "Force update even if already on the latest version")

	return cmd
}

func runSelfUpdate(ctx context.Context, checkOnly, force bool) error {
	currentVersion := constants.Version
	ui.Info("Current version: %s", currentVersion)

	// Fetch latest version from GitHub
	ui.Info("Checking for updates...")
	latestVersion, err := github.FetchLatestVersion(ctx)
	if err != nil {
		return fmt.Errorf("failed to check for updates: %w", err)
	}

	ui.Info("Latest version: %s", latestVersion)

	// Normalize versions for comparison (strip 'v' prefix if present)
	normalizedCurrent := helpers.NormalizeVersion(currentVersion)
	normalizedLatest := helpers.NormalizeVersion(latestVersion)

	// Check if update is needed
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

	// Find current binary path
	execPath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("failed to determine executable path: %w", err)
	}

	// Resolve symlinks to get the actual binary path
	execPath, err = resolveSymlinks(execPath)
	if err != nil {
		return fmt.Errorf("failed to resolve executable path: %w", err)
	}

	ui.Info("Updating haloyadm...")
	ui.Info("Binary path: %s", execPath)

	// Download and install
	if err := downloadAndInstall(ctx, execPath, latestVersion); err != nil {
		return err
	}

	ui.Success("Successfully updated haloyadm to %s!", latestVersion)
	return nil
}

// downloadAndInstall downloads and installs the new haloyadm binary
func downloadAndInstall(ctx context.Context, currentPath, version string) error {
	// Detect platform and architecture
	platform := runtime.GOOS
	arch := runtime.GOARCH

	// Construct download URL
	binaryName := fmt.Sprintf("haloyadm-%s-%s", platform, arch)
	downloadURL := fmt.Sprintf("https://github.com/haloydev/haloy/releases/download/%s/%s", version, binaryName)

	ui.Info("Downloading %s...", binaryName)

	// Download to temp file
	tmpFile, err := os.CreateTemp("", "haloyadm-update-*")
	if err != nil {
		return fmt.Errorf("failed to create temp file: %w", err)
	}
	tmpPath := tmpFile.Name()
	defer os.Remove(tmpPath) // Clean up on error

	// Download the binary
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

	// Write to temp file
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

	// Verify the downloaded binary works
	ui.Info("Verifying download...")
	verifyCmd := exec.CommandContext(ctx, tmpPath, "version")
	if output, err := verifyCmd.Output(); err != nil {
		return fmt.Errorf("downloaded binary verification failed: %w", err)
	} else {
		ui.Info("Downloaded version: %s", string(output))
	}

	// Backup existing binary
	backupPath := currentPath + ".backup"
	ui.Info("Backing up current binary to %s", backupPath)
	if err := copyFile(currentPath, backupPath); err != nil {
		return fmt.Errorf("failed to backup current binary: %w", err)
	}

	// Install new binary
	ui.Info("Installing new binary...")
	if err := copyFile(tmpPath, currentPath); err != nil {
		// Try to restore backup
		ui.Warn("Installation failed, restoring backup...")
		if restoreErr := copyFile(backupPath, currentPath); restoreErr != nil {
			return fmt.Errorf("installation failed and could not restore backup: %w (restore error: %v)", err, restoreErr)
		}
		return fmt.Errorf("installation failed (backup restored): %w", err)
	}

	// Clean up backup on success
	os.Remove(backupPath)

	return nil
}

// resolveSymlinks resolves symlinks to get the actual file path
func resolveSymlinks(path string) (string, error) {
	resolved, err := os.Readlink(path)
	if err != nil {
		// Not a symlink, return original path
		if os.IsNotExist(err) {
			return "", err
		}
		return path, nil
	}

	// If the resolved path is relative, make it absolute
	if !isAbsolutePath(resolved) {
		dir := dirPath(path)
		resolved = dir + "/" + resolved
	}

	// Recursively resolve in case of chained symlinks
	return resolveSymlinks(resolved)
}

// isAbsolutePath checks if a path is absolute
func isAbsolutePath(path string) bool {
	if len(path) == 0 {
		return false
	}
	if runtime.GOOS == "windows" {
		return len(path) >= 2 && path[1] == ':'
	}
	return path[0] == '/'
}

// dirPath returns the directory portion of a path
func dirPath(path string) string {
	for i := len(path) - 1; i >= 0; i-- {
		if path[i] == '/' || path[i] == '\\' {
			return path[:i]
		}
	}
	return "."
}

// copyFile copies a file from src to dst, preserving permissions
func copyFile(src, dst string) error {
	sourceFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer sourceFile.Close()

	// Get source file info for permissions
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
