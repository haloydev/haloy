package api

import (
	"context"
	"net/http"
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

		ctx := r.Context()
		ctx, cancel := context.WithTimeout(ctx, execTimeout)
		defer cancel()

		cli, containerList, err := getAppContainers(ctx, appName)
		if err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		defer cli.Close()

		targetIDs, err := selectContainers(containerList, req.ContainerID, req.AllContainers)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
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
