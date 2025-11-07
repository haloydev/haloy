package haloyadm

import (
	"github.com/haloydev/haloy/internal/config"
	"github.com/haloydev/haloy/internal/ui"
	"github.com/spf13/cobra"
)

func StopCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "stop",
		Short: "Stop the haloy services",
		Long:  "Stop the haloy services, including HAProxy and haloyd.",
		Args:  cobra.NoArgs,
		Run: func(cmd *cobra.Command, args []string) {
			ctx := cmd.Context()

			if err := stopContainer(ctx, config.HaloydLabelRole); err != nil {
				ui.Error("Failed to stop haloyd: %v", err)
				return
			}

			if err := stopContainer(ctx, config.HAProxyLabelRole); err != nil {
				ui.Error("Failed to stop HAProxy: %v", err)
				return
			}

			ui.Success("Haloy services stopped successfully")
		},
	}
	return cmd
}
