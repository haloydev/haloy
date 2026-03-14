package constants

import (
	"fmt"
	"os"
	"runtime/debug"
)

var Version = "dev"

const (
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

func defaultVersion() string {
	buildInfo, ok := debug.ReadBuildInfo()
	if !ok {
		return "dev"
	}

	return versionFromBuildInfo(buildInfo.Main.Version, buildInfo.Settings)
}

func versionFromBuildInfo(mainVersion string, settings []debug.BuildSetting) string {
	if mainVersion != "" && mainVersion != "(devel)" {
		return mainVersion
	}

	var revision string
	var modified bool
	for _, setting := range settings {
		switch setting.Key {
		case "vcs.revision":
			revision = setting.Value
		case "vcs.modified":
			modified = setting.Value == "true"
		}
	}

	if revision == "" {
		return "dev"
	}

	if len(revision) > 12 {
		revision = revision[:12]
	}

	version := fmt.Sprintf("dev-%s", revision)
	if modified {
		version += "-dirty"
	}

	return version
}

func init() {
	if Version != "dev" {
		return
	}

	Version = defaultVersion()
}
