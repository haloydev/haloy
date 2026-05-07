package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/haloydev/haloy/internal/apitypes"
	"github.com/haloydev/haloy/internal/config"
	"github.com/haloydev/haloy/internal/constants"
)

func TestHandleRegistryLoginStoresCredentials(t *testing.T) {
	t.Setenv(constants.EnvVarConfigDir, t.TempDir())
	s := newTestAPIServerForDeploy()

	body := `{"server":"registry-1.docker.io","username":"docker-user","password":"docker-token"}`
	req := httptest.NewRequest(http.MethodPost, "/v1/registries/login", strings.NewReader(body))
	rr := httptest.NewRecorder()

	s.handleRegistryLogin().ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body = %q", rr.Code, http.StatusOK, rr.Body.String())
	}

	var response apitypes.RegistryEntry
	if err := json.Unmarshal(rr.Body.Bytes(), &response); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}
	if response.Server != "docker.io" {
		t.Fatalf("response.Server = %q, want docker.io", response.Server)
	}
	if strings.Contains(rr.Body.String(), "docker-token") {
		t.Fatalf("response leaked registry password: %q", rr.Body.String())
	}

	path, err := config.ServerRegistriesPath()
	if err != nil {
		t.Fatalf("ServerRegistriesPath() error = %v", err)
	}
	registries, err := config.LoadServerRegistries(path)
	if err != nil {
		t.Fatalf("LoadServerRegistries() error = %v", err)
	}
	auth := registries.Registries["docker.io"]
	if auth.Username.Value != "docker-user" || auth.Password.Value != "docker-token" {
		t.Fatalf("stored auth = %#v, want stored credentials", auth)
	}
}

func TestHandleRegistriesListRedactsPasswords(t *testing.T) {
	t.Setenv(constants.EnvVarConfigDir, t.TempDir())
	path, err := config.ServerRegistriesPath()
	if err != nil {
		t.Fatalf("ServerRegistriesPath() error = %v", err)
	}
	if err := config.SaveServerRegistries(&config.ServerRegistriesConfig{
		Registries: map[string]config.RegistryAuth{
			"docker.io": {
				Server:   "docker.io",
				Username: config.ValueSource{Value: "docker-user"},
				Password: config.ValueSource{Value: "docker-token"},
			},
		},
	}, path); err != nil {
		t.Fatalf("SaveServerRegistries() error = %v", err)
	}

	s := newTestAPIServerForDeploy()
	req := httptest.NewRequest(http.MethodGet, "/v1/registries", nil)
	rr := httptest.NewRecorder()

	s.handleRegistriesList().ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body = %q", rr.Code, http.StatusOK, rr.Body.String())
	}
	if strings.Contains(rr.Body.String(), "docker-token") {
		t.Fatalf("response leaked registry password: %q", rr.Body.String())
	}

	var response apitypes.RegistriesResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &response); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}
	if len(response.Registries) != 1 {
		t.Fatalf("len(response.Registries) = %d, want 1", len(response.Registries))
	}
	if response.Registries[0].Server != "docker.io" || response.Registries[0].Username != "docker-user" {
		t.Fatalf("response.Registries[0] = %#v, want docker.io/docker-user", response.Registries[0])
	}
}

func TestHandleRegistryLogoutRemovesCredentials(t *testing.T) {
	t.Setenv(constants.EnvVarConfigDir, t.TempDir())
	path, err := config.ServerRegistriesPath()
	if err != nil {
		t.Fatalf("ServerRegistriesPath() error = %v", err)
	}
	if err := config.SaveServerRegistries(&config.ServerRegistriesConfig{
		Registries: map[string]config.RegistryAuth{
			"docker.io": {
				Server:   "docker.io",
				Username: config.ValueSource{Value: "docker-user"},
				Password: config.ValueSource{Value: "docker-token"},
			},
		},
	}, path); err != nil {
		t.Fatalf("SaveServerRegistries() error = %v", err)
	}

	s := newTestAPIServerForDeploy()
	req := httptest.NewRequest(http.MethodPost, "/v1/registries/logout", strings.NewReader(`{"server":"docker.io"}`))
	rr := httptest.NewRecorder()

	s.handleRegistryLogout().ServeHTTP(rr, req)

	if rr.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d, body = %q", rr.Code, http.StatusNoContent, rr.Body.String())
	}

	registries, err := config.LoadServerRegistries(path)
	if err != nil {
		t.Fatalf("LoadServerRegistries() error = %v", err)
	}
	if _, exists := registries.Registries["docker.io"]; exists {
		t.Fatal("docker.io registry credentials still exist after logout")
	}
}
