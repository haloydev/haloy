package haloy

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/haloydev/haloy/internal/apitypes"
	"github.com/haloydev/haloy/internal/constants"
)

func TestServerRegistryCommandsCallAPI(t *testing.T) {
	t.Setenv(constants.EnvVarConfigDir, t.TempDir())
	t.Setenv(constants.EnvVarAPIToken, "test-token")

	var loginRequest apitypes.RegistryLoginRequest
	var logoutRequest apitypes.RegistryLogoutRequest
	requests := make([]string, 0, 3)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health" {
			w.WriteHeader(http.StatusOK)
			return
		}

		if r.Header.Get("Authorization") != "Bearer test-token" {
			http.Error(w, "invalid auth", http.StatusUnauthorized)
			return
		}

		requests = append(requests, r.Method+" "+r.URL.Path)

		switch r.URL.Path {
		case "/v1/registries/login":
			if r.Method != http.MethodPost {
				http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
				return
			}
			if err := json.NewDecoder(r.Body).Decode(&loginRequest); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			json.NewEncoder(w).Encode(apitypes.RegistryEntry{
				Server:   loginRequest.Server,
				Username: loginRequest.Username,
			})
		case "/v1/registries":
			if r.Method != http.MethodGet {
				http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
				return
			}
			json.NewEncoder(w).Encode(apitypes.RegistriesResponse{
				Registries: []apitypes.RegistryEntry{
					{Server: "docker.io", Username: "docker-user"},
				},
			})
		case "/v1/registries/logout":
			if r.Method != http.MethodPost {
				http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
				return
			}
			if err := json.NewDecoder(r.Body).Decode(&logoutRequest); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			w.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	loginCmd := ServerRegistryLoginCmd()
	loginCmd.SetArgs([]string{srv.URL, "registry-1.docker.io", "--username", "docker-user", "--password-stdin"})
	loginCmd.SetIn(strings.NewReader("docker-token\n"))
	if err := loginCmd.Execute(); err != nil {
		t.Fatalf("login command error = %v", err)
	}

	listCmd := ServerRegistryListCmd()
	listCmd.SetArgs([]string{srv.URL})
	if err := listCmd.Execute(); err != nil {
		t.Fatalf("list command error = %v", err)
	}

	logoutCmd := ServerRegistryLogoutCmd()
	logoutCmd.SetArgs([]string{srv.URL, "registry-1.docker.io"})
	if err := logoutCmd.Execute(); err != nil {
		t.Fatalf("logout command error = %v", err)
	}

	if loginRequest.Server != "docker.io" {
		t.Fatalf("login server = %q, want docker.io", loginRequest.Server)
	}
	if loginRequest.Username != "docker-user" || loginRequest.Password != "docker-token" {
		t.Fatalf("login request = %#v, want docker-user/docker-token", loginRequest)
	}
	if logoutRequest.Server != "docker.io" {
		t.Fatalf("logout server = %q, want docker.io", logoutRequest.Server)
	}

	wantRequests := []string{
		"POST /v1/registries/login",
		"GET /v1/registries",
		"POST /v1/registries/logout",
	}
	if strings.Join(requests, ",") != strings.Join(wantRequests, ",") {
		t.Fatalf("requests = %#v, want %#v", requests, wantRequests)
	}
}
