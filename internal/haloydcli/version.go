package haloydcli

import (
	"encoding/json"
	"fmt"

	"github.com/haloydev/haloy/internal/constants"
	"github.com/haloydev/haloy/internal/proxywire"
	"github.com/spf13/cobra"
)

func versionCmd() *cobra.Command {
	var jsonOutput bool

	cmd := &cobra.Command{
		Use:   "version",
		Short: "Print the version number",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if jsonOutput {
				return json.NewEncoder(cmd.OutOrStdout()).Encode(struct {
					Version                    string `json:"version"`
					RequiredProxyGeneration    int    `json:"required_proxy_generation"`
					RequiredProxySchemaVersion int    `json:"required_proxy_schema_version"`
				}{
					Version:                    constants.Version,
					RequiredProxyGeneration:    proxywire.ProxyGeneration,
					RequiredProxySchemaVersion: proxywire.SchemaVersion,
				})
			}
			fmt.Println(constants.Version)
			return nil
		},
	}
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "Print structured version metadata")
	return cmd
}
