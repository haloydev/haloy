package haloy

import (
	"context"
	"fmt"
	"sort"

	"github.com/haloydev/haloy/internal/config"
	"github.com/haloydev/haloy/internal/configloader"
	"github.com/haloydev/haloy/internal/helpers"
	"github.com/spf13/cobra"
)

type serverTarget struct {
	Server       string
	TargetConfig *config.TargetConfig
	TargetNames  []string
}

func resolveServerTargets(ctx context.Context, cmd *cobra.Command, configPath string, flags *appCmdFlags) ([]serverTarget, error) {
	selected := appCmdFlags{}
	if flags != nil {
		selected = *flags
	}
	if err := selected.validateTargetFlags(); err != nil {
		return nil, err
	}

	deployConfig, format, err := loadServerTargetsDeployConfig(ctx, cmd, configPath, selected)
	if err != nil {
		return nil, err
	}
	if format == "" {
		format = deployConfig.Format
	}

	targets, err := configloader.ExtractTargets(deployConfig, format)
	if err != nil {
		return nil, err
	}

	targetNames := make([]string, 0, len(targets))
	for targetName := range targets {
		targetNames = append(targetNames, targetName)
	}
	sort.Strings(targetNames)

	indexByServer := make(map[string]int)
	servers := make([]serverTarget, 0, len(targetNames))
	for _, targetName := range targetNames {
		target := targets[targetName]
		normalized, err := helpers.NormalizeServerURL(target.Server)
		if err != nil {
			return nil, fmt.Errorf("target '%s': invalid server URL %q: %w", targetName, target.Server, err)
		}

		if idx, exists := indexByServer[normalized]; exists {
			servers[idx].TargetNames = append(servers[idx].TargetNames, targetName)
			continue
		}

		targetCopy := target
		if err := resolveServerTargetAPIToken(ctx, &targetCopy, deployConfig, configPath, format); err != nil {
			return nil, fmt.Errorf("target '%s': failed to resolve API token: %w", targetName, err)
		}

		indexByServer[normalized] = len(servers)
		servers = append(servers, serverTarget{
			Server:       normalized,
			TargetConfig: &targetCopy,
			TargetNames:  []string{targetName},
		})
	}

	return servers, nil
}

func resolveServerTargetAPIToken(ctx context.Context, target *config.TargetConfig, deployConfig config.DeployConfig, configPath, format string) error {
	if target == nil || target.APIToken == nil || target.APIToken.From == nil {
		return nil
	}

	resolvedAPIToken, err := configloader.ResolveValueSource(ctx, target.APIToken, deployConfig.SecretProviders, format, configPath)
	if err != nil {
		return err
	}
	target.APIToken = resolvedAPIToken
	return nil
}

func loadServerTargetsDeployConfig(ctx context.Context, cmd *cobra.Command, configPath string, flags appCmdFlags) (config.DeployConfig, string, error) {
	if !serverTargetSelectorsChanged(cmd) {
		rawDeployConfig, format, err := configloader.LoadRawDeployConfig(configPath)
		if err != nil {
			return config.DeployConfig{}, "", fmt.Errorf("unable to load config: %w", err)
		}
		rawDeployConfig.Format = format
		return rawDeployConfig, format, nil
	}

	deployConfig, format, err := configloader.Load(ctx, configPath, flags.targets, flags.all)
	if err != nil {
		return config.DeployConfig{}, "", fmt.Errorf("unable to load config: %w", err)
	}
	deployConfig.Format = format
	return deployConfig, format, nil
}

func serverTargetSelectorsChanged(cmd *cobra.Command) bool {
	if cmd == nil {
		return false
	}
	return cmd.Flags().Changed("targets") || cmd.Flags().Changed("all")
}
