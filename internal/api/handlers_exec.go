package api

import (
	"context"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/haloydev/haloy/internal/apitypes"
	"github.com/haloydev/haloy/internal/docker"
	"github.com/haloydev/haloy/internal/helpers"
)

const execTimeout = 60 * time.Second

func (s *APIServer) handleExec() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		appName := r.PathValue("appName")
		if appName == "" {
			http.Error(w, "App name is required", http.StatusBadRequest)
			return
		}
		var req apitypes.ExecRequest

		if err := decodeJSON(r.Body, &req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		if len(req.Command) == 0 {
			http.Error(w, "Command is required", http.StatusBadRequest)
			return
		}

		// Validate mutually exclusive options
		if req.ContainerID != "" && req.AllContainers {
			http.Error(w, "Cannot specify both containerId and allContainers", http.StatusBadRequest)
			return
		}

		ctx := r.Context()
		ctx, cancel := context.WithTimeout(ctx, execTimeout)
		defer cancel()

		cli, err := docker.NewClient(ctx)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		defer cli.Close()

		// Get running containers for the app (only running, not all)
		containerList, err := docker.GetAppContainers(ctx, cli, false, appName)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if len(containerList) == 0 {
			http.Error(w, "No running containers found for the specified app", http.StatusNotFound)
			return
		}

		// Determine which containers to target
		var targetIDs []string

		switch {
		case req.ContainerID != "":
			// Find specific container by ID (supports short IDs)
			found := false
			for _, c := range containerList {
				if c.ID == req.ContainerID || strings.HasPrefix(c.ID, req.ContainerID) {
					targetIDs = append(targetIDs, c.ID)
					found = true
					break
				}
			}
			if !found {
				http.Error(w, "Specified container not found for this app", http.StatusNotFound)
				return
			}
		case req.AllContainers:
			// Target all containers
			for _, c := range containerList {
				targetIDs = append(targetIDs, c.ID)
			}
		default:
			// Default: first container
			targetIDs = append(targetIDs, containerList[0].ID)
		}

		// Execute command on each target container concurrently
		results := make([]apitypes.ExecResult, len(targetIDs))
		var wg sync.WaitGroup

		for i, containerID := range targetIDs {
			wg.Add(1)
			go func(idx int, cID string) {
				defer wg.Done()

				stdout, stderr, exitCode, err := docker.ExecInContainer(ctx, cli, cID, req.Command)

				result := apitypes.ExecResult{
					ContainerID: helpers.SafeIDPrefix(cID),
					ExitCode:    exitCode,
					Stdout:      stdout,
					Stderr:      stderr,
				}

				if err != nil {
					result.Error = err.Error()
				}

				results[idx] = result
			}(i, containerID)
		}

		wg.Wait()

		response := apitypes.ExecResponse{
			Results: results,
		}
		encodeJSON(w, http.StatusOK, response)
	}
}
