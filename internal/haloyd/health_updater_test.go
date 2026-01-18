package haloyd

import (
	"crypto/tls"
	"io"
	"log/slog"
	"net/http"
	"testing"

	"github.com/haloydev/haloy/internal/config"
	"github.com/haloydev/haloy/internal/constants"
	"github.com/haloydev/haloy/internal/proxy"
)

type noopCertLoader struct{}

func (noopCertLoader) GetCertificate(*tls.ClientHelloInfo) (*tls.Certificate, error) {
	return nil, nil
}

func TestHealthUpdaterKeepsRoutesWithoutHealthyBackends(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	proxyServer := proxy.New(logger, noopCertLoader{}, http.NewServeMux())
	deploymentManager := NewDeploymentManager(nil, nil)

	labels := &config.ContainerLabels{
		AppName:      "app",
		DeploymentID: "1",
		Port:         config.Port(constants.DefaultContainerPort),
		Domains: []config.Domain{
			{Canonical: "app.example.com"},
		},
	}

	healthy := []HealthyContainer{
		{
			ContainerID: "container-1",
			Labels:      labels,
			IP:          "10.0.0.1",
			Port:        "8080",
		},
	}

	deploymentManager.UpdateDeployments(healthy)

	updater := NewHealthConfigUpdater(deploymentManager, proxyServer, "api.example.com", logger)
	updater.OnHealthChange(nil)

	config := proxyServer.GetConfig()
	if config == nil {
		t.Fatal("expected proxy config to be set")
	}

	route := config.Routes["app.example.com"]
	if route == nil {
		t.Fatal("expected route to remain when backends unhealthy")
	}

	if len(route.Backends) != 0 {
		t.Fatalf("expected no backends, got %d", len(route.Backends))
	}
}
