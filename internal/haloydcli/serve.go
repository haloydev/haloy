package haloydcli

import (
	"os"

	"github.com/haloydev/haloy/internal/constants"
	"github.com/haloydev/haloy/internal/haloyd"
	"github.com/spf13/cobra"
)

func serveCmd() *cobra.Command {
	var debug bool

	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Start the haloyd daemon",
		Long: `Start the haloyd daemon, the haloy control plane.

This command is typically run by systemd. It will:
  - Manage container deployments and health checks
  - Handle certificate provisioning via Let's Encrypt
  - Push routing configuration to the haloy-proxy daemon
  - Provide the API for the haloy CLI

Traffic on ports 80/443 is served by the separate haloy-proxy daemon,
which keeps serving while haloyd restarts or upgrades.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			debugEnv := os.Getenv(constants.EnvVarDebug) == "true"
			haloyd.Run(debug || debugEnv)
			return nil
		},
	}

	cmd.Flags().BoolVar(&debug, "debug", false, "Run in debug mode")

	return cmd
}
