package haloy

import (
	"errors"
	"os"
	"slices"

	"github.com/haloydev/haloy/internal/config"
	"github.com/spf13/cobra"
)

// appCmdFlags holds the values for all flags shared by app-related commands.
type appCmdFlags struct {
	configPath string
	targets    []string
	all        bool
}

// Commands that support target flags and need validation
var targetFlagCommands = []string{
	"deploy",
	"status",
	"stop",
	"logs",
	"rollback",
	"rollback-targets",
	"validate-config",
}

func (f *appCmdFlags) validateTargetFlags() error {
	if len(f.targets) > 0 && f.all {
		return errors.New("cannot specify both --targets and --all flags; use one or the other")
	}
	return nil
}

func NewRootCmd() *cobra.Command {
	appFlags := &appCmdFlags{}
	resolvedConfigPath := "."

	cmd := &cobra.Command{
		Use:   "haloy",
		Short: "haloy builds and runs Docker containers based on a YAML config",
		PersistentPreRun: func(cmd *cobra.Command, args []string) {
			if slices.Contains(targetFlagCommands, cmd.Name()) {
				if err := appFlags.validateTargetFlags(); err != nil {
					cmd.PrintErrln("Error:", err.Error())
					cmd.Usage()
					os.Exit(1)
				}
			}
			config.LoadEnvFiles(appFlags.targets) // load environment variables in .env for all commands.

			if cmd.Name() == "completion" || cmd.Parent().Name() == "server" {
				return
			}

			if appFlags.configPath != "" {
				resolvedConfigPath = appFlags.configPath
			}
		},
		SilenceErrors: true,
		SilenceUsage:  true,
	}

	validateCmd := ValidateAppConfigCmd(&resolvedConfigPath)
	validateCmd.Flags().StringVarP(&appFlags.configPath, "config", "c", "", "Path to config file or directory (default: .)")

	cmd.AddCommand(
		DeployAppCmd(&resolvedConfigPath, appFlags),
		RollbackTargetsCmd(&resolvedConfigPath, appFlags),
		RollbackAppCmd(&resolvedConfigPath, appFlags),
		LogsCmd(&resolvedConfigPath, appFlags),
		StatusAppCmd(&resolvedConfigPath, appFlags),
		StopAppCmd(&resolvedConfigPath, appFlags),
		VersionCmd(&resolvedConfigPath, appFlags),

		validateCmd,

		CompletionCmd(),
		ServerCmd(),
	)

	return cmd
}
