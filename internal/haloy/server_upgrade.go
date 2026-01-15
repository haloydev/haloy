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
	"time"

	"github.com/haloydev/haloy/internal/apiclient"
	"github.com/haloydev/haloy/internal/apitypes"
	"github.com/haloydev/haloy/internal/config"
	"github.com/haloydev/haloy/internal/configloader"
	"github.com/haloydev/haloy/internal/constants"
	"github.com/haloydev/haloy/internal/helpers"
	"github.com/haloydev/haloy/internal/sshrunner"
	"github.com/haloydev/haloy/internal/ui"
	"github.com/spf13/cobra"
	"golang.org/x/sync/errgroup"
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
		useSSH           bool
	)

	cmd := &cobra.Command{
		Use:   "upgrade",
		Short: "Upgrade Haloy server(s) to the latest version",
		Long: `Upgrade Haloy server(s) to the latest version.

This command will:
  - Verify your haloy CLI is on the latest version
  - Check the current server version(s)
  - Download and install the latest haloyd binary
  - Restart the haloyd service

The command can upgrade servers defined in your haloy config file,
or a specific server using the --server flag.

If your config has a single server (defined at top-level or all targets
use the same server), no flags are needed. If multiple servers are found,
use --all or --targets to specify which servers to upgrade.

By default, the upgrade is performed via the haloyd API. Use --use-ssh
to upgrade via SSH instead (requires root SSH access).

Use --manual to get instructions for upgrading manually via SSH.

Examples:
  # Upgrade server from config (single server)
  haloy server upgrade

  # Upgrade all servers (multi-server config)
  haloy server upgrade --all

  # Upgrade servers used by specific targets
  haloy server upgrade --targets production,staging

  # Upgrade a specific server directly
  haloy server upgrade --server haloy.myserver.com

  # Upgrade via SSH instead of API
  haloy server upgrade --use-ssh

  # Get manual upgrade instructions
  haloy server upgrade --manual

  # Skip confirmation prompt
  haloy server upgrade --auto-approve`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()

			// Check CLI version first (once for all servers)
			if !skipVersionCheck {
				if err := checkCLIVersion(ctx); err != nil {
					return err
				}
			}

			latestVersion, err := getLatestReleaseVersion(ctx)
			if err != nil {
				ui.Warn("Could not fetch latest version from GitHub: %v", err)
				latestVersion = "unknown"
			}

			// If --server flag is provided, upgrade that single server directly
			if serverFlag != "" {
				return upgradeServer(ctx, nil, serverFlag, latestVersion, manualMode, autoApprove, useSSH, "")
			}

			// Load raw config (without target filtering)
			rawDeployConfig, _, err := configloader.LoadRawDeployConfig(*configPath)
			if err != nil {
				return fmt.Errorf("unable to load config: %w", err)
			}

			resolvedDeployConfig, err := configloader.ResolveSecrets(ctx, rawDeployConfig)
			if err != nil {
				return fmt.Errorf("failed to resolve secrets: %w", err)
			}

			uniqueServers, err := extractServersFromConfig(resolvedDeployConfig, flags.targets, flags.all)
			if err != nil {
				return err
			}

			if len(uniqueServers) == 0 {
				return fmt.Errorf("no servers found in configuration")
			}

			ui.Info("Servers to upgrade:")
			for server := range uniqueServers {
				ui.Info("  - %s", server)
			}
			ui.Info("")
			ui.Info("Current CLI version: %s", constants.Version)
			if latestVersion != "unknown" {
				ui.Info("Latest available version: %s", helpers.NormalizeVersion(latestVersion))
			}

			if manualMode {
				for server := range uniqueServers {
					printManualUpgradeInstructions(server, latestVersion)
				}
				return nil
			}

			if !autoApprove {
				ui.Info("")
				if len(uniqueServers) == 1 {
					for server := range uniqueServers {
						ui.Info("This will upgrade the server at '%s' to the latest version.", server)
					}
				} else {
					ui.Info("This will upgrade %d servers to the latest version.", len(uniqueServers))
				}
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

			g, ctx := errgroup.WithContext(ctx)
			for server, srvInfo := range uniqueServers {
				prefix := ""
				if len(uniqueServers) > 1 {
					prefix = server
				}
				g.Go(func() error {
					return upgradeServer(ctx, &srvInfo, server, latestVersion, false, true, useSSH, prefix)
				})
			}

			if err := g.Wait(); err != nil {
				return err
			}

			ui.Success("All server upgrades completed successfully!")
			return nil
		},
	}

	cmd.Flags().StringVarP(&flags.configPath, "config", "c", "", "Path to config file or directory (default: .)")
	cmd.Flags().StringVarP(&serverFlag, "server", "s", "", "Server URL (overrides config file)")
	cmd.Flags().StringSliceVarP(&flags.targets, "targets", "t", nil, "Upgrade servers for specific targets (comma-separated)")
	cmd.Flags().BoolVarP(&flags.all, "all", "a", false, "Upgrade servers for all targets")
	cmd.Flags().BoolVar(&manualMode, "manual", false, "Print manual upgrade instructions instead of upgrading via SSH")
	cmd.Flags().BoolVar(&autoApprove, "auto-approve", false, "Skip confirmation prompt")
	cmd.Flags().BoolVar(&skipVersionCheck, "skip-version-check", false, "Skip CLI version check (not recommended)")
	cmd.Flags().BoolVar(&useSSH, "use-ssh", false, "Use SSH instead of API for upgrade (requires root SSH access)")

	return cmd
}

// serverInfo holds the server URL and associated API token for upgrades
type serverInfo struct {
	Server   string
	APIToken *config.ValueSource
}

// toTargetConfig creates a minimal TargetConfig for compatibility with existing functions
func (s *serverInfo) toTargetConfig() *config.TargetConfig {
	if s == nil {
		return nil
	}
	return &config.TargetConfig{
		Server:   s.Server,
		APIToken: s.APIToken,
	}
}

// extractServersFromConfig extracts unique servers from the deploy config.
// It handles:
// - Top-level server definition (used by all targets)
// - Per-target server overrides
// - Filtering by --targets flag
// - Requiring --all or --targets when multiple servers exist
func extractServersFromConfig(cfg config.DeployConfig, targetNames []string, allTargets bool) (map[string]serverInfo, error) {
	// First, collect all servers (either filtered by targets or all)
	servers := make(map[string]serverInfo)

	// If specific targets are requested, only look at those targets' servers
	if len(targetNames) > 0 {
		for _, targetName := range targetNames {
			target, exists := cfg.Targets[targetName]
			if !exists {
				return nil, fmt.Errorf("target '%s' not found in configuration", targetName)
			}

			serverURL := target.Server
			if serverURL == "" {
				serverURL = cfg.Server // Inherit from top-level
			}
			if serverURL == "" {
				continue // No server defined for this target
			}

			if _, exists := servers[serverURL]; !exists {
				apiToken := target.APIToken
				if apiToken == nil {
					apiToken = cfg.APIToken // Inherit from top-level
				}
				servers[serverURL] = serverInfo{
					Server:   serverURL,
					APIToken: apiToken,
				}
			}
		}

		if len(servers) == 0 {
			return nil, fmt.Errorf("no servers found for the specified targets")
		}
		return servers, nil
	}

	// No specific targets requested - collect all unique servers
	// First check the top-level server
	if cfg.Server != "" {
		servers[cfg.Server] = serverInfo{
			Server:   cfg.Server,
			APIToken: cfg.APIToken,
		}
	}

	// Then check each target for different servers
	for _, target := range cfg.Targets {
		serverURL := target.Server
		if serverURL == "" {
			serverURL = cfg.Server // Inherit from top-level
		}
		if serverURL == "" {
			continue
		}

		if _, exists := servers[serverURL]; !exists {
			apiToken := target.APIToken
			if apiToken == nil {
				apiToken = cfg.APIToken // Inherit from top-level
			}
			servers[serverURL] = serverInfo{
				Server:   serverURL,
				APIToken: apiToken,
			}
		}
	}

	if len(servers) == 0 {
		return nil, fmt.Errorf("no servers found in configuration")
	}

	// If there's only one server, no flags needed
	if len(servers) == 1 {
		return servers, nil
	}

	// Multiple servers found - require explicit flag
	if !allTargets {
		serverList := make([]string, 0, len(servers))
		for server := range servers {
			serverList = append(serverList, server)
		}
		return nil, fmt.Errorf(
			"multiple servers found in configuration:\n  - %s\n\nUse --all to upgrade all servers, or --targets to specify which targets' servers to upgrade",
			strings.Join(serverList, "\n  - "),
		)
	}

	return servers, nil
}

func checkCLIVersion(ctx context.Context) error {
	latestVersion, err := getLatestReleaseVersion(ctx)
	if err != nil {
		// Don't block on network errors, but warn
		ui.Warn("Could not verify latest CLI version: %v", err)
		return nil
	}

	// Normalize versions for comparison (strip 'v' prefix if present)
	normalizedCurrent := helpers.NormalizeVersion(constants.Version)
	normalizedLatest := helpers.NormalizeVersion(latestVersion)

	if normalizedLatest != normalizedCurrent {
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

// getLatestReleaseVersion fetches the latest release version from GitHub.
// It first tries to get the latest stable release, and if none exists,
// falls back to the most recent release (including prereleases/betas).
func getLatestReleaseVersion(ctx context.Context) (string, error) {
	client := &http.Client{Timeout: 30 * time.Second}

	// First, try to get the latest stable release
	version, err := fetchGitHubRelease(ctx, client, "https://api.github.com/repos/haloydev/haloy/releases/latest")
	if err == nil && version != "" {
		return version, nil
	}

	// If no stable release found (404), try to get the most recent release including prereleases
	version, err = fetchLatestGitHubPrerelease(ctx, client)
	if err != nil {
		return "", err
	}

	if version == "" {
		return "", fmt.Errorf("no releases found on GitHub")
	}

	return version, nil
}

// fetchGitHubRelease fetches version from a specific GitHub releases endpoint
func fetchGitHubRelease(ctx context.Context, client *http.Client, url string) (string, error) {
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

	var release GitHubRelease
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return "", fmt.Errorf("failed to decode GitHub response: %w", err)
	}

	return release.TagName, nil
}

// fetchLatestGitHubPrerelease fetches the most recent release including prereleases
func fetchLatestGitHubPrerelease(ctx context.Context, client *http.Client) (string, error) {
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

	var releases []GitHubRelease
	if err := json.NewDecoder(resp.Body).Decode(&releases); err != nil {
		return "", fmt.Errorf("failed to decode GitHub response: %w", err)
	}

	if len(releases) == 0 {
		return "", nil
	}

	// Return the first (most recent) release
	return releases[0].TagName, nil
}

func printManualUpgradeInstructions(server, latestVersion string) error {
	// Extract just the hostname for SSH instructions
	sshHost, err := extractSSHHost(server)
	if err != nil {
		sshHost = server
	}

	ui.Info("")
	ui.Info("To upgrade server '%s' manually:", server)
	ui.Info("")
	ui.Info("1. SSH into your server:")
	ui.Info("   ssh root@%s", sshHost)
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

// upgradeServer upgrades a single Haloy server.
// If srvInfo is provided, it will be used to get the API token.
// If srvInfo is nil, the token will be retrieved from the client config.
func upgradeServer(ctx context.Context, srvInfo *serverInfo, server, latestVersion string, manualMode, autoApprove, useSSH bool, prefix string) error {
	pui := &ui.PrefixedUI{Prefix: prefix}

	// Check current server version
	currentServerVersion, err := getServerVersion(ctx, srvInfo.toTargetConfig(), server, prefix)
	if err != nil {
		return &PrefixedError{Err: fmt.Errorf("failed to get current server version: %w", err), Prefix: prefix}
	}

	pui.Info("Current server version: %s", currentServerVersion.Version)

	if latestVersion != "unknown" {
		pui.Info("Latest available version: %s", latestVersion)
	}

	// Handle manual mode for single server case
	if manualMode {
		return printManualUpgradeInstructions(server, latestVersion)
	}

	// Handle confirmation for single server case (when called directly with --server flag)
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

	// Perform upgrade via SSH or API
	if useSSH {
		return performSSHUpgrade(ctx, server, latestVersion, prefix)
	}
	return performAPIUpgrade(ctx, srvInfo.toTargetConfig(), server, latestVersion, prefix)
}

// performAPIUpgrade upgrades a server using the haloyd API (two-phase upgrade)
func performAPIUpgrade(ctx context.Context, targetConfig *config.TargetConfig, server, latestVersion, prefix string) error {
	pui := &ui.PrefixedUI{Prefix: prefix}

	// Get API token
	token, err := getToken(targetConfig, server)
	if err != nil {
		return &PrefixedError{Err: fmt.Errorf("unable to get token: %w", err), Prefix: prefix}
	}

	// Create API client
	api, err := apiclient.New(server, token)
	if err != nil {
		return &PrefixedError{Err: fmt.Errorf("unable to create API client: %w", err), Prefix: prefix}
	}

	// Phase 1: Update haloyd binary
	pui.Info("Phase 1: Updating haloyd binary...")
	var upgradeResp apitypes.UpgradeResponse
	if err := api.Post(ctx, "upgrade", nil, &upgradeResp); err != nil {
		return &PrefixedError{Err: fmt.Errorf("failed to start upgrade: %w", err), Prefix: prefix}
	}

	if upgradeResp.Status == "failed" {
		return &PrefixedError{Err: fmt.Errorf("upgrade failed: %s", upgradeResp.Message), Prefix: prefix}
	}

	if upgradeResp.Status == "completed" && upgradeResp.Message == "Already running the latest version" {
		pui.Info("Server is already running the latest version (%s)", upgradeResp.TargetVersion)
		return nil
	}

	pui.Info("haloyd updated from %s to %s", upgradeResp.PreviousVersion, upgradeResp.TargetVersion)

	// Phase 2: Restart services
	pui.Info("Phase 2: Restarting services...")
	var restartResp apitypes.UpgradeResponse
	if err := api.Post(ctx, "upgrade/restart", nil, &restartResp); err != nil {
		return &PrefixedError{Err: fmt.Errorf("failed to restart services: %w", err), Prefix: prefix}
	}

	pui.Info("Services are restarting...")

	// Phase 3: Poll for completion
	pui.Info("Waiting for server to come back online...")
	newVersion, err := pollForUpgradeCompletion(ctx, server, token, latestVersion)
	if err != nil {
		pui.Warn("Could not verify upgrade completion: %v", err)
		pui.Info("Please check the server status manually")
		return nil
	}

	if latestVersion != "unknown" && newVersion != latestVersion {
		pui.Warn("Server version after upgrade: %s (expected %s)", newVersion, latestVersion)
	} else {
		pui.Info("Server version after upgrade: %s", newVersion)
	}

	pui.Success("Server upgrade completed successfully!")
	return nil
}

// pollForUpgradeCompletion polls the server until it comes back online with the new version
func pollForUpgradeCompletion(ctx context.Context, server, token, expectedVersion string) (string, error) {
	// Create a context with timeout for polling
	pollCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()

	// Wait a bit for the restart to begin
	time.Sleep(2 * time.Second)

	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-pollCtx.Done():
			return "", fmt.Errorf("timeout waiting for server to come back online")
		case <-ticker.C:
			// Try to connect to the server
			api, err := apiclient.New(server, token)
			if err != nil {
				continue // Server not ready yet
			}

			var versionResp apitypes.VersionResponse
			err = api.Get(pollCtx, "version", &versionResp)
			if err != nil {
				continue // Server not ready yet
			}

			// Server is back online
			return versionResp.Version, nil
		}
	}
}

// performSSHUpgrade upgrades a server using SSH (legacy method)
func performSSHUpgrade(ctx context.Context, server, latestVersion, prefix string) error {
	pui := &ui.PrefixedUI{Prefix: prefix}

	// For now, assume root user (consistent with server setup)
	user := "root"

	// Extract just the hostname for SSH (strip protocol/port)
	sshHost, err := extractSSHHost(server)
	if err != nil {
		return &PrefixedError{Err: fmt.Errorf("invalid server URL: %w", err), Prefix: prefix}
	}

	sshCfg := sshrunner.Config{
		User: user,
		Host: sshHost,
		Port: 22,
	}

	pui.Info("Connecting to %s@%s over SSH...", user, sshHost)

	pui.Info("Executing upgrade script on remote server...")
	if _, err := sshrunner.RunStreaming(ctx, sshCfg, upgradeServerScript, os.Stdout, os.Stderr); err != nil {
		return &PrefixedError{Err: fmt.Errorf("remote upgrade failed: %w", err), Prefix: prefix}
	}

	pui.Info("Verifying upgrade...")
	version, err := getServerVersion(ctx, nil, server, prefix)
	if err != nil {
		pui.Warn("Could not verify server version after upgrade: %v", err)
		pui.Info("Please check the server status manually")
		return nil
	}

	if latestVersion != "unknown" && version.Version != latestVersion {
		pui.Warn("Server version after upgrade: %s (expected %s)", version.Version, latestVersion)
	} else {
		pui.Info("Server version after upgrade: %s", version.Version)
	}

	pui.Success("Server upgrade completed successfully!")
	return nil
}
