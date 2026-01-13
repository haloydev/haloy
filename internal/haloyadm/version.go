package haloyadm

import (
	"fmt"

	"github.com/haloydev/haloy/internal/constants"
	"github.com/spf13/cobra"
)

func VersionCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "version",
		Short: "Show the current version",
		Args:  cobra.NoArgs,
		Run: func(cmd *cobra.Command, _ []string) {
			fmt.Println(constants.Version)
		},
	}
	return cmd
}
