package haloy

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/haloydev/haloy/internal/apiclient"
	"github.com/haloydev/haloy/internal/apitypes"
	"github.com/haloydev/haloy/internal/appconfigloader"
	"github.com/haloydev/haloy/internal/config"
	"github.com/haloydev/haloy/internal/helpers"
	"github.com/haloydev/haloy/internal/ui"
	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"
)

func StatusAppCmd(configPath *string, flags *appCmdFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show status for an application",
		Long:  "Show current status of a deployed application using a haloy configuration file.",
		Args:  cobra.NoArgs,
		Run: func(cmd *cobra.Command, _ []string) {
			ctx := cmd.Context()
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
					getAppStatus(ctx, &target, target.Server, target.Name)
				}(target)
			}

			wg.Wait()
		},
	}

	cmd.Flags().StringVarP(&flags.configPath, "config", "c", "", "Path to config file or directory (default: .)")
	cmd.Flags().StringSliceVarP(&flags.targets, "targets", "t", nil, "Show status for specific targets (comma-separated)")
	cmd.Flags().BoolVarP(&flags.all, "all", "a", false, "Show status for all targets")
	return cmd
}

func getAppStatus(ctx context.Context, targetConfig *config.TargetConfig, targetServer, appName string) {
	token, err := getToken(targetConfig, targetServer)
	if err != nil {
		ui.Error("%v", err)
		return
	}

	ui.Info("Getting status for application: %s using server %s", appName, targetServer)

	api, err := apiclient.New(targetServer, token)
	if err != nil {
		ui.Error("Failed to create API client: %v", err)
		return
	}
	path := fmt.Sprintf("status/%s", appName)
	var response apitypes.AppStatusResponse
	if err := api.Get(ctx, path, &response); err != nil {
		ui.Error("Failed to get app status: %v", err)
		return

	}

	containerIDs := make([]string, 0, len(response.ContainerIDs))
	for _, id := range response.ContainerIDs {
		containerIDs = append(containerIDs, helpers.SafeIDPrefix(id))
	}

	canonicalDomains := make([]string, 0, len(response.Domains))
	for _, domain := range response.Domains {
		canonicalDomains = append(canonicalDomains, domain.Canonical)
	}

	state := displayState(response.State)
	formattedOutput := []string{
		fmt.Sprintf("State: %s", state),
		fmt.Sprintf("Deployment ID: %s", response.DeploymentID),
		fmt.Sprintf("Running container(s): %s", strings.Join(containerIDs, ", ")),
		fmt.Sprintf("Domain(s): %s", strings.Join(canonicalDomains, ", ")),
	}

	ui.Section(fmt.Sprintf("Status for %s", appName), formattedOutput)
}

func displayState(state string) string {
	switch strings.ToLower(state) {
	case "running":
		return lipgloss.NewStyle().Foreground(ui.Green).Render("Running")
	case "restarting":
		return lipgloss.NewStyle().Foreground(ui.Amber).Render("Restarting")
	case "paused":
		return lipgloss.NewStyle().Foreground(ui.Blue).Render("Paused")
	case "exited":
		return lipgloss.NewStyle().Foreground(ui.Red).Render("Exited")
	case "stopped":
		return lipgloss.NewStyle().Foreground(ui.Red).Render("Stopped")
	default:
		return lipgloss.NewStyle().Foreground(ui.LightGray).Italic(true).Render(state)
	}
}
