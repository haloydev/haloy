package haloy

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/haloydev/haloy/internal/apitypes"
	"github.com/haloydev/haloy/internal/constants"
)

func TestServerRegistryCommandsCallAPILegacyPositional(t *testing.T) {
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

	loginCmd := ServerRegistryLoginCmd(nil, nil)
	loginCmd.SetArgs([]string{srv.URL, "registry-1.docker.io", "--username", "docker-user", "--password-stdin"})
	loginCmd.SetIn(strings.NewReader("docker-token\n"))
	if err := loginCmd.Execute(); err != nil {
		t.Fatalf("login command error = %v", err)
	}

	listCmd := ServerRegistryListCmd(nil, nil)
	listCmd.SetArgs([]string{srv.URL})
	if err := listCmd.Execute(); err != nil {
		t.Fatalf("list command error = %v", err)
	}

	logoutCmd := ServerRegistryLogoutCmd(nil, nil)
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

func TestServerRegistryCommandsUseConfigServer(t *testing.T) {
	t.Setenv(constants.EnvVarConfigDir, t.TempDir())

	var loginRequest apitypes.RegistryLoginRequest
	requests := make([]string, 0, 2)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health" {
			w.WriteHeader(http.StatusOK)
			return
		}

		if r.Header.Get("Authorization") != "Bearer config-token" {
			http.Error(w, "invalid auth", http.StatusUnauthorized)
			return
		}

		requests = append(requests, r.Method+" "+r.URL.Path)

		switch r.URL.Path {
		case "/v1/registries/login":
			if err := json.NewDecoder(r.Body).Decode(&loginRequest); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			json.NewEncoder(w).Encode(apitypes.RegistryEntry{
				Server:   loginRequest.Server,
				Username: loginRequest.Username,
			})
		case "/v1/registries":
			json.NewEncoder(w).Encode(apitypes.RegistriesResponse{
				Registries: []apitypes.RegistryEntry{
					{Server: "docker.io", Username: "docker-user"},
				},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	configPath := writeRegistryCommandConfig(t, `
name: app
server: `+srv.URL+`
api_token:
  value: config-token
`)
	flags := &appCmdFlags{}

	loginCmd := ServerRegistryLoginCmd(&configPath, flags)
	loginCmd.SetArgs([]string{"registry-1.docker.io", "--username", "docker-user", "--password-stdin"})
	loginCmd.SetIn(strings.NewReader("docker-token\n"))
	if err := loginCmd.Execute(); err != nil {
		t.Fatalf("login command error = %v", err)
	}

	listCmd := ServerRegistryListCmd(&configPath, &appCmdFlags{})
	listCmd.SetArgs([]string{})
	if err := listCmd.Execute(); err != nil {
		t.Fatalf("list command error = %v", err)
	}

	if loginRequest.Server != "docker.io" {
		t.Fatalf("login server = %q, want docker.io", loginRequest.Server)
	}
	if loginRequest.Username != "docker-user" || loginRequest.Password != "docker-token" {
		t.Fatalf("login request = %#v, want docker-user/docker-token", loginRequest)
	}

	wantRequests := []string{
		"POST /v1/registries/login",
		"GET /v1/registries",
	}
	if strings.Join(requests, ",") != strings.Join(wantRequests, ",") {
		t.Fatalf("requests = %#v, want %#v", requests, wantRequests)
	}
}

func TestServerRegistryRootCommandUsesConfigFlag(t *testing.T) {
	t.Setenv(constants.EnvVarConfigDir, t.TempDir())

	var loginRequest apitypes.RegistryLoginRequest

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health" {
			w.WriteHeader(http.StatusOK)
			return
		}

		if r.Header.Get("Authorization") != "Bearer config-token" {
			http.Error(w, "invalid auth", http.StatusUnauthorized)
			return
		}

		if r.URL.Path != "/v1/registries/login" {
			http.NotFound(w, r)
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
	}))
	defer srv.Close()

	configPath := writeRegistryCommandConfig(t, `
name: app
server: `+srv.URL+`
api_token:
  value: config-token
`)

	cmd := NewRootCmd()
	cmd.SetArgs([]string{"server", "registry", "login", "docker.io", "--config", configPath, "--username", "docker-user", "--password-stdin"})
	cmd.SetIn(strings.NewReader("docker-token\n"))
	if err := cmd.Execute(); err != nil {
		t.Fatalf("login command error = %v", err)
	}

	if loginRequest.Server != "docker.io" {
		t.Fatalf("login server = %q, want docker.io", loginRequest.Server)
	}
}

func TestServerRegistryLoginPasswordStdinRejectsTerminalInput(t *testing.T) {
	originalIsTerminal := isTerminal
	isTerminal = func(fd uintptr) bool { return true }
	t.Cleanup(func() { isTerminal = originalIsTerminal })

	stdin, err := os.CreateTemp(t.TempDir(), "stdin")
	if err != nil {
		t.Fatalf("failed to create stdin file: %v", err)
	}
	defer stdin.Close()

	cmd := ServerRegistryLoginCmd(nil, nil)
	cmd.SetArgs([]string{"docker.io", "--username", "docker-user", "--password-stdin"})
	cmd.SetIn(stdin)

	err = cmd.Execute()
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "--password-stdin requires piped input") {
		t.Fatalf("expected piped input error, got %v", err)
	}
}

func TestServerRegistryCommandServerFlagBypassesConfig(t *testing.T) {
	t.Setenv(constants.EnvVarConfigDir, t.TempDir())
	t.Setenv(constants.EnvVarAPIToken, "server-flag-token")

	var loginRequest apitypes.RegistryLoginRequest

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health" {
			w.WriteHeader(http.StatusOK)
			return
		}

		if r.Header.Get("Authorization") != "Bearer server-flag-token" {
			http.Error(w, "invalid auth", http.StatusUnauthorized)
			return
		}

		if r.URL.Path != "/v1/registries/login" {
			http.NotFound(w, r)
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
	}))
	defer srv.Close()

	missingConfigPath := "/does/not/exist/haloy.yaml"
	cmd := ServerRegistryLoginCmd(&missingConfigPath, &appCmdFlags{})
	cmd.SetArgs([]string{"docker.io", "--server", srv.URL, "--username", "docker-user", "--password-stdin"})
	cmd.SetIn(strings.NewReader("docker-token\n"))
	if err := cmd.Execute(); err != nil {
		t.Fatalf("login command error = %v", err)
	}

	if loginRequest.Server != "docker.io" {
		t.Fatalf("login server = %q, want docker.io", loginRequest.Server)
	}
}

func TestServerRegistryCommandRequiresSelectorForMultiTargetConfig(t *testing.T) {
	t.Setenv(constants.EnvVarConfigDir, t.TempDir())

	configPath := writeRegistryCommandConfig(t, `
targets:
  web:
    server: haloy-one.example.com
    api_token:
      value: token
  worker:
    server: haloy-two.example.com
    api_token:
      value: token
`)

	cmd := ServerRegistryLoginCmd(&configPath, &appCmdFlags{})
	cmd.SetArgs([]string{"docker.io", "--username", "docker-user", "--password-stdin"})
	cmd.SetIn(strings.NewReader("docker-token\n"))
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "multiple servers available") {
		t.Fatalf("expected multi-server selector error, got %v", err)
	}
}

func TestServerRegistryCommandUsesInheritedTopLevelServerForMultiTargetConfig(t *testing.T) {
	t.Setenv(constants.EnvVarConfigDir, t.TempDir())

	requests := 0
	srv := httptest.NewServer(registryLoginTestHandler(t, "top-level-token", &requests))
	defer srv.Close()

	configPath := writeRegistryCommandConfig(t, `
server: `+srv.URL+`
api_token:
  value: top-level-token
targets:
  api:
    domains:
      - domain: api.example.com
  worker:
    domains:
      - domain: worker.example.com
`)

	cmd := ServerRegistryLoginCmd(&configPath, &appCmdFlags{})
	cmd.SetArgs([]string{"docker.io", "--username", "docker-user", "--password-stdin"})
	cmd.SetIn(strings.NewReader("docker-token\n"))
	if err := cmd.Execute(); err != nil {
		t.Fatalf("login command error = %v", err)
	}

	if requests != 1 {
		t.Fatalf("requests = %d, want 1", requests)
	}
}

func TestServerRegistryCommandAllTargetsDedupesServers(t *testing.T) {
	t.Setenv(constants.EnvVarConfigDir, t.TempDir())

	requestsOne := 0
	srvOne := httptest.NewServer(registryLoginTestHandler(t, "token-one", &requestsOne))
	defer srvOne.Close()

	requestsTwo := 0
	srvTwo := httptest.NewServer(registryLoginTestHandler(t, "token-two", &requestsTwo))
	defer srvTwo.Close()

	configPath := writeRegistryCommandConfig(t, `
targets:
  api:
    server: `+srvOne.URL+`
    api_token:
      value: token-one
  worker:
    server: `+srvOne.URL+`
    api_token:
      value: token-one
  db:
    server: `+srvTwo.URL+`
    api_token:
      value: token-two
`)

	cmd := ServerRegistryLoginCmd(&configPath, &appCmdFlags{})
	cmd.SetArgs([]string{"docker.io", "--all", "--username", "docker-user", "--password-stdin"})
	cmd.SetIn(strings.NewReader("docker-token\n"))
	if err := cmd.Execute(); err != nil {
		t.Fatalf("login command error = %v", err)
	}

	if requestsOne != 1 {
		t.Fatalf("server one requests = %d, want 1", requestsOne)
	}
	if requestsTwo != 1 {
		t.Fatalf("server two requests = %d, want 1", requestsTwo)
	}
}

func registryLoginTestHandler(t *testing.T, token string, requests *int) http.HandlerFunc {
	t.Helper()
	return func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health" {
			w.WriteHeader(http.StatusOK)
			return
		}

		if r.Header.Get("Authorization") != "Bearer "+token {
			http.Error(w, "invalid auth", http.StatusUnauthorized)
			return
		}
		if r.URL.Path != "/v1/registries/login" {
			http.NotFound(w, r)
			return
		}

		*requests = *requests + 1
		var request apitypes.RegistryLoginRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		json.NewEncoder(w).Encode(apitypes.RegistryEntry{
			Server:   request.Server,
			Username: request.Username,
		})
	}
}

func writeRegistryCommandConfig(t *testing.T, contents string) string {
	t.Helper()
	path := t.TempDir() + "/haloy.yaml"
	if err := os.WriteFile(path, []byte(strings.TrimSpace(contents)+"\n"), 0o600); err != nil {
		t.Fatalf("failed to write config: %v", err)
	}
	return path
}
