package api

import (
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/haloydev/haloy/internal/config"
	"github.com/haloydev/haloy/internal/logging"
)

func newTestAPIServerForDeploy() *APIServer {
	return &APIServer{
		logBroker: logging.NewLogBroker(),
		logLevel:  slog.LevelInfo,
	}
}

func TestHandleDeploy_InvalidJSON(t *testing.T) {
	s := newTestAPIServerForDeploy()
	h := s.handleDeploy()

	req := httptest.NewRequest(http.MethodPost, "/v1/deploy", strings.NewReader("{"))
	rr := httptest.NewRecorder()

	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusBadRequest)
	}
	if !strings.Contains(rr.Body.String(), "unexpected EOF") {
		t.Fatalf("body = %q, expected JSON parsing error", rr.Body.String())
	}
}

func TestHandleDeploy_MissingDeploymentID(t *testing.T) {
	s := newTestAPIServerForDeploy()
	h := s.handleDeploy()

	body := `{"targetConfig":{"name":"app","server":"example.com","image":{"repository":"nginx"}}}`
	req := httptest.NewRequest(http.MethodPost, "/v1/deploy", strings.NewReader(body))
	rr := httptest.NewRecorder()

	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusBadRequest)
	}
	if !strings.Contains(rr.Body.String(), "Deployment ID is required") {
		t.Fatalf("body = %q, expected missing deployment ID error", rr.Body.String())
	}
}

func TestHandleDeploy_InvalidTargetConfig(t *testing.T) {
	s := newTestAPIServerForDeploy()
	h := s.handleDeploy()

	body := `{"deploymentID":"dep-1","targetConfig":{}}`
	req := httptest.NewRequest(http.MethodPost, "/v1/deploy", strings.NewReader(body))
	rr := httptest.NewRecorder()

	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusBadRequest)
	}
	if !strings.Contains(rr.Body.String(), "Invalid deploy configuration") {
		t.Fatalf("body = %q, expected invalid deploy configuration error", rr.Body.String())
	}
}

func TestHandleDeploy_RejectsUnknownFields(t *testing.T) {
	s := newTestAPIServerForDeploy()
	h := s.handleDeploy()

	body := `{"deploymentID":"dep-1","targetConfig":{},"unexpected":true}`
	req := httptest.NewRequest(http.MethodPost, "/v1/deploy", strings.NewReader(body))
	rr := httptest.NewRecorder()

	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusBadRequest)
	}
	if !strings.Contains(rr.Body.String(), "unknown field") {
		t.Fatalf("body = %q, expected unknown field error", rr.Body.String())
	}
}

func TestApplyServerRegistryAuth(t *testing.T) {
	serverAuth := &config.RegistryAuth{
		Server:   "docker.io",
		Username: config.ValueSource{Value: "server-user"},
		Password: config.ValueSource{Value: "server-token"},
	}
	s := newTestAPIServerForDeploy()
	s.registryAuthProvider = func(image config.Image) (*config.RegistryAuth, error) {
		if image.Repository != "postgres" {
			t.Fatalf("provider image.Repository = %q, want postgres", image.Repository)
		}
		return serverAuth, nil
	}

	target := config.TargetConfig{
		Image: &config.Image{Repository: "postgres", Tag: "18"},
	}
	if err := s.applyServerRegistryAuth(&target); err != nil {
		t.Fatalf("applyServerRegistryAuth() unexpected error = %v", err)
	}
	if target.Image.RegistryAuth == nil {
		t.Fatal("RegistryAuth = nil, want server auth")
	}
	if target.Image.RegistryAuth.Username.Value != "server-user" {
		t.Fatalf("RegistryAuth.Username.Value = %q, want server-user", target.Image.RegistryAuth.Username.Value)
	}
}

func TestApplyServerRegistryAuth_DoesNotOverrideTargetAuth(t *testing.T) {
	s := newTestAPIServerForDeploy()
	s.registryAuthProvider = func(image config.Image) (*config.RegistryAuth, error) {
		t.Fatal("provider should not be called when target auth is set")
		return nil, nil
	}

	targetAuth := &config.RegistryAuth{
		Server:   "docker.io",
		Username: config.ValueSource{Value: "target-user"},
		Password: config.ValueSource{Value: "target-token"},
	}
	target := config.TargetConfig{
		Image: &config.Image{
			Repository:   "postgres",
			Tag:          "18",
			RegistryAuth: targetAuth,
		},
	}
	if err := s.applyServerRegistryAuth(&target); err != nil {
		t.Fatalf("applyServerRegistryAuth() unexpected error = %v", err)
	}
	if target.Image.RegistryAuth != targetAuth {
		t.Fatal("RegistryAuth was overridden, want target auth to win")
	}
}

func TestApplyServerRegistryAuth_DoesNotApplyToServerBuild(t *testing.T) {
	s := newTestAPIServerForDeploy()
	s.registryAuthProvider = func(image config.Image) (*config.RegistryAuth, error) {
		t.Fatal("provider should not be called for images uploaded from local builds")
		return nil, nil
	}

	build := true
	target := config.TargetConfig{
		Image: &config.Image{
			Repository: "my-app",
			Build:      &build,
		},
	}
	if err := s.applyServerRegistryAuth(&target); err != nil {
		t.Fatalf("applyServerRegistryAuth() unexpected error = %v", err)
	}
	if target.Image.RegistryAuth != nil {
		t.Fatal("RegistryAuth was set for a server build, want nil")
	}
}
