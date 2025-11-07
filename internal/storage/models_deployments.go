package storage

import (
	"database/sql"
	"encoding/json"
	"fmt"

	"github.com/haloydev/haloy/internal/config"
)

type Deployment struct {
	ID             string          `db:"id" json:"id"`
	AppName        string          `db:"app_name" json:"appName"`
	RawAppConfig   json.RawMessage `db:"raw_app_config" json:"rawAppConfig"`
	DeployedImage  json.RawMessage `db:"deployed_image" json:"deployedImage"`
	RolledBackFrom *string         `db:"rolled_back_from" json:"rolledBackFrom,omitempty"`
}

func createDeploymentsTable(db *DB) error {
	schema := `
CREATE TABLE IF NOT EXISTS deployments (
    id TEXT PRIMARY KEY,                    -- Timestamp-based ID
    app_name TEXT NOT NULL,                 -- App being deployed
    raw_app_config JSON NOT NULL,           -- config.AppConfig as JSON
    deployed_image json not null,           -- Resolved config.Image config that was actually deployed
    rolled_back_from TEXT,                  -- ID of deployment this was rolled back from

    -- Foreign key constraint (optional)
    FOREIGN KEY (rolled_back_from) REFERENCES deployments(id)
);

-- Indexes for efficient queries
CREATE INDEX IF NOT EXISTS idx_deployments_app_name ON deployments(app_name);
`

	_, err := db.Exec(schema)
	if err != nil {
		return fmt.Errorf("failed to create deployments table: %w", err)
	}
	return nil
}

func (db *DB) SaveDeployment(deployment Deployment) error {
	query := `INSERT INTO deployments (id, app_name, raw_app_config,  deployed_image, rolled_back_from)
              VALUES (?, ?, ?, ?, ?)`
	_, err := db.Exec(query, deployment.ID, deployment.AppName, deployment.RawAppConfig,
		deployment.DeployedImage, deployment.RolledBackFrom)
	return err
}

func (db *DB) GetDeployment(deploymentID string) (Deployment, error) {
	var deployment Deployment
	query := `SELECT id, app_name, raw_app_config, deployed_image, rolled_back_from
              FROM deployments WHERE id = ?`

	row := db.QueryRow(query, deploymentID)
	err := row.Scan(&deployment.ID, &deployment.AppName, &deployment.RawAppConfig, &deployment.DeployedImage, &deployment.RolledBackFrom)
	if err != nil {
		if err == sql.ErrNoRows {
			return deployment, fmt.Errorf("deployment '%s' not found", deploymentID)
		}
		return deployment, fmt.Errorf("failed to get deployment: %w", err)
	}

	return deployment, nil
}

func (db *DB) GetDeploymentHistory(appName string, limit int) ([]Deployment, error) {
	var deployments []Deployment
	query := `SELECT id, app_name, raw_app_config, deployed_image, rolled_back_from
              FROM deployments
              WHERE app_name = ?
              ORDER BY id DESC
              LIMIT ?`

	rows, err := db.Query(query, appName, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to query deployment history: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var deployment Deployment
		err := rows.Scan(&deployment.ID, &deployment.AppName, &deployment.RawAppConfig,
			&deployment.DeployedImage, &deployment.RolledBackFrom)
		if err != nil {
			return nil, fmt.Errorf("failed to scan deployment: %w", err)
		}
		deployments = append(deployments, deployment)
	}

	return deployments, nil
}

func (db *DB) PruneOldDeployments(appName string, deploymentsToKeep int) error {
	// Keep the N most recent deployments for this app, delete the rest
	// Since ID is in YYYYMMDDHHMMSS format, we can sort by ID directly
	query := `
        DELETE FROM deployments
        WHERE app_name = ?
        AND id NOT IN (
            SELECT id FROM deployments
            WHERE app_name = ?
            ORDER BY id DESC
            LIMIT ?
        )
    `

	result, err := db.Exec(query, appName, appName, deploymentsToKeep)
	if err != nil {
		return fmt.Errorf("failed to prune old deployments: %w", err)
	}

	rowsAffected, _ := result.RowsAffected()
	if rowsAffected > 0 {
		fmt.Printf("Pruned %d old deployment(s) for app '%s'\n", rowsAffected, appName)
	}

	return nil
}

func (d *Deployment) GetImageRef() (string, error) {
	var deployedImage config.Image
	if err := json.Unmarshal(d.DeployedImage, &deployedImage); err != nil {
		return "", fmt.Errorf("failed to parse deployed image: %w", err)
	}
	return deployedImage.ImageRef(), nil
}

// GetDeployedImage parses and returns the deployed image configuration
func (d *Deployment) GetDeployedImage() (config.Image, error) {
	var deployedImage config.Image
	if err := json.Unmarshal(d.DeployedImage, &deployedImage); err != nil {
		return deployedImage, fmt.Errorf("failed to parse deployed image: %w", err)
	}
	return deployedImage, nil
}
