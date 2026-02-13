package configloader

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"

	"github.com/go-viper/mapstructure/v2"
	"github.com/haloydev/haloy/internal/config"
	"github.com/haloydev/haloy/internal/constants"
	"github.com/haloydev/haloy/internal/helpers"
	"github.com/jinzhu/copier"
	"github.com/knadh/koanf/providers/file"
	"github.com/knadh/koanf/v2"
)

func Load(
	ctx context.Context,
	configPath string,
	targets []string,
	allTargets bool,
) (deployConfig config.DeployConfig, format string, err error) {
	rawDeployConfig, format, err := LoadRawDeployConfig(configPath)
	if err != nil {
		return config.DeployConfig{}, "", err
	}

	rawDeployConfig.Format = format

	if len(rawDeployConfig.Targets) > 0 { // is multi target

		if len(targets) == 0 && !allTargets {
			return config.DeployConfig{}, "", errors.New("multiple targets available, please specify targets with --targets or use --all")
		}

		if len(targets) > 0 {
			filteredTargets := make(map[string]*config.TargetConfig)
			for _, targetName := range targets {
				if _, exists := rawDeployConfig.Targets[targetName]; exists {
					filteredTargets[targetName] = rawDeployConfig.Targets[targetName]
				} else {
					return config.DeployConfig{}, "", fmt.Errorf("target '%s' not found in configuration", targetName)
				}
			}
			rawDeployConfig.Targets = filteredTargets
		}

	} else {
		if len(targets) > 0 || allTargets {
			return config.DeployConfig{}, "", errors.New("the --targets and --all flags are not applicable for a single-target configuration file")
		}
	}

	return rawDeployConfig, "", nil
}

func mergeImage(targetConfig config.TargetConfig, images map[string]*config.Image, baseImage *config.Image) (*config.Image, error) {
	// Priority: target.Image > target.ImageKey > base.Image
	if targetConfig.Image != nil {
		// If base image exists, merge the override with the base
		if baseImage != nil {
			merged := *baseImage // Copy base image
			// Override with target's image fields if they are set
			if targetConfig.Image.Repository != "" {
				merged.Repository = targetConfig.Image.Repository
			}
			if targetConfig.Image.Tag != "" {
				merged.Tag = targetConfig.Image.Tag
			}
			if targetConfig.Image.History != nil {
				merged.History = targetConfig.Image.History
			}
			if targetConfig.Image.RegistryAuth != nil {
				merged.RegistryAuth = targetConfig.Image.RegistryAuth
			}
			if targetConfig.Image.Build != nil {
				merged.Build = targetConfig.Image.Build
			}
			if targetConfig.Image.BuildConfig != nil {
				merged.BuildConfig = targetConfig.Image.BuildConfig
			}
			return &merged, nil
		}
		return targetConfig.Image, nil
	}

	if targetConfig.ImageKey != "" {
		if images == nil {
			return nil, fmt.Errorf("imageRef '%s' specified but no images map defined", targetConfig.ImageKey)
		}
		img, exists := images[targetConfig.ImageKey]
		if !exists {
			return nil, fmt.Errorf("imageRef '%s' not found in images map", targetConfig.ImageKey)
		}
		return img, nil
	}

	if baseImage != nil {
		return baseImage, nil
	}

	return nil, nil
}

func mergeEnvArrays(deployConfigEnv, targetConfigEnv []config.EnvVar) []config.EnvVar {
	if len(targetConfigEnv) == 0 {
		return deployConfigEnv
	}

	if len(deployConfigEnv) == 0 {
		return targetConfigEnv
	}

	mergedMap := make(map[string]config.EnvVar)

	for _, envVar := range deployConfigEnv {
		mergedMap[envVar.Name] = envVar
	}

	for _, envVar := range targetConfigEnv {
		mergedMap[envVar.Name] = envVar // override deployConfig if exists
	}

	mergedEnv := make([]config.EnvVar, 0, len(mergedMap))

	// Preserve order defined in deployConfigEnv (base)
	for _, envVar := range deployConfigEnv {
		if mergedEnvVar, exists := mergedMap[envVar.Name]; exists {
			mergedEnv = append(mergedEnv, mergedEnvVar)
			delete(mergedMap, envVar.Name)
		}
	}

	// Add remaining env vars from targetConfigEnv in their original order
	for _, envVar := range targetConfigEnv {
		if mergedEnvVar, exists := mergedMap[envVar.Name]; exists {
			mergedEnv = append(mergedEnv, mergedEnvVar)
			delete(mergedMap, envVar.Name)
		}
	}

	return mergedEnv
}

// MergeToTarget merges the global DeployConfig into a specific TargetConfig.
// The configuration hierarchy is (from highest to lowest specificity):
// 1. Target Config (explicitly set in the 'targets' map)
// 2. Preset Defaults (applied if fields are empty)
// 3. Global DeployConfig (applied if fields are still empty)
func MergeToTarget(deployConfig config.DeployConfig, targetConfig config.TargetConfig, targetName, format string) (config.TargetConfig, error) {
	var tc config.TargetConfig
	if err := copier.Copy(&tc, &targetConfig); err != nil {
		return config.TargetConfig{}, fmt.Errorf("failed to deep copy target config for merging: %w", err)
	}

	tc.TargetName = targetName
	tc.Format = format

	if tc.Name == "" {
		tc.Name = targetName
	}

	if tc.Preset == "" {
		tc.Preset = deployConfig.Preset
	}

	mergedImage, err := mergeImage(tc, deployConfig.Images, deployConfig.Image)
	if err != nil {
		return config.TargetConfig{}, fmt.Errorf("failed to resolve image for target '%s': %w", targetName, err)
	}
	tc.Image = mergedImage

	if tc.Server == "" {
		tc.Server = deployConfig.Server
	}

	if tc.APIToken == nil {
		tc.APIToken = deployConfig.APIToken
	}

	if tc.DeploymentStrategy == "" {
		tc.DeploymentStrategy = deployConfig.DeploymentStrategy
	}

	if tc.NamingStrategy == "" {
		tc.NamingStrategy = deployConfig.NamingStrategy
	}

	if tc.Protected == nil {
		tc.Protected = deployConfig.Protected
	}

	if tc.Domains == nil {
		tc.Domains = deployConfig.Domains
	}

	// Merge Env arrays if the target has an explicit Env block, otherwise inherit (which is handled by copier)
	// Only merge if both base and target have elements. If target.Env is nil (copied from targetConfig, which is nil),
	// it will inherit the base config value. If target.Env is non-nil (meaning it was set explicitly in the target block,
	// even if empty), we proceed to merge with the base.
	if len(targetConfig.Env) > 0 || len(deployConfig.Env) > 0 {
		mergedEnv := mergeEnvArrays(deployConfig.Env, targetConfig.Env)
		tc.Env = mergedEnv
	} else if tc.Env == nil {
		// Fallback to base config if nothing was explicitly set on target
		tc.Env = deployConfig.Env
	}

	if tc.HealthCheckPath == "" {
		tc.HealthCheckPath = deployConfig.HealthCheckPath
	}

	if tc.Port == "" {
		tc.Port = deployConfig.Port
	}

	if tc.Replicas == nil {
		tc.Replicas = deployConfig.Replicas
	}

	if tc.Network == "" {
		tc.Network = deployConfig.Network
	}

	if tc.Volumes == nil {
		tc.Volumes = deployConfig.Volumes
	}

	if tc.PreDeploy == nil {
		tc.PreDeploy = deployConfig.PreDeploy
	}

	if tc.PostDeploy == nil {
		tc.PostDeploy = deployConfig.PostDeploy
	}

	if err := applyPreset(&tc); err != nil {
		return config.TargetConfig{}, err
	}

	normalizeTargetConfig(&tc)

	if err := mergeBuildArgsFromEnv(&tc); err != nil {
		return config.TargetConfig{}, err
	}

	return tc, nil
}

func applyPreset(tc *config.TargetConfig) error {
	switch tc.Preset {
	case "":
		// No preset, nothing to apply
		return nil
	case config.PresetDatabase:
		if tc.DeploymentStrategy == "" {
			tc.DeploymentStrategy = config.DeploymentStrategyReplace
		}
		if tc.NamingStrategy == "" {
			tc.NamingStrategy = config.NamingStrategyStatic
		}

		if tc.Image != nil && tc.Image.History == nil {
			tc.Image.History = &config.ImageHistory{
				Strategy: config.HistoryStrategyNone,
			}
		}

		tc.Protected = helpers.Ptr(true)

	case config.PresetService:
		if tc.DeploymentStrategy == "" {
			tc.DeploymentStrategy = config.DeploymentStrategyReplace
		}
		if tc.NamingStrategy == "" {
			tc.NamingStrategy = config.NamingStrategyStatic
		}

		if tc.Image != nil && tc.Image.History == nil {
			tc.Image.History = &config.ImageHistory{
				Strategy: config.HistoryStrategyNone,
			}
		}

	}
	return nil
}

// normalizeTargetConfig applies default values to a target config
func normalizeTargetConfig(tc *config.TargetConfig) {
	if tc.Server == "" {
		tc.Server = "localhost"
	}

	if tc.Image == nil {
		tc.Image = &config.Image{
			Repository: tc.Name,
			Build:      helpers.Ptr(true),
		}
	}

	if tc.Image.History == nil {
		tc.Image.History = &config.ImageHistory{
			Strategy: config.HistoryStrategyLocal,
			Count:    helpers.Ptr(int(constants.DefaultDeploymentsToKeep)),
		}
	}

	if tc.DeploymentStrategy == "" {
		tc.DeploymentStrategy = config.DeploymentStrategyRolling
	}

	if tc.HealthCheckPath == "" {
		tc.HealthCheckPath = constants.DefaultHealthCheckPath
	}

	if tc.Port == "" {
		tc.Port = config.Port(constants.DefaultContainerPort)
	}

	if tc.Replicas == nil {
		tc.Replicas = helpers.Ptr(constants.DefaultReplicas)
	}
}

// mergeBuildArgsFromEnv expands environment variables marked with BuildArg: true into the image's BuildConfig.Args.
// If a build arg with the same name already exists, the explicit build arg takes precedence (no override).
func mergeBuildArgsFromEnv(tc *config.TargetConfig) error {
	if tc.Image == nil || !tc.Image.ShouldBuild() {
		return nil
	}

	// Collect env vars that should be build args
	var envBuildArgs []config.EnvVar
	for _, env := range tc.Env {
		if env.BuildArg {
			envBuildArgs = append(envBuildArgs, env)
		}
	}

	if len(envBuildArgs) == 0 {
		return nil
	}

	if tc.Image.BuildConfig == nil {
		tc.Image.BuildConfig = &config.BuildConfig{}
	}

	// Build a set of existing build arg names for conflict detection
	existingArgs := make(map[string]struct{})
	for _, arg := range tc.Image.BuildConfig.Args {
		existingArgs[arg.Name] = struct{}{}
	}

	// Add env vars as build args (explicit build args take precedence)
	for _, env := range envBuildArgs {
		if _, exists := existingArgs[env.Name]; exists {
			// Explicit build arg already exists, skip (explicit takes precedence)
			continue
		}

		tc.Image.BuildConfig.Args = append(tc.Image.BuildConfig.Args, config.BuildArg{
			Name:        env.Name,
			ValueSource: env.ValueSource,
		})
	}

	return nil
}

func TargetsByServer(targets map[string]config.TargetConfig) map[string][]string {
	servers := make(map[string][]string)
	for targetName, target := range targets {
		servers[target.Server] = append(servers[target.Server], targetName)
	}
	return servers
}

func ExtractTargets(deployConfig config.DeployConfig, format string) (map[string]config.TargetConfig, error) {
	if err := deployConfig.Validate(); err != nil {
		return nil, err
	}

	extractedTargetConfigs := make(map[string]config.TargetConfig)

	if len(deployConfig.Targets) > 0 {
		for targetName, target := range deployConfig.Targets {
			mergedTargetConfig, err := MergeToTarget(deployConfig, *target, targetName, format)
			if err != nil {
				return nil, fmt.Errorf("failed to resolve target '%s': %w", targetName, err)
			}

			if err := mergedTargetConfig.Validate(deployConfig.Format); err != nil {
				return nil, fmt.Errorf("validation failed for target '%s': %w", targetName, err)
			}
			extractedTargetConfigs[targetName] = mergedTargetConfig
		}
	} else {
		mergedSingleTargetConfig, err := MergeToTarget(deployConfig, deployConfig.TargetConfig, deployConfig.Name, format)
		if err != nil {
			return nil, fmt.Errorf("failed to merge config: %w", err)
		}
		if err := mergedSingleTargetConfig.Validate(deployConfig.Format); err != nil {
			return nil, fmt.Errorf("config invalid: %w", err)
		}
		extractedTargetConfigs[deployConfig.Name] = mergedSingleTargetConfig
	}

	return extractedTargetConfigs, nil
}

func LoadRawDeployConfig(configPath string) (config.DeployConfig, string, error) {
	configFile, err := FindConfigFile(configPath)
	if err != nil {
		return config.DeployConfig{}, "", err
	}

	format, err := config.GetConfigFormat(configFile)
	if err != nil {
		return config.DeployConfig{}, "", err
	}

	parser, err := config.GetConfigParser(format)
	if err != nil {
		return config.DeployConfig{}, "", err
	}

	k := koanf.New(".")
	if err := k.Load(file.Provider(configFile), parser); err != nil {
		return config.DeployConfig{}, "", EnhanceConfigError(configFile, format, err)
	}

	configKeys := k.Keys()
	deployConfigType := reflect.TypeOf(config.DeployConfig{})

	if err := config.CheckUnknownFields(deployConfigType, configKeys, format); err != nil {
		return config.DeployConfig{}, "", err
	}

	var deployConfig config.DeployConfig
	decoderConfig := &mapstructure.DecoderConfig{
		TagName: format,
		Result:  &deployConfig,
		// This ensures that embedded structs with inline tags work properly
		Squash: true,
		DecodeHook: mapstructure.ComposeDecodeHookFunc(
			config.PortDecodeHook(),
			config.ImageDecodeHook(),
		),
	}

	unmarshalConf := koanf.UnmarshalConf{
		Tag:           format,
		DecoderConfig: decoderConfig,
	}

	if err := k.UnmarshalWithConf("", &deployConfig, unmarshalConf); err != nil {
		return config.DeployConfig{}, "", fmt.Errorf("failed to unmarshal config: %w", err)
	}

	return deployConfig, format, nil
}

var (
	supportedExtensions  = []string{".json", ".yaml", ".yml", ".toml"}
	supportedConfigNames = []string{"haloy.json", "haloy.yaml", "haloy.yml", "haloy.toml"}
)

// FindConfigFile finds a haloy config file based on the given path
// It supports:
// - Full path to a config file
// - Directory containing a haloy config file
// - Relative paths
func FindConfigFile(path string) (string, error) {
	// If no path provided, use current directory
	if path == "" {
		path = "."
	}

	// Convert to absolute path
	absPath, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("failed to get absolute path: %w", err)
	}

	// Check if the path exists
	stat, err := os.Stat(absPath)
	if err != nil {
		return "", fmt.Errorf("config file not found in path '%s'", absPath)
	}

	// If it's a file, validate it's a supported extension
	if !stat.IsDir() {
		ext := filepath.Ext(absPath)
		if !slices.Contains(supportedExtensions, ext) {
			return "", fmt.Errorf("file %s is not a valid haloy config file (must be .json, .yaml, .yml, or .toml)", absPath)
		}
		return absPath, nil
	}

	// If it's a directory, look for haloy config files
	for _, configName := range supportedConfigNames {
		configPath := filepath.Join(absPath, configName)
		if _, err := os.Stat(configPath); err == nil {
			return configPath, nil
		}
	}

	// Get the directory name for the error (use base name if path is ".")
	dirName := path
	if path == "." {
		if cwd, err := os.Getwd(); err == nil {
			dirName = filepath.Base(cwd)
		}
	}

	return "", fmt.Errorf("no haloy config file found in directory %s (looking for: %s)",
		dirName, strings.Join(supportedConfigNames, ", "))
}
