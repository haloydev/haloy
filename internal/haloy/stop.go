package haloy

import (
	"context"
	"fmt"
	"sync"

	"github.com/haloydev/haloy/internal/apiclient"
	"github.com/haloydev/haloy/internal/apitypes"
	"github.com/haloydev/haloy/internal/appconfigloader"
	"github.com/haloydev/haloy/internal/config"
	"github.com/haloydev/haloy/internal/ui"
	"github.com/spf13/cobra"
)

func StopAppCmd(configPath *string, flags *appCmdFlags) *cobra.Command {
	var serverFlag string
	var removeContainersFlag bool

	cmd := &cobra.Command{
		Use:   "stop",
		Short: "Stop an application's running containers",
		Long:  "Stop all running containers for an application using a haloy configuration file.",
		Args:  cobra.NoArgs,
		Run: func(cmd *cobra.Command, args []string) {
			ctx := cmd.Context()
			if serverFlag != "" {
				stopApp(ctx, nil, serverFlag, "", removeContainersFlag)
			} else {
				rawAppConfig, format, err := appconfigloader.Load(ctx, *configPath, flags.targets, flags.all)
				if err != nil {
					ui.Error("%v", err)
					return
				}

				targets, err := appconfigloader.ExtractTargets(rawAppConfig, format)
				if err != nil {
					ui.Error("Unable to create deploy targets: %v", err)
					return
				}

				var wg sync.WaitGroup
				for _, target := range targets {
					wg.Add(1)
					go func(target config.TargetConfig) {
						defer wg.Done()
						stopApp(ctx, &target, target.Server, target.Name, removeContainersFlag)
					}(target)
				}

				wg.Wait()
			}
		},
	}

	cmd.Flags().StringVarP(&flags.configPath, "config", "c", "", "Path to config file or directory (default: .)")
	cmd.Flags().StringVarP(&serverFlag, "server", "s", "", "Haloy server URL (overrides config)")
	cmd.Flags().StringSliceVarP(&flags.targets, "targets", "t", nil, "Stop app on specific targets (comma-separated)")
	cmd.Flags().BoolVarP(&flags.all, "all", "a", false, "Stop app on all targets")
	cmd.Flags().BoolVarP(&removeContainersFlag, "remove-containers", "r", false, "Remove containers after stopping them")

	return cmd
}

func stopApp(ctx context.Context, targetConfig *config.TargetConfig, targetServer, appName string, removeContainers bool) {
	token, err := getToken(targetConfig, targetServer)
	if err != nil {
		ui.Error("%v", err)
		return
	}

	ui.Info("Stopping application: %s using server %s", appName, targetServer)

	api, err := apiclient.New(targetServer, token)
	if err != nil {
		ui.Error("Failed to create API client: %v", err)
		return
	}
	path := fmt.Sprintf("stop/%s", appName)

	// Add query parameter if removeContainers is true
	if removeContainers {
		path += "?remove-containers=true"
	}

	var response apitypes.StopAppResponse
	if err := api.Post(ctx, path, nil, &response); err != nil {
		ui.Error("Failed to stop app: %v", err)
		return
	}

	ui.Success("%s", response.Message)
}
