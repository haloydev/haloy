package api

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/haloydev/haloy/internal/logging"
	"golang.org/x/time/rate"
)

type APIServer struct {
	router      *http.ServeMux
	logBroker   logging.StreamPublisher
	logLevel    slog.Level
	apiToken    string
	rateLimiter *RateLimiter
}

func NewServer(apiToken string, logBroker logging.StreamPublisher, logLevel slog.Level) *APIServer {
	s := &APIServer{
		router:      http.NewServeMux(),
		logBroker:   logBroker,
		logLevel:    logLevel,
		apiToken:    apiToken,
		rateLimiter: NewRateLimiter(rate.Limit(5), 10), // 5 req/sec, burst of 10
	}
	s.setupRoutes()
	return s
}

func (s *APIServer) ListenAndServe(addr string) error {
	srv := &http.Server{
		Addr:              addr,
		Handler:           s.router,
		ReadHeaderTimeout: 5 * time.Second,  // Prevent Slowloris
		IdleTimeout:       60 * time.Second, // Keep-alive connections
	}
	return srv.ListenAndServe()
}

// Handler returns the HTTP handler for this API server.
// This is used by the proxy to route API requests internally.
func (s *APIServer) Handler() http.Handler {
	return s.router
}
