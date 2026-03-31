package haloy

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/haloydev/haloy/internal/apiclient"
	"github.com/haloydev/haloy/internal/apitypes"
	"github.com/haloydev/haloy/internal/config"
	"github.com/haloydev/haloy/internal/configloader"
	"github.com/haloydev/haloy/internal/constants"
	"github.com/haloydev/haloy/internal/ui"
	"github.com/spf13/cobra"
)

func PruneImagesCmd(configPath *string, flags *appCmdFlags) *cobra.Command {
	var keepFlag int
	var yesFlag bool

	cmd := &cobra.Command{
		Use:   "prune-images",
		Short: "Remove old image tags for deployed applications",
		Long: `Remove old image tags for deployed applications using a haloy configuration file.

By default this command performs a dry run and shows which image tags would be removed.
Use --yes to apply the prune plan.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()

			rawDeployConfig, format, err := configloader.Load(ctx, *configPath, flags.targets, flags.all)
			if err != nil {
				return fmt.Errorf("unable to load config: %w", err)
			}

			resolvedDeployConfig, err := configloader.ResolveSecrets(ctx, rawDeployConfig, *configPath)
			if err != nil {
				return fmt.Errorf("failed to resolve secrets: %w", err)
			}

			targets, err := configloader.ExtractTargets(resolvedDeployConfig, format)
			if err != nil {
				return err
			}

			targetNames := make([]string, 0, len(targets))
			for name := range targets {
				targetNames = append(targetNames, name)
			}
			sort.Strings(targetNames)

			for _, targetName := range targetNames {
				target := targets[targetName]
				keep := keepFlag
				if keep < 0 {
					keep = defaultPruneKeep(target)
				}
				if keep < 0 {
					return fmt.Errorf("target '%s': keep must be at least 0", targetName)
				}

				response, err := pruneImagesForTarget(ctx, target, keep, yesFlag)
				if err != nil {
					prefix := ""
					if len(targets) > 1 {
						prefix = target.TargetName
					}
					return &PrefixedError{Err: err, Prefix: prefix}
				}

				displayImagePruneResult(target, response, *configPath, len(targets) > 1)
			}

			return nil
		},
	}

	cmd.Flags().StringVarP(&flags.configPath, "config", "c", "", "Path to config file or directory (default: .)")
	cmd.Flags().StringSliceVarP(&flags.targets, "targets", "t", nil, "Prune images for specific target(s) (comma-separated)")
	cmd.Flags().BoolVarP(&flags.all, "all", "a", false, "Prune images for all targets")
	cmd.Flags().IntVar(&keepFlag, "keep", -1, "Number of deployment images to keep locally (defaults to the target's image history policy)")
	cmd.Flags().BoolVar(&yesFlag, "yes", false, "Apply the prune plan instead of running a dry run")

	cmd.RegisterFlagCompletionFunc("targets", completeTargetNames)

	return cmd
}

func pruneImagesForTarget(ctx context.Context, target config.TargetConfig, keep int, apply bool) (*apitypes.ImagePruneResponse, error) {
	ui.Info("Checking old images for application: %s using server %s", target.Name, target.Server)

	token, err := getToken(&target, target.Server)
	if err != nil {
		return nil, fmt.Errorf("unable to get token: %w", err)
	}

	api, err := apiclient.New(target.Server, token)
	if err != nil {
		return nil, fmt.Errorf("unable to create API client: %w", err)
	}

	req := apitypes.ImagePruneRequest{
		AppName: target.Name,
		Keep:    keep,
		Apply:   apply,
	}
	var resp apitypes.ImagePruneResponse
	if err := api.Post(ctx, "images/prune", req, &resp); err != nil {
		return nil, fmt.Errorf("failed to prune images: %w", err)
	}

	return &resp, nil
}

func defaultPruneKeep(target config.TargetConfig) int {
	if target.Image == nil || target.Image.History == nil {
		return int(constants.DefaultDeploymentsToKeep)
	}

	switch target.Image.History.Strategy {
	case config.HistoryStrategyNone:
		return 0
	case config.HistoryStrategyRegistry:
		if target.Image.History.Count != nil {
			return *target.Image.History.Count
		}
		return 1
	case config.HistoryStrategyLocal, "":
		if target.Image.History.Count != nil {
			return *target.Image.History.Count
		}
		return int(constants.DefaultDeploymentsToKeep)
	default:
		return int(constants.DefaultDeploymentsToKeep)
	}
}

func targetSelectorName(target config.TargetConfig) string {
	if target.TargetName != "" {
		return target.TargetName
	}
	return target.Name
}

func imagePruneCommandHint(configPath string, target config.TargetConfig, keep int, apply bool, includeTarget bool) string {
	args := []string{"haloy", "prune-images"}
	if configPath != "" && configPath != "." {
		args = append(args, "--config", configPath)
	}
	if includeTarget {
		args = append(args, "--targets", targetSelectorName(target))
	}
	args = append(args, "--keep", fmt.Sprintf("%d", keep))
	if apply {
		args = append(args, "--yes")
	}
	return strings.Join(args, " ")
}

func imagePruneErrorHint(target config.TargetConfig) []string {
	command := imagePruneCommandHint(".", target, defaultPruneKeep(target), true, false)
	hints := []string{
		fmt.Sprintf("Hint: run '%s' to remove old images for this target on %s.", command, target.Server),
	}

	if target.Image != nil && target.Image.History != nil && target.Image.History.Count != nil && *target.Image.History.Count > 1 {
		reducedKeep := *target.Image.History.Count - 1
		hints = append(hints,
			fmt.Sprintf("Hint: current image.history.count is %d; if you can keep one fewer rollback, try '%s'.",
				*target.Image.History.Count,
				imagePruneCommandHint(".", target, reducedKeep, true, false),
			),
			fmt.Sprintf("Hint: if that helps, lower image.history.count from %d to %d in your config for future deploys.",
				*target.Image.History.Count,
				reducedKeep,
			),
		)
	}

	return hints
}

func isDiskSpaceRelatedError(err error) bool {
	if err == nil {
		return false
	}

	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "insufficient disk space") ||
		strings.Contains(msg, "server disk space too low") ||
		strings.Contains(msg, "failed disk space preflight")
}

func withImagePruneHint(err error, target config.TargetConfig) error {
	if !isDiskSpaceRelatedError(err) {
		return err
	}

	return fmt.Errorf("%w\n%s", err, strings.Join(imagePruneErrorHint(target), "\n"))
}

func displayImagePruneResult(target config.TargetConfig, response *apitypes.ImagePruneResponse, configPath string, includeTarget bool) {
	mode := "dry-run"
	if response.Applied {
		mode = "applied"
	}

	title := fmt.Sprintf("Image prune for %s", targetSelectorName(target))
	lines := []string{
		fmt.Sprintf("App: %s", response.AppName),
		fmt.Sprintf("Server: %s", target.Server),
		fmt.Sprintf("Keep: %d", response.Keep),
		fmt.Sprintf("Mode: %s", mode),
	}
	if len(response.RunningDeploymentIDs) > 0 {
		lines = append(lines, fmt.Sprintf("Running deployment(s): %s", strings.Join(response.RunningDeploymentIDs, ", ")))
	} else {
		lines = append(lines, "Running deployment(s): none")
	}

	ui.Section(title, lines)

	if len(response.Tags) == 0 {
		if response.Applied {
			ui.Success("No old image tags needed removal for %s", response.AppName)
		} else {
			ui.Info("No old image tags would be removed for %s", response.AppName)
		}
		return
	}

	rows := make([][]string, 0, len(response.Tags))
	for _, tag := range response.Tags {
		rows = append(rows, []string{tag.Tag, tag.DeploymentID})
	}

	ui.Table([]string{"TAG", "DEPLOYMENT ID"}, rows)

	if response.Applied {
		ui.Success("Removed %d old image tag(s) for %s", len(response.Tags), response.AppName)
		return
	}

	ui.Basic("To apply this prune plan, run:")
	ui.Basic("  %s", imagePruneCommandHint(configPath, target, response.Keep, true, includeTarget))
}
