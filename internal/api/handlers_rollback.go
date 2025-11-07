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

func (s *APIServer) handleRollback() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req apitypes.RollbackRequest
		if err := decodeJSON(r.Body, &req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		appConfig := req.NewTargetConfig

		if req.TargetDeploymentID == "" {
			http.Error(w, "Target deployment ID is required", http.StatusBadRequest)
			return
		}
		if req.NewDeploymentID == "" {
			http.Error(w, "New deployment ID is required", http.StatusBadRequest)
			return
		}

		if err := appConfig.Validate(appConfig.Format); err != nil {
			http.Error(w, fmt.Sprintf("Invalid app configuration: %v", err), http.StatusBadRequest)
			return
		}

		deploymentLogger := logging.NewDeploymentLogger(req.NewDeploymentID, s.logLevel, s.logBroker)

		go func() {
			ctx := context.Background()
			ctx, cancel := context.WithTimeout(ctx, defaultContextTimeout)
			defer cancel()

			cli, err := docker.NewClient(ctx)
			if err != nil {
				deploymentLogger.Error("Failed to create Docker client", "error", err)
				return
			}
			defer cli.Close()

			if err := deploy.RollbackApp(ctx, cli, appConfig, req.TargetDeploymentID, req.NewDeploymentID, deploymentLogger); err != nil {
				deploymentLogger.Error("Deployment failed", "app", appConfig.Name, "error", err)
				return
			}
			deploymentLogger.Info("Rollback initiated", "app", appConfig.Name, "deploymentID", req.NewDeploymentID)
		}()

		w.WriteHeader(http.StatusAccepted)
	}
}

func (s *APIServer) handleRollbackTargets() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		appName := r.PathValue("appName")
		if appName == "" {
			http.Error(w, "App name is required", http.StatusBadRequest)
			return
		}

		ctx := r.Context()
		ctx, cancel := context.WithTimeout(ctx, defaultContextTimeout)
		defer cancel()

		cli, err := docker.NewClient(ctx)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		defer cli.Close()

		targets, err := deploy.GetRollbackTargets(ctx, cli, appName)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		response := apitypes.RollbackTargetsResponse{
			Targets: targets,
		}

		encodeJSON(w, http.StatusOK, response)
	}
}
