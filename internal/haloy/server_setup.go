package haloy

import (
	"fmt"
	"os"
	"strings"

	"github.com/haloydev/haloy/internal/helpers"
	"github.com/haloydev/haloy/internal/sshrunner"
	"github.com/haloydev/haloy/internal/ui"
	"github.com/spf13/cobra"
)

const (
	installHaloyadmScript = "curl -sL https://raw.githubusercontent.com/haloydev/haloy/main/scripts/install-haloyadm.sh | sh"
	checkDockerCmd        = "command -v docker >/dev/null 2>&1 && echo 'installed' || echo 'missing'"
	installDockerScript   = "curl -fsSL https://raw.githubusercontent.com/haloydev/haloy/main/scripts/install-docker.sh | sh"
)

func ServerSetupCmd() *cobra.Command {
	var (
		user       string
		port       int
		apiDomain  string
		acmeEmail  string
		override   bool
		noServices bool
		noLogs     bool
		identity   string
	)

	cmd := &cobra.Command{
		Use:   "setup <host>",
		Short: "Provision a remote Haloy server over SSH",
		Long: `Set up a remote Haloy server over SSH with a single command.

This will:
  - SSH into the remote host
  - Install the haloyadm admin tool
  - Run 'haloyadm init' with your domain/email
  - Read the API token from the server
  - Add the server to your local haloy config

Examples:
  haloy server setup 192.168.1.100 --api-domain api.example.com --acme-email admin@example.com
  haloy server setup myserver.com -u ubuntu --ssh-identity ~/.ssh/id_rsa`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			host := args[0]

			if user == "" {
				user = "root"
			}

			if user != "root" {
				return fmt.Errorf("non-root remote setup is not supported yet; please use --user root or run setup manually on the server")
			}

			if apiDomain != "" {
				if err := helpers.IsValidDomain(apiDomain); err != nil {
					return fmt.Errorf("invalid --api-domain: %w", err)
				}
			}

			if acmeEmail != "" && strings.ContainsAny(acmeEmail, " \t\n\";'|&") {
				return fmt.Errorf("invalid --acme-email: must not contain spaces or shell control characters")
			}

			sshCfg := sshrunner.Config{
				User:     user,
				Host:     host,
				Port:     port,
				Identity: identity,
			}

			ui.Info("Connecting to %s@%s:%d over SSH...", user, host, port)

			ui.Info("Checking if Docker is installed...")
			dockerCheck, err := sshrunner.Run(ctx, sshCfg, checkDockerCmd)
			if err != nil {
				return fmt.Errorf("failed to check Docker status: %w", err)
			}

			if strings.TrimSpace(dockerCheck.Stdout) != "installed" {
				ui.Info("Docker not found, installing...")
				if _, err := sshrunner.RunStreaming(ctx, sshCfg, installDockerScript, os.Stdout, os.Stderr); err != nil {
					return fmt.Errorf("failed to install Docker on remote server: %w", err)
				}
				ui.Success("Docker installed successfully")
			} else {
				ui.Info("Docker is already installed")
			}

			ui.Info("Installing haloyadm on remote server...")
			if _, err := sshrunner.RunStreaming(ctx, sshCfg, installHaloyadmScript, os.Stdout, os.Stderr); err != nil {
				return fmt.Errorf("failed to install haloyadm on remote server: %w", err)
			}

			initCmd := buildInitCommand(apiDomain, acmeEmail, override, noServices, noLogs)
			ui.Info("Running remote: %s", initCmd)

			if _, err := sshrunner.RunStreaming(ctx, sshCfg, initCmd, os.Stdout, os.Stderr); err != nil {
				return fmt.Errorf("remote haloyadm init failed: %w", err)
			}

			ui.Info("Reading API token from remote server...")
			tokenRes, err := sshrunner.Run(ctx, sshCfg, "/usr/local/bin/haloyadm api token --raw")
			if err != nil {
				serverURL := serverURLFromDomainOrHost(apiDomain, host)
				ui.Warn("Could not retrieve API token from remote server.")
				ui.Info("You can still add the server manually:")
				ui.Info("  On the server, run: haloyadm api token")
				ui.Info("  Then locally, run: haloy server add %s <token>", serverURL)
				return fmt.Errorf("failed to get API token: %w", err)
			}

			apiToken := strings.TrimSpace(tokenRes.Stdout)
			if apiToken == "" {
				serverURL := serverURLFromDomainOrHost(apiDomain, host)
				ui.Warn("API token is empty.")
				ui.Info("You can still add the server manually:")
				ui.Info("  On the server, run: haloyadm api token")
				ui.Info("  Then locally, run: haloy server add %s <token>", serverURL)
				return fmt.Errorf("API token is empty")
			}

			serverURL := serverURLFromDomainOrHost(apiDomain, host)
			ui.Info("Adding server '%s' to local haloy config...", serverURL)

			if err := addServerURL(serverURL, apiToken, true); err != nil {
				return fmt.Errorf("failed to add server locally: %w", err)
			}

			ui.Success("Remote server setup complete!")
			return nil
		},
	}

	cmd.Flags().StringVarP(&user, "user", "u", "root", "SSH username")
	cmd.Flags().IntVarP(&port, "port", "p", 22, "SSH port")
	cmd.Flags().StringVar(&apiDomain, "api-domain", "", "Domain for the haloyd API (e.g., api.yourserver.com)")
	cmd.Flags().StringVar(&acmeEmail, "acme-email", "", "Email address for Let's Encrypt certificate registration")
	cmd.Flags().BoolVar(&override, "override", false, "Override existing Haloy data/config on server")
	cmd.Flags().BoolVar(&noServices, "no-services", false, "Don't start HAProxy and haloyd containers on server")
	cmd.Flags().BoolVar(&noLogs, "no-logs", false, "Don't stream haloyd initialization logs on server")
	cmd.Flags().StringVar(&identity, "ssh-identity", "", "Path to SSH private key (optional; uses default ssh behavior if not set)")

	return cmd
}

func buildInitCommand(apiDomain, acmeEmail string, override, noServices, noLogs bool) string {
	args := []string{"/usr/local/bin/haloyadm", "init", "--remote-install"}

	if apiDomain != "" {
		args = append(args, "--api-domain", apiDomain)
	}
	if acmeEmail != "" {
		args = append(args, "--acme-email", acmeEmail)
	}
	if override {
		args = append(args, "--override")
	}
	if noServices {
		args = append(args, "--no-services")
	}
	if noLogs {
		args = append(args, "--no-logs")
	}

	return strings.Join(args, " ")
}

func serverURLFromDomainOrHost(apiDomain, host string) string {
	if apiDomain != "" {
		return apiDomain
	}
	return host
}
