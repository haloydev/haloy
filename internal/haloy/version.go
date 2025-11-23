package haloy

import (
	"context"
	"fmt"

	"github.com/haloydev/haloy/internal/apiclient"
	"github.com/haloydev/haloy/internal/apitypes"
	"github.com/haloydev/haloy/internal/appconfigloader"
	"github.com/haloydev/haloy/internal/config"
	"github.com/haloydev/haloy/internal/constants"
	"github.com/haloydev/haloy/internal/ui"
	"github.com/spf13/cobra"
	"golang.org/x/sync/errgroup"
)

func VersionCmd(configPath *string, flags *appCmdFlags) *cobra.Command {
	var serverFlag string

	cmd := &cobra.Command{
		Use:   "version",
		Short: "Show the current version of haloyd and HAProxy",
		Long:  "Display the current version of haloyd and the HAProxy version it is using.",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			if serverFlag != "" {
				return getVersion(ctx, nil, serverFlag, "")
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
					return getVersion(ctx, &target, target.Server, prefix)
				})
			}

			return g.Wait()
		},
	}
	cmd.Flags().StringVarP(&flags.configPath, "config", "c", "", "Path to config file or directory (default: .)")
	cmd.Flags().StringSliceVarP(&flags.targets, "targets", "t", nil, "Get version for specific targets (comma-separated)")
	cmd.Flags().BoolVarP(&flags.all, "all", "a", false, "Get version for all targets")
	return cmd
}

func getVersion(ctx context.Context, targetConfig *config.TargetConfig, targetServer, prefix string) error {
	token, err := getToken(targetConfig, targetServer)
	if err != nil {
		return &PrefixedError{Err: fmt.Errorf("unable to get token: %w", err), Prefix: prefix}
	}
	ui.Info("Getting version using server %s", targetServer)

	cliVersion := constants.Version
	api, err := apiclient.New(targetServer, token)
	if err != nil {
		return &PrefixedError{Err: fmt.Errorf("unable to create API client: %w", err), Prefix: prefix}
	}
	var response apitypes.VersionResponse
	if err := api.Get(ctx, "version", &response); err != nil {
		return &PrefixedError{Err: fmt.Errorf("failed to get version from API: %w", err), Prefix: prefix}
	}

	if cliVersion == response.Version {
		ui.Success("haloy version %s running with HAProxy version %s", cliVersion, response.HAProxyVersion)
	} else {
		ui.Warn("haloy version %s does not match haloyd (server) version %s", cliVersion, response.Version)
		ui.Warn("HAProxy version: %s", response.HAProxyVersion)
	}
	return nil
}
