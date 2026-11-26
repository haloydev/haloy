package haloyd

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"

	"github.com/docker/docker/api/types/events"
	"github.com/docker/docker/client"
	"github.com/haloydev/haloy/internal/config"
	"github.com/haloydev/haloy/internal/helpers"
	"github.com/haloydev/haloy/internal/proxyclient"
)

type certRefresher interface {
	Refresh(*slog.Logger, []CertificatesDomain)
	RefreshSync(*slog.Logger, []CertificatesDomain) error
	CleanupExpiredCertificates(*slog.Logger, []CertificatesDomain)
}

type Updater struct {
	runtime           containerRuntime
	deploymentManager *DeploymentManager
	certManager       certRefresher
	publisher         *routePublisher
	// mu serializes Update calls. Concurrent updates would race on the
	// deployments map: the slower one would overwrite newer state with its
	// stale discovery snapshot and push a stale proxy config.
	mu sync.Mutex
}

type UpdaterConfig struct {
	Cli               *client.Client
	DeploymentManager *DeploymentManager
	CertManager       *CertificatesManager
	ProxyPusher       ProxyPusher
	APIDomain         string
	// Logger receives publisher messages that concern the whole server, so
	// they do not end up in the log stream of whichever deployment happened
	// to trigger the publication.
	Logger *slog.Logger
}

func NewUpdater(config UpdaterConfig) *Updater {
	return &Updater{
		runtime:           dockerRuntime{config.Cli},
		deploymentManager: config.DeploymentManager,
		certManager:       config.CertManager,
		publisher:         &routePublisher{dm: config.DeploymentManager, pusher: config.ProxyPusher, apiDomain: config.APIDomain, logger: config.Logger},
	}
}

type TriggeredByApp struct {
	startedSelected   bool
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
	u.mu.Lock()
	defer u.mu.Unlock()

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

	return result, u.reconcile(ctx, logger, reason, app, healthy)
}

func (u *Updater) reconcile(ctx context.Context, logger *slog.Logger, reason TriggerReason, app *TriggeredByApp, healthy []HealthyContainer) error {
	deploymentsHasChanged := u.deploymentManager.UpdateDeployments(healthy)

	// Certificate maintenance must run even when deployments are unchanged:
	// renewals come due by expiry alone, and a failed obtain (e.g. DNS not
	// ready yet after a domain change) is retried on later refreshes.
	certDomains, err := u.deploymentManager.GetCertificateDomains()
	if err != nil {
		return fmt.Errorf("failed to get certificate domains: %w", err)
	}

	deployments := u.deploymentManager.Deployments()
	eligible := reason == TriggerReasonAppUpdated && shouldCleanupOldContainers(deployments, app)
	publish := deploymentsHasChanged || reason == TriggerReasonInitial || !u.publisher.confirmed()
	if !publish {
		logger.Debug("Deployments unchanged, refreshing certificates")
		u.certManager.Refresh(logger, certDomains)
		if reason == TriggerPeriodicRefresh {
			u.certManager.CleanupExpiredCertificates(logger, certDomains)
		}
		if !eligible {
			return nil
		}
	}

	// Log successful health checks
	if len(healthy) > 0 {
		apps := make(map[string]struct{})
		for _, c := range healthy {
			if app != nil && c.Labels.AppName != app.appName {
				continue
			}
			apps[c.Labels.AppName] = struct{}{}
		}
		for appName := range apps {
			logger.Info(fmt.Sprintf("Health check passed for %s", appName))
		}
	}

	// Make discovered healthy containers routable before synchronous
	// certificate renewal. Existing certificates can still serve traffic, ACME
	// challenges are forwarded to haloyd regardless of the route table, and a
	// transient ACME failure should not leave the proxy config stale or the
	// route table empty on startup.
	unreachable := false
	if publish {
		if err := u.publisher.publish(ctx); err != nil {
			if !errors.Is(err, proxyclient.ErrUnreachable) {
				// An unconfirmed publisher republishes on every reconcile,
				// so while pushes keep failing this is the only certificate
				// maintenance that runs. Renewals must not wait for routing
				// to recover.
				if reason != TriggerReasonInitial {
					u.certManager.Refresh(logger, certDomains)
				}
				return fmt.Errorf("failed to push proxy config: %w", err)
			}
			unreachable = true
			if eligible {
				logger.Warn("Proxy unreachable during config push. Skipped cleanup for this deployment. Older containers may still be running.",
					"app", app.appName, "deployment_id", app.deploymentID, "error", err)
			} else {
				logger.Warn("Proxy unreachable during config push", "error", err)
			}
		}
	}

	// Certificates refresh logic based on trigger reason.
	// If an app is provided we refresh the certs synchronously so we can log the result.
	// Otherwise, we refresh them asynchronously to avoid blocking the main update process.
	// We also refresh the certs for that app only.
	var domains []config.Domain
	if eligible {
		domains = deployments[app.appName].Labels.Domains
	} else if app != nil {
		domains = app.domains
	}
	if len(domains) > 0 {
		appCanonicalDomains := make(map[string]struct{}, len(domains))
		for _, domain := range domains {
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

	if eligible && !unreachable {
		return u.retire(ctx, logger, app)
	}
	return nil
}

// shouldCleanupOldContainers reports whether the containers of other
// deployments of the triggering app may be stopped and removed. That is only
// safe when the triggering deployment is the app's healthy current one: a
// batch made of an old container's stop events carries the old id, and a
// failed new deployment must not take the healthy old one down with it.
func shouldCleanupOldContainers(deployments map[string]Deployment, app *TriggeredByApp) bool {
	if app == nil || !app.startedSelected {
		return false
	}
	current, ok := deployments[app.appName]
	return ok && current.Labels != nil && current.Labels.DeploymentID == app.deploymentID
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
