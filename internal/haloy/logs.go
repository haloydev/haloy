package haloy

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"

	"github.com/haloydev/haloy/internal/apiclient"
	"github.com/haloydev/haloy/internal/config"
	"github.com/haloydev/haloy/internal/configloader"
	"github.com/haloydev/haloy/internal/docker"
	"github.com/haloydev/haloy/internal/ui"
	"github.com/spf13/cobra"
	"golang.org/x/sync/errgroup"
)

func LogsCmd(configPath *string, flags *appCmdFlags) *cobra.Command {
	var (
		allContainers bool
		containerID   string
		tail          int
	)

	cmd := &cobra.Command{
		Use:   "logs",
		Short: "Stream application container logs",
		Long: `Stream stdout/stderr logs from application containers in real-time.

By default, streams logs from the first container. Use flags to target
specific containers or all containers.

The logs are streamed in real-time and will continue until interrupted (Ctrl+C).

Examples:
  # Stream logs from the default container
  haloy logs

  # Stream last 50 lines, then follow
  haloy logs --tail 50

  # Stream from all containers
  haloy logs --all-containers

  # Stream from a specific container
  haloy logs --container abc123

  # Stream from specific targets (multi-target config)
  haloy logs --targets prod`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()

			if allContainers && containerID != "" {
				return fmt.Errorf("cannot specify both --all-containers and --container")
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

			g, ctx := errgroup.WithContext(ctx)
			for _, target := range targets {
				g.Go(func() error {
					prefix := ""
					if len(targets) > 1 {
						prefix = target.TargetName
					}
					return streamAppLogs(ctx, &target, target.Server, target.Name, tail, containerID, allContainers, prefix)
				})
			}

			return g.Wait()
		},
	}

	cmd.Flags().StringVarP(&flags.configPath, "config", "c", "", "Path to config file or directory (default: .)")
	cmd.Flags().StringSliceVarP(&flags.targets, "targets", "t", nil, "Stream logs for specific targets (comma-separated)")
	cmd.Flags().BoolVarP(&flags.all, "all", "a", false, "Stream logs for all targets")
	cmd.Flags().IntVar(&tail, "tail", 100, "Number of historical log lines to show")
	cmd.Flags().StringVar(&containerID, "container", "", "Stream logs from a specific container ID")
	cmd.Flags().BoolVar(&allContainers, "all-containers", false, "Stream logs from all containers")

	return cmd
}

func streamAppLogs(ctx context.Context, targetConfig *config.TargetConfig, targetServer, appName string, tail int, containerID string, allContainers bool, prefix string) error {
	pui := &ui.PrefixedUI{Prefix: prefix}

	token, err := getToken(targetConfig, targetServer)
	if err != nil {
		return &PrefixedError{Err: fmt.Errorf("unable to get token: %w", err), Prefix: prefix}
	}

	api, err := apiclient.New(targetServer, token)
	if err != nil {
		return &PrefixedError{Err: fmt.Errorf("failed to create API client: %w", err), Prefix: prefix}
	}

	pui.Info("Streaming container logs... (Press Ctrl+C to stop)")

	params := url.Values{}
	params.Set("tail", strconv.Itoa(tail))
	if containerID != "" {
		params.Set("containerId", containerID)
	}
	if allContainers {
		params.Set("allContainers", "true")
	}

	path := fmt.Sprintf("logs/%s?%s", appName, params.Encode())

	showContainerID := allContainers

	streamHandler := func(data string) bool {
		var logLine docker.LogLine
		if err := json.Unmarshal([]byte(data), &logLine); err != nil {
			pui.Error("failed to parse log line: %v", err)
			return false
		}

		line := logLine.Line
		if showContainerID && logLine.ContainerID != "" {
			shortID := logLine.ContainerID
			if len(shortID) > 12 {
				shortID = shortID[:12]
			}
			line = fmt.Sprintf("[%s] %s", shortID, logLine.Line)
		}

		if prefix != "" {
			pui.Info("%s", line)
		} else {
			fmt.Println(line)
		}

		return false
	}

	err = api.Stream(ctx, path, streamHandler)
	if err != nil && ctx.Err() != nil {
		return nil
	}
	if err != nil {
		return &PrefixedError{Err: fmt.Errorf("log stream error: %w", err), Prefix: prefix}
	}
	return nil
}
