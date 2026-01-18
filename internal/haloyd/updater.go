package haloyd

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/docker/docker/api/types/events"
	"github.com/docker/docker/client"
	"github.com/haloydev/haloy/internal/config"
	"github.com/haloydev/haloy/internal/docker"
	"github.com/haloydev/haloy/internal/helpers"
	"github.com/haloydev/haloy/internal/proxy"
)

type Updater struct {
	cli               *client.Client
	deploymentManager *DeploymentManager
	certManager       *CertificatesManager
	proxy             *proxy.Proxy
	apiDomain         string
}

type UpdaterConfig struct {
	Cli               *client.Client
	DeploymentManager *DeploymentManager
	CertManager       *CertificatesManager
	Proxy             *proxy.Proxy
	APIDomain         string
}

func NewUpdater(config UpdaterConfig) *Updater {
	return &Updater{
		cli:               config.Cli,
		deploymentManager: config.DeploymentManager,
		certManager:       config.CertManager,
		proxy:             config.Proxy,
		apiDomain:         config.APIDomain,
	}
}

type TriggeredByApp struct {
	appName           string
	domains           []config.Domain
	deploymentID      string
	dockerEventAction events.Action // Action that triggered the update (e.g., "start", "stop", etc.)
}

func (tba *TriggeredByApp) Validate() error {
	if tba.appName == "" {
		return fmt.Errorf("triggered by app: app name cannot be empty")
	}

	if len(tba.domains) > 0 {
		for i, domain := range tba.domains {
			if domain.Canonical == "" {
				return fmt.Errorf("triggered by app: Canonical name cannot be empty in index %d", i)
			}
		}
	}

	if tba.deploymentID == "" {
		return fmt.Errorf("triggered by app: latest deployment ID cannot be empty")
	}
	if tba.dockerEventAction == "" {
		return fmt.Errorf("triggered by app: docker event action cannot be empty")
	}
	return nil
}

type TriggerReason int

const (
	TriggerReasonInitial    TriggerReason = iota // Initial update at startup
	TriggerReasonAppUpdated                      // An app container was stopped, killed or removed
	TriggerPeriodicRefresh                       // Periodic refresh (e.g., every 5 minutes)
)

func (r TriggerReason) String() string {
	switch r {
	case TriggerReasonInitial:
		return "initial update"
	case TriggerReasonAppUpdated:
		return "app updated"
	case TriggerPeriodicRefresh:
		return "periodic refresh"
	default:
		return "unknown"
	}
}

// UpdateResult contains information about the update operation.
type UpdateResult struct {
	// FailedContainers contains containers that failed discovery or health check.
	// This is used by the caller to determine if a triggered app deployment failed.
	FailedContainers []FailedContainer
}

func (u *Updater) Update(ctx context.Context, logger *slog.Logger, reason TriggerReason, app *TriggeredByApp) (UpdateResult, error) {
	result := UpdateResult{}

	discovered, discoveryFailed, err := u.deploymentManager.DiscoverContainers(ctx, logger)
	if err != nil {
		return result, fmt.Errorf("failed to discover containers: %w", err)
	}

	logFailedContainers(discoveryFailed, logger, "discovery")

	healthy, healthCheckFailed := u.deploymentManager.HealthCheckContainers(ctx, logger, discovered)

	logFailedContainers(healthCheckFailed, logger, "health check")

	result.FailedContainers = append(result.FailedContainers, discoveryFailed...)
	result.FailedContainers = append(result.FailedContainers, healthCheckFailed...)

	// Log warnings for partial replica failures (some healthy, some failed for same app)
	logPartialReplicaFailures(healthy, healthCheckFailed, logger)

	// Step 3: Update deployments map from healthy containers
	deploymentsHasChanged := u.deploymentManager.UpdateDeployments(healthy)

	// Skip further processing if no changes were detected and the reason is not an initial update.
	// We'll still want to continue on the initial update to ensure the API domain is set up correctly.
	if !deploymentsHasChanged && reason != TriggerReasonInitial {
		logger.Debug("Updater: No changes detected in deployments, skipping further processing")
		return result, nil
	}

	// Log successful health checks
	if len(healthy) > 0 {
		apps := make(map[string]struct{})
		for _, c := range healthy {
			apps[c.Labels.AppName] = struct{}{}
		}
		appNames := make([]string, 0, len(apps))
		for appName := range apps {
			appNames = append(appNames, appName)
		}
		logger.Info("Health check completed", "apps", strings.Join(appNames, ", "))
	}

	deployments := u.deploymentManager.Deployments()

	// On initial startup, wait for the proxy to be ready before requesting certificates.
	// This ensures the proxy is accepting connections to route ACME challenges from Let's Encrypt.
	if reason == TriggerReasonInitial {
		if err := waitForACMERouting(ctx, logger); err != nil {
			logger.Warn("ACME routing check failed, continuing anyway", "error", err)
		}
	}

	// Certificates refresh logic based on trigger reason.
	certDomains, err := u.deploymentManager.GetCertificateDomains()
	if err != nil {
		return result, fmt.Errorf("failed to get certificate domains: %w", err)
	}

	// If an app is provided we refresh the certs synchronously so we can log the result.
	// Otherwise, we refresh them asynchronously to avoid blocking the main update process.
	// We also refresh the certs for that app only.
	if app != nil && len(app.domains) > 0 {
		appCanonicalDomains := make(map[string]struct{}, len(app.domains))
		for _, domain := range app.domains {
			appCanonicalDomains[domain.Canonical] = struct{}{}
		}

		var appCertDomains []CertificatesDomain
		for _, certDomain := range certDomains {
			if _, ok := appCanonicalDomains[certDomain.Canonical]; ok {
				appCertDomains = append(appCertDomains, certDomain)
			}
		}
		if err := u.certManager.RefreshSync(logger, appCertDomains); err != nil {
			return result, fmt.Errorf("failed to refresh certificates for app %s: %w", app.appName, err)
		}
	} else if reason == TriggerReasonInitial {
		// Refresh synchronously on initial update so we can log api domain setup.
		if err := u.certManager.RefreshSync(logger, certDomains); err != nil {
			return result, err
		}
	} else {
		u.certManager.Refresh(logger, certDomains)
	}

	if reason == TriggerPeriodicRefresh {
		u.certManager.CleanupExpiredCertificates(logger, certDomains)
	}

	// Update proxy configuration
	proxyConfig, err := u.buildProxyConfig(deployments)
	if err != nil {
		return result, fmt.Errorf("failed to build proxy config: %w", err)
	}
	u.proxy.UpdateConfig(proxyConfig)
	logger.Info("Proxy configuration applied successfully")

	// If an app is provided:
	// - stop old containers, remove and log the result.
	// - log successful deployment for app.
	if app != nil {
		stopCtx, cancelStop := context.WithTimeout(ctx, 10*time.Minute)
		defer cancelStop()
		_, err := docker.StopContainers(stopCtx, u.cli, logger, app.appName, app.deploymentID)
		if err != nil {
			return result, fmt.Errorf("failed to stop old containers: %w", err)
		}
		_, err = docker.RemoveContainers(stopCtx, u.cli, logger, app.appName, app.deploymentID)
		if err != nil {
			return result, fmt.Errorf("failed to remove old containers: %w", err)
		}
	}

	return result, nil
}

// logFailedContainers logs warnings about containers that failed during a specific phase.
// The final deployment success/failure is logged by the caller (haloyd.go).
func logFailedContainers(failed []FailedContainer, logger *slog.Logger, phase string) {
	for _, f := range failed {
		if f.Labels != nil {
			logger.Warn(fmt.Sprintf("Container failed %s: %s", phase, f.Reason),
				"container_id", helpers.SafeIDPrefix(f.ContainerID),
				"app", f.Labels.AppName,
				"deployment_id", f.Labels.DeploymentID,
				"error", f.Err)
		} else {
			logger.Warn(fmt.Sprintf("Container failed %s: %s", phase, f.Reason),
				"container_id", helpers.SafeIDPrefix(f.ContainerID),
				"error", f.Err)
		}
	}
}

// logPartialReplicaFailures logs warnings when some replicas of the same deployment are healthy but others failed.
// This only logs warnings for actual partial failures within a single deployment, not when a new deployment
// fails while an old deployment is still running.
func logPartialReplicaFailures(healthy []HealthyContainer, failed []FailedContainer, logger *slog.Logger) {
	// Build map of healthy apps by deployment ID: map[appName][deploymentID]count
	healthyApps := make(map[string]map[string]int)
	for _, c := range healthy {
		if healthyApps[c.Labels.AppName] == nil {
			healthyApps[c.Labels.AppName] = make(map[string]int)
		}
		healthyApps[c.Labels.AppName][c.Labels.DeploymentID]++
	}

	// Build map of failed apps by deployment ID: map[appName][deploymentID]count
	failedApps := make(map[string]map[string]int)
	for _, f := range failed {
		if f.Labels != nil {
			if failedApps[f.Labels.AppName] == nil {
				failedApps[f.Labels.AppName] = make(map[string]int)
			}
			failedApps[f.Labels.AppName][f.Labels.DeploymentID]++
		}
	}

	// Only log warnings when there are both healthy AND failed containers with the SAME deployment ID
	for appName, failedDeployments := range failedApps {
		healthyDeployments, hasHealthy := healthyApps[appName]
		if hasHealthy {
			for deploymentID, failedCount := range failedDeployments {
				if healthyCount, sameDeployment := healthyDeployments[deploymentID]; sameDeployment {
					// True partial failure: some replicas of the same deployment succeeded, others failed
					logger.Warn("Partial replica failure: some containers of the same deployment failed health check but deployment continues with healthy instances",
						"app", appName,
						"deployment_id", deploymentID,
						"healthy_count", healthyCount,
						"failed_count", failedCount)
				}
			}
		}
	}
}

// GetAppFailures returns failures for a specific app from the update result.
func (r *UpdateResult) GetAppFailures(appName string) []FailedContainer {
	var failures []FailedContainer
	for _, f := range r.FailedContainers {
		if f.Labels != nil && f.Labels.AppName == appName {
			failures = append(failures, f)
		}
	}
	return failures
}

// buildProxyConfig converts deployments to proxy configuration.
func (u *Updater) buildProxyConfig(deployments map[string]Deployment) (*proxy.Config, error) {
	haloydDeployments := make(map[string]proxy.HaloydDeployment)

	for appName, d := range deployments {
		var domains []proxy.HaloydDomain
		for _, domain := range d.Labels.Domains {
			domains = append(domains, proxy.HaloydDomain{
				Canonical: domain.Canonical,
				Aliases:   domain.Aliases,
			})
		}

		var instances []proxy.HaloydInstance
		for _, inst := range d.Instances {
			instances = append(instances, proxy.HaloydInstance{
				IP:   inst.IP,
				Port: inst.Port,
			})
		}

		haloydDeployments[appName] = proxy.HaloydDeployment{
			AppName:   appName,
			Domains:   domains,
			Instances: instances,
		}
	}

	return proxy.BuildConfigFromHaloydDeployments(haloydDeployments, u.apiDomain)
}

// waitForACMERouting verifies that ACME challenge routing is ready.
// Since the proxy is embedded in haloyd, ACME challenges are routed directly
// to the lego HTTP-01 server on port 8080 and are immediately available.
func waitForACMERouting(_ context.Context, logger *slog.Logger) error {
	logger.Debug("ACME routing ready (embedded proxy)")
	return nil
}
