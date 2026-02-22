package api

import (
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

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
