package haloy

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/haloydev/haloy/internal/config"
)

// resolvedBuildPaths holds the absolute build context and dockerfile paths
// resolved from a build config.
type resolvedBuildPaths struct {
	ContextDir string
	Dockerfile string
}

// resolveBuildPaths resolves the build context and dockerfile to absolute
// paths and verifies both exist. Matching Docker Compose semantics, the
// context is resolved relative to the config file's directory and the
// dockerfile is resolved relative to the context. Absolute paths are used
// as-is.
func resolveBuildPaths(configPath string, buildConfig *config.BuildConfig) (resolvedBuildPaths, error) {
	if buildConfig == nil {
		buildConfig = &config.BuildConfig{}
	}

	configDir, err := filepath.Abs(getBuilderWorkDir(configPath))
	if err != nil {
		return resolvedBuildPaths{}, fmt.Errorf("failed to resolve config directory: %w", err)
	}

	contextValue := buildConfig.Context
	if contextValue == "" {
		contextValue = "."
	}
	contextDir := contextValue
	if !filepath.IsAbs(contextDir) {
		contextDir = filepath.Join(configDir, contextDir)
	}

	if stat, err := os.Stat(contextDir); err != nil {
		return resolvedBuildPaths{}, fmt.Errorf(
			"build context %q not found (resolved to %s relative to the config directory %s)",
			contextValue, contextDir, configDir,
		)
	} else if !stat.IsDir() {
		return resolvedBuildPaths{}, fmt.Errorf(
			"build context %q is not a directory (resolved to %s relative to the config directory %s)",
			contextValue, contextDir, configDir,
		)
	}

	dockerfileValue := buildConfig.Dockerfile
	if dockerfileValue == "" {
		dockerfileValue = "Dockerfile"
	}
	dockerfile := dockerfileValue
	if !filepath.IsAbs(dockerfile) {
		dockerfile = filepath.Join(contextDir, dockerfile)
	}

	if stat, err := os.Stat(dockerfile); err != nil {
		return resolvedBuildPaths{}, fmt.Errorf(
			"dockerfile %q not found (resolved to %s relative to the build context %s; config directory is %s)",
			dockerfileValue, dockerfile, contextDir, configDir,
		)
	} else if stat.IsDir() {
		return resolvedBuildPaths{}, fmt.Errorf(
			"dockerfile %q is a directory, not a file (resolved to %s relative to the build context %s; config directory is %s)",
			dockerfileValue, dockerfile, contextDir, configDir,
		)
	}

	return resolvedBuildPaths{ContextDir: contextDir, Dockerfile: dockerfile}, nil
}

// validateBuildPaths checks that the build context and dockerfile exist for
// targets that build locally.
func validateBuildPaths(configPath string, target config.TargetConfig) error {
	if target.Image == nil || !target.Image.ShouldBuild() {
		return nil
	}
	_, err := resolveBuildPaths(configPath, target.Image.BuildConfig)
	return err
}
