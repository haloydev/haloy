package config

import (
	"reflect"
	"testing"

	"github.com/haloydev/haloy/internal/helpers"
)

func TestIsValidAppName(t *testing.T) {
	tests := []struct {
		name    string
		appName string
		wantErr bool
	}{
		{"valid simple", "myapp", true},
		{"valid with hyphen", "my-app", true},
		{"valid with underscore", "my_app", true},
		{"valid with numbers", "app123", true},
		{"invalid with space", "my app", false},
		{"invalid with dot", "my.app", false},
		{"invalid with special char", "my@app", false},
		{"invalid empty", "", false},
		{"invalid with slash", "my/app", false},
		{"invalid starts with underscore", "_my-app", false},
		{"invalid starts with hyphen", "-my-app", false},
		{"invalid starts with dot", ".my-app", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isValidAppName(tt.appName)
			if got != tt.wantErr {
				t.Errorf("isValidAppName(%q) = %v, want %v", tt.appName, got, tt.wantErr)
			}
		})
	}
}

func TestValidateDomain(t *testing.T) {
	tests := []struct {
		name    string
		domain  string
		wantErr bool
	}{
		{"valid domain", "example.com", false},
		{"valid domain with subdomain", "sub.example.co.uk", false},
		{"valid domain with hyphen", "example-domain.com", false},
		{"invalid TLD too short", "example.c", true},
		{"invalid no TLD", "example", true},
		{"invalid starting with hyphen", "-example.com", true},
		{"invalid domain with space", "example domain.com", true},
		{"empty domain", "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := helpers.IsValidDomain(tt.domain); (err == nil) == tt.wantErr {
				t.Errorf("ValidateDomain() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestTargetConfig_Validate_Comprehensive(t *testing.T) {
	tests := []struct {
		name        string
		target      TargetConfig
		format      string
		expectError bool
		errMsg      string
	}{
		{
			name: "valid minimal target config",
			target: TargetConfig{
				Name:   "haloy-test-app",
				Server: "haloy.dev",
				Image: &Image{
					Repository: "nginx",
					Tag:        "latest",
				},
			},
			format:      "yaml",
			expectError: false,
		},
		{
			name: "valid target config with all fields",
			target: TargetConfig{
				Name: "haloy-test-app",
				Image: &Image{
					Repository: "nginx",
					Tag:        "1.21",
				},
				Server:          "haloy.dev",
				HealthCheckPath: "/health",
				Port:            "8080",
				Replicas:        helpers.Ptr(2),
				Network:         "bridge",
				Volumes:         []string{"/host:/container"},
				PreDeploy:       []string{"echo pre"},
				PostDeploy:      []string{"echo post"},
				Env: []EnvVar{
					{
						Name:        "ENV_VAR",
						ValueSource: ValueSource{Value: "value"},
					},
				},
				Domains: []Domain{
					{Canonical: "example.com", Aliases: []string{"www.example.com"}},
				},
			},
			format:      "yaml",
			expectError: false,
		},
		{
			name: "invalid - missing server",
			target: TargetConfig{
				Name: "haloy-test-app",
				Image: &Image{
					Repository: "nginx",
					Tag:        "1.21",
				},
			},
			format:      "yaml",
			expectError: true,
			errMsg:      "server is required",
		},
		{
			name: "invalid image - missing repository",
			target: TargetConfig{
				Name:   "haloy-test-app",
				Server: "haloy.dev",
				Image: &Image{
					Tag: "latest",
				},
			},
			format:      "yaml",
			expectError: true,
			errMsg:      "image.repository is required",
		},
		{
			name: "invalid domain",
			target: TargetConfig{
				Name:   "haloy-test-app",
				Server: "haloy.dev",
				Image: &Image{
					Repository: "nginx",
					Tag:        "latest",
				},
				Domains: []Domain{
					{Canonical: "invalid domain"},
				},
			},
			format:      "yaml",
			expectError: true,
			errMsg:      "domain must have at least two labels",
		},
		{
			name: "invalid env var",
			target: TargetConfig{
				Name:   "haloy-test-app",
				Server: "haloy.dev",
				Image: &Image{
					Repository: "nginx",
					Tag:        "latest",
				},
				Env: []EnvVar{
					{
						Name:        "",
						ValueSource: ValueSource{Value: "value"},
					},
				},
			},
			format:      "yaml",
			expectError: true,
			errMsg:      "environment variable 'name' cannot be empty",
		},
		{
			name: "invalid volume mapping",
			target: TargetConfig{
				Name:   "haloy-test-app",
				Server: "haloy.dev",
				Image: &Image{
					Repository: "nginx",
					Tag:        "latest",
				},
				Volumes: []string{"invalid-volume-format"},
			},
			format:      "yaml",
			expectError: true,
			errMsg:      "invalid volume mapping",
		},
		{
			name: "invalid health check path",
			target: TargetConfig{
				Name:   "haloy-test-app",
				Server: "haloy.dev",
				Image: &Image{
					Repository: "nginx",
					Tag:        "latest",
				},
				HealthCheckPath: "no-leading-slash",
			},
			format:      "yaml",
			expectError: true,
			errMsg:      "must start with a slash",
		},
		{
			name: "invalid replicas",
			target: TargetConfig{
				Name:   "haloy-test-app",
				Server: "haloy.dev",
				Image: &Image{
					Repository: "nginx",
					Tag:        "latest",
				},
				Replicas: helpers.Ptr(0),
			},
			format:      "yaml",
			expectError: true,
			errMsg:      "replicas must be at least 1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.target.Validate(tt.format)
			if tt.expectError {
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

func TestDomain_Validate(t *testing.T) {
	tests := []struct {
		name    string
		domain  Domain
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid domain with no aliases",
			domain: Domain{
				Canonical: "example.com",
				Aliases:   []string{},
			},
			wantErr: false,
		},
		{
			name: "valid domain with valid aliases",
			domain: Domain{
				Canonical: "example.com",
				Aliases:   []string{"www.example.com", "api.example.com"},
			},
			wantErr: false,
		},
		{
			name: "invalid canonical domain",
			domain: Domain{
				Canonical: "invalid domain",
				Aliases:   []string{},
			},
			wantErr: true,
			errMsg:  "domain must have at least two labels",
		},
		{
			name: "invalid alias domain",
			domain: Domain{
				Canonical: "example.com",
				Aliases:   []string{"valid.com", "invalid domain"},
			},
			wantErr: true,
			errMsg:  "alias 'invalid domain'",
		},
		{
			name: "empty canonical domain",
			domain: Domain{
				Canonical: "",
				Aliases:   []string{},
			},
			wantErr: true,
			errMsg:  "domain length must be between 1 and 253 characters",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.domain.Validate()
			if tt.wantErr {
				if err == nil {
					t.Errorf("Validate() expected error but got none")
				} else if tt.errMsg != "" && !findInString(err.Error(), tt.errMsg) {
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

func findInString(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

func TestEnvVar_Validate(t *testing.T) {
	tests := []struct {
		name    string
		envVar  EnvVar
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid env var with value",
			envVar: EnvVar{
				Name: "DATABASE_URL",
				ValueSource: ValueSource{
					Value: "postgres://localhost:5432/mydb",
				},
			},
			wantErr: false,
		},
		{
			name: "valid env var with from reference",
			envVar: EnvVar{
				Name: "API_KEY",
				ValueSource: ValueSource{
					From: &SourceReference{
						Env: "API_KEY_ENV",
					},
				},
			},
			wantErr: false,
		},
		{
			name: "empty env var name",
			envVar: EnvVar{
				Name: "",
				ValueSource: ValueSource{
					Value: "some-value",
				},
			},
			wantErr: true,
			errMsg:  "environment variable 'name' cannot be empty",
		},
		{
			name: "invalid value source - both value and from",
			envVar: EnvVar{
				Name: "INVALID_VAR",
				ValueSource: ValueSource{
					Value: "direct-value",
					From: &SourceReference{
						Env: "ENV_VAR",
					},
				},
			},
			wantErr: true,
			errMsg:  "cannot provide both 'value' and 'from'",
		},
		{
			name: "invalid value source - neither value nor from",
			envVar: EnvVar{
				Name:        "INVALID_VAR",
				ValueSource: ValueSource{},
			},
			wantErr: true,
			errMsg:  "must provide either 'value' or 'from'",
		},
		{
			name: "invalid from reference - both env and secret",
			envVar: EnvVar{
				Name: "INVALID_VAR",
				ValueSource: ValueSource{
					From: &SourceReference{
						Env:    "ENV_VAR",
						Secret: "secret-key",
					},
				},
			},
			wantErr: true,
			errMsg:  "only one source reference",
		},
		{
			name: "invalid from reference - neither env nor secret",
			envVar: EnvVar{
				Name: "INVALID_VAR",
				ValueSource: ValueSource{
					From: &SourceReference{},
				},
			},
			wantErr: true,
			errMsg:  "a source reference (e.g., 'env' or 'secret') must be specified",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.envVar.Validate("yaml")
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

func TestSourceReference_Validate(t *testing.T) {
	tests := []struct {
		name    string
		ref     SourceReference
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid env reference",
			ref: SourceReference{
				Env: "DATABASE_URL",
			},
			wantErr: false,
		},
		{
			name: "valid secret reference",
			ref: SourceReference{
				Secret: "api-key",
			},
			wantErr: false,
		},
		{
			name:    "empty reference",
			ref:     SourceReference{},
			wantErr: true,
			errMsg:  "a source reference (e.g., 'env' or 'secret') must be specified",
		},
		{
			name: "both env and secret set",
			ref: SourceReference{
				Env:    "DATABASE_URL",
				Secret: "api-key",
			},
			wantErr: true,
			errMsg:  "only one source reference ('env' or 'secret') can be specified at a time",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.ref.Validate()
			if tt.wantErr {
				if err == nil {
					t.Errorf("Validate() expected error but got none")
				} else if tt.errMsg != "" && err.Error() != tt.errMsg {
					t.Errorf("Validate() error = %v, expected %v", err, tt.errMsg)
				}
			} else {
				if err != nil {
					t.Errorf("Validate() unexpected error = %v", err)
				}
			}
		})
	}
}

func TestValueSource_Validate(t *testing.T) {
	tests := []struct {
		name    string
		vs      ValueSource
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid value source",
			vs: ValueSource{
				Value: "direct-value",
			},
			wantErr: false,
		},
		{
			name: "valid from reference",
			vs: ValueSource{
				From: &SourceReference{
					Env: "ENV_VAR",
				},
			},
			wantErr: false,
		},
		{
			name:    "empty value source",
			vs:      ValueSource{},
			wantErr: true,
			errMsg:  "must provide either 'value' or 'from'",
		},
		{
			name: "both value and from set",
			vs: ValueSource{
				Value: "direct-value",
				From: &SourceReference{
					Env: "ENV_VAR",
				},
			},
			wantErr: true,
			errMsg:  "cannot provide both 'value' and 'from'",
		},
		{
			name: "invalid from reference",
			vs: ValueSource{
				From: &SourceReference{
					Env:    "ENV_VAR",
					Secret: "secret-key",
				},
			},
			wantErr: true,
			errMsg:  "invalid 'from' block",
		},
		{
			name: "valid secret from reference",
			vs: ValueSource{
				From: &SourceReference{
					Secret: "api-key",
				},
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.vs.Validate()
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

func TestPortDecodeHook(t *testing.T) {
	decodeHook := PortDecodeHook()
	portType := reflect.TypeOf(Port(""))

	tests := []struct {
		name        string
		data        any
		expectError bool
		expected    Port
		errMsg      string
	}{
		{
			name:     "string port",
			data:     "8080",
			expected: Port("8080"),
		},
		{
			name:     "integer port",
			data:     8080,
			expected: Port("8080"),
		},
		{
			name:     "int64 port",
			data:     int64(8080),
			expected: Port("8080"),
		},
		{
			name:     "float64 port that is integer",
			data:     8080.0,
			expected: Port("8080"),
		},
		{
			name:        "float64 port that is not integer",
			data:        8080.5,
			expectError: true,
			errMsg:      "port must be an integer, got float: 8080.5",
		},
		{
			name:        "boolean data",
			data:        true,
			expectError: true,
			errMsg:      "port must be a string or integer, got bool: true",
		},
		{
			name:        "slice data",
			data:        []string{"8080"},
			expectError: true,
			errMsg:      "port must be a string or integer, got []string: [8080]",
		},
		{
			name:     "zero integer",
			data:     0,
			expected: Port("0"),
		},
		{
			name:     "empty string",
			data:     "",
			expected: Port(""),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := decodeHook(reflect.TypeOf(tt.data), portType, tt.data)

			if tt.expectError {
				if err == nil {
					t.Errorf("expected error but got none")
				} else if tt.errMsg != "" && err.Error() != tt.errMsg {
					t.Errorf("error = %v, expected %v", err, tt.errMsg)
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error = %v", err)
				}
				if result != tt.expected {
					t.Errorf("result = %v, expected %v", result, tt.expected)
				}
			}
		})
	}
}

func TestPortDecodeHook_NonPortType(t *testing.T) {
	decodeHook := PortDecodeHook()
	stringType := reflect.TypeOf("")

	// Test that the hook returns data unchanged when target is not Port type
	data := "8080"
	result, err := decodeHook(reflect.TypeOf(data), stringType, data)
	if err != nil {
		t.Errorf("expected no error for non-Port target type, got %v", err)
	}
	if result != data {
		t.Errorf("expected data to be returned unchanged for non-Port target type, got %v", result)
	}
}

func TestDeployConfig_Validate(t *testing.T) {
	tests := []struct {
		name        string
		config      DeployConfig
		expectError bool
		errMsg      string
	}{
		{
			name: "valid single-target config with name",
			config: DeployConfig{
				TargetConfig: TargetConfig{
					Name: "my-app",
				},
			},
			expectError: false,
		},
		{
			name: "invalid single-target config without name",
			config: DeployConfig{
				TargetConfig: TargetConfig{
					Server: "example.com",
				},
			},
			expectError: true,
			errMsg:      "'name' is required for single-target configurations",
		},
		{
			name: "valid multi-target config with unique target keys",
			config: DeployConfig{
				Targets: map[string]*TargetConfig{
					"app-one": {Server: "example.com"},
					"app-two": {Server: "example.com"},
				},
			},
			expectError: false,
		},
		{
			name: "valid multi-target config with explicit unique names",
			config: DeployConfig{
				Targets: map[string]*TargetConfig{
					"target-a": {Name: "app-one", Server: "example.com"},
					"target-b": {Name: "app-two", Server: "example.com"},
				},
			},
			expectError: false,
		},
		{
			name: "invalid multi-target config with duplicate explicit names",
			config: DeployConfig{
				Targets: map[string]*TargetConfig{
					"target-a": {Name: "same-name", Server: "example.com"},
					"target-b": {Name: "same-name", Server: "example.com"},
				},
			},
			expectError: true,
			errMsg:      "duplicate name 'same-name'",
		},
		{
			name: "invalid multi-target config with explicit name matching another target key",
			config: DeployConfig{
				Targets: map[string]*TargetConfig{
					"app-one":  {Server: "example.com"},
					"target-b": {Name: "app-one", Server: "example.com"},
				},
			},
			expectError: true,
			errMsg:      "duplicate name 'app-one'",
		},
		{
			name: "valid multi-target config ignores global name",
			config: DeployConfig{
				TargetConfig: TargetConfig{
					Name: "global-name",
				},
				Targets: map[string]*TargetConfig{
					"app-one": {Server: "example.com"},
					"app-two": {Server: "example.com"},
				},
			},
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			if tt.expectError {
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
