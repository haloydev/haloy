package layerstore

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/haloydev/haloy/internal/config"
	"github.com/haloydev/haloy/internal/constants"
	"github.com/haloydev/haloy/internal/storage"
)

// LayerStore manages content-addressable layer storage on the filesystem
type LayerStore struct {
	basePath string
	db       *storage.DB
}

// New creates a new LayerStore
func New(db *storage.DB) (*LayerStore, error) {
	dataDir, err := config.DataDir()
	if err != nil {
		return nil, fmt.Errorf("failed to get data directory: %w", err)
	}

	basePath := filepath.Join(dataDir, constants.LayersDir)
	if err := os.MkdirAll(basePath, constants.ModeDirPrivate); err != nil {
		return nil, fmt.Errorf("failed to create layers directory: %w", err)
	}

	return &LayerStore{
		basePath: basePath,
		db:       db,
	}, nil
}

// HasLayers checks multiple digests and returns which exist and which are missing
func (s *LayerStore) HasLayers(digests []string) (missing, exists []string, err error) {
	return s.db.HasLayers(digests)
}

// StoreLayer saves a layer from a reader and verifies its digest
// The digest should be in the format "sha256:hexstring"
func (s *LayerStore) StoreLayer(digest string, reader io.Reader) (int64, error) {
	// Validate digest format
	if !strings.HasPrefix(digest, "sha256:") {
		return 0, fmt.Errorf("invalid digest format: must start with sha256:")
	}

	expectedHash := strings.TrimPrefix(digest, "sha256:")

	layerDir := filepath.Join(s.basePath, expectedHash)
	if err := os.MkdirAll(layerDir, constants.ModeDirPrivate); err != nil {
		return 0, fmt.Errorf("failed to create layer directory: %w", err)
	}

	layerPath := filepath.Join(layerDir, "layer.tar")

	tempFile, err := os.CreateTemp(layerDir, "layer-*.tar.tmp")
	if err != nil {
		return 0, fmt.Errorf("failed to create temporary file: %w", err)
	}
	tempPath := tempFile.Name()
	defer func() {
		tempFile.Close()
		os.Remove(tempPath) // Clean up temp file if we fail
	}()

	hasher := sha256.New()
	writer := io.MultiWriter(tempFile, hasher)

	size, err := io.Copy(writer, reader)
	if err != nil {
		return 0, fmt.Errorf("failed to write layer: %w", err)
	}

	if err := tempFile.Close(); err != nil {
		return 0, fmt.Errorf("failed to close temporary file: %w", err)
	}

	actualHash := hex.EncodeToString(hasher.Sum(nil))
	if actualHash != expectedHash {
		return 0, fmt.Errorf("digest mismatch: expected %s, got %s", expectedHash, actualHash)
	}

	if err := os.Rename(tempPath, layerPath); err != nil {
		return 0, fmt.Errorf("failed to rename layer file: %w", err)
	}

	now := time.Now()
	layer := storage.Layer{
		Digest:     digest,
		Size:       size,
		CreatedAt:  now,
		LastUsedAt: now,
	}
	if err := s.db.SaveLayer(layer); err != nil {
		os.Remove(layerPath)
		return 0, fmt.Errorf("failed to save layer to database: %w", err)
	}

	return size, nil
}

// GetLayerPath returns the filesystem path for a layer
func (s *LayerStore) GetLayerPath(digest string) (string, error) {
	if !strings.HasPrefix(digest, "sha256:") {
		return "", fmt.Errorf("invalid digest format: must start with sha256:")
	}

	hash := strings.TrimPrefix(digest, "sha256:")
	layerPath := filepath.Join(s.basePath, hash, "layer.tar")

	if _, err := os.Stat(layerPath); os.IsNotExist(err) {
		return "", fmt.Errorf("layer not found: %s", digest)
	}

	return layerPath, nil
}

// TouchLayers updates the last_used_at timestamp for multiple layers
func (s *LayerStore) TouchLayers(digests []string) error {
	return s.db.TouchLayers(digests)
}

// DeleteLayer removes a layer from storage and database
func (s *LayerStore) DeleteLayer(digest string) error {
	if !strings.HasPrefix(digest, "sha256:") {
		return fmt.Errorf("invalid digest format: must start with sha256:")
	}

	hash := strings.TrimPrefix(digest, "sha256:")
	layerDir := filepath.Join(s.basePath, hash)

	if err := os.RemoveAll(layerDir); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to remove layer directory: %w", err)
	}

	if err := s.db.DeleteLayer(digest); err != nil {
		return fmt.Errorf("failed to remove layer from database: %w", err)
	}

	return nil
}
