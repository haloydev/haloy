package haloy

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/haloydev/haloy/internal/apiclient"
	"github.com/haloydev/haloy/internal/config"
	"github.com/haloydev/haloy/internal/configloader"
	"github.com/haloydev/haloy/internal/logging"
	"github.com/haloydev/haloy/internal/ui"
	"github.com/spf13/cobra"
	"golang.org/x/sync/errgroup"
)

func ServerLogsCmd(configPath *string, flags *appCmdFlags) *cobra.Command {
	var serverFlag string
	var accessLogs bool

	cmd := &cobra.Command{
		Use:   "logs",
		Short: "Stream platform logs from haloy server",
		Long: `Stream platform logs from haloy server in real-time.

By default, proxy access logs are filtered out. Use --access-logs to include them.

The logs are streamed in real-time and will continue until interrupted (Ctrl+C).`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			if serverFlag != "" {
				return streamServerLogs(ctx, nil, serverFlag, accessLogs)
			}

			if len(flags.targets) == 0 && !flags.all {
				flags.all = true
			}

			rawDeployConfig, format, err := configloader.Load(ctx, *configPath, flags.targets, flags.all)
			if err != nil {
				return fmt.Errorf("unable to load config: %w", err)
			}

			resolvedDeployConfig, err := configloader.ResolveSecrets(ctx, rawDeployConfig)
			if err != nil {
				return fmt.Errorf("failed to resolve secrets: %w", err)
			}

			targets, err := configloader.ExtractTargets(resolvedDeployConfig, format)
			if err != nil {
				return err
			}

			servers := configloader.TargetsByServer(targets)

			g, ctx := errgroup.WithContext(ctx)
			for server, targetNames := range servers {
				targetConfig, exists := targets[targetNames[0]]
				if !exists {
					return fmt.Errorf("failed to find target config for server")
				}
				g.Go(func() error {
					return streamServerLogs(ctx, &targetConfig, server, accessLogs)
				})
			}

			return g.Wait()
		},
	}

	cmd.Flags().StringVarP(&flags.configPath, "config", "c", "", "Path to config file or directory (default: .)")
	cmd.Flags().StringVarP(&serverFlag, "server", "s", "", "Haloy server URL")
	cmd.Flags().StringSliceVarP(&flags.targets, "targets", "t", nil, "Show logs for specific targets (comma-separated)")
	cmd.Flags().BoolVarP(&flags.all, "all", "a", false, "Show all target logs")
	cmd.Flags().BoolVar(&accessLogs, "access-logs", false, "Include proxy access logs in output")

	cmd.RegisterFlagCompletionFunc("targets", completeTargetNames)

	return cmd
}

func streamServerLogs(ctx context.Context, targetConfig *config.TargetConfig, targetServer string, accessLogs bool) error {
	token, err := getToken(targetConfig, targetServer)
	if err != nil {
		return fmt.Errorf("unable to get token: %w", err)
	}

	ui.Info("Connecting to haloy server at %s", targetServer)
	if accessLogs {
		ui.Info("Streaming all logs including access logs... (Press Ctrl+C to stop)")
	} else {
		ui.Info("Streaming platform logs... (Press Ctrl+C to stop)")
	}

	api, err := apiclient.New(targetServer, token)
	if err != nil {
		return fmt.Errorf("failed to create API client: %w", err)
	}
	streamHandler := func(data string) bool {
		var logEntry logging.LogEntry
		if err := json.Unmarshal([]byte(data), &logEntry); err != nil {
			ui.Error("failed to parse log entry: %v", err)
		}

		prefix := ""
		if logEntry.DeploymentID != "" {
			prefix = fmt.Sprintf("[id: %s] -> ", logEntry.DeploymentID[:8])
		}

		ui.DisplayLogEntry(logEntry, prefix)

		return false
	}
	path := "server-logs"
	if accessLogs {
		path += "?access-logs=true"
	}
	return api.Stream(ctx, path, streamHandler)
}
