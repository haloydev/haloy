package haloyd

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/docker/docker/api/types/events"
	"github.com/docker/docker/client"
	"github.com/haloydev/haloy/internal/config"
	"github.com/haloydev/haloy/internal/constants"
	"github.com/haloydev/haloy/internal/docker"
	"github.com/haloydev/haloy/internal/helpers"
)

type Updater struct {
	cli               *client.Client
	deploymentManager *DeploymentManager
	certManager       *CertificatesManager
	haproxyManager    *HAProxyManager
}

type UpdaterConfig struct {
	Cli               *client.Client
	DeploymentManager *DeploymentManager
	CertManager       *CertificatesManager
	HAProxyManager    *HAProxyManager
}

func NewUpdater(config UpdaterConfig) *Updater {
	return &Updater{
		cli:               config.Cli,
		deploymentManager: config.DeploymentManager,
		certManager:       config.CertManager,
		haproxyManager:    config.HAProxyManager,
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

	// On initial startup, wait for HAProxy to be ready before requesting certificates.
	// This is necessary because haloyd starts before HAProxy, and we need HAProxy
	// to be accepting connections to route ACME challenges from Let's Encrypt.
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

	// Apply the HAProxy configuration
	if err := u.haproxyManager.ApplyConfig(ctx, logger, deployments); err != nil {
		return result, fmt.Errorf("failed to apply HAProxy config for app: %w", err)
	}
	logger.Info("HAProxy configuration applied successfully")

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

// logPartialReplicaFailures logs warnings when some replicas of an app are healthy but others failed.
func logPartialReplicaFailures(healthy []HealthyContainer, failed []FailedContainer, logger *slog.Logger) {
	// Build map of healthy apps
	healthyApps := make(map[string]int)
	for _, c := range healthy {
		healthyApps[c.Labels.AppName]++
	}

	// Check for failed containers that have healthy siblings
	failedApps := make(map[string]int)
	for _, f := range failed {
		if f.Labels != nil {
			failedApps[f.Labels.AppName]++
		}
	}

	for appName, failedCount := range failedApps {
		if healthyCount, hasHealthy := healthyApps[appName]; hasHealthy {
			logger.Warn("Partial replica failure: some containers failed health check but deployment continues with healthy instances",
				"app", appName,
				"healthy_count", healthyCount,
				"failed_count", failedCount)
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

// waitForACMERouting waits for HAProxy to be accepting HTTP connections so that
// ACME HTTP-01 challenges can be routed to haloyd. This is called during initial
// startup before requesting certificates from Let's Encrypt.
func waitForACMERouting(ctx context.Context, logger *slog.Logger) error {
	const (
		maxRetries    = 30
		retryInterval = time.Second
	)

	client := &http.Client{
		Timeout: 5 * time.Second,
	}

	// HAProxy is on the same Docker network, accessible via container name.
	// We make a request to the root path which should return 404 from HAProxy's default backend.
	url := fmt.Sprintf("http://%s/", constants.HAProxyContainerName)

	for attempt := 1; attempt <= maxRetries; attempt++ {
		if ctx.Err() != nil {
			return fmt.Errorf("context canceled while waiting for HAProxy: %w", ctx.Err())
		}

		resp, err := client.Get(url)
		if err != nil {
			logger.Debug("Waiting for HAProxy to be ready", "attempt", attempt, "error", err)
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(retryInterval):
				continue
			}
		}
		resp.Body.Close()

		// Any HTTP response means HAProxy is accepting connections.
		// The default backend returns 404, but any response is fine.
		logger.Debug("HAProxy is ready", "status", resp.StatusCode, "attempt", attempt)
		return nil
	}

	return fmt.Errorf("timed out waiting for HAProxy after %d attempts", maxRetries)
}
