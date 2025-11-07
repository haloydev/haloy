package api

import (
	"net/http"
)

func (s *APIServer) handleLogs() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		logChan, subscriberID := s.logBroker.SubscribeGeneral()

		streamConfig := sseStreamConfig{
			logChan: logChan,
			cleanup: func() { s.logBroker.UnsubscribeGeneral(subscriberID) },
		}

		streamSSELogs(w, r, streamConfig)
	}
}
