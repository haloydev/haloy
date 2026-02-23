package haloy

import (
	"context"
	"fmt"
	"strings"

	"github.com/haloydev/haloy/internal/apiclient"
	"github.com/haloydev/haloy/internal/apitypes"
	"github.com/haloydev/haloy/internal/config"
	"github.com/haloydev/haloy/internal/configloader"
	"github.com/haloydev/haloy/internal/ui"
	"github.com/spf13/cobra"
	"golang.org/x/sync/errgroup"
)

func ExecCmd(configPath *string, flags *appCmdFlags) *cobra.Command {
	var (
		allContainers bool
		containerID   string
	)

	cmd := &cobra.Command{
		Use:   "exec [flags] -- <command> [args...]",
		Short: "Execute a command in an application container",
		Long: `Execute a command in running container(s) for an application using a haloy configuration file.

By default, the command runs on the first container. Use flags to target
specific containers or all containers.

Examples:
  # Run 'ls -la' in the first container
  haloy exec -- ls -la

  # Run on all containers
  haloy exec --all-containers -- whoami

  # Run on a specific container by ID
  haloy exec --container abc123 -- env

  # With target selection (multi-target config)
  haloy exec --targets prod -- ls -la`,
		Args: cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()

			// Get command args after '--'
			dashIdx := cmd.ArgsLenAtDash()
			if dashIdx == -1 || len(args) == 0 {
				return fmt.Errorf("command required after '--' separator (e.g., haloy exec -- ls -la)")
			}

			execCommand := args[dashIdx:]
			if len(execCommand) == 0 {
				return fmt.Errorf("command required after '--' separator")
			}

			if allContainers && containerID != "" {
				return fmt.Errorf("cannot specify both --all-containers and --container")
			}

			rawDeployConfig, format, err := configloader.Load(ctx, *configPath, flags.targets, flags.all)
			if err != nil {
				return fmt.Errorf("unable to load config: %w", err)
			}

			resolvedDeployConfig, err := configloader.ResolveSecrets(ctx, rawDeployConfig)
			if err != nil {
				return fmt.Errorf("unable to resolve secrets: %w", err)
			}

			targets, err := configloader.ExtractTargets(resolvedDeployConfig, format)
			if err != nil {
				return err
			}

			// Build exec request
			execReq := apitypes.ExecRequest{
				Command:       execCommand,
				AllContainers: allContainers,
				ContainerID:   containerID,
			}

			g, ctx := errgroup.WithContext(ctx)
			for _, target := range targets {
				g.Go(func() error {
					prefix := ""
					if len(targets) > 1 {
						prefix = target.TargetName
					}
					return execInApp(ctx, &target, target.Server, target.Name, execReq, prefix)
				})
			}

			return g.Wait()
		},
	}

	cmd.Flags().StringVarP(&flags.configPath, "config", "c", "", "Path to config file or directory (default: .)")
	cmd.Flags().StringSliceVarP(&flags.targets, "targets", "t", nil, "Execute on specific targets (comma-separated)")
	cmd.Flags().BoolVarP(&flags.all, "all", "a", false, "Execute on all targets")
	cmd.Flags().BoolVar(&allContainers, "all-containers", false, "Execute on all containers")
	cmd.Flags().StringVar(&containerID, "container", "", "Execute on specific container ID")

	cmd.RegisterFlagCompletionFunc("targets", completeTargetNames)

	return cmd
}

func execInApp(ctx context.Context, targetConfig *config.TargetConfig, targetServer, appName string, execReq apitypes.ExecRequest, prefix string) error {
	pui := &ui.PrefixedUI{Prefix: prefix}

	token, err := getToken(targetConfig, targetServer)
	if err != nil {
		return &PrefixedError{Err: fmt.Errorf("unable to get token: %w", err), Prefix: prefix}
	}

	api, err := apiclient.New(targetServer, token)
	if err != nil {
		return &PrefixedError{Err: fmt.Errorf("unable to create API client: %w", err), Prefix: prefix}
	}

	path := fmt.Sprintf("exec/%s", appName)
	var response apitypes.ExecResponse
	if err := api.Post(ctx, path, execReq, &response); err != nil {
		return &PrefixedError{Err: fmt.Errorf("failed to execute command: %w", err), Prefix: prefix}
	}

	multipleResults := len(response.Results) > 1

	var hasError bool
	for _, result := range response.Results {
		if multipleResults {
			pui.Info("Container %s", result.ContainerID)
		}

		if result.Error != "" {
			pui.Error("Error: %s", result.Error)
			hasError = true
			continue
		}

		if result.Stdout != "" {
			stdout := strings.TrimSuffix(result.Stdout, "\n")
			for line := range strings.SplitSeq(stdout, "\n") {
				if prefix != "" {
					pui.Info("%s", line)
				} else {
					fmt.Println(line)
				}
			}
		}

		if result.Stderr != "" {
			stderr := strings.TrimSuffix(result.Stderr, "\n")
			for line := range strings.SplitSeq(stderr, "\n") {
				if prefix != "" {
					pui.Warn("[stderr] %s", line)
				} else {
					ui.Warn("[stderr] %s", line)
				}
			}
		}

		if result.ExitCode != 0 {
			if prefix != "" {
				pui.Warn("Exit code: %d", result.ExitCode)
			} else {
				ui.Warn("Exit code: %d", result.ExitCode)
			}
			hasError = true
		}

		if multipleResults {
			ui.Basic("") // Empty line between results
		}
	}

	if hasError {
		return &PrefixedError{Err: fmt.Errorf("command failed"), Prefix: prefix}
	}

	return nil
}
