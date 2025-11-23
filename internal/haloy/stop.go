package haloy

import (
	"context"
	"fmt"

	"github.com/haloydev/haloy/internal/apiclient"
	"github.com/haloydev/haloy/internal/apitypes"
	"github.com/haloydev/haloy/internal/appconfigloader"
	"github.com/haloydev/haloy/internal/config"
	"github.com/haloydev/haloy/internal/ui"
	"github.com/spf13/cobra"
	"golang.org/x/sync/errgroup"
)

func StopAppCmd(configPath *string, flags *appCmdFlags) *cobra.Command {
	var serverFlag string
	var removeContainersFlag bool

	cmd := &cobra.Command{
		Use:   "stop",
		Short: "Stop an application's running containers",
		Long:  "Stop all running containers for an application using a haloy configuration file.",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			if serverFlag != "" {
				return stopApp(ctx, nil, serverFlag, "", removeContainersFlag, "")
			}

			rawAppConfig, format, err := appconfigloader.Load(ctx, *configPath, flags.targets, flags.all)
			if err != nil {
				return fmt.Errorf("unable to load config: %w", err)
			}

			targets, err := appconfigloader.ExtractTargets(rawAppConfig, format)
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
					return stopApp(ctx, &target, target.Server, target.Name, removeContainersFlag, prefix)
				})
			}

			return g.Wait()
		},
	}

	cmd.Flags().StringVarP(&flags.configPath, "config", "c", "", "Path to config file or directory (default: .)")
	cmd.Flags().StringVarP(&serverFlag, "server", "s", "", "Haloy server URL (overrides config)")
	cmd.Flags().StringSliceVarP(&flags.targets, "targets", "t", nil, "Stop app on specific targets (comma-separated)")
	cmd.Flags().BoolVarP(&flags.all, "all", "a", false, "Stop app on all targets")
	cmd.Flags().BoolVarP(&removeContainersFlag, "remove-containers", "r", false, "Remove containers after stopping them")

	return cmd
}

func stopApp(ctx context.Context, targetConfig *config.TargetConfig, targetServer, appName string, removeContainers bool, prefix string) error {
	ui.Info("Stopping application: %s using server %s", appName, targetServer)

	token, err := getToken(targetConfig, targetServer)
	if err != nil {
		return &PrefixedError{Err: fmt.Errorf("unable to get token: %w", err), Prefix: prefix}
	}

	api, err := apiclient.New(targetServer, token)
	if err != nil {
		return &PrefixedError{Err: fmt.Errorf("unable to create API client: %w", err), Prefix: prefix}
	}
	path := fmt.Sprintf("stop/%s", appName)

	// Add query parameter if removeContainers is true
	if removeContainers {
		path += "?remove-containers=true"
	}

	var response apitypes.StopAppResponse
	if err := api.Post(ctx, path, nil, &response); err != nil {
		return &PrefixedError{Err: fmt.Errorf("failed to stop app: %w", err), Prefix: prefix}
	}

	ui.Success("%s", response.Message)
	return nil
}
