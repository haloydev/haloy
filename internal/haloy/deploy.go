package haloy

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/haloydev/haloy/internal/apiclient"
	"github.com/haloydev/haloy/internal/apitypes"
	"github.com/haloydev/haloy/internal/appconfigloader"
	"github.com/haloydev/haloy/internal/cmdexec"
	"github.com/haloydev/haloy/internal/config"
	"github.com/haloydev/haloy/internal/docker"
	"github.com/haloydev/haloy/internal/logging"
	"github.com/haloydev/haloy/internal/ui"
	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"
)

func DeployAppCmd(configPath *string, flags *appCmdFlags) *cobra.Command {
	var noLogsFlag bool

	cmd := &cobra.Command{
		Use:   "deploy",
		Short: "Deploy an application",
		Long:  "Deploy an application using a haloy configuration file.",
		Args:  cobra.NoArgs,
		Run: func(cmd *cobra.Command, _ []string) {
			ctx := cmd.Context()

			rawAppConfig, format, err := appconfigloader.Load(ctx, *configPath, flags.targets, flags.all)
			if err != nil {
				ui.Error("Unable to load config: %v", err)
				return
			}

			resolvedAppConfig, err := appconfigloader.ResolveSecrets(ctx, rawAppConfig)
			if err != nil {
				ui.Error("%v", err)
				return
			}

			rawTargets, err := appconfigloader.ExtractTargets(rawAppConfig, format)
			if err != nil {
				ui.Error("%v", err)
				return
			}

			resolvedTargets, err := appconfigloader.ExtractTargets(resolvedAppConfig, format)
			if err != nil {
				ui.Error("%v", err)
				return
			}

			if len(rawTargets) != len(resolvedTargets) {
				ui.Error("Mismatch between raw targets (%d) and resolved targets (%d). This indicates a configuration processing error.", len(rawTargets), len(resolvedTargets))
				return
			}

			builds, pushes, uploads := ResolveImageBuilds(resolvedTargets)
			for imageRef, image := range builds {
				if err := BuildImage(ctx, imageRef, image, *configPath); err != nil {
					ui.Error("%v", err)
					return
				}
			}
			for imageRef, targetConfigs := range uploads {
				if err := UploadImage(ctx, imageRef, targetConfigs); err != nil {
					ui.Error("%v", err)
					return
				}
			}

			if len(pushes) > 0 {
				cli, err := docker.NewClient(ctx)
				if err != nil {
					ui.Error("Unable to create docker client for push image: %v", err)
					return
				}
				for imageRef, images := range pushes {
					for _, image := range images {
						registryServer := docker.GetRegistryServer(image)
						ui.Info("Pushing image '%s' to %s", imageRef, registryServer)
						if err := docker.PushImage(ctx, cli, imageRef, image); err != nil {
							ui.Error("%v", err)
							return
						}
					}
				}
			}

			if len(rawAppConfig.GlobalPreDeploy) > 0 {
				for _, hookCmd := range rawAppConfig.GlobalPreDeploy {
					if err := cmdexec.RunCommand(ctx, hookCmd, getHooksWorkDir(*configPath)); err != nil {
						ui.Error("%s hook failed: %v", config.GetFieldNameForFormat(config.AppConfig{}, "GlobalPreDeploy", rawAppConfig.Format), err)
						return
					}
				}
			}

			servers := appconfigloader.TargetsByServer(rawTargets)

			// Create deployment IDs per app name
			deploymentIDs := make(map[string]string)
			for _, target := range resolvedTargets {
				if _, exists := deploymentIDs[target.Name]; !exists {
					deploymentIDs[target.Name] = createDeploymentID()
				}
			}

			var wg sync.WaitGroup
			for server, targetNames := range servers {
				wg.Add(1)
				go func(
					server string,
					targetNames []string,
					rawTargets, resolvedTargets map[string]config.TargetConfig,
					deploymentIDs map[string]string,
				) {
					defer wg.Done()
					for _, targetName := range targetNames {

						rawTargetConfig, rawTargetExists := rawTargets[targetName]
						if !rawTargetExists {
							ui.Error("Could not find raw target for %s", targetName)
							return

						}
						resolvedTargetConfig, resolvedTargetExists := resolvedTargets[targetName]
						if !resolvedTargetExists {
							ui.Error("Could not find resolved target for %s", targetName)
							return
						}

						deploymentID, deploymentIDExists := deploymentIDs[resolvedTargetConfig.Name]
						if !deploymentIDExists {
							ui.Error("Could not find deployment ID for app '%s'", resolvedTargetConfig.Name)
							return
						}

						// Recreate the AppConfig with just the target for rollbacks
						rollbackAppConfig := config.AppConfig{
							TargetConfig:    rawTargetConfig,
							SecretProviders: rawAppConfig.SecretProviders,
						}

						prefix := ""
						if len(rawTargets) > 1 {
							prefix = lipgloss.NewStyle().Bold(true).Foreground(ui.White).Render(fmt.Sprintf("%s ", targetName))
						}

						deployTarget(
							ctx,
							resolvedTargetConfig,
							rollbackAppConfig,
							*configPath,
							deploymentID,
							prefix,
							noLogsFlag,
						)

					}
				}(server, targetNames, rawTargets, resolvedTargets, deploymentIDs)
			}

			wg.Wait()

			if len(rawAppConfig.GlobalPostDeploy) > 0 {
				for _, hookCmd := range rawAppConfig.GlobalPostDeploy {
					if err := cmdexec.RunCommand(ctx, hookCmd, getHooksWorkDir(*configPath)); err != nil {
						ui.Error("%s hook failed: %v", config.GetFieldNameForFormat(config.AppConfig{}, "GlobalPostDeploy", rawAppConfig.Format), err)
						return
					}
				}
			}
		},
	}

	cmd.Flags().StringVarP(&flags.configPath, "config", "c", "", "Path to config file or directory (default: .)")
	cmd.Flags().StringSliceVarP(&flags.targets, "targets", "t", nil, "Deploy to a specific targets (comma-separated)")
	cmd.Flags().BoolVarP(&flags.all, "all", "a", false, "Deploy to all targets")

	return cmd
}

func deployTarget(
	ctx context.Context,
	targetConfig config.TargetConfig,
	rollbackAppConfig config.AppConfig,
	configPath, deploymentID, prefix string,
	noLogs bool,
) {
	format := targetConfig.Format
	server := targetConfig.Server
	preDeploy := targetConfig.PreDeploy
	postDeploy := targetConfig.PostDeploy

	pui := &ui.PrefixedUI{Prefix: prefix}

	pui.Info("Deployment started for %s", targetConfig.Name)

	if len(preDeploy) > 0 {
		for _, hookCmd := range preDeploy {
			if err := cmdexec.RunCommand(ctx, hookCmd, getHooksWorkDir(configPath)); err != nil {
				pui.Error("%s hook failed: %v", config.GetFieldNameForFormat(config.AppConfig{}, "PreDeploy", format), err)
				return
			}
		}
	}

	token, err := getToken(&targetConfig, server)
	if err != nil {
		pui.Error("%v", err)
		return
	}

	// Send the deploy request
	api, err := apiclient.New(server, token)
	if err != nil {
		pui.Error("Failed to create API client: %v", err)
		return
	}

	request := apitypes.DeployRequest{
		TargetConfig:      targetConfig,
		RollbackAppConfig: rollbackAppConfig,
		DeploymentID:      deploymentID,
	}
	err = api.Post(ctx, "deploy", request, nil)
	if err != nil {
		pui.Error("Deployment request failed: %v", err)
		return
	}

	if !noLogs {
		streamPath := fmt.Sprintf("deploy/%s/logs", deploymentID)

		streamHandler := func(data string) bool {
			var logEntry logging.LogEntry
			if err := json.Unmarshal([]byte(data), &logEntry); err != nil {
				pui.Error("failed to ummarshal json: %v", err)
				return false // we don't stop on errors.
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
				pui.Error("%s hook failed: %v", config.GetFieldNameForFormat(config.AppConfig{}, "PostDeploy", format), err)
			}
		}
	}
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
