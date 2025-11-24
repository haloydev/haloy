package constants

import "os"

const (
	Version                  = "0.1.0-beta.4"
	HAProxyVersion           = "3.2"
	HaloydContainerName      = "haloyd"
	HAProxyContainerName     = "haloy-haproxy"
	DockerNetwork            = "haloy"
	DefaultDeploymentsToKeep = 6
	DefaultHealthCheckPath   = "/"
	DefaultContainerPort     = "8080"
	DefaultReplicas          = 1

	CertificatesHTTPProviderPort = "8080"
	APIServerPort                = "9999"

	// Environment variables
	EnvVarAPIToken      = "HALOY_API_TOKEN"
	EnvVarReplicaID     = "HALOY_REPLICA_ID" // available in all containers.
	EnvVarDataDir       = "HALOY_DATA_DIR"   // used to override default data directory.
	EnvVarConfigDir     = "HALOY_CONFIG_DIR" // used to override default config directory for haloy.
	EnvVarDebug         = "HALOY_DEBUG"
	EnvVarSystemInstall = "HALOY_SYSTEM_INSTALL" // used to disable system wide install

	// Directories
	SystemDataDir   = "/var/lib/haloy"
	SystemConfigDir = "/etc/haloy"
	UserDataDir     = "~/.local/share/haloy"
	UserConfigDir   = "~/.config/haloy"

	// Subdirectories
	DBDir            = "db"
	HAProxyConfigDir = "haproxy-config"
	CertStorageDir   = "cert-storage"

	// File names
	HaloydConfigFileName  = "haloyd.yaml"
	ClientConfigFileName  = "client.yaml"
	ConfigEnvFileName     = ".env"
	HAProxyConfigFileName = "haproxy.cfg"
	DBFileName            = "haloy.db"
)

// File and directory permissions
const (
	ModeFileSecret  os.FileMode = 0o600 // secrets: .env, keys
	ModeFileDefault os.FileMode = 0o644 // non-secret configs
	ModeFileExec    os.FileMode = 0o755 // scripts/binaries
	ModeDirPrivate  os.FileMode = 0o700 // private dirs
)
