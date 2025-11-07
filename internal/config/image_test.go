package config

import (
	"testing"

	"github.com/haloydev/haloy/internal/helpers"
)

func TestImage_ImageRef(t *testing.T) {
	tests := []struct {
		name     string
		image    Image
		expected string
	}{
		{
			name: "repository with tag",
			image: Image{
				Repository: "nginx",
				Tag:        "1.21",
			},
			expected: "nginx:1.21",
		},
		{
			name: "repository without tag defaults to latest",
			image: Image{
				Repository: "nginx",
				Tag:        "",
			},
			expected: "nginx:latest",
		},
		{
			name: "repository with whitespace in tag",
			image: Image{
				Repository: "nginx",
				Tag:        " 1.21 ",
			},
			expected: "nginx:1.21",
		},
		{
			name: "repository with whitespace in repository",
			image: Image{
				Repository: " nginx ",
				Tag:        "1.21",
			},
			expected: "nginx:1.21",
		},
		{
			name: "full registry path",
			image: Image{
				Repository: "registry.example.com/myapp",
				Tag:        "v1.0.0",
			},
			expected: "registry.example.com/myapp:v1.0.0",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.image.ImageRef()
			if result != tt.expected {
				t.Errorf("ImageRef() = %s, expected %s", result, tt.expected)
			}
		})
	}
}

func TestImage_Validate(t *testing.T) {
	tests := []struct {
		name    string
		image   Image
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid image with repository and tag",
			image: Image{
				Repository: "nginx",
				Tag:        "1.21",
			},
			wantErr: false,
		},
		{
			name: "valid image with registry source",
			image: Image{
				Repository: "nginx",
				Tag:        "1.21",
			},
			wantErr: false,
		},
		{
			name: "valid image with local source",
			image: Image{
				Repository: "myapp",
				Tag:        "latest",
			},
			wantErr: false,
		},
		{
			name: "empty repository",
			image: Image{
				Repository: "",
				Tag:        "1.21",
			},
			wantErr: true,
			errMsg:  "image.repository is required",
		},
		{
			name: "repository with whitespace",
			image: Image{
				Repository: "nginx latest",
				Tag:        "1.21",
			},
			wantErr: true,
			errMsg:  "contains whitespace",
		},
		{
			name: "tag with whitespace",
			image: Image{
				Repository: "nginx",
				Tag:        "1.21 2.0",
			},
			wantErr: true,
			errMsg:  "contains whitespace",
		},
		{
			name: "registry strategy with latest tag",
			image: Image{
				Repository: "nginx",
				Tag:        "latest",
				History: &ImageHistory{
					Strategy: HistoryStrategyRegistry,
					Count:    helpers.IntPtr(5),
					Pattern:  "v*",
				},
			},
			wantErr: true,
			errMsg:  "cannot be 'latest' or empty with registry strategy",
		},
		{
			name: "registry strategy with mutable tag",
			image: Image{
				Repository: "myapp",
				Tag:        "main",
				History: &ImageHistory{
					Strategy: HistoryStrategyRegistry,
					Count:    helpers.IntPtr(5),
					Pattern:  "v*",
				},
			},
			wantErr: true,
			errMsg:  "is mutable and not recommended",
		},
		{
			name: "valid registry strategy with immutable tag",
			image: Image{
				Repository: "myapp",
				Tag:        "v1.2.3",
				History: &ImageHistory{
					Strategy: HistoryStrategyRegistry,
					Count:    helpers.IntPtr(5),
					Pattern:  "v*",
				},
			},
			wantErr: false,
		},
		{
			name: "valid registry auth",
			image: Image{
				Repository: "private.registry.com/myapp",
				Tag:        "v1.0.0",
				RegistryAuth: &RegistryAuth{
					Username: ValueSource{Value: "user"},
					Password: ValueSource{Value: "pass"},
				},
			},
			wantErr: false,
		},
		{
			name: "registry auth with whitespace in server",
			image: Image{
				Repository: "private.registry.com/myapp",
				Tag:        "v1.0.0",
				RegistryAuth: &RegistryAuth{
					Server:   "private registry.com",
					Username: ValueSource{Value: "user"},
					Password: ValueSource{Value: "pass"},
				},
			},
			wantErr: true,
			errMsg:  "contains whitespace",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.image.Validate("yaml")
			if tt.wantErr {
				if err == nil {
					t.Errorf("Validate() expected error but got none")
				} else if tt.errMsg != "" && !helpers.Contains(err.Error(), tt.errMsg) {
					t.Errorf("Validate() error = %v, expected to contain %v", err, tt.errMsg)
				}
			} else {
				if err != nil {
					t.Errorf("Validate() unexpected error = %v", err)
				}
			}
		})
	}
}

func TestImageHistory_Validate(t *testing.T) {
	tests := []struct {
		name    string
		history ImageHistory
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid local strategy with count",
			history: ImageHistory{
				Strategy: HistoryStrategyLocal,
				Count:    helpers.IntPtr(5),
			},
			wantErr: false,
		},
		{
			name: "valid registry strategy with count and pattern",
			history: ImageHistory{
				Strategy: HistoryStrategyRegistry,
				Count:    helpers.IntPtr(10),
				Pattern:  "v*",
			},
			wantErr: false,
		},
		{
			name: "valid none strategy",
			history: ImageHistory{
				Strategy: HistoryStrategyNone,
			},
			wantErr: false,
		},
		{
			name: "empty strategy defaults to valid",
			history: ImageHistory{
				Strategy: "",
			},
			wantErr: false,
		},
		{
			name: "invalid strategy",
			history: ImageHistory{
				Strategy: "invalid-strategy",
			},
			wantErr: true,
			errMsg:  "must be 'local', 'registry', or 'none'",
		},
		{
			name: "local strategy missing count",
			history: ImageHistory{
				Strategy: HistoryStrategyLocal,
				Count:    nil,
			},
			wantErr: true,
			errMsg:  "count is required for local strategy",
		},
		{
			name: "registry strategy missing count",
			history: ImageHistory{
				Strategy: HistoryStrategyRegistry,
				Count:    nil,
			},
			wantErr: true,
			errMsg:  "count is required for registry strategy",
		},
		{
			name: "local strategy with zero count",
			history: ImageHistory{
				Strategy: HistoryStrategyLocal,
				Count:    helpers.IntPtr(0),
			},
			wantErr: true,
			errMsg:  "must be at least 1",
		},
		{
			name: "registry strategy with negative count",
			history: ImageHistory{
				Strategy: HistoryStrategyRegistry,
				Count:    helpers.IntPtr(-1),
			},
			wantErr: true,
			errMsg:  "must be at least 1",
		},
		{
			name: "registry strategy missing pattern",
			history: ImageHistory{
				Strategy: HistoryStrategyRegistry,
				Count:    helpers.IntPtr(5),
				Pattern:  "",
			},
			wantErr: true,
			errMsg:  "pattern is required for registry strategy",
		},
		{
			name: "registry strategy with whitespace pattern",
			history: ImageHistory{
				Strategy: HistoryStrategyRegistry,
				Count:    helpers.IntPtr(5),
				Pattern:  "   ",
			},
			wantErr: true,
			errMsg:  "pattern is required for registry strategy",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.history.Validate()
			if tt.wantErr {
				if err == nil {
					t.Errorf("Validate() expected error but got none")
				} else if tt.errMsg != "" && !helpers.Contains(err.Error(), tt.errMsg) {
					t.Errorf("Validate() error = %v, expected to contain %v", err, tt.errMsg)
				}
			} else {
				if err != nil {
					t.Errorf("Validate() unexpected error = %v", err)
				}
			}
		})
	}
}

func TestBuildConfig_Validate(t *testing.T) {
	tests := []struct {
		name    string
		build   BuildConfig
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid build config with push to server",
			build: BuildConfig{
				Context:    ".",
				Dockerfile: "Dockerfile",
				Platform:   "linux/amd64",
				Push:       BuildPushOptionServer,
			},
			wantErr: false,
		},
		{
			name: "valid build config with push to registry",
			build: BuildConfig{
				Context:    ".",
				Dockerfile: "Dockerfile",
				Platform:   "linux/amd64",
				Push:       BuildPushOptionRegistry,
			},
			wantErr: false,
		},
		{
			name: "valid build config with empty push (defaults to registry)",
			build: BuildConfig{
				Context:    ".",
				Dockerfile: "Dockerfile",
				Platform:   "linux/amd64",
				Push:       "",
			},
			wantErr: false,
		},
		{
			name: "valid build config with minimal fields",
			build: BuildConfig{
				Push: BuildPushOptionServer,
			},
			wantErr: false,
		},
		{
			name: "invalid push option",
			build: BuildConfig{
				Context:    ".",
				Dockerfile: "Dockerfile",
				Push:       "invalid-option",
			},
			wantErr: true,
			errMsg:  "builder.push must be 'server' or 'registry'",
		},
		{
			name: "context with whitespace",
			build: BuildConfig{
				Context: "./my context",
				Push:    BuildPushOptionServer,
			},
			wantErr: true,
			errMsg:  "contains whitespace",
		},
		{
			name: "dockerfile with whitespace",
			build: BuildConfig{
				Dockerfile: "my dockerfile",
				Push:       BuildPushOptionServer,
			},
			wantErr: true,
			errMsg:  "contains whitespace",
		},
		{
			name: "platform with whitespace",
			build: BuildConfig{
				Platform: "linux amd64",
				Push:     BuildPushOptionServer,
			},
			wantErr: true,
			errMsg:  "contains whitespace",
		},
		{
			name: "valid build config with args",
			build: BuildConfig{
				Push: BuildPushOptionRegistry,
				Args: []BuildArg{
					{
						Name:        "NODE_ENV",
						ValueSource: ValueSource{Value: "production"},
					},
					{
						Name:        "VERSION",
						ValueSource: ValueSource{From: &SourceReference{Env: "BUILD_VERSION"}},
					},
				},
			},
			wantErr: false,
		},
		{
			name: "build arg with empty name",
			build: BuildConfig{
				Push: BuildPushOptionServer,
				Args: []BuildArg{
					{
						Name:        "",
						ValueSource: ValueSource{Value: "test"},
					},
				},
			},
			wantErr: true,
			errMsg:  "name' cannot be empty",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.build.Validate("yaml")
			if tt.wantErr {
				if err == nil {
					t.Errorf("Validate() expected error but got none")
				} else if tt.errMsg != "" && !helpers.Contains(err.Error(), tt.errMsg) {
					t.Errorf("Validate() error = %v, expected to contain %v", err, tt.errMsg)
				}
			} else {
				if err != nil {
					t.Errorf("Validate() unexpected error = %v", err)
				}
			}
		})
	}
}

func TestImage_Validate_WithBuildConfig(t *testing.T) {
	tests := []struct {
		name    string
		image   Image
		wantErr bool
		errMsg  string
	}{
		{
			name: "build with push to server - registry auth not required",
			image: Image{
				Repository: "myapp",
				Tag:        "latest",
				BuildConfig: &BuildConfig{
					Push: BuildPushOptionServer,
				},
			},
			wantErr: false,
		},
		{
			name: "build with push to registry - registry auth required",
			image: Image{
				Repository: "ghcr.io/myorg/myapp",
				Tag:        "latest",
				BuildConfig: &BuildConfig{
					Push: BuildPushOptionRegistry,
				},
				RegistryAuth: nil,
			},
			wantErr: true,
			errMsg:  "image.registry authentication required when building with registry push strategy",
		},
		{
			name: "build with push to registry - registry auth provided",
			image: Image{
				Repository: "ghcr.io/myorg/myapp",
				Tag:        "latest",
				BuildConfig: &BuildConfig{
					Push: BuildPushOptionRegistry,
				},
				RegistryAuth: &RegistryAuth{
					Username: ValueSource{Value: "user"},
					Password: ValueSource{Value: "pass"},
				},
			},
			wantErr: false,
		},
		{
			name: "build with push to server - registry auth not allowed",
			image: Image{
				Repository: "myapp",
				Tag:        "latest",
				BuildConfig: &BuildConfig{
					Push: BuildPushOptionServer,
				},
				RegistryAuth: &RegistryAuth{
					Username: ValueSource{Value: "user"},
					Password: ValueSource{Value: "pass"},
				},
			},
			wantErr: true,
			errMsg:  "image.registry cannot be set when image.build_config.push is 'server'",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.image.Validate("yaml")
			if tt.wantErr {
				if err == nil {
					t.Errorf("Validate() expected error but got none")
				} else if tt.errMsg != "" && !helpers.Contains(err.Error(), tt.errMsg) {
					t.Errorf("Validate() error = %v, expected to contain %v", err, tt.errMsg)
				}
			} else {
				if err != nil {
					t.Errorf("Validate() unexpected error = %v", err)
				}
			}
		})
	}
}
