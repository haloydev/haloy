package api

import (
	"encoding/json"
	"net/http"

	"github.com/haloydev/haloy/internal/apitypes"
	"github.com/haloydev/haloy/internal/constants"
)

// handleHealth returns a simple health check endpoint
func (s *APIServer) handleHealth() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		response := apitypes.HealthResponse{
			Status:  "ok",
			Service: "haloyd",
			Version: constants.Version,
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)

		if err := json.NewEncoder(w).Encode(response); err != nil {
			http.Error(w, "Failed to encode response", http.StatusInternalServerError)
			return
		}
	}
}
