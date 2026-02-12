package api

import (
	"net/http"

	"github.com/haloydev/haloy/internal/logging"
)

func (s *APIServer) handleServerLogs() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		logChan, subscriberID := s.logBroker.SubscribeGeneral()
		includeAccessLogs := r.URL.Query().Get("access-logs") == "true"

		streamConfig := sseStreamConfig{
			logChan: logChan,
			cleanup: func() { s.logBroker.UnsubscribeGeneral(subscriberID) },
		}

		if !includeAccessLogs {
			streamConfig.shouldSkip = func(entry logging.LogEntry) bool {
				return entry.Message == "request"
			}
		}

		streamSSELogs(w, r, streamConfig)
	}
}
