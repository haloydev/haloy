package haloy

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/haloydev/haloy/internal/apiclient"
	"github.com/haloydev/haloy/internal/apitypes"
	"github.com/haloydev/haloy/internal/config"
	"github.com/haloydev/haloy/internal/configloader"
	"github.com/haloydev/haloy/internal/deploytypes"
	"github.com/haloydev/haloy/internal/helpers"
	"github.com/haloydev/haloy/internal/logging"
	"github.com/haloydev/haloy/internal/ui"
	"github.com/spf13/cobra"
	"golang.org/x/sync/errgroup"
)

func RollbackAppCmd(configPath *string, flags *appCmdFlags) *cobra.Command {
	var noLogsFlag bool

	cmd := &cobra.Command{
		Use:   "rollback <deployment-id>",
		Short: "Rollback an application to a specific deployment",
		Long: `Rollback an application to a specific deployment by supplying a deployment ID.

Use 'haloy rollback-targets' to list available deployment IDs.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()

			targetDeploymentID := args[0]

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

			newDeploymentID := createDeploymentID()

			servers := configloader.TargetsByServer(targets)

			g, ctx := errgroup.WithContext(ctx)
			for _, targetNames := range servers {
				g.Go(func() error {
					for _, targetName := range targetNames {

						targetConfig, exists := targets[targetName]
						if !exists {
							return fmt.Errorf("could not find target for %s", targetName)
						}

						server := targetConfig.Server
						prefix := ""
						if len(targets) > 1 {
							prefix = targetName
						}

						token, err := getToken(&targetConfig, server)
						if err != nil {
							return &PrefixedError{Err: fmt.Errorf("unable to get token: %w", err), Prefix: prefix}
						}

						api, err := apiclient.New(server, token)
						if err != nil {
							return &PrefixedError{Err: fmt.Errorf("unable to create API client: %w", err), Prefix: prefix}
						}

						rollbackTargetsResponse, err := getRollbackTargets(ctx, api, targetConfig.Name)
						if err != nil {
							return &PrefixedError{Err: fmt.Errorf("failed to get available rollback targets: %w", err), Prefix: prefix}
						}
						var availableTarget deploytypes.RollbackTarget
						for _, at := range rollbackTargetsResponse.Targets {
							if at.DeploymentID == targetDeploymentID {
								availableTarget = at
							}
						}
						if availableTarget.DeploymentID == "" {
							return &PrefixedError{Err: fmt.Errorf("deployment ID %s not found in available rollback targets", targetDeploymentID), Prefix: prefix}
						}

						if availableTarget.RawDeployConfig == nil {
							return &PrefixedError{Err: errors.New("unable to find configuration for rollback"), Prefix: prefix}
						}
						newResolvedDeployConfig, err := configloader.ResolveSecrets(ctx, *availableTarget.RawDeployConfig)
						if err != nil {
							return &PrefixedError{Err: fmt.Errorf("unable to resolve secrets for the deploy config. This usually occurs when secrets names have been changed or deleted between deployments: %w", err), Prefix: prefix}
						}
						newResolvedTargetConfig, err := configloader.MergeToTarget(newResolvedDeployConfig, config.TargetConfig{}, newResolvedDeployConfig.Name, format)
						if err != nil {
							return &PrefixedError{Err: fmt.Errorf("failed to merge to target: %w", err), Prefix: prefix}
						}
						request := apitypes.RollbackRequest{
							TargetDeploymentID: targetDeploymentID,
							NewDeploymentID:    newDeploymentID,
							NewTargetConfig:    newResolvedTargetConfig,
						}

						ui.Info("Starting rollback for application: %s using server %s", targetConfig.Name, server)

						if err := api.Post(ctx, "rollback", request, nil); err != nil {
							return &PrefixedError{Err: fmt.Errorf("rollback failed: %w", err), Prefix: prefix}
						}

						if !noLogsFlag {
							streamPath := fmt.Sprintf("deploy/%s/logs", newDeploymentID)

							streamHandler := func(data string) bool {
								var logEntry logging.LogEntry
								if err := json.Unmarshal([]byte(data), &logEntry); err != nil {
									ui.Warn("failed to unmarshal json: %v", err)
									return false // we don't stop on errors.
								}

								ui.DisplayLogEntry(logEntry, "")

								// If deployment is complete we'll return true to signal stream should stop
								return logEntry.IsDeploymentComplete
							}

							api.Stream(ctx, streamPath, streamHandler)
						}

					}

					return nil
				})
			}

			return g.Wait()
		},
	}

	cmd.Flags().StringVarP(&flags.configPath, "config", "c", "", "Path to config file or directory (default: .)")
	cmd.Flags().StringSliceVarP(&flags.targets, "targets", "t", nil, "Deploy to specific targets (comma-separated)")
	cmd.Flags().BoolVarP(&flags.all, "all", "a", false, "Deploy to all targets")
	cmd.Flags().BoolVar(&noLogsFlag, "no-logs", false, "Don't stream deployment logs")

	return cmd
}

func RollbackTargetsCmd(configPath *string, flags *appCmdFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "rollback-targets",
		Short: "List available rollback targets for an application",
		Long:  `List available rollback targets for an application using a haloy configuration file.`,
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()

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

					token, err := getToken(&target, target.Server)
					if err != nil {
						return &PrefixedError{Err: fmt.Errorf("unable to get token: %w", err), Prefix: prefix}
					}

					api, err := apiclient.New(target.Server, token)
					if err != nil {
						return &PrefixedError{Err: fmt.Errorf("unable to create API client: %w", err), Prefix: prefix}
					}
					rollbackTargets, err := getRollbackTargets(ctx, api, target.Name)
					if err != nil {
						return &PrefixedError{Err: fmt.Errorf("failed to get rollback targets: %w", err), Prefix: prefix}
					}
					if len(rollbackTargets.Targets) == 0 {
						pui := &ui.PrefixedUI{Prefix: prefix}
						pui.Info("No rollback targets available for app '%s'", target.Name)
						return nil
					}

					displayRollbackTargets(target.Name, rollbackTargets.Targets, *configPath, target.TargetName)
					return nil
				})
			}

			return g.Wait()
		},
	}

	cmd.Flags().StringVarP(&flags.configPath, "config", "c", "", "Path to config file or directory (default: .)")
	cmd.Flags().StringSliceVarP(&flags.targets, "targets", "t", nil, "Deploy to specific targets (comma-separated)")
	cmd.Flags().BoolVarP(&flags.all, "all", "a", false, "Deploy to all targets")

	return cmd
}

func getRollbackTargets(ctx context.Context, api *apiclient.APIClient, appName string) (*apitypes.RollbackTargetsResponse, error) {
	path := fmt.Sprintf("rollback/%s", appName)
	var response apitypes.RollbackTargetsResponse
	if err := api.Get(ctx, path, &response); err != nil {
		return nil, err
	}
	return &response, nil
}

func displayRollbackTargets(appName string, rollbackTargets []deploytypes.RollbackTarget, configPath, targetName string) {
	if len(rollbackTargets) == 0 {
		ui.Info("No rollback targets available for app '%s'", appName)
		return
	}

	header := fmt.Sprintf("Available rollback targets for '%s':", appName)
	if targetName != "" {
		header = fmt.Sprintf("%s on %s", header, targetName)
	}
	ui.Info("%s", header)

	headers := []string{"DEPLOYMENT ID", "IMAGE REFERENCE", "DATE", "STATUS"}
	rows := make([][]string, 0, len(rollbackTargets))

	for _, rollbackTarget := range rollbackTargets {

		date := "N/A"
		if deploymentTime, err := helpers.GetTimestampFromDeploymentID(rollbackTarget.DeploymentID); err == nil {
			date = helpers.FormatTime(deploymentTime)
		}

		status := ""
		if rollbackTarget.IsRunning {
			status = "🟢 CURRENT"
		}

		rows = append(rows, []string{
			rollbackTarget.DeploymentID,
			rollbackTarget.ImageRef,
			date,
			status,
		})
	}

	ui.Table(headers, rows)
	ui.Basic("To rollback, run:")
	ui.Basic("  haloy rollback <deployment-id>")
	if configPath != "." {
		ui.Basic("  # Or with explicit config:")
		ui.Basic("  haloy rollback --config %s <deployment-id>", configPath)
	}
}
