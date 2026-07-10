package api

import (
	"context"
	"net/http"
	"time"

	"github.com/haloydev/haloy/internal/apitypes"
	"github.com/haloydev/haloy/internal/constants"
	"github.com/haloydev/haloy/internal/proxywire"
)

func (s *APIServer) handleVersion() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		response := apitypes.VersionResponse{
			Version:                    constants.Version,
			RequiredProxyGeneration:    proxywire.ProxyGeneration,
			RequiredProxySchemaVersion: proxywire.SchemaVersion,
			Capabilities:               []string{constants.CapabilityLayerUpload, constants.CapabilityImagePreflight},
		}

		if s.proxyStatus != nil {
			ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
			defer cancel()
			if status, err := s.proxyStatus(ctx); err == nil {
				compatible := proxywire.IsProxyCompatible(status.Generation, status.SchemaVersion)
				response.ProxyVersion = status.Version
				response.ProxyGeneration = proxywire.NormalizeProxyGeneration(status.Generation)
				response.ProxySchemaVersion = status.SchemaVersion
				response.ProxyCompatible = &compatible
				response.ProxyConfigHash = status.ConfigHash
			}
		}

		encodeJSON(w, http.StatusOK, response)
	}
}
