package haloy

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strings"

	"github.com/haloydev/haloy/internal/constants"
	"github.com/haloydev/haloy/internal/helpers"
	"github.com/haloydev/haloy/internal/sshrunner"
	"github.com/haloydev/haloy/internal/ui"
	"github.com/spf13/cobra"
)

const (
	upgradeServerScript = "curl -sL https://raw.githubusercontent.com/haloydev/haloy/main/scripts/upgrade-server.sh | sh"
)

type GitHubRelease struct {
	TagName string `json:"tag_name"`
}

func ServerUpgradeCmd(configPath *string, flags *appCmdFlags) *cobra.Command {
	var (
		serverFlag       string
		manualMode       bool
		autoApprove      bool
		skipVersionCheck bool
	)

	cmd := &cobra.Command{
		Use:   "upgrade [<url>]",
		Short: "Upgrade a Haloy server to the latest version",
		Long: `Upgrade a Haloy server to the latest version.

This command will:
  - Verify your haloy CLI is on the latest version
  - Check the current server version
  - Download and install the latest haloyd and haloyadm binaries
  - Restart the Haloy services

Use --manual to get instructions for upgrading manually via SSH.

Examples:
  haloy server upgrade haloy.myserver.com
  haloy server upgrade --manual haloy.myserver.com
  haloy server upgrade haloy.myserver.com --auto-approve`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()

			// Get server URL from args or flag
			server := serverFlag
			if len(args) > 0 {
				server = args[0]
			}
			if server == "" {
				return fmt.Errorf("server URL is required. Use --server or pass it as an argument")
			}

			// Check CLI version first
			if !skipVersionCheck {
				if err := checkCLIVersion(ctx); err != nil {
					return err
				}
			}

			// Check current server version
			currentServerVersion, err := getServerVersion(ctx, nil, server, "")
			if err != nil {
				return fmt.Errorf("failed to get current server version: %w", err)
			}

			ui.Info("Current server version: %s", currentServerVersion.Version)
			ui.Info("Current CLI version: %s", constants.Version)

			// Fetch latest version
			latestVersion, err := getLatestReleaseVersion(ctx)
			if err != nil {
				ui.Warn("Could not fetch latest version from GitHub: %v", err)
				latestVersion = "unknown"
			} else {
				ui.Info("Latest available version: %s", latestVersion)
			}

			if manualMode {
				return printManualUpgradeInstructions(server, latestVersion)
			}

			// Confirm upgrade with user
			if !autoApprove {
				ui.Info("")
				ui.Info("This will upgrade the server at '%s' to the latest version.", server)
				ui.Info("The Haloy services will be restarted during the upgrade.")
				response, err := ui.Prompt("Do you want to proceed? (yes/no)")
				if err != nil {
					return err
				}
				if strings.ToLower(response) != "yes" && strings.ToLower(response) != "y" {
					ui.Info("Upgrade cancelled")
					return nil
				}
			}

			// Perform SSH-based upgrade
			return performSSHUpgrade(ctx, server, latestVersion)
		},
	}

	cmd.Flags().StringVarP(&serverFlag, "server", "s", "", "Server URL (can also be passed as an argument)")
	cmd.Flags().BoolVar(&manualMode, "manual", false, "Print manual upgrade instructions instead of upgrading via SSH")
	cmd.Flags().BoolVar(&autoApprove, "auto-approve", false, "Skip confirmation prompt")
	cmd.Flags().BoolVar(&skipVersionCheck, "skip-version-check", false, "Skip CLI version check (not recommended)")

	return cmd
}

func checkCLIVersion(ctx context.Context) error {
	latestVersion, err := getLatestReleaseVersion(ctx)
	if err != nil {
		// Don't block on network errors, but warn
		ui.Warn("Could not verify latest CLI version: %v", err)
		return nil
	}

	if latestVersion != constants.Version {
		return fmt.Errorf(
			"CLI version mismatch: you have %s but latest is %s\n\n"+
				"You must upgrade your CLI before upgrading the server to ensure feature parity.\n\n"+
				"For upgrade instructions, visit:\n"+
				"  https://haloy.dev/docs/upgrading#upgrading-haloy-cli\n\n"+
				"Then try again:\n"+
				"  haloy server upgrade <server>",
			constants.Version, latestVersion)
	}

	return nil
}

func getLatestReleaseVersion(ctx context.Context) (string, error) {
	const githubAPI = "https://api.github.com/repos/haloydev/haloy/releases/latest"

	req, err := http.NewRequestWithContext(ctx, "GET", githubAPI, nil)
	if err != nil {
		return "", err
	}

	req.Header.Set("Accept", "application/json")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to fetch latest release: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("GitHub API returned status %d: %s", resp.StatusCode, string(body))
	}

	var release GitHubRelease
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return "", fmt.Errorf("failed to decode GitHub response: %w", err)
	}

	if release.TagName == "" {
		return "", fmt.Errorf("no releases found on GitHub")
	}

	return release.TagName, nil
}

func printManualUpgradeInstructions(server, latestVersion string) error {
	ui.Info("")
	ui.Info("To upgrade your server manually:")
	ui.Info("")
	ui.Info("1. SSH into your server:")
	ui.Info("   ssh root@%s", server)
	ui.Info("")
	ui.Info("2. Run the upgrade script:")
	ui.Info("   %s", upgradeServerScript)
	ui.Info("")
	if latestVersion != "unknown" {
		ui.Info("This will upgrade to version: %s", latestVersion)
	}
	ui.Info("")
	return nil
}

// extractSSHHost extracts just the hostname from a server URL, stripping protocol and port.
func extractSSHHost(server string) (string, error) {
	normalized, err := helpers.NormalizeServerURL(server)
	if err != nil {
		return "", err
	}
	// NormalizeServerURL returns host:port or just host
	host, _, err := net.SplitHostPort(normalized)
	if err != nil {
		// No port present, use as-is
		return normalized, nil
	}
	return host, nil
}

func performSSHUpgrade(ctx context.Context, server, latestVersion string) error {
	// For now, assume root user (consistent with server setup)
	user := "root"

	// Extract just the hostname for SSH (strip protocol/port)
	sshHost, err := extractSSHHost(server)
	if err != nil {
		return fmt.Errorf("invalid server URL: %w", err)
	}

	sshCfg := sshrunner.Config{
		User: user,
		Host: sshHost,
		Port: 22,
	}

	ui.Info("")
	ui.Info("Connecting to %s@%s over SSH...", user, server)

	ui.Info("Executing upgrade script on remote server...")
	if _, err := sshrunner.RunStreaming(ctx, sshCfg, upgradeServerScript, os.Stdout, os.Stderr); err != nil {
		return fmt.Errorf("remote upgrade failed: %w", err)
	}

	ui.Info("")
	ui.Info("Verifying upgrade...")
	version, err := getServerVersion(ctx, nil, server, "")
	if err != nil {
		ui.Warn("Could not verify server version after upgrade: %v", err)
		ui.Info("Please check the server status manually")
		return nil
	}

	if latestVersion != "unknown" && version.Version != latestVersion {
		ui.Warn("Server version after upgrade: %s (expected %s)", version.Version, latestVersion)
	} else {
		ui.Info("Server version after upgrade: %s", version.Version)
	}

	ui.Success("Server upgrade completed successfully!")
	return nil
}
