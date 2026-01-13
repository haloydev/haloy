package layerstore

import (
	"archive/tar"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/haloydev/haloy/internal/apitypes"
)

// AssembleImageTar creates a docker-loadable tar from cached layers
// Returns the path to the temporary tar file (caller must clean up)
func (s *LayerStore) AssembleImageTar(req apitypes.ImageAssembleRequest) (string, error) {
	// Create temporary file for the assembled tar
	tempFile, err := os.CreateTemp("", "haloy-assembled-*.tar")
	if err != nil {
		return "", fmt.Errorf("failed to create temporary file: %w", err)
	}
	tempPath := tempFile.Name()

	// If we fail, clean up
	success := false
	defer func() {
		tempFile.Close()
		if !success {
			os.Remove(tempPath)
		}
	}()

	tw := tar.NewWriter(tempFile)
	defer tw.Close()

	// Wrap the single manifest entry in an array (docker save format)
	manifestJSON, err := json.Marshal([]apitypes.ImageManifestEntry{req.Manifest})
	if err != nil {
		return "", fmt.Errorf("failed to marshal manifest: %w", err)
	}
	if err := writeToTar(tw, "manifest.json", manifestJSON); err != nil {
		return "", fmt.Errorf("failed to write manifest.json: %w", err)
	}

	// The config path is specified in the manifest (e.g., "sha256:abc123.json" or "abc123.json")
	configPath := req.Manifest.Config
	if err := writeToTar(tw, configPath, req.Config); err != nil {
		return "", fmt.Errorf("failed to write config: %w", err)
	}

	// The manifest.Layers contains paths like "<digest>/layer.tar"
	for _, layerPath := range req.Manifest.Layers {
		// Extract the digest from the layer path
		// Format is typically "sha256:abc123/layer.tar" or just "abc123/layer.tar"
		digest, err := extractDigestFromLayerPath(layerPath)
		if err != nil {
			return "", fmt.Errorf("failed to parse layer path %s: %w", layerPath, err)
		}

		storedLayerPath, err := s.GetLayerPath(digest)
		if err != nil {
			return "", fmt.Errorf("layer not found: %s: %w", digest, err)
		}

		if err := copyFileToTar(tw, layerPath, storedLayerPath); err != nil {
			return "", fmt.Errorf("failed to copy layer %s: %w", digest, err)
		}

		layerDir := filepath.Dir(layerPath)
		versionPath := filepath.Join(layerDir, "VERSION")
		if err := writeToTar(tw, versionPath, []byte("1.0")); err != nil {
			return "", fmt.Errorf("failed to write VERSION for layer %s: %w", digest, err)
		}

		// Write minimal json file for this layer directory
		// This is expected by docker load for legacy format compatibility
		layerJSONPath := filepath.Join(layerDir, "json")
		layerJSON := fmt.Sprintf(`{"id":"%s"}`, strings.TrimSuffix(filepath.Base(layerDir), "/"))
		if err := writeToTar(tw, layerJSONPath, []byte(layerJSON)); err != nil {
			return "", fmt.Errorf("failed to write json for layer %s: %w", digest, err)
		}
	}

	// Touch all layers to update last_used_at
	var digests []string
	for _, layerPath := range req.Manifest.Layers {
		digest, _ := extractDigestFromLayerPath(layerPath)
		digests = append(digests, digest)
	}
	if err := s.TouchLayers(digests); err != nil {
		// Non-fatal, just log
		fmt.Printf("Warning: failed to touch layers: %v\n", err)
	}

	if err := tw.Close(); err != nil {
		return "", fmt.Errorf("failed to close tar writer: %w", err)
	}

	if err := tempFile.Close(); err != nil {
		return "", fmt.Errorf("failed to close temp file: %w", err)
	}

	success = true
	return tempPath, nil
}

// extractDigestFromLayerPath extracts the sha256 digest from a layer path
// Handles formats like "blobs/sha256/<hash>", "blobs/sha256/<hash>/layer.tar", "sha256:<hash>/layer.tar", or "<hash>/layer.tar"
func extractDigestFromLayerPath(layerPath string) (string, error) {
	dir := filepath.Dir(layerPath)

	// Handle modern Docker buildkit OCI format: blobs/sha256/<hash>
	// where the file itself is named with the hash (no layer.tar subdirectory)
	if dir == "blobs/sha256" {
		hash := filepath.Base(layerPath)
		return "sha256:" + hash, nil
	}

	// Handle older buildkit format: blobs/sha256/<hash>/layer.tar
	if strings.HasPrefix(dir, "blobs/sha256/") {
		hash := strings.TrimPrefix(dir, "blobs/sha256/")
		return "sha256:" + hash, nil
	}

	// Handle legacy format: sha256:<hash>/layer.tar
	if strings.HasPrefix(dir, "sha256:") {
		return dir, nil
	}

	// Handle simple format: <hash>/layer.tar
	return "sha256:" + dir, nil
}

// writeToTar writes data to the tar archive at the specified path
func writeToTar(tw *tar.Writer, name string, data []byte) error {
	header := &tar.Header{
		Name: name,
		Mode: 0o644,
		Size: int64(len(data)),
	}
	if err := tw.WriteHeader(header); err != nil {
		return err
	}
	_, err := tw.Write(data)
	return err
}

// copyFileToTar copies a file from disk into the tar archive
func copyFileToTar(tw *tar.Writer, tarPath string, filePath string) error {
	file, err := os.Open(filePath)
	if err != nil {
		return err
	}
	defer file.Close()

	stat, err := file.Stat()
	if err != nil {
		return err
	}

	header := &tar.Header{
		Name: tarPath,
		Mode: 0o644,
		Size: stat.Size(),
	}
	if err := tw.WriteHeader(header); err != nil {
		return err
	}

	_, err = io.Copy(tw, file)
	return err
}
