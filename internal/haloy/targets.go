package haloy

import (
	"fmt"
	"sort"
	"strings"

	"github.com/haloydev/haloy/internal/configloader"
	"github.com/haloydev/haloy/internal/ui"
	"github.com/spf13/cobra"
)

func TargetsCmd(configPath *string, flags *appCmdFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "targets",
		Short: "List deployment targets defined in the config",
		Long:  "List all deployment targets defined in a haloy configuration file.",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			rawDeployConfig, format, err := configloader.LoadRawDeployConfig(*configPath)
			if err != nil {
				return fmt.Errorf("unable to load config: %w", err)
			}
			rawDeployConfig.Format = format

			targets, err := configloader.ExtractTargets(rawDeployConfig, format)
			if err != nil {
				return fmt.Errorf("unable to extract targets: %w", err)
			}

			if len(flags.targets) > 0 {
				for _, name := range flags.targets {
					if _, exists := targets[name]; !exists {
						return fmt.Errorf("target '%s' not found in configuration", name)
					}
				}
				for name := range targets {
					found := false
					for _, t := range flags.targets {
						if t == name {
							found = true
							break
						}
					}
					if !found {
						delete(targets, name)
					}
				}
			}

			names := make([]string, 0, len(targets))
			for name := range targets {
				names = append(names, name)
			}
			sort.Strings(names)

			rows := make([][]string, 0, len(names))
			for _, name := range names {
				target := targets[name]

				domains := "-"
				if len(target.Domains) > 0 {
					domainNames := make([]string, 0, len(target.Domains))
					for _, d := range target.Domains {
						domainNames = append(domainNames, d.Canonical)
					}
					domains = strings.Join(domainNames, ", ")
				}

				replicas := "1"
				if target.Replicas != nil {
					replicas = fmt.Sprintf("%d", *target.Replicas)
				}

				protected := "no"
				if target.Protected != nil && *target.Protected {
					protected = "yes"
				}

				rows = append(rows, []string{name, target.Server, domains, replicas, protected})
			}

			ui.Table([]string{"NAME", "SERVER", "DOMAINS", "REPLICAS", "PROTECTED"}, rows)
			return nil
		},
	}

	cmd.Flags().StringVarP(&flags.configPath, "config", "c", "", "Path to config file or directory (default: .)")
	cmd.Flags().StringSliceVarP(&flags.targets, "targets", "t", nil, "Filter specific targets (comma-separated)")

	return cmd
}
