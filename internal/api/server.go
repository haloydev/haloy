package api

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/haloydev/haloy/internal/apitypes"
	"github.com/haloydev/haloy/internal/logging"
	"github.com/haloydev/haloy/internal/storage"
	"golang.org/x/time/rate"
)

type APIServer struct {
	router                 *http.ServeMux
	db                     *storage.DB
	logBroker              logging.StreamPublisher
	logLevel               slog.Level
	apiToken               string
	rateLimiter            *RateLimiter
	layerRateLimiter       *RateLimiter
	uploadDiskSpaceCheck   func(context.Context, int64) error
	assembleDiskSpaceCheck func(context.Context, apitypes.ImageAssembleRequest) error
	imageDiskSpaceCheck    func(context.Context, apitypes.ImageDiskSpaceCheckRequest) (diskSpaceCheckResult, error)
	imagePrune             func(context.Context, apitypes.ImagePruneRequest) (apitypes.ImagePruneResponse, error)
}

func NewServer(apiToken string, db *storage.DB, logBroker logging.StreamPublisher, logLevel slog.Level) *APIServer {
	s := &APIServer{
		router:           http.NewServeMux(),
		db:               db,
		logBroker:        logBroker,
		logLevel:         logLevel,
		apiToken:         apiToken,
		rateLimiter:      NewRateLimiter(rate.Limit(5), 10),   // 5 req/sec, burst of 10
		layerRateLimiter: NewRateLimiter(rate.Limit(50), 100), // 50 req/sec, burst of 100 for layer uploads
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
