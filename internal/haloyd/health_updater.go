package haloyd

import (
	"log/slog"

	"github.com/haloydev/haloy/internal/healthcheck"
	"github.com/haloydev/haloy/internal/proxy"
)

// HealthConfigUpdater bridges the health monitor to the proxy configuration.
// When health state changes, it rebuilds the proxy config with only healthy backends.
type HealthConfigUpdater struct {
	deploymentManager *DeploymentManager
	proxy             *proxy.Proxy
	apiDomain         string
	logger            *slog.Logger
}

// NewHealthConfigUpdater creates a new health config updater.
func NewHealthConfigUpdater(
	deploymentManager *DeploymentManager,
	proxy *proxy.Proxy,
	apiDomain string,
	logger *slog.Logger,
) *HealthConfigUpdater {
	return &HealthConfigUpdater{
		deploymentManager: deploymentManager,
		proxy:             proxy,
		apiDomain:         apiDomain,
		logger:            logger,
	}
}

// OnHealthChange is called when the health state of any target changes.
// It rebuilds the proxy configuration with only the healthy backends.
func (u *HealthConfigUpdater) OnHealthChange(healthyTargets []healthcheck.Target) {
	// Build a set of healthy container IDs for quick lookup
	healthyIDs := make(map[string]struct{}, len(healthyTargets))
	for _, t := range healthyTargets {
		healthyIDs[t.ID] = struct{}{}
	}

	// Get all deployments from the deployment manager
	deployments := u.deploymentManager.Deployments()

	// Build proxy config with only healthy instances
	haloydDeployments := make(map[string]proxy.HaloydDeployment)

	for appName, d := range deployments {
		var domains []proxy.HaloydDomain
		for _, domain := range d.Labels.Domains {
			domains = append(domains, proxy.HaloydDomain{
				Canonical: domain.Canonical,
				Aliases:   domain.Aliases,
			})
		}

		// Filter to only healthy instances
		var healthyInstances []proxy.HaloydInstance
		for _, inst := range d.Instances {
			if _, isHealthy := healthyIDs[inst.ContainerID]; isHealthy {
				healthyInstances = append(healthyInstances, proxy.HaloydInstance{
					IP:   inst.IP,
					Port: inst.Port,
				})
			}
		}

		// Only include the app if it has at least one healthy instance
		if len(healthyInstances) > 0 {
			haloydDeployments[appName] = proxy.HaloydDeployment{
				AppName:   appName,
				Domains:   domains,
				Instances: healthyInstances,
			}
		} else {
			u.logger.Warn("App has no healthy backends",
				"app", appName,
				"total_instances", len(d.Instances))
		}
	}

	proxyConfig, err := proxy.BuildConfigFromHaloydDeployments(haloydDeployments, u.apiDomain)
	if err != nil {
		u.logger.Error("Failed to build proxy config from health check", "error", err)
		return
	}

	u.proxy.UpdateConfig(proxyConfig)
	u.logger.Info("Proxy configuration updated from health monitor",
		"healthy_apps", len(haloydDeployments),
		"healthy_targets", len(healthyTargets))
}
