package config

import (
	"errors"
	"fmt"
	"regexp"
	"slices"

	"github.com/haloydev/haloy/internal/helpers"
)

func (dc *DeployConfig) Validate() error {
	if len(dc.Targets) > 0 {
		// Multi-target config: check for duplicate names
		names := make(map[string]string) // name -> targetKey
		for targetKey, target := range dc.Targets {
			name := target.Name
			if name == "" {
				name = targetKey // Will use target key as name after merge
			}
			if existingKey, exists := names[name]; exists {
				return fmt.Errorf("duplicate name '%s' found in targets '%s' and '%s'", name, existingKey, targetKey)
			}
			names[name] = targetKey
		}
	} else {
		// Single-target config: require global name
		if dc.Name == "" {
			return errors.New("'name' is required for single-target configurations")
		}
	}
	return nil
}

func (tc *TargetConfig) Validate(format string) error {
	if tc.Name == "" {
		return errors.New("app 'name' is required")
	}

	if tc.Server == "" {
		return errors.New("server is required")
	}

	if !isValidAppName(tc.Name) {
		return fmt.Errorf("invalid app name '%s'; must contain only alphanumeric characters, hyphens, and underscores", tc.Name)
	}

	if tc.Preset != "" {
		validPresets := []Preset{PresetDatabase, PresetService}
		if !slices.Contains(validPresets, tc.Preset) {
			return fmt.Errorf("%s is an invalid preset", tc.Preset)
		}
	}

	if tc.Image != nil && tc.ImageKey != "" {
		return fmt.Errorf("cannot specify both 'image' and 'imageRef' in target config")
	}

	if tc.Image != nil {
		if err := tc.Image.Validate(format); err != nil {
			return fmt.Errorf("invalid image: %w", err)
		}
	}

	if tc.NamingStrategy != "" {
		validNamingStrategies := []NamingStrategy{NamingStrategyDynamic, NamingStrategyStatic}
		if !slices.Contains(validNamingStrategies, tc.NamingStrategy) {
			return fmt.Errorf("%s must be 'dynamic' or 'static', got %s", GetFieldNameForFormat(TargetConfig{}, "NamingStrategy", format), tc.NamingStrategy)
		}
	}

	// We can't use default deployment strategy if we want to use static naming because we can't have two container running with the same name.
	if tc.NamingStrategy == NamingStrategyStatic && tc.DeploymentStrategy != DeploymentStrategyReplace {
		return fmt.Errorf("%s 'static' requires %s 'replace' (you cannot use rolling updates with fixed container names)", GetFieldNameForFormat(TargetConfig{}, "NamingStrategy", format), GetFieldNameForFormat(TargetConfig{}, "DeploymentStrategy", format))
	}

	if tc.NamingStrategy == NamingStrategyStatic && tc.Replicas != nil && *tc.Replicas > 1 {
		return fmt.Errorf("%s 'static' does not support multiple replicas", GetFieldNameForFormat(TargetConfig{}, "NamingStrategy", format))
	}

	if tc.DeploymentStrategy != "" {
		validDeploymentStrategies := []DeploymentStrategy{DeploymentStrategyRolling, DeploymentStrategyReplace}
		if !slices.Contains(validDeploymentStrategies, tc.DeploymentStrategy) {
			return fmt.Errorf("%s must be 'rolling' or 'replace', got '%s'", GetFieldNameForFormat(TargetConfig{}, "DeploymentStrategy", format), tc.DeploymentStrategy)
		}
	}

	// Rolling hands traffic over through the proxy, which only routes by domain.
	// A target without domains has nothing to hand over and no stable address
	// under dynamic container names, so only replace can retire its old containers.
	if len(tc.Domains) == 0 && tc.DeploymentStrategy == DeploymentStrategyRolling {
		field := GetFieldNameForFormat(TargetConfig{}, "DeploymentStrategy", format)
		return fmt.Errorf("%s 'rolling' requires at least one domain: a target without domains has no stable address for a zero-downtime cutover. Set %s 'replace' on this target, or remove the global 'rolling' setting", field, field)
	}

	if len(tc.Domains) > 0 {
		for _, domain := range tc.Domains {
			if err := domain.Validate(); err != nil {
				return err
			}
		}
	}

	for j, envVar := range tc.Env {
		if err := envVar.Validate(format); err != nil {
			return fmt.Errorf("env[%d]: %w", j, err)
		}
	}

	if tc.Port != "" {
		if err := helpers.ValidatePort(tc.Port.String()); err != nil {
			return fmt.Errorf("invalid %s: %w", GetFieldNameForFormat(TargetConfig{}, "Port", format), err)
		}
	}

	for _, volume := range tc.Volumes {
		if _, err := ParseVolumeSpec(volume); err != nil {
			return err
		}
	}

	if len(tc.Ports) > 0 {
		if err := tc.validatePorts(format); err != nil {
			return err
		}
	}

	if tc.HealthCheckPath != "" {
		if tc.HealthCheckPath[0] != '/' {
			return fmt.Errorf("%s must start with a slash", GetFieldNameForFormat(TargetConfig{}, "HealthCheckPath", format))
		}
	}

	if tc.Replicas != nil {
		if int(*tc.Replicas) < 1 {
			return errors.New("replicas must be at least 1")
		}
	}

	if tc.MinReadySeconds != nil {
		if *tc.MinReadySeconds < 0 {
			return fmt.Errorf("%s must be >= 0", GetFieldNameForFormat(TargetConfig{}, "MinReadySeconds", format))
		}
		if *tc.MinReadySeconds > 600 {
			return fmt.Errorf("%s must not exceed 600 (10 minutes)", GetFieldNameForFormat(TargetConfig{}, "MinReadySeconds", format))
		}
	}

	return nil
}

// validatePorts checks the published ports and the settings they constrain.
// A host port can be bound by only one container at a time, so a target that
// publishes ports runs a single container and is replaced on deploy.
func (tc *TargetConfig) validatePorts(format string) error {
	portsField := GetFieldNameForFormat(TargetConfig{}, "Ports", format)

	if _, err := ParsePortSpecs(tc.Ports); err != nil {
		return fmt.Errorf("invalid %s: %w", portsField, err)
	}

	if tc.Network == "host" || tc.Network == "none" {
		return fmt.Errorf("%s cannot be used with network '%s': Docker does not publish ports in that network mode", portsField, tc.Network)
	}

	if tc.Replicas != nil && *tc.Replicas > 1 {
		return fmt.Errorf("%s does not support multiple replicas: every replica would bind the same host ports", portsField)
	}

	if tc.DeploymentStrategy == DeploymentStrategyRolling {
		field := GetFieldNameForFormat(TargetConfig{}, "DeploymentStrategy", format)
		return fmt.Errorf("%s 'rolling' cannot be used with %s: the new container cannot bind a host port the old one still holds. Set %s 'replace' on this target", field, portsField, field)
	}

	return nil
}

func isValidAppName(name string) bool {
	// Only allow alphanumeric, hyphens, and underscores
	// Must start with alphanumeric character
	// This is to satisfy docker container name restrictions
	matched, err := regexp.MatchString(`^[a-zA-Z0-9][a-zA-Z0-9_-]*$`, name)
	if err != nil {
		return false
	}
	return matched
}
