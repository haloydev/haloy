package config

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/haloydev/haloy/internal/constants"
)

func TestServerRegistriesConfig_AuthForImage(t *testing.T) {
	registries := &ServerRegistriesConfig{
		Registries: map[string]RegistryAuth{
			"registry-1.docker.io": {
				Username: ValueSource{Value: "docker-user"},
				Password: ValueSource{Value: "docker-token"},
			},
			"ghcr.io": {
				Username: ValueSource{Value: "gh-user"},
				Password: ValueSource{Value: "gh-token"},
			},
		},
	}

	tests := []struct {
		name       string
		image      Image
		wantServer string
		wantUser   string
	}{
		{
			name:       "docker hub official image uses docker hub credentials",
			image:      Image{Repository: "postgres", Tag: "18"},
			wantServer: "docker.io",
			wantUser:   "docker-user",
		},
		{
			name:       "explicit docker hub image uses docker hub credentials",
			image:      Image{Repository: "docker.io/library/postgres", Tag: "18"},
			wantServer: "docker.io",
			wantUser:   "docker-user",
		},
		{
			name:       "other registry uses exact registry credentials",
			image:      Image{Repository: "ghcr.io/example/app", Tag: "latest"},
			wantServer: "ghcr.io",
			wantUser:   "gh-user",
		},
		{
			name:  "missing registry credentials returns nil",
			image: Image{Repository: "quay.io/example/app", Tag: "latest"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			auth, err := registries.AuthForImage(tt.image)
			if err != nil {
				t.Fatalf("AuthForImage() unexpected error = %v", err)
			}
			if tt.wantServer == "" {
				if auth != nil {
					t.Fatalf("AuthForImage() = %#v, want nil", auth)
				}
				return
			}
			if auth == nil {
				t.Fatalf("AuthForImage() = nil, want credentials")
			}
			if auth.Server != tt.wantServer {
				t.Fatalf("auth.Server = %q, want %q", auth.Server, tt.wantServer)
			}
			if auth.Username.Value != tt.wantUser {
				t.Fatalf("auth.Username.Value = %q, want %q", auth.Username.Value, tt.wantUser)
			}
		})
	}
}

func TestNormalizeRegistryServer(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"docker.io", "docker.io"},
		{"registry-1.docker.io", "docker.io"},
		{"https://index.docker.io/v1/", "docker.io"},
		{"GHCR.IO", "ghcr.io"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			if got := NormalizeRegistryServer(tt.input); got != tt.want {
				t.Fatalf("NormalizeRegistryServer() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestServerRegistriesConfig_AuthForImage_EnvSource(t *testing.T) {
	t.Setenv("HALOY_TEST_REGISTRY_USER", "env-user")
	t.Setenv("HALOY_TEST_REGISTRY_PASSWORD", "env-token")

	registries := &ServerRegistriesConfig{
		Registries: map[string]RegistryAuth{
			"docker.io": {
				Username: ValueSource{From: &SourceReference{Env: "HALOY_TEST_REGISTRY_USER"}},
				Password: ValueSource{From: &SourceReference{Env: "HALOY_TEST_REGISTRY_PASSWORD"}},
			},
		},
	}

	auth, err := registries.AuthForImage(Image{Repository: "postgres", Tag: "18"})
	if err != nil {
		t.Fatalf("AuthForImage() unexpected error = %v", err)
	}
	if auth.Username.Value != "env-user" || auth.Password.Value != "env-token" {
		t.Fatalf("AuthForImage() resolved auth = %#v, want env values", auth)
	}
}

func TestServerRegistriesConfig_AuthForImage_EmptyEnvSource(t *testing.T) {
	t.Setenv("HALOY_TEST_REGISTRY_USER", "env-user")
	t.Setenv("HALOY_TEST_REGISTRY_PASSWORD", "")

	registries := &ServerRegistriesConfig{
		Registries: map[string]RegistryAuth{
			"docker.io": {
				Username: ValueSource{From: &SourceReference{Env: "HALOY_TEST_REGISTRY_USER"}},
				Password: ValueSource{From: &SourceReference{Env: "HALOY_TEST_REGISTRY_PASSWORD"}},
			},
		},
	}

	_, err := registries.AuthForImage(Image{Repository: "postgres", Tag: "18"})
	if err == nil {
		t.Fatal("AuthForImage() expected error for empty password env")
	}
	if !strings.Contains(err.Error(), "is empty") {
		t.Fatalf("AuthForImage() error = %v, want empty env message", err)
	}
}

func TestServerRegistriesConfig_Validate(t *testing.T) {
	tests := []struct {
		name      string
		registry  ServerRegistriesConfig
		wantError bool
	}{
		{
			name: "valid registry",
			registry: ServerRegistriesConfig{Registries: map[string]RegistryAuth{
				"docker.io": {
					Username: ValueSource{Value: "user"},
					Password: ValueSource{Value: "token"},
				},
			}},
		},
		{
			name: "empty registry server",
			registry: ServerRegistriesConfig{Registries: map[string]RegistryAuth{
				"": {
					Username: ValueSource{Value: "user"},
					Password: ValueSource{Value: "token"},
				},
			}},
			wantError: true,
		},
		{
			name: "auth server mismatch",
			registry: ServerRegistriesConfig{Registries: map[string]RegistryAuth{
				"docker.io": {
					Server:   "ghcr.io",
					Username: ValueSource{Value: "user"},
					Password: ValueSource{Value: "token"},
				},
			}},
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.registry.Validate()
			if tt.wantError && err == nil {
				t.Fatal("Validate() expected error but got nil")
			}
			if !tt.wantError && err != nil {
				t.Fatalf("Validate() unexpected error = %v", err)
			}
		})
	}
}

func TestLoadSaveServerRegistries(t *testing.T) {
	path := filepath.Join(t.TempDir(), constants.RegistriesFileName)
	registries := &ServerRegistriesConfig{Registries: map[string]RegistryAuth{
		"docker.io": {
			Server:   "docker.io",
			Username: ValueSource{Value: "user"},
			Password: ValueSource{Value: "token"},
		},
	}}

	if err := SaveServerRegistries(registries, path); err != nil {
		t.Fatalf("SaveServerRegistries() unexpected error = %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("registry file was not created: %v", err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm() != constants.ModeFileSecret {
		t.Fatalf("registry file mode = %v, want %v", info.Mode().Perm(), constants.ModeFileSecret)
	}

	loaded, err := LoadServerRegistries(path)
	if err != nil {
		t.Fatalf("LoadServerRegistries() unexpected error = %v", err)
	}
	auth := loaded.Registries["docker.io"]
	if auth.Username.Value != "user" || auth.Password.Value != "token" {
		t.Fatalf("loaded auth = %#v, want saved credentials", auth)
	}
}

func TestServerRegistriesPathUsesDataDir(t *testing.T) {
	dataDir := t.TempDir()
	configDir := t.TempDir()
	t.Setenv(constants.EnvVarDataDir, dataDir)
	t.Setenv(constants.EnvVarConfigDir, configDir)

	path, err := ServerRegistriesPath()
	if err != nil {
		t.Fatalf("ServerRegistriesPath() unexpected error = %v", err)
	}

	want := filepath.Join(dataDir, constants.RegistriesFileName)
	if path != want {
		t.Fatalf("ServerRegistriesPath() = %q, want %q", path, want)
	}
}
