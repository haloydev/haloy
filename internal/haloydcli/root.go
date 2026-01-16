package haloydcli

import (
	"os"

	"github.com/haloydev/haloy/internal/config"
	"github.com/haloydev/haloy/internal/constants"
	"github.com/haloydev/haloy/internal/haloyd"
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
		newServeCmd(),
		newInitCmd(),
		newConfigCmd(),
		newUpgradeCmd(),
		newVersionCmd(),
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

func newServeCmd() *cobra.Command {
	var debug bool

	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Start the haloyd daemon",
		Long: `Start the haloyd daemon which runs the proxy and API server.

This command is typically run by systemd. It will:
  - Listen on port 80 and 443 for HTTP/HTTPS traffic
  - Route traffic to containers based on domain
  - Handle TLS termination and certificate management
  - Provide the API for the haloy CLI`,
		RunE: func(cmd *cobra.Command, args []string) error {
			debugEnv := os.Getenv(constants.EnvVarDebug) == "true"
			haloyd.Run(debug || debugEnv)
			return nil
		},
	}

	cmd.Flags().BoolVar(&debug, "debug", false, "Run in debug mode")

	return cmd
}

func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the version number",
		Run: func(cmd *cobra.Command, args []string) {
			ui.Info("haloyd version %s", constants.Version)
		},
	}
}
