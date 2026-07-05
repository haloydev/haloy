package haloyd

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/events"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/client"
	"github.com/haloydev/haloy/internal/api"
	"github.com/haloydev/haloy/internal/config"
	"github.com/haloydev/haloy/internal/constants"
	"github.com/haloydev/haloy/internal/docker"
	"github.com/haloydev/haloy/internal/healthcheck"
	"github.com/haloydev/haloy/internal/helpers"
	"github.com/haloydev/haloy/internal/layerstore"
	"github.com/haloydev/haloy/internal/logging"
	"github.com/haloydev/haloy/internal/proxyclient"
	"github.com/haloydev/haloy/internal/storage"
)

const (
	maintenanceInterval  = 12 * time.Hour   // Interval for periodic maintenance tasks
	eventDebounceDelay   = 5 * time.Second  // Delay for debouncing container events
	eventDebounceMaxWait = 30 * time.Second // Max debounce postponement while events keep arriving
	updateTimeout        = 15 * time.Minute // Max time for a single update operation
)

type ContainerEvent struct {
	Event     events.Message
	Container container.InspectResponse
	Labels    *config.ContainerLabels
}

func Run(debug bool) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	logLevel := slog.LevelInfo
	if debug {
		logLevel = slog.LevelDebug
	}

	// Allow streaming logs to the API server
	logBroker := logging.NewLogBroker()
	logger := logging.NewLogger(logLevel, logBroker)

	logger.Info("haloyd started",
		"version", constants.Version,
		"network", constants.DockerNetwork,
		"debug", debug)

	if debug {
		logger.Info("Debug mode enabled: Staging certificates will be used for all domains.")
	}

	db, err := storage.New()
	if err != nil {
		logging.LogFatal(logger, "Failed to initialize database", "error", err)
	}
	defer db.Close()
	if err := db.Migrate(); err != nil {
		logging.LogFatal(logger, "Failed to run database migrations", "error", err)
	}
	logger.Info("Database initialized successfully")

	dataDir, err := config.DataDir()
	if err != nil {
		logging.LogFatal(logger, "Failed to get data directory", "error", err)
	}
	configDir, err := config.HaloydConfigDir()
	if err != nil {
		logging.LogFatal(logger, "Failed to get haloyd config directory", "error", err)
	}
	configFilePath := filepath.Join(configDir, constants.HaloydConfigFileName)
	haloydConfig, err := config.LoadHaloydConfig(configFilePath)
	if err != nil {
		logging.LogFatal(logger, "Failed to load configuration file", "error", err)
	}

	cli, err := docker.NewClient(ctx)
	if err != nil {
		logging.LogFatal(logger, "Failed to create Docker client", "error", err)
	}
	defer cli.Close()

	apiToken := os.Getenv(constants.EnvVarAPIToken)
	if apiToken == "" {
		logging.LogFatal(logger, "%s environment variable not set", constants.EnvVarAPIToken)
	}

	apiServer := api.NewServer(apiToken, db, logBroker, logLevel)

	// The API is served on a loopback listener; the proxy forwards API-domain
	// and localhost API traffic to it.
	apiListenAddr := net.JoinHostPort(constants.HaloydAPIHost, constants.HaloydAPIPort)
	go func() {
		if err := apiServer.ListenAndServe(apiListenAddr); err != nil {
			logging.LogFatal(logger, "API listener failed", "addr", apiListenAddr, "error", err)
		}
	}()

	// Get API domain for proxy routing (default to localhost for local development).
	// Seed the proxy with this before the initial deployment discovery so the
	// control plane stays reachable even if discovery or certificate renewal fails.
	apiDomain := "localhost"
	if haloydConfig != nil && haloydConfig.API.Domain != "" {
		apiDomain = haloydConfig.API.Domain
	}

	// Connect to the haloy-proxy data plane. Snapshots are pushed over its
	// control socket and persisted to disk, so the proxy keeps serving (and
	// can restart) while haloyd is down or upgrading.
	proxyClient := proxyclient.New(dataDir, logger)
	proxyClient.Start(ctx)
	apiServer.SetProxyStatusFunc(proxyClient.Status)

	if err := proxyClient.WaitReady(ctx, 30*time.Second); err != nil {
		logger.Error("haloy-proxy is not responding; no traffic is being served. "+
			"Make sure the haloy-proxy service is installed and running "+
			"(re-run the server install/upgrade script if it is missing). "+
			"haloyd keeps retrying in the background.",
			"error", err)
	}

	// Seed the proxy with an API-domain-only snapshot before the initial
	// deployment discovery so the control plane stays reachable even if
	// discovery or certificate renewal fails.
	if err := proxyClient.Push(ctx, buildSnapshot(nil, nil, apiDomain, nil)); err != nil {
		logger.Warn("Failed to push initial proxy config", "error", err)
	}

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	// Channel for signaling cert updates needing proxy reload
	certUpdateSignal := make(chan string, 5)

	deploymentManager := NewDeploymentManager(cli, haloydConfig)
	certManagerConfig := CertificatesManagerConfig{
		CertDir:          filepath.Join(dataDir, constants.CertStorageDir),
		HTTPProviderPort: constants.CertificatesHTTPProviderPort,
		TlsStaging:       debug,
	}
	certManager, err := NewCertificatesManager(certManagerConfig, certUpdateSignal)
	if err != nil {
		logging.LogFatal(logger, "Failed to create certificate manager", "error", err)
	}

	updaterConfig := UpdaterConfig{
		Cli:               cli,
		DeploymentManager: deploymentManager,
		CertManager:       certManager,
		ProxyPusher:       proxyClient,
		APIDomain:         apiDomain,
	}

	updater := NewUpdater(updaterConfig)

	// Start Docker event listener BEFORE initial update so events aren't lost
	// during long-running health check retries. Buffer allows events to queue.
	eventsChan := make(chan ContainerEvent, 100)
	errorsChan := make(chan error)
	go listenForDockerEvents(ctx, cli, eventsChan, errorsChan, logger)

	debouncedEventsChan := make(chan debouncedAppEvent)

	appDebouncer := newAppDebouncer(eventDebounceDelay, eventDebounceMaxWait, debouncedEventsChan, logger)
	defer appDebouncer.stop()

	// Run initial update (Docker events will queue in buffered channel)
	if _, err := updater.Update(ctx, logger, TriggerReasonInitial, nil); err != nil {
		logger.Error("Initial update failed", "error", err)
	}

	logger.Info(
		"haloyd successfully initialized",
		logging.AttrHaloydInitComplete, true, // signal that the initialization is complete (haloyd init), used for logs.
	)

	// Start health monitor (enabled by default)
	var healthMonitor *healthcheck.HealthMonitor
	if haloydConfig == nil || haloydConfig.HealthMonitor.IsEnabled() {
		var healthConfig healthcheck.Config
		if haloydConfig != nil {
			healthConfig = healthcheck.Config{
				Enabled:  true,
				Interval: haloydConfig.HealthMonitor.GetInterval(),
				Fall:     haloydConfig.HealthMonitor.GetFall(),
				Rise:     haloydConfig.HealthMonitor.GetRise(),
				Timeout:  haloydConfig.HealthMonitor.GetTimeout(),
			}
		} else {
			healthConfig = healthcheck.DefaultConfig()
		}

		healthUpdater := NewHealthConfigUpdater(deploymentManager, proxyClient, apiDomain, logger)
		healthMonitor = healthcheck.NewHealthMonitor(healthConfig, deploymentManager, healthUpdater, logger)
		healthMonitor.Start()
	}

	maintenanceTicker := time.NewTicker(maintenanceInterval)
	defer maintenanceTicker.Stop()

	// Main event loop
	for {
		select {

		// All docker events are piped to debouncer
		case e := <-eventsChan:
			appDebouncer.captureEvent(e.Labels.AppName, e)

		// Debounced docker events
		case de := <-debouncedEventsChan:
			go func() {
				deploymentLogger := logging.NewDeploymentLogger(de.DeploymentID, logLevel, logBroker)

				updateCtx, cancelUpdate := context.WithTimeout(ctx, updateTimeout)
				defer cancelUpdate()

				app := &TriggeredByApp{
					appName:           de.AppName,
					domains:           de.Domains,
					deploymentID:      de.DeploymentID,
					dockerEventAction: de.EventAction,
				}

				if err := app.Validate(); err != nil {
					// Signal failure so a CLI streaming this deployment stops waiting.
					logging.LogDeploymentFailed(deploymentLogger, de.DeploymentID, de.AppName,
						"Deployment failed", fmt.Errorf("app data not valid: %w", err))
					return
				}

				result, err := updater.Update(updateCtx, deploymentLogger, TriggerReasonAppUpdated, app)
				if err != nil {
					logging.LogDeploymentFailed(deploymentLogger, de.DeploymentID, de.AppName,
						"Deployment failed", err)
					return
				}

				// Start event indicates that this is a new deployment and we'll signal the logger that the deployment is done.
				if de.CapturedStartEvent {
					// Check if the triggering app had any failures
					appFailures := result.GetAppFailures(de.AppName)

					// Check if there are any healthy instances for this app
					deployments := updater.deploymentManager.Deployments()
					appDeployment, appHasHealthyInstances := deployments[de.AppName]

					// Determine if the new deployment succeeded:
					// - If there are no healthy instances at all, the deployment failed
					// - If there are healthy instances but they're from an OLD deployment (different ID), the new deployment failed
					newDeploymentSucceeded := appHasHealthyInstances && appDeployment.Labels.DeploymentID == de.DeploymentID

					if len(appFailures) > 0 && !newDeploymentSucceeded {
						// New deployment failed - clean up failed containers and report deployment failure
						cleanupCtx, cleanupCancel := context.WithTimeout(ctx, 2*time.Minute)
						defer cleanupCancel()

						// Extract logs from failed containers before removing them to allow debugging
						logContainerFailureLogs(cleanupCtx, cli, deploymentLogger, appFailures)

						if _, err := docker.StopContainersByDeploymentID(cleanupCtx, cli, deploymentLogger, de.AppName, de.DeploymentID); err != nil {
							deploymentLogger.Warn("Failed to stop containers during cleanup", "error", err)
						}
						if _, err := docker.RemoveContainersByDeploymentID(cleanupCtx, cli, deploymentLogger, de.AppName, de.DeploymentID); err != nil {
							deploymentLogger.Warn("Failed to remove containers during cleanup", "error", err)
						}
						var failureReasons []string
						for _, f := range appFailures {
							failureReasons = append(failureReasons, fmt.Sprintf("%s: %v", f.Reason, f.Err))
						}
						logging.LogDeploymentFailed(deploymentLogger, de.DeploymentID, de.AppName,
							"Deployment failed", fmt.Errorf("%s", strings.Join(failureReasons, "; ")))
						return
					}

					canonicalDomains := make([]string, 0, len(de.Domains))
					if newDeploymentSucceeded {
						for _, domain := range appDeployment.Labels.Domains {
							canonicalDomains = append(canonicalDomains, domain.Canonical)
						}
					} else {
						// Fallback to domains from the event
						for _, domain := range de.Domains {
							canonicalDomains = append(canonicalDomains, domain.Canonical)
						}
					}
					logging.LogDeploymentComplete(deploymentLogger, canonicalDomains, de.DeploymentID, de.AppName,
						fmt.Sprintf("Deployed %s", de.AppName))
				} else {
					appFailures := result.GetAppFailures(de.AppName)
					deployments := updater.deploymentManager.Deployments()
					_, hasHealthy := deployments[de.AppName]

					if len(appFailures) > 0 && !hasHealthy {
						logCtx, logCancel := context.WithTimeout(ctx, 30*time.Second)
						defer logCancel()
						logContainerFailureLogs(logCtx, cli, deploymentLogger, appFailures)
						deploymentLogger.Error(fmt.Sprintf("Container died after deployment for %s, app has no healthy instances", de.AppName))
					}
				}
			}()

		case domainUpdated := <-certUpdateSignal:
			logger.Info("Received cert update signal", "domain", domainUpdated)
			reloadCtx, cancelReload := context.WithTimeout(ctx, 30*time.Second)
			if err := proxyClient.ReloadCerts(reloadCtx); err != nil {
				logger.Error("Failed to reload certificates",
					"reason", "cert update",
					"domain", domainUpdated,
					"error", err)
			}
			cancelReload()

		case <-maintenanceTicker.C:
			logger.Info("Performing periodic maintenance...")
			_, err := docker.PruneImages(ctx, cli, logger)
			if err != nil {
				logger.Warn("Failed to prune images", "error", err)
			}
			if pruned, freed, pruneErr := layerstore.PruneUnusedLayers(ctx, db, logger); pruneErr != nil {
				logger.Warn("Failed to prune unused layers", "error", pruneErr)
			} else if pruned > 0 {
				logger.Info("Pruned unused layers", "count", pruned, "bytes_freed", freed)
			}
			go func() {
				deploymentCtx, cancelDeployment := context.WithTimeout(ctx, updateTimeout)
				defer cancelDeployment()

				if _, err := updater.Update(deploymentCtx, logger, TriggerPeriodicRefresh, nil); err != nil {
					logger.Error("Background update failed", "error", err)
				}
			}()

		case err := <-errorsChan:
			logger.Error("Error from docker events", "error", err)

		case <-sigChan:
			logger.Info("Received shutdown signal, stopping haloyd...")
			if healthMonitor != nil {
				healthMonitor.Stop()
			}
			if certManager != nil {
				certManager.Stop()
			}
			cancel()
			return
		}
	}
}

// listenForDockerEvents sets up a listener for Docker events
func listenForDockerEvents(ctx context.Context, cli *client.Client, eventsChan chan ContainerEvent, errorsChan chan error, logger *slog.Logger) {
	filterArgs := filters.NewArgs()
	filterArgs.Add("type", "container")

	// Define allowed actions for event processing
	allowedActions := map[string]struct{}{
		"start":   {},
		"restart": {},
		"die":     {},
		"stop":    {},
		"kill":    {},
	}

	eventOptions := events.ListOptions{
		Filters: filterArgs,
	}

	events, errs := cli.Events(ctx, eventOptions)

	for {
		select {
		case <-ctx.Done():
			return
		case event := <-events:
			if _, ok := allowedActions[string(event.Action)]; ok {
				container, err := cli.ContainerInspect(ctx, event.Actor.ID)
				if err != nil {
					logger.Error("Error inspecting container",
						"containerID", helpers.SafeIDPrefix(event.Actor.ID),
						"error", err)
					continue
				}

				// We'll only process events for containers that have been marked with haloy app label.
				isHaloyApp := container.Config.Labels[config.LabelAppName] != ""
				if isHaloyApp {
					labels, err := config.ParseContainerLabels(container.Config.Labels)
					if err != nil {
						logger.Error("Error parsing container labels", "error", err)
						continue
					}

					logger.Debug("Container is eligible",
						"event", string(event.Action),
						"containerID", helpers.SafeIDPrefix(event.Actor.ID),
						"deploymentID", labels.DeploymentID)

					containerEvent := ContainerEvent{
						Event:     event,
						Container: container,
						Labels:    labels,
					}
					eventsChan <- containerEvent
				} else {
					logger.Debug("Container not eligible for haloy management",
						"containerID", helpers.SafeIDPrefix(event.Actor.ID))
				}
			}
		case err := <-errs:
			if err != nil {
				errorsChan <- err
				if err == io.EOF || strings.Contains(err.Error(), "connection refused") {
					time.Sleep(5 * time.Second)
					events, errs = cli.Events(ctx, eventOptions)
					continue
				}
			}
			return
		}
	}
}

// logContainerFailureLogs extracts and logs container output from failed containers.
// It deduplicates logs to avoid repeating the same output when multiple replicas fail
// with the same error. This helps users debug why their deployment failed.
func logContainerFailureLogs(ctx context.Context, cli *client.Client, logger *slog.Logger, failures []FailedContainer) {
	if len(failures) == 0 {
		return
	}

	// Track unique logs to avoid duplicating output when multiple replicas fail identically
	seenLogs := make(map[string]bool)
	const maxLogLines = 50

	for _, failure := range failures {
		if failure.ContainerID == "" {
			continue
		}

		logs, err := docker.GetContainerLogs(ctx, cli, failure.ContainerID, maxLogLines)
		if err != nil {
			logger.Debug("Could not retrieve container logs",
				"container_id", helpers.SafeIDPrefix(failure.ContainerID),
				"error", err)
			continue
		}

		// Trim whitespace and skip empty logs
		logs = strings.TrimSpace(logs)
		if logs == "" {
			continue
		}

		// Deduplicate: only show each unique log output once
		if seenLogs[logs] {
			continue
		}
		seenLogs[logs] = true

		// Log the container output
		logger.Error("Container logs from failed instance",
			"container_id", helpers.SafeIDPrefix(failure.ContainerID),
			"logs", "\n"+logs)
	}
}
