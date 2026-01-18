package api

import (
	"net/http"

	"github.com/haloydev/haloy/internal/apitypes"
	"github.com/haloydev/haloy/internal/constants"
)

func (s *APIServer) handleVersion() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		response := apitypes.VersionResponse{
			Version:      constants.Version,
			Capabilities: []string{"layer-upload"},
		}

		encodeJSON(w, http.StatusOK, response)
	}
}
