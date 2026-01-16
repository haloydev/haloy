package haloydcli

import (
	"fmt"

	"github.com/haloydev/haloy/internal/constants"
	"github.com/spf13/cobra"
)

func versionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the version number",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Println(constants.Version)
		},
	}
}
