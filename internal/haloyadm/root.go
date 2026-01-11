package haloyadm

import (
	"github.com/haloydev/haloy/internal/config"
	"github.com/haloydev/haloy/internal/ui"
	"github.com/spf13/cobra"
)

func NewRootCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "haloyadm",
		Short: "Commands to manage the haloy services",
		PersistentPreRun: func(cmd *cobra.Command, args []string) {
			config.LoadEnvFiles()
		},
		SilenceErrors: true, // Don't print errors automatically
		SilenceUsage:  true, // Don't show usage on error
	}

	cmd.AddCommand(
		InitCmd(),
		StartCmd(),
		RestartCmd(),
		StopCmd(),
		APICmd(),
		SelfUpdateCmd(),
	)

	return cmd
}

func Execute() int {
	rootCmd := NewRootCmd()
	if err := rootCmd.Execute(); err != nil {
		ui.Error("%v", err)
		return 1
	}
	return 0
}
