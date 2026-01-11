package configloader

import (
	"testing"

	"github.com/haloydev/haloy/internal/config"
	"github.com/haloydev/haloy/internal/helpers"
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
			ACMEEmail:       "admin@default.com",
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
				ACMEEmail:       "admin@prod.com",
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
			targetName:   "service-target",
			expectedName: "service-target",
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
			targetName:   "db-target",
			expectedName: "db-target",
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
			targetName:   "explicit-override",
			expectedName: "explicit-override",
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
			targetName:   "key-target",
			expectedName: "key-target",
			expectedImage: config.Image{
				Repository: "resolved-repo",
				History: &config.ImageHistory{
					Strategy: config.HistoryStrategyNone,
				},
			},
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
				if tt.expectedImage.History != nil {
					if result.Image.History == nil {
						t.Errorf("MergeToTarget() Image.History should not be nil")
					} else {
						if result.Image.History.Strategy != tt.expectedImage.History.Strategy {
							t.Errorf("MergeToTarget() Image.History.Strategy = %s, expected %s",
								result.Image.History.Strategy, tt.expectedImage.History.Strategy)
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
			Count:    helpers.Ptr(5),
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
			name: "target image overrides base completely",
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
			},
		},
		{
			name: "target image merges with base",
			targetConfig: config.TargetConfig{
				Image: &config.Image{
					Tag: "1.21", // Only override tag
				},
			},
			images:    images,
			baseImage: baseImage,
			expected: &config.Image{
				Repository: "nginx", // From base
				Tag:        "1.21",  // Overridden
				History: &config.ImageHistory{
					Strategy: config.HistoryStrategyLocal,
					Count:    helpers.Ptr(5),
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
			name:         "no image specified",
			targetConfig: config.TargetConfig{},
			images:       images,
			baseImage:    nil,
			expectError:  false,
			errMsg:       "no image specified for target",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := mergeImage(tt.targetConfig, tt.images, tt.baseImage)

			if tt.expectError {
				if err == nil {
					t.Errorf("mergeImage() expected error but got none")
				} else if tt.errMsg != "" && !helpers.Contains(err.Error(), tt.errMsg) {
					t.Errorf("mergeImage() error = %v, expected to contain %v", err, tt.errMsg)
				}
			} else {
				if err != nil {
					t.Errorf("mergeImage() unexpected error = %v", err)
				}
				if result != nil && result.Repository != tt.expected.Repository {
					t.Errorf("mergeImage() Repository = %s, expected %s",
						result.Repository, tt.expected.Repository)
				}
				if result != nil && result.Tag != tt.expected.Tag {
					t.Errorf("mergeImage() Tag = %s, expected %s",
						result.Tag, tt.expected.Tag)
				}
				if result != nil && tt.expected.History != nil {
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
					Build:      helpers.Ptr(true),
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
					Build:      helpers.Ptr(true),
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
					Build:      helpers.Ptr(true),
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
					Build:      helpers.Ptr(true),
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
					Build:      helpers.Ptr(false),
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
					Build:       helpers.Ptr(true),
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
				Build:      helpers.Ptr(true),
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
