package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/haloydev/haloy/internal/logging"
)

type sseStreamConfig struct {
	logChan         <-chan logging.LogEntry
	cleanup         func()
	shouldTerminate func(logging.LogEntry) bool
	shouldSkip      func(logging.LogEntry) bool
}

// streamSSELogs handles the common SSE streaming logic
func streamSSELogs(w http.ResponseWriter, r *http.Request, config sseStreamConfig) {
	defer config.cleanup()

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	// Send initial keepalive to establish connection
	if _, err := w.Write([]byte(": keepalive\n\n")); err != nil {
		return
	}
	flusher.Flush()

	ctx := r.Context()
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

		case logEntry, ok := <-config.logChan:
			if !ok {
				return
			}

			if config.shouldSkip != nil && config.shouldSkip(logEntry) {
				continue
			}

			if err := writeSSEMessage(w, logEntry); err != nil {
				return
			}
			flusher.Flush()

			// Check if we should terminate the stream
			if config.shouldTerminate != nil && config.shouldTerminate(logEntry) {
				return
			}
		}
	}
}

// writeSSEMessage writes a log entry as Server-Sent Event
func writeSSEMessage(w http.ResponseWriter, entry logging.LogEntry) error {
	data, err := json.Marshal(entry)
	if err != nil {
		return fmt.Errorf("failed to marshal log entry: %w", err)
	}

	_, err = fmt.Fprintf(w, "data: %s\n\n", data)
	if err != nil {
		return fmt.Errorf("failed to write SSE data: %w", err)
	}

	return nil
}
