package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/haloydev/haloy/internal/apitypes"
	"github.com/haloydev/haloy/internal/proxywire"
)

func TestHandleVersionReportsLegacyProxyAsCompatible(t *testing.T) {
	s := &APIServer{}
	s.SetProxyStatusFunc(func(_ context.Context) (*proxywire.Status, error) {
		return &proxywire.Status{
			Version:       "v0.1.0-beta.66",
			SchemaVersion: proxywire.SchemaVersion,
		}, nil
	})

	rr := httptest.NewRecorder()
	s.handleVersion().ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/v1/version", nil))

	var response apitypes.VersionResponse
	if err := json.NewDecoder(rr.Body).Decode(&response); err != nil {
		t.Fatal(err)
	}
	if response.ProxyGeneration != proxywire.LegacyProxyGeneration {
		t.Fatalf("proxy generation = %d, want %d", response.ProxyGeneration, proxywire.LegacyProxyGeneration)
	}
	if response.ProxyCompatible == nil || !*response.ProxyCompatible {
		t.Fatalf("legacy proxy compatibility = %v, want true", response.ProxyCompatible)
	}
	if response.RequiredProxyGeneration != proxywire.ProxyGeneration {
		t.Fatalf("required proxy generation = %d, want %d", response.RequiredProxyGeneration, proxywire.ProxyGeneration)
	}
}

func TestHandleVersionReportsIncompatibleSchema(t *testing.T) {
	s := &APIServer{}
	s.SetProxyStatusFunc(func(_ context.Context) (*proxywire.Status, error) {
		return &proxywire.Status{
			Version:       "old",
			Generation:    proxywire.ProxyGeneration,
			SchemaVersion: proxywire.SchemaVersion - 1,
		}, nil
	})

	rr := httptest.NewRecorder()
	s.handleVersion().ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/v1/version", nil))

	var response apitypes.VersionResponse
	if err := json.NewDecoder(rr.Body).Decode(&response); err != nil {
		t.Fatal(err)
	}
	if response.ProxyCompatible == nil || *response.ProxyCompatible {
		t.Fatalf("proxy compatibility = %v, want false", response.ProxyCompatible)
	}
}

func TestHandleVersionOmitsCompatibilityWhenProxyUnavailable(t *testing.T) {
	s := &APIServer{}
	rr := httptest.NewRecorder()
	s.handleVersion().ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/v1/version", nil))

	var response apitypes.VersionResponse
	if err := json.NewDecoder(rr.Body).Decode(&response); err != nil {
		t.Fatal(err)
	}
	if response.ProxyCompatible != nil {
		t.Fatalf("proxy compatibility = %v, want nil", response.ProxyCompatible)
	}
	if response.RequiredProxySchemaVersion != proxywire.SchemaVersion {
		t.Fatalf("required schema = %d, want %d", response.RequiredProxySchemaVersion, proxywire.SchemaVersion)
	}
}
