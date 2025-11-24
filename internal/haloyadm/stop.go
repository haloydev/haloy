package haloyadm

import (
	"fmt"

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
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()

			if err := stopContainer(ctx, config.HaloydLabelRole); err != nil {
				return fmt.Errorf("failed to stop haloyd: %w", err)
			}

			if err := stopContainer(ctx, config.HAProxyLabelRole); err != nil {
				return fmt.Errorf("failed to stop HAProxy: %w", err)
			}

			ui.Success("Haloy services stopped successfully")
			return nil
		},
	}
	return cmd
}
