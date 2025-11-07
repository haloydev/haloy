package haloyd

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/haloydev/haloy/internal/api"
	"github.com/haloydev/haloy/internal/config"
	"github.com/haloydev/haloy/internal/constants"
	"github.com/haloydev/haloy/internal/docker"
	"github.com/haloydev/haloy/internal/helpers"
	"github.com/haloydev/haloy/internal/logging"
	"github.com/haloydev/haloy/internal/storage"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/events"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/client"
)

const (
	maintenanceInterval = 12 * time.Hour   // Interval for periodic maintenance tasks
	eventDebounceDelay  = 5 * time.Second  // Delay for debouncing container events
	updateTimeout       = 15 * time.Minute // Max time for a single update operation
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
		logger.Info("Debug mode enabled: No changes will be applied to HAProxy. Staging certificates will be used for all domains.")
	}

	db, err := storage.New()
	if err != nil {
		logger.Error("Failed to initialize database", "error", err)
		return
	}
	defer db.Close()
	if err := db.Migrate(); err != nil {
		logger.Error("Failed to run database migrations", "error", err)
		return
	}
	logger.Info("Database initialized successfully")

	dataDir, err := config.DataDir()
	if err != nil {
		logger.Error("Failed to get data directory", "error", err)
		return
	}
	configDir, err := config.ConfigDir()
	if err != nil {
		logger.Error("Failed to get haloyd config directory", "error", err)
		return
	}
	configFilePath := filepath.Join(configDir, constants.HaloydConfigFileName)
	haloydConfig, err := config.LoadHaloydConfig(configFilePath)
	if err != nil {
		logger.Error("Failed to load configuration file", "error", err)
		return
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

	apiServer := api.NewServer(apiToken, logBroker, logLevel)
	go func() {
		logger.Info(fmt.Sprintf("Starting API server on :%s...", constants.APIServerPort))
		if err := apiServer.ListenAndServe(fmt.Sprintf(":%s", constants.APIServerPort)); err != nil && err != http.ErrServerClosed {
			logging.LogFatal(logger, "API server failed", "error", err)
		}
	}()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	// Channel for signaling cert updates needing HAProxy reload
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
	haproxyManager := NewHAProxyManager(cli, haloydConfig, filepath.Join(dataDir, constants.HAProxyConfigDir), debug)
	updaterConfig := UpdaterConfig{
		Cli:               cli,
		DeploymentManager: deploymentManager,
		CertManager:       certManager,
		HAProxyManager:    haproxyManager,
	}

	updater := NewUpdater(updaterConfig)
	if err := updater.Update(ctx, logger, TriggerReasonInitial, nil); err != nil {
		logger.Error("Initial update failed", "error", err)
	}

	logger.Info("haloyd successfully initialized",
		logging.AttrHaloydInitComplete, true, // signal that the initialization is complete (haloyadm init), used for logs.
	)

	// Docker event listener
	eventsChan := make(chan ContainerEvent)
	errorsChan := make(chan error)
	go listenForDockerEvents(ctx, cli, eventsChan, errorsChan, logger)

	debouncedEventsChan := make(chan debouncedAppEvent)
	defer close(debouncedEventsChan)

	appDebouncer := newAppDebouncer(eventDebounceDelay, debouncedEventsChan, logger)
	defer appDebouncer.stop()

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
					deploymentLogger.Error("App data not valid", "error", err)
					return
				}

				if err := updater.Update(updateCtx, deploymentLogger, TriggerReasonAppUpdated, app); err != nil {
					logging.LogDeploymentFailed(deploymentLogger, de.DeploymentID, de.AppName,
						"Deployment failed", err)
					return
				}

				// Start event indicates that this is a new deployment and we'll signal the logger that the deployment is done.
				if de.CapturedStartEvent {
					canonicalDomains := make([]string, len(de.Domains))
					for i, domain := range de.Domains {
						canonicalDomains[i] = domain.Canonical
					}
					logging.LogDeploymentComplete(deploymentLogger, canonicalDomains, de.DeploymentID, de.AppName,
						fmt.Sprintf("Successfully deployed %s", de.AppName))
				}
			}()

		case domainUpdated := <-certUpdateSignal:
			logger.Info("Received cert update signal", "domain", domainUpdated)

			go func() {
				// Use a timeout context for this specific task
				updateCtx, cancelUpdate := context.WithTimeout(ctx, 60*time.Second)
				defer cancelUpdate()

				// Update only needs to apply config, not full build/check
				// We assume the deployment state triggering the cert update is still valid.
				currentDeployments := updater.deploymentManager.Deployments()
				if err := updater.haproxyManager.ApplyConfig(updateCtx, logger, currentDeployments); err != nil {
					logger.Error("Background HAProxy update failed",
						"reason", "cert update",
						"domain", domainUpdated,
						"error", err)
				}
			}()

		case <-maintenanceTicker.C:
			logger.Info("Performing periodic maintenance...")
			_, err := docker.PruneImages(ctx, cli, logger)
			if err != nil {
				logger.Warn("Failed to prune images", "error", err)
			}
			go func() {
				deploymentCtx, cancelDeployment := context.WithCancel(ctx)
				defer cancelDeployment()

				if err := updater.Update(deploymentCtx, logger, TriggerPeriodicRefresh, nil); err != nil {
					logger.Error("Background update failed", "error", err)
				}
			}()

		case err := <-errorsChan:
			logger.Error("Error from docker events", "error", err)

		case <-sigChan:
			logger.Info("Received shutdown signal, stopping haloyd...")
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
				isHaloyApp := container.Config.Labels[config.LabelRole] == config.AppLabelRole
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
				// For non-fatal errors we'll try to reconnect instead of exiting
				if err != io.EOF && !strings.Contains(err.Error(), "connection refused") {
					// Attempt to reconnect
					time.Sleep(5 * time.Second)
					events, errs = cli.Events(ctx, eventOptions)
					continue
				}
			}
			return
		}
	}
}
