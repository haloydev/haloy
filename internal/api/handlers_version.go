package api

import (
	"context"
	"net/http"
	"time"

	"github.com/haloydev/haloy/internal/apitypes"
	"github.com/haloydev/haloy/internal/constants"
)

func (s *APIServer) handleVersion() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		response := apitypes.VersionResponse{
			Version:      constants.Version,
			Capabilities: []string{constants.CapabilityLayerUpload, constants.CapabilityImagePreflight},
		}

		if s.proxyStatus != nil {
			ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
			defer cancel()
			if status, err := s.proxyStatus(ctx); err == nil {
				response.ProxyVersion = status.Version
				response.ProxyConfigHash = status.ConfigHash
			}
		}

		encodeJSON(w, http.StatusOK, response)
	}
}
