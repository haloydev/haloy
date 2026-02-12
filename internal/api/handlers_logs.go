package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/haloydev/haloy/internal/docker"
)

func (s *APIServer) handleAppLogs() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		appName := r.PathValue("appName")
		if appName == "" {
			http.Error(w, "App name is required", http.StatusBadRequest)
			return
		}

		tail := 100
		if t := r.URL.Query().Get("tail"); t != "" {
			if parsed, err := strconv.Atoi(t); err == nil && parsed > 0 {
				tail = parsed
			}
		}
		containerIDParam := r.URL.Query().Get("containerId")
		allContainers := r.URL.Query().Get("allContainers") == "true"

		ctx := r.Context()

		cli, containerList, err := getAppContainers(ctx, appName)
		if err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		defer cli.Close()

		targetIDs, err := selectContainers(containerList, containerIDParam, allContainers)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		var channels []<-chan docker.LogLine
		for _, id := range targetIDs {
			ch, err := docker.StreamContainerLogs(ctx, cli, id, tail)
			if err != nil {
				http.Error(w, fmt.Sprintf("failed to stream logs for container: %v", err), http.StatusInternalServerError)
				return
			}
			channels = append(channels, ch)
		}

		merged := mergeLogChannels(ctx, channels)

		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "streaming not supported", http.StatusInternalServerError)
			return
		}

		if _, err := w.Write([]byte(": keepalive\n\n")); err != nil {
			return
		}
		flusher.Flush()

		keepaliveTicker := time.NewTicker(30 * time.Second)
		defer keepaliveTicker.Stop()

		for {
			select {
			case <-ctx.Done():
				return

			case <-keepaliveTicker.C:
				if _, err := w.Write([]byte(": keepalive\n\n")); err != nil {
					return
				}
				flusher.Flush()

			case logLine, ok := <-merged:
				if !ok {
					return
				}
				data, err := json.Marshal(logLine)
				if err != nil {
					return
				}
				if _, err := fmt.Fprintf(w, "data: %s\n\n", data); err != nil {
					return
				}
				flusher.Flush()
			}
		}
	}
}

func mergeLogChannels(ctx context.Context, channels []<-chan docker.LogLine) <-chan docker.LogLine {
	merged := make(chan docker.LogLine, 100)

	var wg sync.WaitGroup
	for _, ch := range channels {
		wg.Add(1)
		go func(c <-chan docker.LogLine) {
			defer wg.Done()
			for line := range c {
				select {
				case <-ctx.Done():
					return
				case merged <- line:
				}
			}
		}(ch)
	}

	go func() {
		wg.Wait()
		close(merged)
	}()

	return merged
}
