package haloy

import (
	"fmt"

	"github.com/haloydev/haloy/internal/constants"
	"github.com/haloydev/haloy/internal/github"
	"github.com/haloydev/haloy/internal/helpers"
	"github.com/haloydev/haloy/internal/ui"
	"github.com/spf13/cobra"
)

func VersionCmd() *cobra.Command {
	var check bool

	cmd := &cobra.Command{
		Use:   "version",
		Short: "Show the current version",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if !check {
				fmt.Println(constants.Version)
				return nil
			}

			currentVersion := constants.Version
			ui.Info("Current version: %s", currentVersion)

			ui.Info("Checking for updates...")
			latestVersion, err := github.FetchLatestVersion(cmd.Context())
			if err != nil {
				return fmt.Errorf("failed to check for updates: %w", err)
			}

			ui.Info("Latest version: %s", latestVersion)

			normalizedCurrent := helpers.NormalizeVersion(currentVersion)
			normalizedLatest := helpers.NormalizeVersion(latestVersion)

			if normalizedCurrent == normalizedLatest {
				ui.Success("You are running the latest version!")
			} else {
				ui.Info("Update available: %s -> %s", currentVersion, latestVersion)
			}

			return nil
		},
	}

	cmd.Flags().BoolVar(&check, "check", false, "Check if a newer version is available")

	return cmd
}
