package storage

import (
	"fmt"
	"time"
)

// Layer represents a cached image layer in the database
type Layer struct {
	Digest     string    `db:"digest" json:"digest"`
	Size       int64     `db:"size" json:"size"`
	CreatedAt  time.Time `db:"created_at" json:"createdAt"`
	LastUsedAt time.Time `db:"last_used_at" json:"lastUsedAt"`
}

func createLayersTable(db *DB) error {
	schema := `
CREATE TABLE IF NOT EXISTS layers (
    digest TEXT PRIMARY KEY,
    size INTEGER NOT NULL,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    last_used_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_layers_last_used ON layers(last_used_at);
`
	_, err := db.Exec(schema)
	if err != nil {
		return fmt.Errorf("failed to create layers table: %w", err)
	}
	return nil
}

// SaveLayer saves or updates a layer record
func (db *DB) SaveLayer(layer Layer) error {
	query := `INSERT OR REPLACE INTO layers (digest, size, created_at, last_used_at)
              VALUES (?, ?, ?, ?)`
	_, err := db.Exec(query, layer.Digest, layer.Size, layer.CreatedAt, layer.LastUsedAt)
	return err
}

// HasLayer checks if a layer exists by digest
func (db *DB) HasLayer(digest string) (bool, error) {
	var count int
	query := `SELECT COUNT(*) FROM layers WHERE digest = ?`
	err := db.QueryRow(query, digest).Scan(&count)
	if err != nil {
		return false, fmt.Errorf("failed to check layer: %w", err)
	}
	return count > 0, nil
}

// HasLayers checks multiple digests and returns which exist and which are missing
func (db *DB) HasLayers(digests []string) (missing, exists []string, err error) {
	for _, digest := range digests {
		has, err := db.HasLayer(digest)
		if err != nil {
			return nil, nil, err
		}
		if has {
			exists = append(exists, digest)
		} else {
			missing = append(missing, digest)
		}
	}
	return missing, exists, nil
}

// TouchLayer updates the last_used_at timestamp
func (db *DB) TouchLayer(digest string) error {
	query := `UPDATE layers SET last_used_at = ? WHERE digest = ?`
	_, err := db.Exec(query, time.Now(), digest)
	return err
}

// TouchLayers updates the last_used_at timestamp for multiple layers
func (db *DB) TouchLayers(digests []string) error {
	for _, digest := range digests {
		if err := db.TouchLayer(digest); err != nil {
			return err
		}
	}
	return nil
}

// DeleteLayer removes a layer record
func (db *DB) DeleteLayer(digest string) error {
	query := `DELETE FROM layers WHERE digest = ?`
	_, err := db.Exec(query, digest)
	return err
}
