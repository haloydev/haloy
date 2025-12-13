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

func (u *Updater) Update(ctx context.Context, logger *slog.Logger, reason TriggerReason, app *TriggeredByApp) error {
	// Build Deployments and check if anything has changed (Thread-safe)
	deploymentsHasChanged, excludedContainers, err := u.deploymentManager.BuildDeployments(ctx, logger)
	if err != nil {
		return fmt.Errorf("failed to build deployments: %w", err)
	}

	logExcludedContainerReasons(excludedContainers, logger)

	// Skip further processing if no changes were detected and the reason is not an initial update.
	// We'll still want to continue on the initial update to ensure the API domain is set up correctly.
	if !deploymentsHasChanged && reason != TriggerReasonInitial {
		logger.Debug("Updater: No changes detected in deployments, skipping further processing")
		return nil
	}

	checkedDeployments, failedContainerIDs := u.deploymentManager.HealthCheckNewContainers(ctx, logger)
	if len(failedContainerIDs) > 0 {
		return fmt.Errorf("deployment aborted: failed to perform health check on containers (%s)", strings.Join(failedContainerIDs, ", "))
	} else {
		apps := make([]string, 0, len(checkedDeployments))
		for _, dep := range checkedDeployments {
			apps = append(apps, dep.Labels.AppName)
		}
		logger.Info("Health check completed", "apps", strings.Join(apps, ", "))
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
		return fmt.Errorf("failed to get certificate domains: %w", err)
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
			return fmt.Errorf("failed to refresh certificates for app %s: %w", app.appName, err)
		}
	} else if reason == TriggerReasonInitial {
		// Refresh synchronously on initial update so we can log api domain setup.
		if err := u.certManager.RefreshSync(logger, certDomains); err != nil {
			return err
		}
	} else {
		u.certManager.Refresh(logger, certDomains)
	}

	if reason == TriggerPeriodicRefresh {
		u.certManager.CleanupExpiredCertificates(logger, certDomains)
	}

	// Apply the HAProxy configuration
	if err := u.haproxyManager.ApplyConfig(ctx, logger, deployments); err != nil {
		return fmt.Errorf("failed to apply HAProxy config for app: %w", err)
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
			return fmt.Errorf("failed to stop old containers: %w", err)
		}
		_, err = docker.RemoveContainers(stopCtx, u.cli, logger, app.appName, app.deploymentID)
		if err != nil {
			return fmt.Errorf("failed to remove old containers: %w", err)
		}
	}

	return nil
}

func logExcludedContainerReasons(containers []ExcludedContainerInfo, logger *slog.Logger) {
	if len(containers) == 0 {
		return
	}
	for _, excluded := range containers {
		switch excluded.Reason {
		case ExclusionReasonInspectionFailed, ExclusionReasonLabelParsingFailed, ExclusionReasonIPExtractionFailed, ExclusionReasonPortMismatch:
			if excluded.Labels != nil {
				logger.Info(fmt.Sprintf("Failed to process container: %v", excluded.Message),
					"container_id", helpers.SafeIDPrefix(excluded.ContainerID),
					"app", excluded.Labels.AppName,
					"deployment_id", excluded.Labels.DeploymentID,
					"reason", excluded.Reason.String())
			} else {
				logger.Info("Container failed to start - no label info available",
					"container_id", helpers.SafeIDPrefix(excluded.ContainerID),
					"reason", excluded.Reason.String())
			}
		case ExclusionReasonNoDomains, ExclusionReasonNotDefaultNetwork:
			logger.Debug("Container excluded from further processing",
				"container_id", helpers.SafeIDPrefix(excluded.ContainerID),
				"reason", excluded.Reason.String(),
				"app", excluded.Labels.AppName)
		}
	}
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
