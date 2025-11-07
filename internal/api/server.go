package api

import (
	"log/slog"
	"net/http"

	"github.com/haloydev/haloy/internal/logging"
)

// Server holds dependencies for the API handlers.
type APIServer struct {
	router    *http.ServeMux
	logBroker logging.StreamPublisher
	logLevel  slog.Level
	apiToken  string
}

func NewServer(apiToken string, logBroker logging.StreamPublisher, logLevel slog.Level) *APIServer {
	s := &APIServer{
		router:    http.NewServeMux(),
		logBroker: logBroker,
		logLevel:  logLevel,

		apiToken: apiToken,
	}
	s.setupRoutes()
	return s
}

// ListenAndServe starts the HTTP server.
func (s *APIServer) ListenAndServe(addr string) error {
	return http.ListenAndServe(addr, s.router)
}
