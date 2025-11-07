package appconfigloader

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
	"github.com/jinzhu/copier"
	"github.com/knadh/koanf/providers/file"
	"github.com/knadh/koanf/v2"
)

func Load(
	ctx context.Context,
	configPath string,
	targets []string,
	allTargets bool,
) (appConfig config.AppConfig, format string, err error) {
	rawAppConfig, format, err := LoadRawAppConfig(configPath)
	if err != nil {
		return config.AppConfig{}, "", err
	}

	rawAppConfig.Format = format

	if len(rawAppConfig.Targets) > 0 { // is multi target

		if len(targets) == 0 && !allTargets {
			return config.AppConfig{}, "", errors.New("multiple targets available, please specify targets with --targets or use --all")
		}

		if len(targets) > 0 {

			filteredTargets := make(map[string]*config.TargetConfig)
			for _, targetName := range targets {
				if _, exists := rawAppConfig.Targets[targetName]; exists {
					filteredTargets[targetName] = rawAppConfig.Targets[targetName]
				} else {
					return config.AppConfig{}, "", fmt.Errorf("target '%s' not found in configuration", targetName)
				}
			}
			rawAppConfig.Targets = filteredTargets
		}

	} else {
		if len(targets) > 0 || allTargets {
			return config.AppConfig{}, "", errors.New("the --targets and --all flags are not applicable for a single-target configuration file")
		}
	}

	return rawAppConfig, "", nil
}

func MergeImage(targetConfig config.TargetConfig, images map[string]*config.Image, baseImage *config.Image) (*config.Image, error) {
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

func MergeToTarget(appConfig config.AppConfig, targetConfig config.TargetConfig, targetName, format string) (config.TargetConfig, error) {
	var tc config.TargetConfig
	if err := copier.Copy(&tc, &targetConfig); err != nil {
		return config.TargetConfig{}, fmt.Errorf("failed to deep copy target config for merging: %w", err)
	}

	tc.TargetName = targetName
	tc.Format = format

	if tc.Name == "" {
		if appConfig.Name != "" {
			tc.Name = appConfig.Name
		} else {
			tc.Name = targetName
		}
	}

	mergedImage, err := MergeImage(targetConfig, appConfig.Images, appConfig.Image)
	if err != nil {
		return config.TargetConfig{}, fmt.Errorf("failed to resolve image for target '%s': %w", targetName, err)
	}
	tc.Image = mergedImage

	if tc.Server == "" {
		tc.Server = appConfig.Server
	}

	if tc.APIToken == nil {
		tc.APIToken = appConfig.APIToken
	}

	if tc.DeploymentStrategy == "" {
		tc.DeploymentStrategy = appConfig.DeploymentStrategy
	}

	if tc.Domains == nil {
		tc.Domains = appConfig.Domains
	}

	if tc.ACMEEmail == "" {
		tc.ACMEEmail = appConfig.ACMEEmail
	}

	if tc.Env == nil {
		tc.Env = appConfig.Env
	}

	if tc.HealthCheckPath == "" {
		tc.HealthCheckPath = appConfig.HealthCheckPath
	}

	if tc.Port == "" {
		tc.Port = appConfig.Port
	}

	if tc.Replicas == nil {
		tc.Replicas = appConfig.Replicas
	}

	if tc.Network == "" {
		tc.Network = appConfig.Network
	}

	if tc.Volumes == nil {
		tc.Volumes = appConfig.Volumes
	}

	if tc.PreDeploy == nil {
		tc.PreDeploy = appConfig.PreDeploy
	}

	if tc.PostDeploy == nil {
		tc.PostDeploy = appConfig.PostDeploy
	}

	normalizeTargetConfig(&tc)

	return tc, nil
}

// normalizeTargetConfig applies default values to a target config
func normalizeTargetConfig(tc *config.TargetConfig) {
	if tc.Image == nil {
		build := true
		tc.Image = &config.Image{
			Repository: tc.Name,
			Build:      &build,
		}
	}

	if tc.Image.History == nil {
		defaultCount := constants.DefaultDeploymentsToKeep
		tc.Image.History = &config.ImageHistory{
			Strategy: config.HistoryStrategyLocal,
			Count:    &defaultCount,
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
		defaultReplicas := constants.DefaultReplicas
		tc.Replicas = &defaultReplicas
	}
}

func TargetsByServer(targets map[string]config.TargetConfig) map[string][]string {
	servers := make(map[string][]string)
	for targetName, target := range targets {
		servers[target.Server] = append(servers[target.Server], targetName)
	}
	return servers
}

func ExtractTargets(appConfig config.AppConfig, format string) (map[string]config.TargetConfig, error) {
	extractedTargetConfigs := make(map[string]config.TargetConfig)

	if len(appConfig.Targets) > 0 {
		for targetName, target := range appConfig.Targets {
			mergedTargetConfig, err := MergeToTarget(appConfig, *target, targetName, format)
			if err != nil {
				return nil, fmt.Errorf("failed to resolve target '%s': %w", targetName, err)
			}

			if err := mergedTargetConfig.Validate(appConfig.Format); err != nil {
				return nil, fmt.Errorf("validation failed for target '%s': %w", targetName, err)
			}
			extractedTargetConfigs[targetName] = mergedTargetConfig
		}
	} else {
		mergedSingleTargetConfig, err := MergeToTarget(appConfig, config.TargetConfig{}, "", format)
		if err != nil {
			return nil, fmt.Errorf("failed to merge config: %w", err)
		}
		if err := mergedSingleTargetConfig.Validate(appConfig.Format); err != nil {
			return nil, fmt.Errorf("config invalid: %w", err)
		}
		extractedTargetConfigs[appConfig.Name] = mergedSingleTargetConfig
	}

	return extractedTargetConfigs, nil
}

func LoadRawAppConfig(configPath string) (config.AppConfig, string, error) {
	configFile, err := FindConfigFile(configPath)
	if err != nil {
		return config.AppConfig{}, "", err
	}

	format, err := config.GetConfigFormat(configFile)
	if err != nil {
		return config.AppConfig{}, "", err
	}

	parser, err := config.GetConfigParser(format)
	if err != nil {
		return config.AppConfig{}, "", err
	}

	k := koanf.New(".")
	if err := k.Load(file.Provider(configFile), parser); err != nil {
		return config.AppConfig{}, "", fmt.Errorf("failed to load config file: %w", err)
	}

	configKeys := k.Keys()
	appConfigType := reflect.TypeOf(config.AppConfig{})

	if err := config.CheckUnknownFields(appConfigType, configKeys, format); err != nil {
		return config.AppConfig{}, "", err
	}

	var appConfig config.AppConfig
	decoderConfig := &mapstructure.DecoderConfig{
		TagName: format,
		Result:  &appConfig,
		// This ensures that embedded structs with inline tags work properly
		Squash:     true,
		DecodeHook: config.PortDecodeHook(),
	}

	unmarshalConf := koanf.UnmarshalConf{
		Tag:           format,
		DecoderConfig: decoderConfig,
	}

	if err := k.UnmarshalWithConf("", &appConfig, unmarshalConf); err != nil {
		return config.AppConfig{}, "", fmt.Errorf("failed to unmarshal config: %w", err)
	}

	return appConfig, format, nil
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
