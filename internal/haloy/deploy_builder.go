package haloy

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/haloydev/haloy/internal/apiclient"
	"github.com/haloydev/haloy/internal/cmdexec"
	"github.com/haloydev/haloy/internal/config"
	"github.com/haloydev/haloy/internal/ui"
)

func ResolveImageBuilds(targets map[string]config.TargetConfig) (map[string]*config.Image, map[string][]*config.Image, map[string][]*config.TargetConfig) {
	builds := make(map[string]*config.Image) // imageRef is key
	uploads := make(map[string][]*config.TargetConfig)
	pushes := make(map[string][]*config.Image)

	for _, target := range targets {
		image := target.Image
		if image == nil || !image.ShouldBuild() {
			continue
		}

		imageRef := image.ImageRef()

		if _, exists := builds[imageRef]; !exists {
			builds[imageRef] = image
		}

		pushStrategy := image.GetEffectivePushStrategy()
		if pushStrategy == config.BuildPushOptionServer {
			uploads[imageRef] = append(uploads[imageRef], &target)
		} else if pushStrategy == config.BuildPushOptionRegistry && image.RegistryAuth != nil {
			pushes[imageRef] = append(pushes[imageRef], target.Image)
		}
	}

	return builds, pushes, uploads
}

// BuildImage builds a Docker image using the provided image configuration
func BuildImage(ctx context.Context, imageRef string, image *config.Image, configPath string) error {
	ui.Info("Building image %s", imageRef)

	buildConfig := image.BuildConfig
	if buildConfig == nil {
		buildConfig = &config.BuildConfig{}
	}

	// Work directory is the config file's directory.
	// All paths (context, dockerfile) are relative to this directory.
	workDir := getBuilderWorkDir(configPath)

	// Context defaults to "." (config directory) if not specified
	buildContext := "."
	if buildConfig.Context != "" {
		buildContext = buildConfig.Context
	}

	args := []string{"build"}

	if buildConfig.Dockerfile != "" {
		args = append(args, "-f", buildConfig.Dockerfile)
	}

	if buildConfig.Platform == "" {
		buildConfig.Platform = "linux/amd64" // most widely used platform and a common pitfall
	}
	args = append(args, "--platform", buildConfig.Platform)

	for _, buildArg := range buildConfig.Args {
		if buildArg.Value != "" {
			args = append(args, "--build-arg", fmt.Sprintf("%s=%q", buildArg.Name, buildArg.Value))
		} else {
			// If no value specified, pass the build arg name only (Docker will use env var)
			args = append(args, "--build-arg", buildArg.Name)
		}
	}

	// Add image tag
	args = append(args, "-t", imageRef)

	// Add build context as the last argument
	args = append(args, buildContext)

	cmd := fmt.Sprintf("docker %s", strings.Join(args, " "))
	if err := cmdexec.RunCommand(ctx, cmd, workDir); err != nil {
		return fmt.Errorf("failed to build image %s: %w", imageRef, err)
	}

	ui.Success("Successfully built image %s", imageRef)
	return nil
}

// getBuilderWorkDir returns the directory containing the config file.
// All build paths (context, dockerfile) are relative to this directory.
func getBuilderWorkDir(configPath string) string {
	if configPath == "" || configPath == "." {
		return "."
	}
	if stat, err := os.Stat(configPath); err == nil && stat.IsDir() {
		return configPath
	}
	return filepath.Dir(configPath)
}

// UploadImage uploads a Docker image tar to the specified server
func UploadImage(ctx context.Context, imageRef string, resolvedTargetConfigs []*config.TargetConfig) error {
	tempFile, err := os.CreateTemp("", fmt.Sprintf("haloy-upload-%s-*.tar", strings.ReplaceAll(imageRef, ":", "-")))
	if err != nil {
		return fmt.Errorf("failed to create temporary file: %w", err)
	}
	defer os.Remove(tempFile.Name())
	defer tempFile.Close()

	saveCmd := fmt.Sprintf("docker save -o %s %s", tempFile.Name(), imageRef)
	if err := cmdexec.RunCommand(ctx, saveCmd, "."); err != nil {
		return fmt.Errorf("failed to save image to tar: %w", err)
	}

	for _, resolvedDeployConfig := range resolvedTargetConfigs {
		ui.Info("Uploading image %s to %s", imageRef, resolvedDeployConfig.Server)

		token, err := getToken(resolvedDeployConfig, resolvedDeployConfig.Server)
		if err != nil {
			return fmt.Errorf("failed to get authentication token: %w", err)
		}

		api, err := apiclient.NewWithTimeout(resolvedDeployConfig.Server, token, 5*time.Minute)
		if err != nil {
			return fmt.Errorf("failed to create API client: %w", err)
		}

		if err := api.PostFile(ctx, "images/upload", "image", tempFile.Name()); err != nil {
			return fmt.Errorf("failed to upload image: %w", err)
		}
	}

	return nil
}
