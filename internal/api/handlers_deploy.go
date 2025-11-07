package api

import (
	"context"
	"fmt"
	"net/http"

	"github.com/haloydev/haloy/internal/apitypes"
	"github.com/haloydev/haloy/internal/deploy"
	"github.com/haloydev/haloy/internal/docker"
	"github.com/haloydev/haloy/internal/logging"
)

// handleDeploy returns an http.HandlerFunc for deploying an app.
func (s *APIServer) handleDeploy() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req apitypes.DeployRequest

		if err := decodeJSON(r.Body, &req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		if req.DeploymentID == "" {
			http.Error(w, "Deployment ID is required", http.StatusBadRequest)
			return
		}

		if err := req.TargetConfig.Validate(req.TargetConfig.Format); err != nil {
			http.Error(w, fmt.Sprintf("Invalid app configuration: %v", err), http.StatusBadRequest)
			return
		}

		deploymentLogger := logging.NewDeploymentLogger(req.DeploymentID, s.logLevel, s.logBroker)

		ctx, cancel := context.WithTimeout(context.Background(), defaultContextTimeout)

		go func() {
			defer cancel()

			cli, err := docker.NewClient(ctx)
			if err != nil {
				deploymentLogger.Error("Failed to create Docker client", "error", err)
				return
			}
			defer cli.Close()

			if err := deploy.DeployApp(ctx, cli, req.DeploymentID, req.TargetConfig, req.RollbackAppConfig, deploymentLogger); err != nil {
				logging.LogDeploymentFailed(deploymentLogger, req.DeploymentID, req.TargetConfig.Name, "Deployment failed", err)
				return
			}
		}()

		w.WriteHeader(http.StatusAccepted)
	}
}

// handleDeploymentLogs handles SSE connections for deployment logs
func (s *APIServer) handleDeploymentLogs() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		deploymentID := r.PathValue("deploymentID")
		if deploymentID == "" {
			http.Error(w, "deployment ID is required", http.StatusBadRequest)
			return
		}

		// Subscribe to logs for this deployment ID
		// Don't pass request context - use background context with manual cleanup
		logChan := s.logBroker.SubscribeDeployment(deploymentID)

		streamConfig := sseStreamConfig{
			logChan: logChan,
			cleanup: func() { s.logBroker.UnsubscribeDeployment(deploymentID) },
			shouldTerminate: func(logEntry logging.LogEntry) bool {
				return logEntry.IsDeploymentComplete || logEntry.IsDeploymentFailed
			},
		}

		streamSSELogs(w, r, streamConfig)
	}
}
