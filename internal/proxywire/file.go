package proxywire

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/haloydev/haloy/internal/constants"
)

// WriteSnapshotFile atomically writes the snapshot as JSON to path using the
// same tmp+rename pattern as certificate storage, so a concurrent reader sees
// either the old or the new snapshot, never a partial write.
func WriteSnapshotFile(path string, s *Snapshot) error {
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal snapshot: %w", err)
	}

	tmpPath := path + ".tmp"
	if err := os.WriteFile(tmpPath, data, constants.ModeFileSecret); err != nil {
		return fmt.Errorf("write snapshot temp file: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("rename snapshot file: %w", err)
	}
	return nil
}

// ReadSnapshotFile reads and decodes a snapshot written by WriteSnapshotFile.
// A missing file is returned as-is so callers can detect it with
// errors.Is(err, os.ErrNotExist) and treat it as a fresh install.
func ReadSnapshotFile(path string) (*Snapshot, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var s Snapshot
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, fmt.Errorf("decode snapshot file %s: %w", path, err)
	}
	return &s, nil
}
