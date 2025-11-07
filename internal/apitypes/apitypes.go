package apitypes

import (
	"github.com/haloydev/haloy/internal/config"
	"github.com/haloydev/haloy/internal/deploytypes"
)

type HealthResponse struct {
	Status  string `json:"status"`
	Version string `json:"version,omitempty"`
	Service string `json:"service"`
}

type DeployRequest struct {
	DeploymentID string              `json:"deploymentID"`
	TargetConfig config.TargetConfig `json:"targetConfig"`
	// AppConfig without resolved secrets and with target extracted. Saved on server for rollbacks
	RollbackAppConfig config.AppConfig `json:"rollbackAppConfig"`
}

type RollbackRequest struct {
	TargetDeploymentID string              `json:"targetDeploymentID"`
	NewDeploymentID    string              `json:"newDeploymentID"`
	NewTargetConfig    config.TargetConfig `json:"newTargetConfig"`
}

type RollbackTargetsResponse struct {
	Targets []deploytypes.RollbackTarget `json:"targets"`
}

type AppStatusResponse struct {
	State        string          `json:"state"`
	DeploymentID string          `json:"deploymentId"`
	ContainerIDs []string        `json:"containerIds"`
	Domains      []config.Domain `json:"domains"`
}

type StopAppResponse struct {
	Message string `json:"message,omitempty"`
}

type ImageUploadResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
}

type VersionResponse struct {
	Version        string `json:"haloyd"`
	HAProxyVersion string `json:"haproxy"`
}
