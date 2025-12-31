package haloy

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/haloydev/haloy/internal/apiclient"
	"github.com/haloydev/haloy/internal/apitypes"
	"github.com/haloydev/haloy/internal/cmdexec"
	"github.com/haloydev/haloy/internal/config"
	"github.com/haloydev/haloy/internal/configloader"
	"github.com/haloydev/haloy/internal/docker"
	"github.com/haloydev/haloy/internal/logging"
	"github.com/haloydev/haloy/internal/ui"
	"github.com/spf13/cobra"
	"golang.org/x/sync/errgroup"
)

func DeployAppCmd(configPath *string, flags *appCmdFlags) *cobra.Command {
	var noLogsFlag bool

	cmd := &cobra.Command{
		Use:   "deploy",
		Short: "Deploy an application",
		Long:  "Deploy an application using a haloy configuration file.",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()

			rawDeployConfig, format, err := configloader.Load(ctx, *configPath, flags.targets, flags.all)
			if err != nil {
				return fmt.Errorf("unable to load config: %w", err)
			}

			resolvedDeployConfig, err := configloader.ResolveSecrets(ctx, rawDeployConfig)
			if err != nil {
				return fmt.Errorf("failed to resolve secrets: %w", err)
			}

			rawTargets, err := configloader.ExtractTargets(rawDeployConfig, format)
			if err != nil {
				return err
			}

			resolvedTargets, err := configloader.ExtractTargets(resolvedDeployConfig, format)
			if err != nil {
				return err
			}

			if len(rawTargets) != len(resolvedTargets) {
				return fmt.Errorf("mismatch between raw targets (%d) and resolved targets (%d). This indicates a configuration processing error.", len(rawTargets), len(resolvedTargets))
			}

			// Filter out protected targets when using --all without --include-protected
			if flags.all && !flags.includeProtected {
				var skippedTargets []string
				for targetName, target := range rawTargets {
					if target.Protected != nil && *target.Protected {
						skippedTargets = append(skippedTargets, targetName)
						delete(rawTargets, targetName)
						delete(resolvedTargets, targetName)
					}
				}
				if len(skippedTargets) > 0 {
					ui.Warn("Skipping protected targets: %s", strings.Join(skippedTargets, ", "))
					ui.Warn("Use --include-protected to deploy these, or --targets to deploy explicitly")
				}
				if len(rawTargets) == 0 {
					return fmt.Errorf("no targets to deploy (all targets are protected)")
				}
			}

			builds, pushes, uploads := ResolveImageBuilds(resolvedTargets)
			for imageRef, image := range builds {
				if err := BuildImage(ctx, imageRef, image, *configPath); err != nil {
					return err
				}
			}
			for imageRef, targetConfigs := range uploads {
				if err := UploadImage(ctx, imageRef, targetConfigs); err != nil {
					return err
				}
			}

			if len(pushes) > 0 {
				cli, err := docker.NewClient(ctx)
				if err != nil {
					return fmt.Errorf("unable to create docker client for push image: %w", err)
				}
				for imageRef, images := range pushes {
					for _, image := range images {
						registryServer := docker.GetRegistryServer(image)
						ui.Info("Pushing image '%s' to %s", imageRef, registryServer)
						if err := docker.PushImage(ctx, cli, imageRef, image); err != nil {
							return err
						}
					}
				}
			}

			if len(rawDeployConfig.GlobalPreDeploy) > 0 {
				for _, hookCmd := range rawDeployConfig.GlobalPreDeploy {
					if err := cmdexec.RunCommand(ctx, hookCmd, getHooksWorkDir(*configPath)); err != nil {
						return fmt.Errorf("%s hook failed: %v", config.GetFieldNameForFormat(config.DeployConfig{}, "GlobalPreDeploy", rawDeployConfig.Format), err)
					}
				}
			}

			// Group targets by server so that deployments to the same server are serialized.
			// This will prevent too many containers starting at the same time, avoids race conditions and conflicts.
			// Targets that are on different server are run in paralell to speed things up.
			servers := configloader.TargetsByServer(rawTargets)

			// Create deployment IDs per app name
			deploymentIDs := make(map[string]string)
			for _, target := range resolvedTargets {
				if _, exists := deploymentIDs[target.Name]; !exists {
					deploymentIDs[target.Name] = createDeploymentID()
				}
			}

			g, ctx := errgroup.WithContext(ctx)
			for _, targetNames := range servers {
				g.Go(func() error {
					for _, targetName := range targetNames {

						rawTargetConfig, rawTargetExists := rawTargets[targetName]
						if !rawTargetExists {
							return fmt.Errorf("could not find raw target for %s", targetName)
						}
						resolvedTargetConfig, resolvedTargetExists := resolvedTargets[targetName]
						if !resolvedTargetExists {
							return fmt.Errorf("could not find resolved target for %s", targetName)
						}

						deploymentID, deploymentIDExists := deploymentIDs[resolvedTargetConfig.Name]
						if !deploymentIDExists {
							return fmt.Errorf("could not find deployment ID for app '%s'", resolvedTargetConfig.Name)
						}

						// Recreate the DeployConfig with just the target for rollbacks
						rollbackDeployConfig := config.DeployConfig{
							TargetConfig:    rawTargetConfig,
							SecretProviders: rawDeployConfig.SecretProviders,
						}

						prefix := ""
						if len(rawTargets) > 1 {
							prefix = targetName
						}

						if err := deployTarget(
							ctx,
							resolvedTargetConfig,
							rollbackDeployConfig,
							*configPath,
							deploymentID,
							prefix,
							noLogsFlag,
						); err != nil {
							return err
						}

					}
					return nil
				})
			}

			if err := g.Wait(); err != nil {
				return err
			}

			if len(rawDeployConfig.GlobalPostDeploy) > 0 {
				for _, hookCmd := range rawDeployConfig.GlobalPostDeploy {
					if err := cmdexec.RunCommand(ctx, hookCmd, getHooksWorkDir(*configPath)); err != nil {
						return fmt.Errorf("%s hook failed: %v", config.GetFieldNameForFormat(config.DeployConfig{}, "GlobalPostDeploy", rawDeployConfig.Format), err)
					}
				}
			}
			return nil
		},
	}

	cmd.Flags().StringVarP(&flags.configPath, "config", "c", "", "Path to config file or directory (default: .)")
	cmd.Flags().StringSliceVarP(&flags.targets, "targets", "t", nil, "Deploy to a specific targets (comma-separated)")
	cmd.Flags().BoolVarP(&flags.all, "all", "a", false, "Deploy to all targets")
	cmd.Flags().BoolVar(&noLogsFlag, "no-logs", false, "Don't stream haloyd deployment logs")
	cmd.Flags().BoolVar(&flags.includeProtected, "include-protected", false, "Include protected targets when using --all")

	return cmd
}

func deployTarget(
	ctx context.Context,
	targetConfig config.TargetConfig,
	rollbackDeployConfig config.DeployConfig,
	configPath, deploymentID, prefix string,
	noLogs bool,
) error {
	format := targetConfig.Format
	server := targetConfig.Server
	preDeploy := targetConfig.PreDeploy
	postDeploy := targetConfig.PostDeploy

	pui := &ui.PrefixedUI{Prefix: prefix}

	if len(preDeploy) > 0 {
		for _, hookCmd := range preDeploy {
			if err := cmdexec.RunCommand(ctx, hookCmd, getHooksWorkDir(configPath)); err != nil {
				return &PrefixedError{Err: fmt.Errorf("%s hook failed: %v", config.GetFieldNameForFormat(config.DeployConfig{}, "PreDeploy", format), err), Prefix: prefix}
			}
		}
	}

	token, err := getToken(&targetConfig, server)
	if err != nil {
		return &PrefixedError{Err: fmt.Errorf("unable to get token: %w", err), Prefix: prefix}
	}

	// Send the deploy request
	api, err := apiclient.New(server, token)
	if err != nil {
		return &PrefixedError{Err: fmt.Errorf("unable to create API client: %w", err), Prefix: prefix}
	}

	request := apitypes.DeployRequest{
		TargetConfig:         targetConfig,
		RollbackDeployConfig: rollbackDeployConfig,
		DeploymentID:         deploymentID,
	}

	pui.Info("Deployment started for %s", targetConfig.Name)

	err = api.Post(ctx, "deploy", request, nil)
	if err != nil {
		return &PrefixedError{Err: err, Prefix: prefix}
	}

	if !noLogs {
		streamPath := fmt.Sprintf("deploy/%s/logs", deploymentID)

		streamHandler := func(data string) bool {
			var logEntry logging.LogEntry
			if err := json.Unmarshal([]byte(data), &logEntry); err != nil {
				pui.Warn("failed to unmarshal json: %v", err)
				return false // we don't stop on these errors.
			}

			ui.DisplayLogEntry(logEntry, prefix)

			// If deployment is complete we'll return true to signal stream should stop
			return logEntry.IsDeploymentComplete
		}

		api.Stream(ctx, streamPath, streamHandler)
	}

	if len(postDeploy) > 0 {
		for _, hookCmd := range postDeploy {
			if err := cmdexec.RunCommand(ctx, hookCmd, getHooksWorkDir(configPath)); err != nil {
				return &PrefixedError{Err: fmt.Errorf("%s hook failed: %v", config.GetFieldNameForFormat(config.DeployConfig{}, "PostDeploy", format), err), Prefix: prefix}
			}
		}
	}
	return nil
}

func getHooksWorkDir(configPath string) string {
	workDir := "."
	if configPath != "." {
		// If a specific config path was provided, use its directory
		if stat, err := os.Stat(configPath); err == nil {
			if stat.IsDir() {
				workDir = configPath
			} else {
				workDir = filepath.Dir(configPath)
			}
		}
	}
	return workDir
}
