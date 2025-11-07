package api

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/docker/docker/api/types/container"
	"github.com/haloydev/haloy/internal/apitypes"
	"github.com/haloydev/haloy/internal/config"
	"github.com/haloydev/haloy/internal/docker"
	"github.com/haloydev/haloy/internal/helpers"
)

func (s *APIServer) handleAppStatus() http.HandlerFunc {
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

		containerList, err := docker.GetAppContainers(ctx, cli, true, appName)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		if len(containerList) == 0 {
			http.Error(w, "No containers found for the specified app", http.StatusNotFound)
			return
		}

		response, err := getResponse(containerList)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		encodeJSON(w, http.StatusOK, response)
	}
}

func getResponse(containers []container.Summary) (apitypes.AppStatusResponse, error) {
	if len(containers) == 0 {
		return apitypes.AppStatusResponse{}, fmt.Errorf("no containers provided")
	}

	// Collect all data by deployment ID in one pass
	type deploymentData struct {
		containerIDs []string
		states       []string
		domains      []config.Domain
	}

	deploymentMap := make(map[string]*deploymentData)
	var latestDeploymentID string

	for _, c := range containers {
		labels, err := config.ParseContainerLabels(c.Labels)
		if err != nil {
			return apitypes.AppStatusResponse{}, fmt.Errorf("failed to parse labels for container %s: %w", helpers.SafeIDPrefix(c.ID), err)
		}

		// Initialize deployment data if not exists
		if deploymentMap[labels.DeploymentID] == nil {
			deploymentMap[labels.DeploymentID] = &deploymentData{
				containerIDs: []string{},
				states:       []string{},
				domains:      []config.Domain{},
			}
		}

		// Add container data to deployment
		deploymentMap[labels.DeploymentID].containerIDs = append(deploymentMap[labels.DeploymentID].containerIDs, c.ID)
		deploymentMap[labels.DeploymentID].states = append(deploymentMap[labels.DeploymentID].states, strings.ToLower(c.State))
		deploymentMap[labels.DeploymentID].domains = append(deploymentMap[labels.DeploymentID].domains, labels.Domains...)

		// Track latest deployment
		if labels.DeploymentID > latestDeploymentID {
			latestDeploymentID = labels.DeploymentID
		}
	}

	if latestDeploymentID == "" {
		return apitypes.AppStatusResponse{}, fmt.Errorf("no valid containers found")
	}

	// Get data for latest deployment
	latestDeployment := deploymentMap[latestDeploymentID]

	// Determine overall state from all containers in latest deployment
	overallState := determineOverallState(latestDeployment.states)

	return apitypes.AppStatusResponse{
		State:        overallState,
		DeploymentID: latestDeploymentID,
		ContainerIDs: latestDeployment.containerIDs,
		Domains:      latestDeployment.domains,
	}, nil
}

func determineOverallState(states []string) string {
	if len(states) == 0 {
		return "unknown"
	}

	// State priority: restarting > paused > running > exited
	statePriority := map[string]int{
		"restarting": 4,
		"paused":     3,
		"running":    2,
		"exited":     1,
		"created":    1,
		"dead":       0,
	}

	highestPriority := -1
	resultState := "unknown"

	for _, state := range states {
		if priority, exists := statePriority[state]; exists {
			if priority > highestPriority {
				highestPriority = priority
				resultState = state
			}
		}
	}

	return resultState
}
