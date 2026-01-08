package haloy

import (
	"fmt"
	"os"
	"path/filepath"

	projectinit "github.com/haloydev/haloy/internal/init"
	"github.com/haloydev/haloy/internal/ui"
	"github.com/spf13/cobra"
)

func InitCmd() *cobra.Command {
	var (
		path   string
		output string
		force  bool
	)

	cmd := &cobra.Command{
		Use:   "init",
		Short: "Initialize a new Haloy project",
		Long:  "Detect the framework and generate a Dockerfile and haloy.yaml configuration.",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runInit(path, output, force)
		},
	}

	cmd.Flags().StringVarP(&path, "path", "p", ".", "Path to the project directory")
	cmd.Flags().StringVarP(&output, "output", "o", "", "Output directory for generated files (defaults to project path)")
	cmd.Flags().BoolVarP(&force, "force", "f", false, "Overwrite existing Dockerfile and haloy.yaml")

	return cmd
}

func runInit(path, output string, force bool) error {
	// Resolve project directory
	projectDir, err := filepath.Abs(path)
	if err != nil {
		return fmt.Errorf("failed to resolve project path: %w", err)
	}

	// Check if project directory exists
	if _, err := os.Stat(projectDir); os.IsNotExist(err) {
		return fmt.Errorf("project directory does not exist: %s", projectDir)
	}

	// Resolve output directory (default to project directory)
	outputDir := projectDir
	if output != "" {
		outputDir, err = filepath.Abs(output)
		if err != nil {
			return fmt.Errorf("failed to resolve output path: %w", err)
		}
	}

	// Check if output directory exists
	if _, err := os.Stat(outputDir); os.IsNotExist(err) {
		return fmt.Errorf("output directory does not exist: %s", outputDir)
	}

	// Check for existing files before doing any work
	dockerfilePath := filepath.Join(outputDir, "Dockerfile")
	haloyYamlPath := filepath.Join(outputDir, "haloy.yaml")

	dockerfileExists := fileExists(dockerfilePath)
	haloyYamlExists := fileExists(haloyYamlPath)

	if !force {
		if dockerfileExists && haloyYamlExists {
			ui.Error("Dockerfile and haloy.yaml already exist at %s", outputDir)
			ui.Info("Use --force to overwrite existing files")
			return fmt.Errorf("files already exist")
		}
		if dockerfileExists {
			ui.Error("Dockerfile already exists at %s", dockerfilePath)
			ui.Info("Use --force to overwrite existing files")
			return fmt.Errorf("Dockerfile already exists")
		}
		if haloyYamlExists {
			ui.Error("haloy.yaml already exists at %s", haloyYamlPath)
			ui.Info("Use --force to overwrite existing files")
			return fmt.Errorf("haloy.yaml already exists")
		}
	}

	// Detect framework
	framework, err := projectinit.DetectFramework(projectDir)
	if err != nil {
		return fmt.Errorf("failed to detect framework: %w", err)
	}

	if framework == projectinit.FrameworkUnknown {
		ui.Error("Could not detect framework in %s", projectDir)
		ui.Basic("")
		ui.Basic("Supported frameworks:")
		ui.Basic("  - Next.js (next.config.js, next.config.mjs, next.config.ts)")
		ui.Basic("  - TanStack Start (@tanstack/start in package.json)")
		ui.Basic("  - Django (manage.py + django in requirements.txt)")
		ui.Basic("")
		ui.Basic("If your project uses a supported framework, please open an issue:")
		ui.Basic("  https://github.com/haloydev/haloy/issues")
		return fmt.Errorf("unknown framework")
	}

	ui.Info("Detected %s project", framework.DisplayName())

	// Framework-specific checks and warnings
	if framework == projectinit.FrameworkNextJS {
		if !projectinit.CheckNextJSStandalone(projectDir) {
			ui.Warn("Next.js standalone mode not detected")
			ui.Basic("")
			ui.Basic("For optimal Docker builds, add the following to your next.config.js:")
			ui.Basic("")
			ui.Basic("  module.exports = {")
			ui.Basic("    output: \"standalone\",")
			ui.Basic("  }")
			ui.Basic("")
			ui.Basic("Learn more: https://nextjs.org/docs/app/api-reference/config/next-config-js/output")
			ui.Basic("")
		}
	}

	// Build template data for Dockerfile
	data := projectinit.BuildDockerfileData(framework, projectDir)

	// Log detected configuration
	switch framework {
	case projectinit.FrameworkNextJS, projectinit.FrameworkTanStackStart:
		ui.Info("Node version: %s", data.NodeVersion)
		ui.Info("Package manager: %s", data.PackageManager)
	case projectinit.FrameworkDjango:
		ui.Info("Python version: %s", data.PythonVersion)
		ui.Info("Django project: %s", data.ProjectName)
	}

	// Generate and write Dockerfile
	dockerfileContent, err := projectinit.GenerateDockerfile(framework, data)
	if err != nil {
		return fmt.Errorf("failed to generate Dockerfile: %w", err)
	}

	if err := os.WriteFile(dockerfilePath, dockerfileContent, 0644); err != nil {
		return fmt.Errorf("failed to write Dockerfile: %w", err)
	}

	ui.Success("Created Dockerfile")

	// Server selection for haloy.yaml
	serverSelection, err := projectinit.SelectServer()
	if err != nil {
		ui.Warn("Could not select server: %v", err)
		ui.Info("Skipping haloy.yaml generation. Run 'haloy server add' first, then re-run 'haloy init'")
		printHealthCheckTip()
		return nil
	}

	if serverSelection.Selected {
		ui.Info("Selected server: %s", serverSelection.Server)
	} else {
		ui.Info("Using server: %s", serverSelection.Server)
	}

	// Parse .env files for environment variables
	envFiles, err := projectinit.ParseEnvFiles(projectDir)
	if err != nil {
		ui.Warn("Could not parse .env files: %v", err)
	}

	if len(envFiles) > 0 {
		ui.Info("Found env files: %s", projectinit.GetEnvFileSummary(envFiles))
	}

	// Get unique env vars, excluding build-time only vars
	envVars := projectinit.GetUniqueEnvVars(envFiles, projectinit.DefaultExcludePatterns())

	if len(envVars) > 0 {
		ui.Info("Found %d environment variables", len(envVars))
	}

	// Generate haloy.yaml
	haloyConfig := projectinit.GenerateHaloyConfig(projectDir, framework, serverSelection.Server, envVars)

	haloyYamlContent, err := haloyConfig.MarshalYAMLWithComments()
	if err != nil {
		return fmt.Errorf("failed to generate haloy.yaml: %w", err)
	}

	if err := os.WriteFile(haloyYamlPath, haloyYamlContent, 0644); err != nil {
		return fmt.Errorf("failed to write haloy.yaml: %w", err)
	}

	ui.Success("Created haloy.yaml")

	// Print summary and tips
	ui.Basic("")
	ui.Info("Next steps:")
	ui.Basic("  1. Review and customize haloy.yaml (configure domains, add secrets, etc.)")
	ui.Basic("  2. Ensure environment variables are set on your server")
	ui.Basic("  3. Run 'haloy deploy' to deploy your application")

	printHealthCheckTip()

	return nil
}

func printHealthCheckTip() {
	ui.Basic("")
	ui.Info("Tip: Add a /health endpoint to your app and uncomment the HEALTHCHECK")
	ui.Basic("     instruction in your Dockerfile for better container orchestration.")
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
