package api

import (
	"context"
	"net/http"

	"github.com/haloydev/haloy/internal/docker"
	"github.com/haloydev/haloy/internal/logging"
)

func (s *APIServer) handleStopApp() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		appName := r.PathValue("appName")
		if appName == "" {
			http.Error(w, "App name is required", http.StatusBadRequest)
			return
		}

		removeContainers := r.URL.Query().Get("remove-containers") == "true"
		removeVolumes := r.URL.Query().Get("remove-volumes") == "true"

		logger := logging.NewLogger(s.logLevel, s.logBroker)

		go func() {
			ctx := context.Background()
			cli, err := docker.NewClient(ctx)
			if err != nil {
				logger.Error("Failed to create Docker client for stop operation", "app", appName, "error", err)
				return
			}
			defer cli.Close()

			logger.Info("Stopping containers", "app", appName)
			stoppedIDs, err := docker.StopContainers(ctx, cli, logger, appName, "")
			if err != nil {
				logger.Error("Failed to stop containers", "app", appName, "error", err)
				return
			}

			if removeContainers {
				logger.Info("Removing containers", "app", appName)
				removedIDs, err := docker.RemoveContainers(ctx, cli, logger, appName, "")
				if err != nil {
					logger.Error("Failed to remove containers", "app", appName, "error", err)
					return
				}
				logger.Info("Successfully removed containers", "app", appName, "removed_count", len(removedIDs), "container_ids", removedIDs)

				if removeVolumes {
					logger.Info("Removing volumes", "app", appName)
					if err := docker.RemoveVolumes(ctx, cli, logger, appName); err != nil {
						logger.Error("Failed to remove volumes", "app", appName, "error", err)
						return
					}
					logger.Info("Successfully removed volumes", "app", appName)
				}
			}

			logger.Info("Successfully stopped containers", "app", appName, "stopped_count", len(stoppedIDs), "container_ids", stoppedIDs)
		}()

		w.WriteHeader(http.StatusAccepted)
	}
}
