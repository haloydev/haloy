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
