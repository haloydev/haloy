package config

import (
	"errors"
	"fmt"
	"slices"
	"strings"
)

type Image struct {
	Repository   string        `json:"repository" yaml:"repository" toml:"repository"`
	Tag          string        `json:"tag,omitempty" yaml:"tag,omitempty" toml:"tag,omitempty"`
	History      *ImageHistory `json:"history,omitempty" yaml:"history,omitempty" toml:"history,omitempty"`
	RegistryAuth *RegistryAuth `json:"registry,omitempty" yaml:"registry,omitempty" toml:"registry,omitempty"`
	Build        *bool         `json:"build,omitempty" yaml:"build,omitempty" toml:"build,omitempty"`
	BuildConfig  *BuildConfig  `json:"buildConfig,omitempty" yaml:"build_config,omitempty" toml:"build_config,omitempty"`
}

type RegistryAuth struct {
	// Server is optional. If not set, it will be parsed from the Repository field.
	Server   string      `json:"server,omitempty" yaml:"server,omitempty" toml:"server,omitempty"`
	Username ValueSource `json:"username" yaml:"username" toml:"username"`
	Password ValueSource `json:"password" yaml:"password" toml:"password"`
}

func (i *Image) ShouldBuild() bool {
	// Build is explicitly set to false
	if i.Build != nil && !*i.Build {
		return false
	}

	// Build is set true, build regardless if Builder is present
	if i.Build != nil && *i.Build {
		return true
	}

	// Build isn't set, but BuildConfig is present
	return i.BuildConfig != nil
}

func (i *Image) GetEffectivePushStrategy() BuildPushOption {
	if i.BuildConfig != nil && i.BuildConfig.Push != "" {
		return i.BuildConfig.Push
	}

	// Auto-detect based on registry auth presence
	if i.RegistryAuth != nil {
		return BuildPushOptionRegistry
	}

	return BuildPushOptionServer
}

func (i *Image) ImageRef() string {
	repo := strings.TrimSpace(i.Repository)
	tag := strings.TrimSpace(i.Tag)

	// If repository already contains a tag, don't add another one
	if strings.Contains(repo, ":") && tag == "" {
		return repo
	}

	if tag == "" {
		tag = "latest"
	}
	return fmt.Sprintf("%s:%s", repo, tag)
}

func (i *Image) Validate(format string) error {
	if strings.TrimSpace(i.Repository) == "" {
		return fmt.Errorf("image.repository is required")
	}
	if strings.ContainsAny(i.Repository, " \t\n\r") {
		return fmt.Errorf("image.repository '%s' contains whitespace", i.Repository)
	}

	if strings.ContainsAny(i.Tag, " \t\n\r") {
		return fmt.Errorf("image.tag '%s' contains whitespace", i.Tag)
	}

	if i.History != nil {
		if err := i.History.Validate(); err != nil {
			return err
		}

		if i.History.Strategy == HistoryStrategyRegistry {
			// Prevent mutable tags with registry strategy
			tag := strings.TrimSpace(i.Tag)
			if tag == "" || tag == "latest" {
				return fmt.Errorf("image.tag cannot be 'latest' or empty with registry strategy - use immutable tags like 'v1.2.3'")
			}

			mutableTags := []string{"main", "master", "develop", "staging", "production"}
			if slices.Contains(mutableTags, tag) {
				return fmt.Errorf("image.tag '%s' is mutable and not recommended with registry strategy - use immutable tags like 'v1.2.3'", tag)
			}
		}
	}

	if i.RegistryAuth != nil {
		reg := i.RegistryAuth
		// Server is optional; if empty, it will be parsed from Repository at runtime.
		if strings.TrimSpace(reg.Server) != "" && strings.ContainsAny(reg.Server, " \t\n\r") {
			return fmt.Errorf("image.registry.server '%s' contains whitespace", reg.Server)
		}
		if err := reg.Username.Validate(); err != nil {
			return err
		}
		if err := reg.Password.Validate(); err != nil {
			return err
		}
	}

	if i.ShouldBuild() {
		pushStrategy := i.GetEffectivePushStrategy()

		if pushStrategy == BuildPushOptionRegistry && i.RegistryAuth == nil {
			return fmt.Errorf("image.registry authentication required when building with registry push strategy")
		}

		if pushStrategy == BuildPushOptionServer && i.RegistryAuth != nil {
			return fmt.Errorf("image.registry cannot be set when image.%s.push is 'server' - uploaded images don't use registry authentication", GetFieldNameForFormat(Image{}, "BuildConfig", format))
		}
	}

	if i.BuildConfig != nil {
		if err := i.BuildConfig.Validate(format); err != nil {
			return err
		}
	}

	return nil
}

type HistoryStrategy string

const (
	HistoryStrategyLocal    HistoryStrategy = "local"    // Keep images locally (default)
	HistoryStrategyRegistry HistoryStrategy = "registry" // Rely on registry tags
	HistoryStrategyNone     HistoryStrategy = "none"     // No rollback support
)

type ImageHistory struct {
	Strategy HistoryStrategy `json:"strategy" yaml:"strategy" toml:"strategy"`
	Count    *int            `json:"count,omitempty" yaml:"count,omitempty" toml:"count,omitempty"`
	Pattern  string          `json:"pattern,omitempty" yaml:"pattern,omitempty" toml:"pattern,omitempty"`
}

func (h *ImageHistory) Validate() error {
	if h.Strategy != "" {
		validStrategies := []HistoryStrategy{HistoryStrategyLocal, HistoryStrategyRegistry, HistoryStrategyNone}
		if !slices.Contains(validStrategies, h.Strategy) {
			return fmt.Errorf("image.history.strategy '%s' is invalid (must be 'local', 'registry', or 'none')", h.Strategy)
		}
	}

	// Count is required for both local and registry strategies
	if h.Strategy == HistoryStrategyLocal || h.Strategy == HistoryStrategyRegistry {
		if h.Count == nil {
			return fmt.Errorf("image.history.count is required for %s strategy", h.Strategy)
		}
		if *h.Count < 1 {
			return fmt.Errorf("image.history.count must be at least 1 for %s strategy", h.Strategy)
		}
	}

	// Pattern validation for registry strategy
	if h.Strategy == HistoryStrategyRegistry && strings.TrimSpace(h.Pattern) == "" {
		return fmt.Errorf("image.history.pattern is required for registry strategy")
	}

	return nil
}

func (b *BuildConfig) Validate(format string) error {
	if b == nil {
		return nil
	}

	if b.Dockerfile != "" {
		if strings.ContainsAny(b.Dockerfile, " \t\n\r") {
			return fmt.Errorf("dockerfile path '%s' contains whitespace", b.Dockerfile)
		}
	}

	if b.Context != "" {
		if strings.ContainsAny(b.Context, " \t\n\r") {
			return fmt.Errorf("context path '%s' contains whitespace", b.Context)
		}
	}

	if b.Platform != "" {
		if strings.ContainsAny(b.Platform, " \t\n\r") {
			return fmt.Errorf("platform '%s' contains whitespace", b.Platform)
		}
	}

	for i, arg := range b.Args {
		if err := arg.Validate(format); err != nil {
			return fmt.Errorf("args[%d]: %w", i, err)
		}
	}

	if b.Push != "" {
		validPushOptions := []BuildPushOption{BuildPushOptionServer, BuildPushOptionRegistry}
		if !slices.Contains(validPushOptions, b.Push) {
			return fmt.Errorf("builder.push must be 'server' or 'registry', got '%s'", b.Push)
		}
	}

	return nil
}

type BuildPushOption string

const (
	BuildPushOptionServer   BuildPushOption = "server"
	BuildPushOptionRegistry BuildPushOption = "registry"
)

type BuildConfig struct {
	Context    string          `json:"context,omitempty" yaml:"context,omitempty" toml:"context,omitempty"`
	Dockerfile string          `json:"dockerfile,omitempty" yaml:"dockerfile,omitempty" toml:"dockerfile,omitempty"`
	Platform   string          `json:"platform,omitempty" yaml:"platform,omitempty" toml:"platform,omitempty"`
	Args       []BuildArg      `json:"args,omitempty" yaml:"args,omitempty" toml:"args,omitempty"`
	Push       BuildPushOption `json:"push,omitempty" yaml:"push,omitempty" toml:"push,omitempty"`
}

type BuildArg struct {
	Name        string `json:"name" yaml:"name" toml:"name"`
	ValueSource `mapstructure:",squash" json:",inline" yaml:",inline" toml:",inline"`
}

func (ba *BuildArg) Validate(format string) error {
	if ba.Name == "" {
		return errors.New("build argument 'name' cannot be empty")
	}

	if err := ba.ValueSource.Validate(); err != nil {
		return fmt.Errorf("build argument '%s': %w", ba.Name, err)
	}

	return nil
}
