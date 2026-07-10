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
	// DeployConfig without resolved secrets and with target extracted. Saved on server for rollbacks
	RollbackDeployConfig config.DeployConfig `json:"rollbackDeployConfig"`
}

type RollbackRequest struct {
	TargetDeploymentID string              `json:"targetDeploymentID"`
	NewDeploymentID    string              `json:"newDeploymentID"`
	NewTargetConfig    config.TargetConfig `json:"newTargetConfig"`
}

type RollbackTargetsResponse struct {
	Targets []deploytypes.RollbackTarget `json:"targets"`
}

type ImagePruneRequest struct {
	AppName string `json:"appName"`
	Keep    int    `json:"keep"`
	Apply   bool   `json:"apply,omitempty"`
}

type ImagePruneTag struct {
	Tag          string `json:"tag"`
	DeploymentID string `json:"deploymentId"`
}

type ImagePruneResponse struct {
	AppName              string          `json:"appName"`
	Keep                 int             `json:"keep"`
	Applied              bool            `json:"applied"`
	RunningDeploymentIDs []string        `json:"runningDeploymentIds,omitempty"`
	Tags                 []ImagePruneTag `json:"tags"`
}

type AppStatusResponse struct {
	State        string          `json:"state"`
	DeploymentID string          `json:"deploymentId"`
	ContainerIDs []string        `json:"containerIds"`
	Domains      []config.Domain `json:"domains"`
}

type ImageUploadResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
}

type VersionResponse struct {
	Version                    string   `json:"haloyd"`
	ProxyVersion               string   `json:"haloy_proxy,omitempty"`
	ProxyGeneration            int      `json:"haloy_proxy_generation,omitempty"`
	RequiredProxyGeneration    int      `json:"haloy_proxy_required_generation,omitempty"`
	ProxySchemaVersion         int      `json:"haloy_proxy_schema_version,omitempty"`
	RequiredProxySchemaVersion int      `json:"haloy_proxy_required_schema_version,omitempty"`
	ProxyCompatible            *bool    `json:"haloy_proxy_compatible,omitempty"`
	ProxyConfigHash            string   `json:"haloy_proxy_config_hash,omitempty"`
	Capabilities               []string `json:"capabilities,omitempty"`
}

type RegistryLoginRequest struct {
	Server   string `json:"server"`
	Username string `json:"username"`
	Password string `json:"password"`
}

type RegistryLogoutRequest struct {
	Server string `json:"server"`
}

type RegistryEntry struct {
	Server   string `json:"server"`
	Username string `json:"username"`
}

type RegistriesResponse struct {
	Registries []RegistryEntry `json:"registries"`
}

type ExecRequest struct {
	Command       []string `json:"command"`                 // Required: command to execute
	ContainerID   string   `json:"containerId,omitempty"`   // Optional: specific container ID
	AllContainers bool     `json:"allContainers,omitempty"` // Optional: run on all containers
}

type ExecResult struct {
	ContainerID string `json:"containerId"`
	ExitCode    int    `json:"exitCode"`
	Stdout      string `json:"stdout"`
	Stderr      string `json:"stderr"`
	Error       string `json:"error,omitempty"` // Set if exec failed for this container
}

type ExecResponse struct {
	Results []ExecResult `json:"results"`
}

// LayerCheckRequest is sent by client to query which layers already exist on server
type LayerCheckRequest struct {
	Digests []string `json:"digests"`
}

// LayerCheckResponse tells client which layers are missing
type LayerCheckResponse struct {
	Missing []string `json:"missing"`
	Exists  []string `json:"exists"`
}

// LayerUploadResponse confirms a layer was stored
type LayerUploadResponse struct {
	Digest string `json:"digest"`
	Size   int64  `json:"size"`
}

// ImageManifestEntry represents one entry from docker save manifest.json
type ImageManifestEntry struct {
	Config   string   `json:"Config"`
	RepoTags []string `json:"RepoTags"`
	Layers   []string `json:"Layers"`
}

// ImageAssembleRequest contains metadata to reassemble an image from layers
type ImageAssembleRequest struct {
	ImageRef string             `json:"imageRef"`
	Config   []byte             `json:"config"`
	Manifest ImageManifestEntry `json:"manifest"`
}

// ImageAssembleResponse confirms image was loaded
type ImageAssembleResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
}

type ImageDiskSpaceCheckRequest struct {
	UploadSizeBytes         uint64 `json:"uploadSizeBytes,omitempty"`
	LayerUploadBytes        uint64 `json:"layerUploadBytes,omitempty"`
	AssembledImageSizeBytes uint64 `json:"assembledImageSizeBytes,omitempty"`
}

type ImageDiskSpaceCheckResponse struct {
	OK             bool   `json:"ok"`
	Path           string `json:"path"`
	RequiredBytes  uint64 `json:"requiredBytes"`
	AvailableBytes uint64 `json:"availableBytes"`
}
