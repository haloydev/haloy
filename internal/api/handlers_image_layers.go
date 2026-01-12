package api

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"strings"

	"github.com/haloydev/haloy/internal/apitypes"
	"github.com/haloydev/haloy/internal/docker"
	"github.com/haloydev/haloy/internal/layerstore"
	"github.com/haloydev/haloy/internal/storage"
)

// handleLayerCheck checks which layers already exist on the server
func (s *APIServer) handleLayerCheck() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req apitypes.LayerCheckRequest
		if err := decodeJSON(r.Body, &req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		if len(req.Digests) == 0 {
			http.Error(w, "digests array cannot be empty", http.StatusBadRequest)
			return
		}

		db, err := storage.New()
		if err != nil {
			http.Error(w, "Failed to connect to database", http.StatusInternalServerError)
			return
		}
		defer db.Close()

		store, err := layerstore.New(db)
		if err != nil {
			http.Error(w, "Failed to initialize layer store", http.StatusInternalServerError)
			return
		}

		missing, exists, err := store.HasLayers(req.Digests)
		if err != nil {
			http.Error(w, fmt.Sprintf("Failed to check layers: %v", err), http.StatusInternalServerError)
			return
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

		if !strings.HasPrefix(digest, "sha256:") {
			http.Error(w, "Invalid digest format: must start with sha256:", http.StatusBadRequest)
			return
		}

		db, err := storage.New()
		if err != nil {
			http.Error(w, "Failed to connect to database", http.StatusInternalServerError)
			return
		}
		defer db.Close()

		store, err := layerstore.New(db)
		if err != nil {
			http.Error(w, "Failed to initialize layer store", http.StatusInternalServerError)
			return
		}

		size, err := store.StoreLayer(digest, r.Body)
		if err != nil {
			http.Error(w, fmt.Sprintf("Failed to store layer: %v", err), http.StatusInternalServerError)
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
		if err := decodeJSON(r.Body, &req); err != nil {
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

		db, err := storage.New()
		if err != nil {
			http.Error(w, "Failed to connect to database", http.StatusInternalServerError)
			return
		}
		defer db.Close()

		store, err := layerstore.New(db)
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
		ctx, cancel := context.WithTimeout(r.Context(), defaultContextTimeout)
		defer cancel()

		cli, err := docker.NewClient(ctx)
		if err != nil {
			http.Error(w, "Failed to create Docker client", http.StatusInternalServerError)
			return
		}
		defer cli.Close()

		if err := docker.LoadImageFromTar(ctx, cli, tarPath); err != nil {
			http.Error(w, fmt.Sprintf("Failed to load image: %v", err), http.StatusInternalServerError)
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
