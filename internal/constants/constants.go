package constants

import "os"

const (
	Version                  = "0.1.0-beta.41"
	DockerNetwork            = "haloy"
	DefaultDeploymentsToKeep = 6
	DefaultHealthCheckPath   = "/"
	DefaultContainerPort     = "8080"
	DefaultReplicas          = 1
	DefaultImageDiskReserve  = 2 * 1024 * 1024 * 1024
	CapabilityLayerUpload    = "layer-upload"
	CapabilityImagePreflight = "image-disk-preflight"

	CertificatesHTTPProviderPort = "8080"
	APIServerPort                = "9999"

	// Environment variables
	EnvVarAPIToken  = "HALOY_API_TOKEN"
	EnvVarReplicaID = "HALOY_REPLICA_ID" // available in all containers.
	EnvVarDataDir   = "HALOY_DATA_DIR"   // used to override default data directory.
	EnvVarConfigDir = "HALOY_CONFIG_DIR" // used to override default config directory.
	EnvVarDebug     = "HALOY_DEBUG"

	// Default directories (system-wide installation)
	SystemDataDir          = "/var/lib/haloy"
	DefaultHaloydConfigDir = "/etc/haloy"
	SystemBinDir           = "/usr/local/bin"

	// Default config directory for haloy CLI
	DefaultHaloyConfigDir = ".config/haloy"

	// Subdirectories
	DBDir          = "db"
	CertStorageDir = "cert-storage"
	LayersDir      = "layers"
	TempDir        = "tmp"

	// File names
	HaloydConfigFileName   = "haloyd.yaml"
	ClientConfigFileName   = "client.yaml"
	ConfigEnvFileName      = ".env"
	ConfigEnvLocalFileName = ".env.local"
	DBFileName             = "haloy.db"
)

// File and directory permissions
const (
	ModeFileSecret  os.FileMode = 0o600 // secrets: .env, keys
	ModeFileDefault os.FileMode = 0o644 // non-secret configs
	ModeFileExec    os.FileMode = 0o755 // scripts/binaries
	ModeDirPrivate  os.FileMode = 0o700 // private dirs
)
