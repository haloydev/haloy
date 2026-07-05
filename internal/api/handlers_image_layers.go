package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"

	"github.com/haloydev/haloy/internal/apitypes"
	"github.com/haloydev/haloy/internal/docker"
	"github.com/haloydev/haloy/internal/layerstore"
	"github.com/haloydev/haloy/internal/logging"
)

// handleLayerCheck checks which layers already exist on the server
func (s *APIServer) handleLayerCheck() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req apitypes.LayerCheckRequest
		if err := decodeJSON(http.MaxBytesReader(w, r.Body, maxJSONBodyBytes), &req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		if len(req.Digests) == 0 {
			http.Error(w, "digests array cannot be empty", http.StatusBadRequest)
			return
		}

		for _, digest := range req.Digests {
			if err := layerstore.ValidateDigest(digest); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
		}

		store, err := layerstore.New(s.db)
		if err != nil {
			http.Error(w, "Failed to initialize layer store", http.StatusInternalServerError)
			return
		}

		missing, exists, err := store.HasLayers(req.Digests)
		if err != nil {
			http.Error(w, fmt.Sprintf("Failed to check layers: %v", err), http.StatusInternalServerError)
			return
		}

		// Refresh last_used_at so layers reported as cached are not pruned
		// between this check and the assemble request.
		if len(exists) > 0 {
			if err := store.TouchLayers(exists); err != nil {
				logging.NewLogger(s.logLevel, s.logBroker).Warn("Failed to touch cached layers", "error", err)
			}
		}

		resp := apitypes.LayerCheckResponse{
			Missing: missing,
			Exists:  exists,
		}

		if err := encodeJSON(w, http.StatusOK, resp); err != nil {
			http.Error(w, "Failed to encode response", http.StatusInternalServerError)
			return
		}
	}
}

// handleLayerUpload receives and stores a single layer blob
func (s *APIServer) handleLayerUpload() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		digest := r.Header.Get("X-Layer-Digest")
		if digest == "" {
			http.Error(w, "X-Layer-Digest header is required", http.StatusBadRequest)
			return
		}

		if err := layerstore.ValidateDigest(digest); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		if err := s.ensureDiskSpaceOrPruneLayers(r.Context(), func() error {
			return s.ensureLayerUploadDiskSpace(r.Context(), r.ContentLength)
		}); err != nil {
			writeImageHandlerError(w, "Failed disk space preflight", err)
			return
		}

		store, err := layerstore.New(s.db)
		if err != nil {
			http.Error(w, "Failed to initialize layer store", http.StatusInternalServerError)
			return
		}

		body := http.MaxBytesReader(w, r.Body, maxLayerUploadBytes)
		size, err := store.StoreLayer(digest, body)
		if err != nil {
			var maxBytesErr *http.MaxBytesError
			switch {
			case errors.Is(err, layerstore.ErrDigestMismatch):
				http.Error(w, err.Error(), http.StatusBadRequest)
			case errors.As(err, &maxBytesErr):
				http.Error(w, fmt.Sprintf("Layer exceeds maximum size of %d bytes", maxBytesErr.Limit), http.StatusRequestEntityTooLarge)
			default:
				http.Error(w, fmt.Sprintf("Failed to store layer: %v", err), http.StatusInternalServerError)
			}
			return
		}

		resp := apitypes.LayerUploadResponse{
			Digest: digest,
			Size:   size,
		}

		if err := encodeJSON(w, http.StatusCreated, resp); err != nil {
			http.Error(w, "Failed to encode response", http.StatusInternalServerError)
			return
		}
	}
}

// handleImageAssemble reassembles layers into a loadable tar and loads it into Docker
func (s *APIServer) handleImageAssemble() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req apitypes.ImageAssembleRequest
		if err := decodeJSON(http.MaxBytesReader(w, r.Body, maxJSONBodyBytes), &req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		if req.ImageRef == "" {
			http.Error(w, "imageRef is required", http.StatusBadRequest)
			return
		}

		if len(req.Manifest.Layers) == 0 {
			http.Error(w, "manifest.layers cannot be empty", http.StatusBadRequest)
			return
		}

		if err := s.ensureDiskSpaceOrPruneLayers(r.Context(), func() error {
			return s.ensureAssembleDiskSpace(r.Context(), req)
		}); err != nil {
			writeImageHandlerError(w, "Failed disk space preflight", err)
			return
		}

		store, err := layerstore.New(s.db)
		if err != nil {
			http.Error(w, "Failed to initialize layer store", http.StatusInternalServerError)
			return
		}

		// Assemble the image tar from cached layers
		tarPath, err := store.AssembleImageTar(req)
		if err != nil {
			http.Error(w, fmt.Sprintf("Failed to assemble image: %v", err), http.StatusInternalServerError)
			return
		}
		defer os.Remove(tarPath)

		// Load the assembled tar into Docker
		ctx, cancel := context.WithTimeout(r.Context(), imageLoadTimeout)
		defer cancel()

		cli, err := docker.NewClient(ctx)
		if err != nil {
			http.Error(w, "Failed to create Docker client", http.StatusInternalServerError)
			return
		}
		defer cli.Close()

		if err := docker.LoadImageFromTar(ctx, cli, tarPath); err != nil {
			writeImageHandlerError(w, "Failed to load image", err)
			return
		}

		resp := apitypes.ImageAssembleResponse{
			Success: true,
			Message: fmt.Sprintf("Image %s assembled and loaded successfully", req.ImageRef),
		}

		if err := encodeJSON(w, http.StatusOK, resp); err != nil {
			http.Error(w, "Failed to encode response", http.StatusInternalServerError)
			return
		}
	}
}
