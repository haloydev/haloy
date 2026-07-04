package haloyd

import (
	"context"
	"crypto/tls"
	"io"
	"log/slog"
	"testing"

	"github.com/haloydev/haloy/internal/config"
	"github.com/haloydev/haloy/internal/constants"
	"github.com/haloydev/haloy/internal/proxy"
	"github.com/haloydev/haloy/internal/proxywire"
)

type noopCertLoader struct{}

func (noopCertLoader) GetCertificate(*tls.ClientHelloInfo) (*tls.Certificate, error) {
	return nil, nil
}

// inProcessPusher applies snapshots directly to an embedded proxy, standing
// in for the proxyclient in tests.
type inProcessPusher struct {
	proxy *proxy.Proxy
}

func newInProcessPusher(p *proxy.Proxy) *inProcessPusher {
	return &inProcessPusher{proxy: p}
}

func (p *inProcessPusher) Push(_ context.Context, snap *proxywire.Snapshot) error {
	cfg, err := proxy.ConfigFromSnapshot(snap)
	if err != nil {
		return err
	}
	p.proxy.UpdateConfig(cfg)
	return nil
}

func TestHealthUpdaterKeepsRoutesWithoutHealthyBackends(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	proxyServer := proxy.New(logger, noopCertLoader{})
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

	updater := NewHealthConfigUpdater(deploymentManager, newInProcessPusher(proxyServer), "api.example.com", logger)
	updater.OnHealthChange(nil)

	config := proxyServer.GetConfig()
	if config == nil {
		t.Fatal("expected proxy config to be set")
	}

	route := config.FindRoute("app.example.com")
	if route == nil {
		t.Fatal("expected route to remain when backends unhealthy")
	}

	if len(route.Backends) != 0 {
		t.Fatalf("expected no backends, got %d", len(route.Backends))
	}
}

func TestHealthUpdaterKeepsRouteAfterDeploymentRemoved(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	proxyServer := proxy.New(logger, noopCertLoader{})
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

	// Container dies: no healthy containers left, deployment gets removed
	deploymentManager.UpdateDeployments(nil)

	failed := deploymentManager.FailedDeployments()
	if _, ok := failed["app"]; !ok {
		t.Fatal("expected app to be in FailedDeployments after removal")
	}

	updater := NewHealthConfigUpdater(deploymentManager, newInProcessPusher(proxyServer), "api.example.com", logger)
	updater.OnHealthChange(nil)

	cfg := proxyServer.GetConfig()
	if cfg == nil {
		t.Fatal("expected proxy config to be set")
	}

	route := cfg.FindRoute("app.example.com")
	if route == nil {
		t.Fatal("expected route to remain for failed deployment")
	}

	if len(route.Backends) != 0 {
		t.Fatalf("expected no backends for failed deployment, got %d", len(route.Backends))
	}
}
