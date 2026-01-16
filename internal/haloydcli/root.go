package haloydcli

import (
	"github.com/haloydev/haloy/internal/config"
	"github.com/haloydev/haloy/internal/ui"
	"github.com/spf13/cobra"
)

func NewRootCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "haloyd",
		Short: "Haloy daemon - manages container deployments and proxy routing",
		Long: `haloyd is the server-side daemon for Haloy.

It manages:
  - Container deployments and health checking
  - TLS certificate provisioning via Let's Encrypt
  - Reverse proxy routing to containers
  - API for the haloy CLI`,
		PersistentPreRun: func(cmd *cobra.Command, args []string) {
			config.LoadEnvFiles()
		},
		SilenceErrors: true,
		SilenceUsage:  true,
	}

	cmd.AddCommand(
		serveCmd(),
		initCmd(),
		configCmd(),
		upgradeCmd(),
		versionCmd(),
	)

	return cmd
}

func Execute() int {
	rootCmd := NewRootCmd()
	if err := rootCmd.Execute(); err != nil {
		ui.Error("%v", err)
		return 1
	}
	return 0
}
