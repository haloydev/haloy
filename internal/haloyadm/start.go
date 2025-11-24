package haloyadm

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/haloydev/haloy/internal/apiclient"
	"github.com/haloydev/haloy/internal/config"
	"github.com/haloydev/haloy/internal/constants"
	"github.com/haloydev/haloy/internal/ui"
	"github.com/spf13/cobra"
)

func StartCmd() *cobra.Command {
	var devMode bool
	var debug bool
	var noLogs bool

	cmd := &cobra.Command{
		Use:   "start",
		Short: "Start the haloy services",
		Long:  "Start the haloy services, including HAProxy and haloyd.",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()

			dataDir, err := config.DataDir()
			if err != nil {
				return fmt.Errorf("failed to determine data directory: %w", err)
			}

			configDir, err := config.ConfigDir()
			if err != nil {
				return fmt.Errorf("failed to determine config directory: %w", err)
			}

			if err := ensureNetwork(ctx); err != nil {
				ui.Info("You can manually create it with:")
				ui.Info("docker network create --driver bridge --attachable %s", constants.DockerNetwork)
				return fmt.Errorf("failed to ensure Docker network exists: %w", err)
			}

			if err := startServices(ctx, dataDir, configDir, devMode, false, debug); err != nil {
				return err
			}

			waitCtx, waitCancel := context.WithTimeout(ctx, 30*time.Second)
			defer waitCancel()

			ui.Info("Waiting for HAProxy to become available...")
			if err := waitForHAProxy(waitCtx); err != nil {
				return fmt.Errorf("HAProxy failed to become ready: %w", err)
			}

			if !noLogs {
				apiToken := os.Getenv(constants.EnvVarAPIToken)
				if apiToken == "" {
					return fmt.Errorf("failed to get API token")
				}

				apiURL := fmt.Sprintf("http://localhost:%s", constants.APIServerPort)
				api, err := apiclient.New(apiURL, apiToken)
				if err != nil {
					return fmt.Errorf("failed to create API client: %w", err)
				}
				ui.Info("Waiting for haloyd API to become available...")
				if err := waitForAPI(waitCtx, api); err != nil {
					return fmt.Errorf("haloyd API not available: %w", err)
				}

				ui.Info("Streaming haloyd initialization logs...")
				if err := streamHaloydInitLogs(ctx, api); err != nil {
					ui.Warn("Failed to stream haloyd initialization logs: %v", err)
					ui.Info("haloyd is starting in the background. Check logs with: docker logs haloyd")
				}
			}

			return nil
		},
	}
	cmd.Flags().BoolVar(&devMode, "dev", false, "Start in development mode using the local haloyd image")
	cmd.Flags().BoolVar(&debug, "debug", false, "Enable debug mode")
	cmd.Flags().BoolVar(&noLogs, "no-logs", false, "Don't stream haloyd initialization logs")

	return cmd
}
