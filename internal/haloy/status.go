package haloy

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/haloydev/haloy/internal/apiclient"
	"github.com/haloydev/haloy/internal/apitypes"
	"github.com/haloydev/haloy/internal/appconfigloader"
	"github.com/haloydev/haloy/internal/config"
	"github.com/haloydev/haloy/internal/helpers"
	"github.com/haloydev/haloy/internal/ui"
	"github.com/spf13/cobra"
	"golang.org/x/sync/errgroup"
)

func StatusAppCmd(configPath *string, flags *appCmdFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show status for an application",
		Long:  "Show current status of a deployed application using a haloy configuration file.",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()

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
					return getAppStatus(ctx, &target, target.Server, target.Name, prefix)
				})
			}

			return g.Wait()
		},
	}

	cmd.Flags().StringVarP(&flags.configPath, "config", "c", "", "Path to config file or directory (default: .)")
	cmd.Flags().StringSliceVarP(&flags.targets, "targets", "t", nil, "Show status for specific targets (comma-separated)")
	cmd.Flags().BoolVarP(&flags.all, "all", "a", false, "Show status for all targets")

	return cmd
}

func getAppStatus(ctx context.Context, targetConfig *config.TargetConfig, targetServer, appName, prefix string) error {
	ui.Info("Getting status for application: %s using server %s", appName, targetServer)

	token, err := getToken(targetConfig, targetServer)
	if err != nil {
		return &PrefixedError{Err: fmt.Errorf("unable to get token: %w", err), Prefix: prefix}
	}

	api, err := apiclient.New(targetServer, token)
	if err != nil {
		return &PrefixedError{Err: fmt.Errorf("unable to create API client: %w", err), Prefix: prefix}
	}
	path := fmt.Sprintf("status/%s", appName)
	var response apitypes.AppStatusResponse
	if err := api.Get(ctx, path, &response); err != nil {

		// Handle 404 specifically - app not deployed/running
		if errors.Is(err, apiclient.ErrNotFound) {
			return &PrefixedError{
				Err:    fmt.Errorf("application '%s' is not currently deployed or running", appName),
				Prefix: prefix,
			}
		}

		return &PrefixedError{Err: fmt.Errorf("failed to get status: %w", err), Prefix: prefix}
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

	return nil
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
