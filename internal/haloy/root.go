package haloy

import (
	"errors"

	"github.com/haloydev/haloy/internal/config"
	"github.com/haloydev/haloy/internal/ui"
	"github.com/spf13/cobra"
)

// appCmdFlags holds the values for all flags shared by app-related commands.
type appCmdFlags struct {
	configPath       string
	targets          []string
	all              bool
	includeProtected bool
}

func (f *appCmdFlags) validateTargetFlags() error {
	if len(f.targets) > 0 && f.all {
		return errors.New("cannot specify both --targets and --all flags; use one or the other")
	}
	return nil
}

// isDirectSubcommand returns true if the command is a direct child of the root command.
func isDirectSubcommand(cmd *cobra.Command) bool {
	return cmd.Parent() != nil && cmd.Parent().Name() == "haloy"
}

func NewRootCmd() *cobra.Command {
	appFlags := &appCmdFlags{}
	resolvedConfigPath := "."

	cmd := &cobra.Command{
		Use:   "haloy",
		Short: "haloy builds and runs Docker containers based on a YAML config",
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			// Skip commands that don't need any config or validation
			if isDirectSubcommand(cmd) && (cmd.Name() == "completion" || cmd.Name() == "version") {
				return nil
			}

			// Load environment files from default locations
			config.LoadHaloyEnvFiles()

			// Skip server subcommands that don't use deploy config (add, delete, list)
			if cmd.Parent() != nil && cmd.Parent().Name() == "server" && cmd.Name() != "version" {
				return nil
			}

			if err := appFlags.validateTargetFlags(); err != nil {
				return err
			}

			if appFlags.configPath != "" {
				resolvedConfigPath = appFlags.configPath
			}

			config.LoadEnvFilesForTargets(appFlags.targets)

			return nil
		},
		SilenceErrors: true,
		SilenceUsage:  true,
	}

	validateCmd := ValidateDeployConfigCmd(&resolvedConfigPath)
	validateCmd.Flags().StringVarP(&appFlags.configPath, "config", "c", "", "Path to config file or directory (default: .)")

	cmd.AddCommand(
		DeployAppCmd(&resolvedConfigPath, appFlags),
		RollbackTargetsCmd(&resolvedConfigPath, appFlags),
		RollbackAppCmd(&resolvedConfigPath, appFlags),
		LogsCmd(&resolvedConfigPath, appFlags),
		StatusAppCmd(&resolvedConfigPath, appFlags),
		StopAppCmd(&resolvedConfigPath, appFlags),
		ExecCmd(&resolvedConfigPath, appFlags),
		TunnelCmd(&resolvedConfigPath, appFlags),
		ServerCmd(&resolvedConfigPath, appFlags),

		validateCmd,

		CompletionCmd(),
		VersionCmd(),
	)

	return cmd
}

func Execute() int {
	rootCmd := NewRootCmd()
	if err := rootCmd.Execute(); err != nil {
		var prefixedErr *PrefixedError
		if errors.As(err, &prefixedErr) {
			pui := &ui.PrefixedUI{Prefix: prefixedErr.GetPrefix()}
			pui.Error("%v", err)
		} else {
			ui.Error("%v", err)
		}
		return 1
	}
	return 0
}
