package configloader

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/haloydev/haloy/internal/config"
)

func TestMergeToTarget(t *testing.T) {
	defaultReplicas := 2
	overrideReplicas := 5
	defaultCount := 10

	baseDeployConfig := config.DeployConfig{
		TargetConfig: config.TargetConfig{
			Name: "myapp",
			Image: &config.Image{
				Repository: "nginx",
				Tag:        "1.20",
			},
			Server:          "default.haloy.dev",
			HealthCheckPath: "/health",
			Port:            "8080",
			Replicas:        &defaultReplicas,
			Network:         "bridge",
			Volumes:         []string{"/host:/container"},
			PreDeploy:       []string{"echo 'pre'"},
			PostDeploy:      []string{"echo 'post'"},
		},
	}

	tests := []struct {
		name            string
		deployConfig    config.DeployConfig
		targetConfig    config.TargetConfig
		targetName      string
		expectedName    string
		expectedServer  string
		expectedImage   config.Image
		expectedBuild   *bool
		expectedEnv     []config.EnvVar
		expectNilTarget bool
	}{
		{
			name:           "empty target config inherits from base",
			deployConfig:   baseDeployConfig,
			targetConfig:   config.TargetConfig{},
			targetName:     "test-target",
			expectedName:   "test-target",
			expectedServer: "default.haloy.dev",
			expectedImage:  *baseDeployConfig.Image,
		},
		{
			name:         "override server only",
			deployConfig: baseDeployConfig,
			targetConfig: config.TargetConfig{
				Server: "override.haloy.dev",
			},
			targetName:     "test-target",
			expectedName:   "test-target",
			expectedServer: "override.haloy.dev",
			expectedImage:  *baseDeployConfig.Image,
		},
		{
			name:         "override image repository and tag",
			deployConfig: baseDeployConfig,
			targetConfig: config.TargetConfig{
				Image: &config.Image{
					Repository: "custom-nginx",
					Tag:        "1.21",
				},
			},
			targetName:     "test-target",
			expectedName:   "test-target",
			expectedServer: "default.haloy.dev",
			expectedImage: config.Image{
				Repository: "custom-nginx",
				Tag:        "1.21",
			},
		},
		{
			name:         "override all fields",
			deployConfig: baseDeployConfig,
			targetConfig: config.TargetConfig{
				Image: &config.Image{
					Repository: "apache",
					Tag:        "2.4",
				},
				Server:          "prod.haloy.dev",
				HealthCheckPath: "/status",
				Port:            "9090",
				Replicas:        &overrideReplicas,
				Network:         "host",
				Volumes:         []string{"/prod/host:/prod/container"},
				PreDeploy:       []string{"echo 'prod pre'"},
				PostDeploy:      []string{"echo 'prod post'"},
			},
			targetName:     "test-target",
			expectedName:   "test-target",
			expectedServer: "prod.haloy.dev",
			expectedImage: config.Image{
				Repository: "apache",
				Tag:        "2.4",
			},
		},
		{
			name:         "override with image history",
			deployConfig: baseDeployConfig,
			targetConfig: config.TargetConfig{
				Image: &config.Image{
					History: &config.ImageHistory{
						Strategy: config.HistoryStrategyRegistry,
						Count:    &defaultCount,
						Pattern:  "v*",
					},
				},
			},
			targetName:     "test-target",
			expectedName:   "test-target",
			expectedServer: "default.haloy.dev",
			expectedImage: config.Image{
				Repository: "nginx", // Base repository
				Tag:        "1.20",  // Base tag
				History: &config.ImageHistory{
					Strategy: config.HistoryStrategyRegistry,
					Count:    &defaultCount,
					Pattern:  "v*",
				},
			},
			expectedBuild: new(false),
		},
		{
			name:         "override with registry auth",
			deployConfig: baseDeployConfig,
			targetConfig: config.TargetConfig{
				Image: &config.Image{
					RegistryAuth: &config.RegistryAuth{
						Server:   "private.registry.com",
						Username: config.ValueSource{Value: "user"},
						Password: config.ValueSource{Value: "pass"},
					},
				},
			},
			targetName:     "test-target",
			expectedName:   "test-target",
			expectedServer: "default.haloy.dev",
			expectedImage: config.Image{
				Repository: "nginx", // Base repository
				Tag:        "1.20",  // Base tag
				RegistryAuth: &config.RegistryAuth{
					Server:   "private.registry.com",
					Username: config.ValueSource{Value: "user"},
					Password: config.ValueSource{Value: "pass"},
				},
			},
		},
		{
			name:         "override with domains",
			deployConfig: baseDeployConfig,
			targetConfig: config.TargetConfig{
				Domains: []config.Domain{
					{Canonical: "prod.example.com", Aliases: []string{"www.prod.example.com"}},
				},
			},
			targetName:     "test-target",
			expectedName:   "test-target",
			expectedServer: "default.haloy.dev",
		},
		{
			name:         "override with env vars",
			deployConfig: baseDeployConfig,
			targetConfig: config.TargetConfig{
				Env: []config.EnvVar{
					{Name: "ENV", ValueSource: config.ValueSource{Value: "production"}},
				},
			},
			targetName:     "test-target",
			expectedName:   "test-target",
			expectedServer: "default.haloy.dev",
		},
		{
			name: "target name used when no name in base or target",
			deployConfig: config.DeployConfig{
				TargetConfig: config.TargetConfig{
					Image: &config.Image{
						Repository: "nginx",
						Tag:        "latest",
					},
					Server: "test.haloy.dev",
				},
			},
			targetConfig:   config.TargetConfig{},
			targetName:     "my-target",
			expectedName:   "my-target",
			expectedServer: "test.haloy.dev",
		},
		{
			name: "target key used as name even when global name exists",
			deployConfig: config.DeployConfig{
				TargetConfig: config.TargetConfig{
					Name: "global-name",
					Image: &config.Image{
						Repository: "nginx",
						Tag:        "latest",
					},
					Server: "test.haloy.dev",
				},
			},
			targetConfig:   config.TargetConfig{},
			targetName:     "my-target-key",
			expectedName:   "my-target-key",
			expectedServer: "test.haloy.dev",
		},
		{
			name: "target name overrides base name",
			deployConfig: config.DeployConfig{
				TargetConfig: config.TargetConfig{
					Name: "base-name",
					Image: &config.Image{
						Repository: "nginx",
						Tag:        "latest",
					},
					Server: "test.haloy.dev",
				},
			},
			targetConfig: config.TargetConfig{
				Name: "target-override-name",
			},
			targetName:     "my-target",
			expectedName:   "target-override-name",
			expectedServer: "test.haloy.dev",
		},
		{
			name: "merge env with override and new item",
			deployConfig: config.DeployConfig{
				TargetConfig: config.TargetConfig{
					Name: "myapp",
					Image: &config.Image{
						Repository: "base-repo",
						Tag:        "base-tag",
					},
					Server: "default.haloy.dev",
					Env: []config.EnvVar{ // Base Env (ORDER: A, B)
						{Name: "VAR_A", ValueSource: config.ValueSource{Value: "base-value-A"}},
						{Name: "VAR_B", ValueSource: config.ValueSource{Value: "base-value-B"}},
					},
				},
			},
			targetConfig: config.TargetConfig{
				Server: "override.haloy.dev",
				Env: []config.EnvVar{ // Target Env (ORDER: C, A)
					{Name: "VAR_C", ValueSource: config.ValueSource{Value: "target-value-C"}}, // New
					{Name: "VAR_A", ValueSource: config.ValueSource{Value: "target-value-A"}}, // Override
				},
			},
			targetName:     "test-target",
			expectedName:   "test-target",
			expectedServer: "override.haloy.dev",
			expectedImage: config.Image{
				Repository: "base-repo",
				Tag:        "base-tag",
			},
			expectedEnv: []config.EnvVar{ // Expected Final Env (ORDER: A, B, C)
				{Name: "VAR_A", ValueSource: config.ValueSource{Value: "target-value-A"}}, // Overridden, kept base position (1st)
				{Name: "VAR_B", ValueSource: config.ValueSource{Value: "base-value-B"}},   // Inherited, kept base position (2nd)
				{Name: "VAR_C", ValueSource: config.ValueSource{Value: "target-value-C"}}, // New, appended last (preserved target's internal order relative to other new items)
			},
		},
		{
			name: "merge env with all new items preserves target order",
			deployConfig: config.DeployConfig{
				TargetConfig: config.TargetConfig{
					Name: "myapp",
					Image: &config.Image{
						Repository: "base-repo",
						Tag:        "base-tag",
					},
					Server: "default.haloy.dev",
					Env: []config.EnvVar{
						{Name: "VAR_A", ValueSource: config.ValueSource{Value: "base-value-A"}},
					},
				},
			},
			targetConfig: config.TargetConfig{
				Env: []config.EnvVar{
					{Name: "VAR_C", ValueSource: config.ValueSource{Value: "target-value-C"}},
					{Name: "VAR_B", ValueSource: config.ValueSource{Value: "target-value-B"}},
					{Name: "VAR_D", ValueSource: config.ValueSource{Value: "target-value-D"}},
				},
			},
			targetName:     "test-target",
			expectedName:   "test-target",
			expectedServer: "default.haloy.dev",
			expectedImage: config.Image{
				Repository: "base-repo",
				Tag:        "base-tag",
			},
			expectedEnv: []config.EnvVar{
				{Name: "VAR_A", ValueSource: config.ValueSource{Value: "base-value-A"}},   // From base
				{Name: "VAR_C", ValueSource: config.ValueSource{Value: "target-value-C"}}, // New, in target order
				{Name: "VAR_B", ValueSource: config.ValueSource{Value: "target-value-B"}}, // New, in target order
				{Name: "VAR_D", ValueSource: config.ValueSource{Value: "target-value-D"}}, // New, in target order
			},
		},
		{
			name: "preset service applies defaults",
			deployConfig: config.DeployConfig{
				TargetConfig: config.TargetConfig{
					Image: &config.Image{Repository: "nginx"},
				},
			},
			targetConfig: config.TargetConfig{
				Preset: config.PresetService,
			},
			targetName:     "service-target",
			expectedName:   "service-target",
			expectedServer: "localhost",
			expectedImage: config.Image{
				Repository: "nginx",
				History: &config.ImageHistory{
					Strategy: config.HistoryStrategyNone,
				},
			},
		},
		{
			name: "preset database applies defaults",
			deployConfig: config.DeployConfig{
				TargetConfig: config.TargetConfig{
					Image: &config.Image{Repository: "postgres"},
				},
			},
			targetConfig: config.TargetConfig{
				Preset: config.PresetDatabase,
			},
			targetName:     "db-target",
			expectedName:   "db-target",
			expectedServer: "localhost",
			expectedImage: config.Image{
				Repository: "postgres",
				History: &config.ImageHistory{
					Strategy: config.HistoryStrategyNone,
				},
			},
		},
		{
			name: "preset does not override explicit values",
			deployConfig: config.DeployConfig{
				TargetConfig: config.TargetConfig{
					Image: &config.Image{Repository: "nginx"},
				},
			},
			targetConfig: config.TargetConfig{
				Preset: config.PresetService,
				Image: &config.Image{
					History: &config.ImageHistory{
						Strategy: config.HistoryStrategyLocal,
					},
				},
			},
			targetName:     "explicit-override",
			expectedName:   "explicit-override",
			expectedServer: "localhost",
			expectedImage: config.Image{
				Repository: "nginx",
				History: &config.ImageHistory{
					Strategy: config.HistoryStrategyLocal,
				},
			},
		},
		{
			name: "regression: single target with preset and image in base",
			deployConfig: config.DeployConfig{
				TargetConfig: config.TargetConfig{
					Preset: config.PresetService,
					Server: "api.example.com",
					Image:  &config.Image{Repository: "nginx"},
				},
			},
			targetConfig:   config.TargetConfig{},
			targetName:     "regression-target",
			expectedName:   "regression-target",
			expectedServer: "api.example.com",
			expectedImage: config.Image{
				Repository: "nginx",
				History: &config.ImageHistory{
					Strategy: config.HistoryStrategyNone,
				},
			},
		},
		{
			name: "preset with imageKey should respect resolved image",
			deployConfig: config.DeployConfig{
				Images: map[string]*config.Image{
					"my-img": {Repository: "resolved-repo"},
				},
			},
			targetConfig: config.TargetConfig{
				Preset:   config.PresetService,
				ImageKey: "my-img",
			},
			targetName:     "key-target",
			expectedName:   "key-target",
			expectedServer: "localhost",
			expectedImage: config.Image{
				Repository: "resolved-repo",
				History: &config.ImageHistory{
					Strategy: config.HistoryStrategyNone,
				},
			},
		},
		{
			name:         "partial image with build_config defaults repository to target name",
			deployConfig: config.DeployConfig{},
			targetConfig: config.TargetConfig{
				Image: &config.Image{
					BuildConfig: &config.BuildConfig{
						Dockerfile: "Dockerfile.prod",
					},
				},
			},
			targetName:     "my-service",
			expectedName:   "my-service",
			expectedServer: "localhost",
			expectedImage: config.Image{
				Repository: "my-service",
			},
			expectedBuild: new(true),
		},
		{
			name:         "partial image with only tag defaults repository to target name",
			deployConfig: config.DeployConfig{},
			targetConfig: config.TargetConfig{
				Image: &config.Image{
					Tag: "v2.0",
				},
			},
			targetName:     "my-app",
			expectedName:   "my-app",
			expectedServer: "localhost",
			expectedImage: config.Image{
				Repository: "my-app",
				Tag:        "v2.0",
			},
			expectedBuild: new(true),
		},
		{
			name:         "partial image with only history defaults repository to target name and keeps local build",
			deployConfig: config.DeployConfig{},
			targetConfig: config.TargetConfig{
				Image: &config.Image{
					History: &config.ImageHistory{
						Strategy: config.HistoryStrategyNone,
					},
				},
			},
			targetName:     "my-app",
			expectedName:   "my-app",
			expectedServer: "localhost",
			expectedImage: config.Image{
				Repository: "my-app",
				History: &config.ImageHistory{
					Strategy: config.HistoryStrategyNone,
				},
			},
			expectedBuild: new(true),
		},
		{
			name:         "partial image history count overrides default local history and keeps local build",
			deployConfig: config.DeployConfig{},
			targetConfig: config.TargetConfig{
				Image: &config.Image{
					History: &config.ImageHistory{
						Count: new(3),
					},
				},
			},
			targetName:     "my-app",
			expectedName:   "my-app",
			expectedServer: "localhost",
			expectedImage: config.Image{
				Repository: "my-app",
				History: &config.ImageHistory{
					Strategy: config.HistoryStrategyLocal,
					Count:    new(3),
				},
			},
			expectedBuild: new(true),
		},
		{
			name: "base partial image with only history defaults repository to target name and keeps local build",
			deployConfig: config.DeployConfig{
				TargetConfig: config.TargetConfig{
					Image: &config.Image{
						History: &config.ImageHistory{
							Strategy: config.HistoryStrategyNone,
						},
					},
				},
			},
			targetConfig:   config.TargetConfig{},
			targetName:     "my-app",
			expectedName:   "my-app",
			expectedServer: "localhost",
			expectedImage: config.Image{
				Repository: "my-app",
				History: &config.ImageHistory{
					Strategy: config.HistoryStrategyNone,
				},
			},
			expectedBuild: new(true),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := MergeToTarget(tt.deployConfig, tt.targetConfig, tt.targetName, "yaml")
			if err != nil {
				t.Fatalf("MergeToTarget() unexpected error = %v", err)
			}

			if result.Name != tt.expectedName {
				t.Errorf("MergeToTarget() Name = %s, expected %s", result.Name, tt.expectedName)
			}

			if result.Server != tt.expectedServer {
				t.Errorf("MergeToTarget() Server = %s, expected %s", result.Server, tt.expectedServer)
			}

			if result.MinReadySeconds == nil {
				t.Errorf("MergeToTarget() MinReadySeconds should be normalized to default value")
			}

			if result.TargetName != tt.targetName {
				t.Errorf("MergeToTarget() TargetName = %s, expected %s", result.TargetName, tt.targetName)
			}

			if tt.expectedImage.Repository != "" {
				if result.Image.Repository != tt.expectedImage.Repository {
					t.Errorf("MergeToTarget() Image.Repository = %s, expected %s",
						result.Image.Repository, tt.expectedImage.Repository)
				}
				if result.Image.Tag != tt.expectedImage.Tag {
					t.Errorf("MergeToTarget() Image.Tag = %s, expected %s",
						result.Image.Tag, tt.expectedImage.Tag)
				}
				if tt.expectedBuild != nil && result.Image.ShouldBuild() != *tt.expectedBuild {
					t.Errorf("MergeToTarget() Image.ShouldBuild() = %t, expected %t",
						result.Image.ShouldBuild(), *tt.expectedBuild)
				}
				if tt.expectedImage.History != nil {
					if result.Image.History == nil {
						t.Errorf("MergeToTarget() Image.History should not be nil")
					} else {
						if result.Image.History.Strategy != tt.expectedImage.History.Strategy {
							t.Errorf("MergeToTarget() Image.History.Strategy = %s, expected %s",
								result.Image.History.Strategy, tt.expectedImage.History.Strategy)
						}
						if tt.expectedImage.History.Count != nil {
							if result.Image.History.Count == nil {
								t.Errorf("MergeToTarget() Image.History.Count should not be nil")
							} else if *result.Image.History.Count != *tt.expectedImage.History.Count {
								t.Errorf("MergeToTarget() Image.History.Count = %d, expected %d",
									*result.Image.History.Count, *tt.expectedImage.History.Count)
							}
						}
						if result.Image.History.Pattern != tt.expectedImage.History.Pattern {
							t.Errorf("MergeToTarget() Image.History.Pattern = %s, expected %s",
								result.Image.History.Pattern, tt.expectedImage.History.Pattern)
						}
					}
				}
				if tt.expectedImage.RegistryAuth != nil {
					if result.Image.RegistryAuth == nil {
						t.Errorf("MergeToTarget() Image.RegistryAuth should not be nil")
					} else {
						if result.Image.RegistryAuth.Server != tt.expectedImage.RegistryAuth.Server {
							t.Errorf("MergeToTarget() Image.RegistryAuth.Server = %s, expected %s",
								result.Image.RegistryAuth.Server, tt.expectedImage.RegistryAuth.Server)
						}
					}
				}
			}

			if len(tt.expectedEnv) > 0 {
				if len(result.Env) != len(tt.expectedEnv) {
					t.Errorf("MergeToTarget() Env length mismatch. Got %d, expected %d. Got: %v", len(result.Env), len(tt.expectedEnv), result.Env)
				} else {
					for i, expectedEnvVar := range tt.expectedEnv {
						actualEnvVar := result.Env[i]
						if actualEnvVar.Name != expectedEnvVar.Name || actualEnvVar.ValueSource.Value != expectedEnvVar.ValueSource.Value {
							t.Errorf("MergeToTarget() Env[%d] mismatch. Got %+v, Expected %+v", i, actualEnvVar, expectedEnvVar)
						}
					}
				}
			}

			// Test that normalization was applied
			if result.HealthCheckPath == "" {
				t.Errorf("MergeToTarget() HealthCheckPath should be normalized to default value")
			}
			if result.Port == "" {
				t.Errorf("MergeToTarget() Port should be normalized to default value")
			}
			if result.Replicas == nil {
				t.Errorf("MergeToTarget() Replicas should be normalized to default value")
			}
		})
	}
}

func TestMergeImage(t *testing.T) {
	baseImage := &config.Image{
		Repository: "nginx",
		Tag:        "1.20",
		History: &config.ImageHistory{
			Strategy: config.HistoryStrategyLocal,
			Count:    new(5),
		},
	}

	images := map[string]*config.Image{
		"web": {
			Repository: "apache",
			Tag:        "2.4",
		},
		"api": {
			Repository: "node",
			Tag:        "16",
		},
	}

	tests := []struct {
		name         string
		targetConfig config.TargetConfig
		images       map[string]*config.Image
		baseImage    *config.Image
		expected     *config.Image
		expectError  bool
		errMsg       string
	}{
		{
			name: "target image fields override base, unset fields kept",
			targetConfig: config.TargetConfig{
				Image: &config.Image{
					Repository: "custom",
					Tag:        "latest",
				},
			},
			images:    images,
			baseImage: baseImage,
			expected: &config.Image{
				Repository: "custom",
				Tag:        "latest",
				History: &config.ImageHistory{
					Strategy: config.HistoryStrategyLocal,
					Count:    new(5),
				},
			},
		},
		{
			name: "target image merges with base",
			targetConfig: config.TargetConfig{
				Image: &config.Image{
					Tag:        "1.21", // Only override tag
					PullPolicy: config.PullPolicyIfMissing,
				},
			},
			images:    images,
			baseImage: baseImage,
			expected: &config.Image{
				Repository: "nginx", // From base
				Tag:        "1.21",  // Overridden
				PullPolicy: config.PullPolicyIfMissing,
				History: &config.ImageHistory{
					Strategy: config.HistoryStrategyLocal,
					Count:    new(5),
				},
			},
		},
		{
			name: "imageKey resolves to images map",
			targetConfig: config.TargetConfig{
				ImageKey: "web",
			},
			images:    images,
			baseImage: baseImage,
			expected: &config.Image{
				Repository: "apache",
				Tag:        "2.4",
			},
		},
		{
			name: "imageKey not found in images map",
			targetConfig: config.TargetConfig{
				ImageKey: "nonexistent",
			},
			images:      images,
			baseImage:   baseImage,
			expectError: true,
			errMsg:      "imageRef 'nonexistent' not found in images map",
		},
		{
			name: "imageKey with nil images map",
			targetConfig: config.TargetConfig{
				ImageKey: "web",
			},
			images:      nil,
			baseImage:   baseImage,
			expectError: true,
			errMsg:      "imageRef 'web' specified but no images map defined",
		},
		{
			name:         "fallback to base image",
			targetConfig: config.TargetConfig{},
			images:       images,
			baseImage:    baseImage,
			expected:     baseImage,
		},
		{
			name:         "no image specified returns nil",
			targetConfig: config.TargetConfig{},
			images:       images,
			baseImage:    nil,
			expected:     nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := mergeImage(tt.targetConfig, tt.images, tt.baseImage)

			if tt.expectError {
				if err == nil {
					t.Errorf("mergeImage() expected error but got none")
				} else if tt.errMsg != "" && !strings.Contains(err.Error(), tt.errMsg) {
					t.Errorf("mergeImage() error = %v, expected to contain %v", err, tt.errMsg)
				}
			} else {
				if err != nil {
					t.Fatalf("mergeImage() unexpected error = %v", err)
				}
				if tt.expected == nil {
					if result != nil {
						t.Fatalf("mergeImage() = %+v, want nil", result)
					}
					return
				}
				if result == nil {
					t.Fatal("mergeImage() = nil, want image")
				}
				if result.Repository != tt.expected.Repository {
					t.Errorf("mergeImage() Repository = %s, expected %s",
						result.Repository, tt.expected.Repository)
				}
				if result.Tag != tt.expected.Tag {
					t.Errorf("mergeImage() Tag = %s, expected %s",
						result.Tag, tt.expected.Tag)
				}
				if result.PullPolicy != tt.expected.PullPolicy {
					t.Errorf("mergeImage() PullPolicy = %s, expected %s",
						result.PullPolicy, tt.expected.PullPolicy)
				}
				if tt.expected.History != nil {
					if result.History == nil {
						t.Errorf("mergeImage() History should not be nil")
					} else if result.History.Strategy != tt.expected.History.Strategy {
						t.Errorf("mergeImage() History.Strategy = %s, expected %s",
							result.History.Strategy, tt.expected.History.Strategy)
					}
				}
			}
		})
	}
}

func TestExtractTargets(t *testing.T) {
	tests := []struct {
		name         string
		deployConfig config.DeployConfig
		expectError  bool
		errMsg       string
		expectCount  int
	}{
		{
			name: "single target config",
			deployConfig: config.DeployConfig{
				TargetConfig: config.TargetConfig{
					Name: "myapp",
					Image: &config.Image{
						Repository: "nginx",
						Tag:        "latest",
					},
					Server: "test.haloy.dev",
				},
			},
			expectCount: 1,
		},
		{
			name: "multi target config",
			deployConfig: config.DeployConfig{
				TargetConfig: config.TargetConfig{
					Name: "myapp",
					Image: &config.Image{
						Repository: "nginx",
						Tag:        "latest",
					},
					Server: "default.haloy.dev",
				},
				Targets: map[string]*config.TargetConfig{
					"prod": {
						Server: "prod.haloy.dev",
					},
					"staging": {
						Server: "staging.haloy.dev",
					},
				},
			},
			expectCount: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := ExtractTargets(tt.deployConfig, "yaml")
			if err != nil {
				t.Errorf("ExtractTargets() unexpected error = %v", err)
			}
			if len(result) != tt.expectCount {
				t.Errorf("ExtractTargets() result count = %d, expected %d", len(result), tt.expectCount)
			}
		})
	}
}

func TestExpandBuildArgsFromEnv(t *testing.T) {
	tests := []struct {
		name              string
		targetConfig      config.TargetConfig
		expectedBuildArgs []config.BuildArg
	}{
		{
			name: "env with build_arg true expands to build args",
			targetConfig: config.TargetConfig{
				Image: &config.Image{
					Repository: "myapp",
					Build:      new(true),
				},
				Env: []config.EnvVar{
					{Name: "NODE_ENV", ValueSource: config.ValueSource{Value: "production"}, BuildArg: true},
					{Name: "API_URL", ValueSource: config.ValueSource{Value: "https://api.example.com"}, BuildArg: true},
					{Name: "RUNTIME_ONLY", ValueSource: config.ValueSource{Value: "secret"}},
				},
			},
			expectedBuildArgs: []config.BuildArg{
				{Name: "NODE_ENV", ValueSource: config.ValueSource{Value: "production"}},
				{Name: "API_URL", ValueSource: config.ValueSource{Value: "https://api.example.com"}},
			},
		},
		{
			name: "no build_arg true means no expansion",
			targetConfig: config.TargetConfig{
				Image: &config.Image{
					Repository: "myapp",
					Build:      new(true),
				},
				Env: []config.EnvVar{
					{Name: "NODE_ENV", ValueSource: config.ValueSource{Value: "production"}},
					{Name: "API_URL", ValueSource: config.ValueSource{Value: "https://api.example.com"}},
				},
			},
			expectedBuildArgs: nil,
		},
		{
			name: "explicit build arg takes precedence over env with build_arg",
			targetConfig: config.TargetConfig{
				Image: &config.Image{
					Repository: "myapp",
					Build:      new(true),
					BuildConfig: &config.BuildConfig{
						Args: []config.BuildArg{
							{Name: "NODE_ENV", ValueSource: config.ValueSource{Value: "development"}},
						},
					},
				},
				Env: []config.EnvVar{
					{Name: "NODE_ENV", ValueSource: config.ValueSource{Value: "production"}, BuildArg: true},
					{Name: "API_URL", ValueSource: config.ValueSource{Value: "https://api.example.com"}, BuildArg: true},
				},
			},
			expectedBuildArgs: []config.BuildArg{
				{Name: "NODE_ENV", ValueSource: config.ValueSource{Value: "development"}},            // Explicit takes precedence
				{Name: "API_URL", ValueSource: config.ValueSource{Value: "https://api.example.com"}}, // Added from env
			},
		},
		{
			name: "build_arg with from source reference",
			targetConfig: config.TargetConfig{
				Image: &config.Image{
					Repository: "myapp",
					Build:      new(true),
				},
				Env: []config.EnvVar{
					{
						Name: "DB_PASSWORD",
						ValueSource: config.ValueSource{
							From: &config.SourceReference{Secret: "db-password"},
						},
						BuildArg: true,
					},
				},
			},
			expectedBuildArgs: []config.BuildArg{
				{
					Name: "DB_PASSWORD",
					ValueSource: config.ValueSource{
						From: &config.SourceReference{Secret: "db-password"},
					},
				},
			},
		},
		{
			name: "no expansion when image should not build",
			targetConfig: config.TargetConfig{
				Image: &config.Image{
					Repository: "myapp",
					Build:      new(false),
				},
				Env: []config.EnvVar{
					{Name: "NODE_ENV", ValueSource: config.ValueSource{Value: "production"}, BuildArg: true},
				},
			},
			expectedBuildArgs: nil,
		},
		{
			name: "no expansion when image is nil",
			targetConfig: config.TargetConfig{
				Image: nil,
				Env: []config.EnvVar{
					{Name: "NODE_ENV", ValueSource: config.ValueSource{Value: "production"}, BuildArg: true},
				},
			},
			expectedBuildArgs: nil,
		},
		{
			name: "creates build config when needed",
			targetConfig: config.TargetConfig{
				Image: &config.Image{
					Repository:  "myapp",
					Build:       new(true),
					BuildConfig: nil, // No build config initially
				},
				Env: []config.EnvVar{
					{Name: "NODE_ENV", ValueSource: config.ValueSource{Value: "production"}, BuildArg: true},
				},
			},
			expectedBuildArgs: []config.BuildArg{
				{Name: "NODE_ENV", ValueSource: config.ValueSource{Value: "production"}},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tc := tt.targetConfig
			err := mergeBuildArgsFromEnv(&tc)
			if err != nil {
				t.Fatalf("expandBuildArgsFromEnv() unexpected error = %v", err)
			}

			// Check build args
			var actualArgs []config.BuildArg
			if tc.Image != nil && tc.Image.BuildConfig != nil {
				actualArgs = tc.Image.BuildConfig.Args
			}

			if len(actualArgs) != len(tt.expectedBuildArgs) {
				t.Errorf("expandBuildArgsFromEnv() args count = %d, expected %d. Got: %+v",
					len(actualArgs), len(tt.expectedBuildArgs), actualArgs)
				return
			}

			for i, expected := range tt.expectedBuildArgs {
				actual := actualArgs[i]
				if actual.Name != expected.Name {
					t.Errorf("expandBuildArgsFromEnv() arg[%d].Name = %s, expected %s", i, actual.Name, expected.Name)
				}
				if actual.ValueSource.Value != expected.ValueSource.Value {
					t.Errorf("expandBuildArgsFromEnv() arg[%d].Value = %s, expected %s", i, actual.ValueSource.Value, expected.ValueSource.Value)
				}
				if (actual.ValueSource.From == nil) != (expected.ValueSource.From == nil) {
					t.Errorf("expandBuildArgsFromEnv() arg[%d].From nil mismatch", i)
				}
				if actual.ValueSource.From != nil && expected.ValueSource.From != nil {
					if actual.ValueSource.From.Secret != expected.ValueSource.From.Secret {
						t.Errorf("expandBuildArgsFromEnv() arg[%d].From.Secret = %s, expected %s",
							i, actual.ValueSource.From.Secret, expected.ValueSource.From.Secret)
					}
				}
			}
		})
	}
}

func TestMergeToTargetWithBuildArgExpansion(t *testing.T) {
	// Integration test to ensure build arg expansion happens during MergeToTarget
	deployConfig := config.DeployConfig{
		TargetConfig: config.TargetConfig{
			Name: "myapp",
			Image: &config.Image{
				Repository: "myapp",
				Build:      new(true),
			},
			Server: "test.haloy.dev",
			Env: []config.EnvVar{
				{Name: "SHARED_VAR", ValueSource: config.ValueSource{Value: "shared-value"}, BuildArg: true},
				{Name: "RUNTIME_ONLY", ValueSource: config.ValueSource{Value: "runtime-value"}},
			},
		},
	}

	result, err := MergeToTarget(deployConfig, config.TargetConfig{}, "test-target", "yaml")
	if err != nil {
		t.Fatalf("MergeToTarget() unexpected error = %v", err)
	}

	// Verify build arg was expanded
	if result.Image.BuildConfig == nil {
		t.Fatal("MergeToTarget() expected BuildConfig to be created")
	}

	if len(result.Image.BuildConfig.Args) != 1 {
		t.Errorf("MergeToTarget() expected 1 build arg, got %d", len(result.Image.BuildConfig.Args))
	}

	if result.Image.BuildConfig.Args[0].Name != "SHARED_VAR" {
		t.Errorf("MergeToTarget() expected build arg name 'SHARED_VAR', got '%s'", result.Image.BuildConfig.Args[0].Name)
	}

	if result.Image.BuildConfig.Args[0].ValueSource.Value != "shared-value" {
		t.Errorf("MergeToTarget() expected build arg value 'shared-value', got '%s'", result.Image.BuildConfig.Args[0].ValueSource.Value)
	}

	// Verify env vars are still present
	if len(result.Env) != 2 {
		t.Errorf("MergeToTarget() expected 2 env vars, got %d", len(result.Env))
	}
}

func TestImageShorthandShouldNotBuild(t *testing.T) {
	deployConfig := config.DeployConfig{
		TargetConfig: config.TargetConfig{
			Name:   "myapp",
			Image:  &config.Image{Repository: "myapp"},
			Server: "test.haloy.dev",
		},
	}

	result, err := MergeToTarget(deployConfig, config.TargetConfig{}, "myapp", "yaml")
	if err != nil {
		t.Fatalf("MergeToTarget() unexpected error = %v", err)
	}

	if result.Image.ShouldBuild() {
		t.Error("image shorthand 'image: myapp' should not build (ShouldBuild() should be false)")
	}
}

func TestOmittedImageFieldShouldBuild(t *testing.T) {
	deployConfig := config.DeployConfig{
		TargetConfig: config.TargetConfig{
			Name:   "myapp",
			Server: "test.haloy.dev",
		},
	}

	result, err := MergeToTarget(deployConfig, config.TargetConfig{}, "myapp", "yaml")
	if err != nil {
		t.Fatalf("MergeToTarget() unexpected error = %v", err)
	}

	if !result.Image.ShouldBuild() {
		t.Error("omitted image field should produce ShouldBuild() == true")
	}

	if result.Image.Repository != "myapp" {
		t.Errorf("omitted image field should set Repository to app name, got '%s'", result.Image.Repository)
	}
}

func TestPresetImagesDefaultToPullIfMissing(t *testing.T) {
	for _, preset := range []config.Preset{config.PresetDatabase, config.PresetService} {
		t.Run(string(preset), func(t *testing.T) {
			deployConfig := config.DeployConfig{
				TargetConfig: config.TargetConfig{
					Name:   "postgres",
					Server: "test.haloy.dev",
					Preset: preset,
					Image:  &config.Image{Repository: "postgres", Tag: "18"},
				},
			}

			result, err := MergeToTarget(deployConfig, config.TargetConfig{}, "postgres", "yaml")
			if err != nil {
				t.Fatalf("MergeToTarget() unexpected error = %v", err)
			}
			if result.Image.PullPolicy != config.PullPolicyIfMissing {
				t.Fatalf("Image.PullPolicy = %q, want %q", result.Image.PullPolicy, config.PullPolicyIfMissing)
			}
		})
	}
}

func TestLoadRawDeployConfig_ImageShorthand(t *testing.T) {
	tests := []struct {
		name           string
		yaml           string
		expectedRepo   string
		expectedImages map[string]string
	}{
		{
			name: "string shorthand on target image",
			yaml: `
name: myapp
server: test.haloy.dev
image: "nginx:alpine"
`,
			expectedRepo: "nginx:alpine",
		},
		{
			name: "string shorthand in images map",
			yaml: `
name: myapp
server: test.haloy.dev
image: "nginx:alpine"
images:
  db: "postgres:18"
  cache: "redis:7"
`,
			expectedRepo: "nginx:alpine",
			expectedImages: map[string]string{
				"db":    "postgres:18",
				"cache": "redis:7",
			},
		},
		{
			name: "mixed shorthand and object forms",
			yaml: `
name: myapp
server: test.haloy.dev
image: "nginx:alpine"
images:
  db: "postgres:18"
  api:
    repository: "node"
    tag: "20"
`,
			expectedRepo: "nginx:alpine",
			expectedImages: map[string]string{
				"db":  "postgres:18",
				"api": "node",
			},
		},
		{
			name: "object form still works",
			yaml: `
name: myapp
server: test.haloy.dev
image:
  repository: "nginx"
  tag: "1.21"
`,
			expectedRepo: "nginx",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			configPath := filepath.Join(tmpDir, "haloy.yaml")
			if err := os.WriteFile(configPath, []byte(tt.yaml), 0o644); err != nil {
				t.Fatalf("failed to write config file: %v", err)
			}

			dc, _, err := LoadRawDeployConfig(configPath)
			if err != nil {
				t.Fatalf("LoadRawDeployConfig() unexpected error = %v", err)
			}

			if dc.Image == nil {
				t.Fatal("LoadRawDeployConfig() Image should not be nil")
			}
			if dc.Image.Repository != tt.expectedRepo {
				t.Errorf("Image.Repository = %s, expected %s", dc.Image.Repository, tt.expectedRepo)
			}

			for key, expectedRepo := range tt.expectedImages {
				img, ok := dc.Images[key]
				if !ok {
					t.Errorf("Images[%s] not found", key)
					continue
				}
				if img.Repository != expectedRepo {
					t.Errorf("Images[%s].Repository = %s, expected %s", key, img.Repository, expectedRepo)
				}
			}
		})
	}
}

func TestMergeToTarget_MinReadySeconds(t *testing.T) {
	tests := []struct {
		name     string
		base     *int
		target   *int
		expected int
	}{
		{
			name:     "inherits from base when target is nil",
			base:     new(10),
			target:   nil,
			expected: 10,
		},
		{
			name:     "target overrides base",
			base:     new(10),
			target:   new(30),
			expected: 30,
		},
		{
			name:     "defaults to 0 when both nil",
			base:     nil,
			target:   nil,
			expected: 0,
		},
		{
			name:     "target explicit zero overrides base",
			base:     new(10),
			target:   new(0),
			expected: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			deployConfig := config.DeployConfig{
				TargetConfig: config.TargetConfig{
					Name:            "myapp",
					Server:          "test.haloy.dev",
					MinReadySeconds: tt.base,
				},
			}
			targetConfig := config.TargetConfig{
				MinReadySeconds: tt.target,
			}

			result, err := MergeToTarget(deployConfig, targetConfig, "test-target", "yaml")
			if err != nil {
				t.Fatalf("MergeToTarget() unexpected error = %v", err)
			}

			if result.MinReadySeconds == nil {
				t.Fatal("MergeToTarget() MinReadySeconds should not be nil after normalization")
			}

			if *result.MinReadySeconds != tt.expected {
				t.Errorf("MergeToTarget() MinReadySeconds = %d, expected %d", *result.MinReadySeconds, tt.expected)
			}
		})
	}
}

func TestDeploymentStrategyDefaultsByDomains(t *testing.T) {
	image := &config.Image{Repository: "nginx", Tag: "latest"}
	domains := []config.Domain{{Canonical: "app.example.com"}}

	tests := []struct {
		name         string
		deployConfig config.DeployConfig
		targetConfig config.TargetConfig
		wantStrategy config.DeploymentStrategy
		wantNaming   config.NamingStrategy
	}{
		{
			name: "no domains and no strategy resolves to replace",
			deployConfig: config.DeployConfig{
				TargetConfig: config.TargetConfig{Name: "worker", Server: "test.haloy.dev", Image: image},
			},
			wantStrategy: config.DeploymentStrategyReplace,
		},
		{
			name: "domains and no strategy resolves to rolling",
			deployConfig: config.DeployConfig{
				TargetConfig: config.TargetConfig{Name: "web", Server: "test.haloy.dev", Image: image, Domains: domains},
			},
			wantStrategy: config.DeploymentStrategyRolling,
		},
		{
			name: "target inheriting root domains resolves to rolling",
			deployConfig: config.DeployConfig{
				TargetConfig: config.TargetConfig{Name: "web", Server: "test.haloy.dev", Image: image, Domains: domains},
			},
			targetConfig: config.TargetConfig{Server: "prod.haloy.dev"},
			wantStrategy: config.DeploymentStrategyRolling,
		},
		{
			name: "explicit replace with domains is kept",
			deployConfig: config.DeployConfig{
				TargetConfig: config.TargetConfig{Name: "web", Server: "test.haloy.dev", Image: image, Domains: domains, DeploymentStrategy: config.DeploymentStrategyReplace},
			},
			wantStrategy: config.DeploymentStrategyReplace,
		},
		{
			name: "database preset still resolves to replace and static",
			deployConfig: config.DeployConfig{
				TargetConfig: config.TargetConfig{Name: "postgres", Server: "test.haloy.dev", Image: image, Preset: config.PresetDatabase},
			},
			wantStrategy: config.DeploymentStrategyReplace,
			wantNaming:   config.NamingStrategyStatic,
		},
		{
			name: "service preset still resolves to replace and static",
			deployConfig: config.DeployConfig{
				TargetConfig: config.TargetConfig{Name: "redis", Server: "test.haloy.dev", Image: image, Preset: config.PresetService},
			},
			wantStrategy: config.DeploymentStrategyReplace,
			wantNaming:   config.NamingStrategyStatic,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := MergeToTarget(tt.deployConfig, tt.targetConfig, tt.deployConfig.Name, "yaml")
			if err != nil {
				t.Fatalf("MergeToTarget() unexpected error = %v", err)
			}
			if result.DeploymentStrategy != tt.wantStrategy {
				t.Errorf("DeploymentStrategy = %q, want %q", result.DeploymentStrategy, tt.wantStrategy)
			}
			if tt.wantNaming != "" && result.NamingStrategy != tt.wantNaming {
				t.Errorf("NamingStrategy = %q, want %q", result.NamingStrategy, tt.wantNaming)
			}
		})
	}
}

func TestExtractTargetsRejectsRollingWithoutDomains(t *testing.T) {
	image := &config.Image{Repository: "nginx", Tag: "latest"}

	t.Run("target with explicit rolling and no domains is rejected", func(t *testing.T) {
		deployConfig := config.DeployConfig{
			TargetConfig: config.TargetConfig{Name: "worker", Server: "test.haloy.dev", Image: image, DeploymentStrategy: config.DeploymentStrategyRolling},
		}
		_, err := ExtractTargets(deployConfig, "yaml")
		if err == nil {
			t.Fatal("ExtractTargets() expected error, got none")
		}
		if !strings.Contains(err.Error(), "requires at least one domain") {
			t.Fatalf("ExtractTargets() error = %v, expected it to mention domains", err)
		}
	})

	t.Run("root rolling is rejected for a domainless target and names it", func(t *testing.T) {
		deployConfig := config.DeployConfig{
			TargetConfig: config.TargetConfig{Server: "test.haloy.dev", Image: image, DeploymentStrategy: config.DeploymentStrategyRolling},
			Targets: map[string]*config.TargetConfig{
				"web":    {Domains: []config.Domain{{Canonical: "app.example.com"}}},
				"worker": {},
			},
		}
		_, err := ExtractTargets(deployConfig, "yaml")
		if err == nil {
			t.Fatal("ExtractTargets() expected error, got none")
		}
		if !strings.Contains(err.Error(), "'worker'") || !strings.Contains(err.Error(), "requires at least one domain") {
			t.Fatalf("ExtractTargets() error = %v, expected it to name the worker target and mention domains", err)
		}
	})

	t.Run("root rolling with per-target replace passes", func(t *testing.T) {
		deployConfig := config.DeployConfig{
			TargetConfig: config.TargetConfig{Server: "test.haloy.dev", Image: image, DeploymentStrategy: config.DeploymentStrategyRolling},
			Targets: map[string]*config.TargetConfig{
				"web":    {Domains: []config.Domain{{Canonical: "app.example.com"}}},
				"worker": {DeploymentStrategy: config.DeploymentStrategyReplace},
			},
		}
		targets, err := ExtractTargets(deployConfig, "yaml")
		if err != nil {
			t.Fatalf("ExtractTargets() unexpected error = %v", err)
		}
		if targets["web"].DeploymentStrategy != config.DeploymentStrategyRolling {
			t.Errorf("web DeploymentStrategy = %q, want rolling", targets["web"].DeploymentStrategy)
		}
		if targets["worker"].DeploymentStrategy != config.DeploymentStrategyReplace {
			t.Errorf("worker DeploymentStrategy = %q, want replace", targets["worker"].DeploymentStrategy)
		}
	})
}

func TestPublishedPorts(t *testing.T) {
	image := &config.Image{Repository: "noray-go", Tag: "latest"}
	domains := []config.Domain{{Canonical: "app.example.com"}}

	t.Run("loads from yaml and defaults to replace even with domains", func(t *testing.T) {
		configPath := filepath.Join(t.TempDir(), "haloy.yaml")
		yaml := `
name: noray
server: test.haloy.dev
image: "noray-go:latest"
domains:
  - domain: "noray.example.com"
ports:
  - "8890:8890"
  - "8809:8809/udp"
  - "49152-49351:49152-49351/udp"
`
		if err := os.WriteFile(configPath, []byte(yaml), 0o644); err != nil {
			t.Fatalf("failed to write config file: %v", err)
		}
		dc, format, err := LoadRawDeployConfig(configPath)
		if err != nil {
			t.Fatalf("LoadRawDeployConfig() unexpected error = %v", err)
		}
		targets, err := ExtractTargets(dc, format)
		if err != nil {
			t.Fatalf("ExtractTargets() unexpected error = %v", err)
		}
		target := targets["noray"]
		if len(target.Ports) != 3 {
			t.Fatalf("Ports = %v, want 3 entries", target.Ports)
		}
		if target.DeploymentStrategy != config.DeploymentStrategyReplace {
			t.Errorf("DeploymentStrategy = %q, want replace", target.DeploymentStrategy)
		}
	})

	t.Run("targets inherit root ports", func(t *testing.T) {
		deployConfig := config.DeployConfig{
			TargetConfig: config.TargetConfig{Image: image, Ports: []string{"8890:8890"}},
			Targets: map[string]*config.TargetConfig{
				"eu": {Server: "eu.haloy.dev"},
				"us": {Server: "us.haloy.dev", Ports: []string{"9890:8890"}},
			},
		}
		targets, err := ExtractTargets(deployConfig, "yaml")
		if err != nil {
			t.Fatalf("ExtractTargets() unexpected error = %v", err)
		}
		if got := targets["eu"].Ports; len(got) != 1 || got[0] != "8890:8890" {
			t.Errorf("eu Ports = %v, want inherited [8890:8890]", got)
		}
		if got := targets["us"].Ports; len(got) != 1 || got[0] != "9890:8890" {
			t.Errorf("us Ports = %v, want own [9890:8890]", got)
		}
	})

	t.Run("root rolling is rejected for a target with ports", func(t *testing.T) {
		deployConfig := config.DeployConfig{
			TargetConfig: config.TargetConfig{
				Name: "game", Server: "test.haloy.dev", Image: image, Domains: domains,
				DeploymentStrategy: config.DeploymentStrategyRolling,
				Ports:              []string{"27015:27015/udp"},
			},
		}
		_, err := ExtractTargets(deployConfig, "yaml")
		if err == nil || !strings.Contains(err.Error(), "cannot be used with ports") {
			t.Fatalf("ExtractTargets() error = %v, want rolling/ports error", err)
		}
	})

	conflictTests := []struct {
		name    string
		targets map[string]*config.TargetConfig
		wantErr string
	}{
		{
			name: "same host port on the same server",
			targets: map[string]*config.TargetConfig{
				"a": {Server: "one.haloy.dev", Ports: []string{"49152-49351:49152-49351/udp"}},
				"b": {Server: "one.haloy.dev", Ports: []string{"49300:9000/udp"}},
			},
			wantErr: "targets 'a' and 'b' both publish host port 49300/udp on server one.haloy.dev",
		},
		{
			name: "same host port on different servers",
			targets: map[string]*config.TargetConfig{
				"a": {Server: "one.haloy.dev", Ports: []string{"8890:8890"}},
				"b": {Server: "two.haloy.dev", Ports: []string{"8890:8890"}},
			},
		},
		{
			name: "same port number on different protocols",
			targets: map[string]*config.TargetConfig{
				"a": {Server: "one.haloy.dev", Ports: []string{"8890:8890"}},
				"b": {Server: "one.haloy.dev", Ports: []string{"8890:8890/udp"}},
			},
		},
		{
			name: "same port on different host ips",
			targets: map[string]*config.TargetConfig{
				"a": {Server: "one.haloy.dev", Ports: []string{"127.0.0.1:8891:8891"}},
				"b": {Server: "one.haloy.dev", Ports: []string{"10.0.0.2:8891:8891"}},
			},
		},
	}
	for _, tt := range conflictTests {
		t.Run(tt.name, func(t *testing.T) {
			deployConfig := config.DeployConfig{
				TargetConfig: config.TargetConfig{Image: image},
				Targets:      tt.targets,
			}
			_, err := ExtractTargets(deployConfig, "yaml")
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("ExtractTargets() unexpected error = %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("ExtractTargets() error = %v, want it to contain %q", err, tt.wantErr)
			}
		})
	}
}
