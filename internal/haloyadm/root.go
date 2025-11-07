package haloyadm

import (
	"github.com/haloydev/haloy/internal/config"
	"github.com/spf13/cobra"
)

// NewRootCmd creates the root command
func NewRootCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "haloyadm",
		Short: "Commands to manage the haloyd",
		PersistentPreRun: func(cmd *cobra.Command, args []string) {
			config.LoadEnvFiles([]string{}) // load environment variables in .env for all commands.
		},
		SilenceErrors: true, // Don't print errors automatically
		SilenceUsage:  true, // Don't show usage on error
	}

	// Add all subcommands
	cmd.AddCommand(
		InitCmd(),
		StartCmd(),
		RestartCmd(),
		StopCmd(),
		APICmd(),
	)

	return cmd
}
