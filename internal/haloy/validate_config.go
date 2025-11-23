package haloy

import (
	"encoding/json"
	"fmt"
	"path/filepath"

	"github.com/haloydev/haloy/internal/appconfigloader"
	"github.com/haloydev/haloy/internal/config"
	"github.com/haloydev/haloy/internal/ui"
	"github.com/pelletier/go-toml/v2"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

func ValidateAppConfigCmd(configPath *string) *cobra.Command {
	var showResolvedConfigFlag bool

	cmd := &cobra.Command{
		Use:   "validate-config",
		Short: "Validate a haloy config file",
		Long:  "Validate a haloy configuration file.",

		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()

			configFileName, err := appconfigloader.FindConfigFile(*configPath)
			if err != nil {
				return err
			}

			rawAppConfig, format, err := appconfigloader.LoadRawAppConfig(*configPath)
			if err != nil {
				return fmt.Errorf("unable to load config file from %s: %w", *configPath, err)
			}

			errors := make([]error, 0)
			if len(rawAppConfig.Targets) > 0 {
				for targetName, target := range rawAppConfig.Targets {
					mergedTargetConfig, err := appconfigloader.MergeToTarget(rawAppConfig, *target, targetName, format)
					if err != nil {
						errors = append(errors, fmt.Errorf("unable to extract target '%s': %w", targetName, err))
						continue
					}

					if err := mergedTargetConfig.Validate(rawAppConfig.Format); err != nil {
						errors = append(errors, fmt.Errorf("target '%s' validation failed: %w", targetName, err))
					}
				}
			} else {
				mergedSingleTargetConfig, err := appconfigloader.MergeToTarget(rawAppConfig, config.TargetConfig{}, rawAppConfig.Name, format)
				if err != nil {
					errors = append(errors, fmt.Errorf("unable to extract config: %w", err))
				} else {
					if err := mergedSingleTargetConfig.Validate(rawAppConfig.Format); err != nil {
						errors = append(errors, fmt.Errorf("configuration validation failed: %w", err))
					}
				}
			}

			resolvedTargets := make(map[string]config.TargetConfig)
			if len(errors) == 0 {
				resolvedAppConfig, err := appconfigloader.ResolveSecrets(ctx, rawAppConfig)
				if err != nil {
					errors = append(errors, fmt.Errorf("unable to resolve secrets: %w", err))
				} else {
					resolvedTargets, err = appconfigloader.ExtractTargets(resolvedAppConfig, format)
					if err != nil {
						errors = append(errors, err)
					}

				}
			}

			// Return all errors
			if len(errors) > 0 {
				// Print all validation errors to provide complete feedback
				for _, validationErr := range errors {
					ui.Error("%v", validationErr)
				}
				// Return the first error to trigger non-zero exit code
				return errors[0]
			}

			if showResolvedConfigFlag {
				for _, resolvedTarget := range resolvedTargets {
					if err := displayResolvedConfig(resolvedTarget); err != nil {
						return fmt.Errorf("failed to display resolved config: %w", err)
					}
				}
			}

			ui.Success("Config file '%s' is valid!", filepath.Base(configFileName))
			return nil
		},
	}
	cmd.Flags().BoolVar(&showResolvedConfigFlag, "show-resolved-config", false, "Print the resolved configuration with all fields and secrets resolved and visible in plain text (WARNING: sensitive data will be displayed)")
	return cmd
}

func displayResolvedConfig(targetConfig config.TargetConfig) error {
	var output string

	switch targetConfig.Format {
	case "json":
		data, err := json.MarshalIndent(targetConfig, "", "  ")
		if err != nil {
			return err
		}
		output = string(data)
	case "yaml", "yml":
		data, err := yaml.Marshal(targetConfig)
		if err != nil {
			return err
		}
		output = string(data)
	case "toml":
		data, err := toml.Marshal(targetConfig)
		if err != nil {
			return err
		}
		output = string(data)
	default:
		return fmt.Errorf("unsupported format: %s", targetConfig.Format)
	}

	targetName := targetConfig.TargetName
	if targetName == "" {
		targetName = targetConfig.Name
	}

	ui.Section(fmt.Sprintf("Resolved Configuration for %s", targetName), []string{output})
	return nil
}
