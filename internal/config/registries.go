package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/haloydev/haloy/internal/constants"
	"github.com/knadh/koanf/providers/file"
	"github.com/knadh/koanf/v2"
	"gopkg.in/yaml.v3"
)

type ServerRegistriesConfig struct {
	Registries map[string]RegistryAuth `json:"registries" yaml:"registries" toml:"registries"`
}

func NormalizeRegistryServer(server string) string {
	server = strings.ToLower(strings.TrimSpace(server))
	server = strings.TrimPrefix(server, "https://")
	server = strings.TrimPrefix(server, "http://")
	server = strings.TrimRight(server, "/")
	server = strings.TrimSuffix(server, "/v1")
	switch server {
	case "index.docker.io", "registry-1.docker.io":
		return "docker.io"
	default:
		return server
	}
}

func ServerRegistriesPath() (string, error) {
	dataDir, err := DataDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dataDir, constants.RegistriesFileName), nil
}

func LoadServerRegistries(path string) (*ServerRegistriesConfig, error) {
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return &ServerRegistriesConfig{Registries: map[string]RegistryAuth{}}, nil
	}

	format, err := GetConfigFormat(path)
	if err != nil {
		return nil, err
	}

	parser, err := GetConfigParser(format)
	if err != nil {
		return nil, err
	}

	k := koanf.New(".")
	if err := k.Load(file.Provider(path), parser); err != nil {
		return nil, fmt.Errorf("failed to load registry config file: %w", err)
	}

	var registries ServerRegistriesConfig
	if err := k.UnmarshalWithConf("", &registries, koanf.UnmarshalConf{Tag: format}); err != nil {
		return nil, fmt.Errorf("failed to unmarshal registry config: %w", err)
	}
	if registries.Registries == nil {
		registries.Registries = map[string]RegistryAuth{}
	}
	return &registries, nil
}

func SaveServerRegistries(registries *ServerRegistriesConfig, path string) error {
	if registries == nil {
		registries = &ServerRegistriesConfig{}
	}
	if registries.Registries == nil {
		registries.Registries = map[string]RegistryAuth{}
	}

	ext := filepath.Ext(path)
	var data []byte
	var err error

	switch ext {
	case ".json":
		data, err = json.MarshalIndent(registries, "", "  ")
	default:
		data, err = yaml.Marshal(registries)
	}

	if err != nil {
		return fmt.Errorf("failed to marshal registry config: %w", err)
	}

	if err := os.MkdirAll(filepath.Dir(path), constants.ModeDirPrivate); err != nil {
		return fmt.Errorf("failed to create registry data directory: %w", err)
	}
	if err := os.WriteFile(path, data, constants.ModeFileSecret); err != nil {
		return err
	}
	return os.Chmod(path, constants.ModeFileSecret)
}

func (registries *ServerRegistriesConfig) Validate() error {
	if registries == nil {
		return nil
	}

	for server, auth := range registries.Registries {
		if strings.TrimSpace(server) == "" {
			return fmt.Errorf("registry server cannot be empty")
		}
		if strings.ContainsAny(server, " \t\n\r") {
			return fmt.Errorf("registry server '%s' contains whitespace", server)
		}
		if auth.Server != "" && NormalizeRegistryServer(auth.Server) != NormalizeRegistryServer(server) {
			return fmt.Errorf("registry server '%s' does not match auth server '%s'", server, auth.Server)
		}
		if err := auth.Username.Validate(); err != nil {
			return fmt.Errorf("registry %s username: %w", server, err)
		}
		if err := auth.Password.Validate(); err != nil {
			return fmt.Errorf("registry %s password: %w", server, err)
		}
	}
	return nil
}

func (registries *ServerRegistriesConfig) AuthForImage(image Image) (*RegistryAuth, error) {
	if registries == nil || len(registries.Registries) == 0 {
		return nil, nil
	}

	imageServer := NormalizeRegistryServer(image.GetRegistryServer())
	auth, ok := registries.Registries[imageServer]
	if !ok {
		for server, candidate := range registries.Registries {
			if NormalizeRegistryServer(server) == imageServer {
				auth = candidate
				ok = true
				break
			}
		}
	}
	if !ok {
		return nil, nil
	}

	if auth.Server == "" {
		auth.Server = imageServer
	}
	resolved, err := ResolveRegistryAuth(auth)
	if err != nil {
		return nil, fmt.Errorf("registry %s: %w", imageServer, err)
	}
	return resolved, nil
}
