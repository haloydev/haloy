package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/haloydev/haloy/internal/constants"
	"github.com/haloydev/haloy/internal/helpers"
	"github.com/knadh/koanf/providers/file"
	"github.com/knadh/koanf/v2"
	"gopkg.in/yaml.v3"
)

type ClientConfig struct {
	Servers map[string]ServerConfig `json:"servers" yaml:"servers" toml:"servers"`
}

type ServerConfig struct {
	TokenEnv string `json:"token_env" yaml:"token_env" toml:"token_env"`
}

func (cc *ClientConfig) AddServer(url, tokenEnv string, force bool) error {
	normalizedURL, err := helpers.NormalizeServerURL(url)
	if err != nil {
		return err
	}

	if cc.Servers == nil {
		cc.Servers = make(map[string]ServerConfig)
	}

	if !force {
		if _, exists := cc.Servers[normalizedURL]; exists {
			return fmt.Errorf("server %s already exists. Use --force to override", normalizedURL)
		}
	}

	cc.Servers[normalizedURL] = ServerConfig{TokenEnv: tokenEnv}
	return nil
}

func (cc *ClientConfig) DeleteServer(url string) error {
	normalizedURL, err := helpers.NormalizeServerURL(url)
	if err != nil {
		return err
	}

	if _, exists := cc.Servers[normalizedURL]; !exists {
		return fmt.Errorf("server %s not found", normalizedURL)
	}
	delete(cc.Servers, normalizedURL)
	return nil
}

func (cc *ClientConfig) ListServers() []string {
	var urls []string
	for url := range cc.Servers {
		urls = append(urls, url)
	}
	sort.Strings(urls)
	return urls
}

func LoadClientConfig(path string) (*ClientConfig, error) {
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return nil, nil
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
		return nil, fmt.Errorf("failed to load client config file: %w", err)
	}

	var clientConfig ClientConfig
	if err := k.UnmarshalWithConf("", &clientConfig, koanf.UnmarshalConf{Tag: format}); err != nil {
		return nil, fmt.Errorf("failed to unmarshal client config: %w", err)
	}
	return &clientConfig, nil
}

func SaveClientConfig(config *ClientConfig, path string) error {
	ext := filepath.Ext(path)
	var data []byte
	var err error

	switch ext {
	case ".json":
		data, err = json.MarshalIndent(config, "", "  ")
	default: // yaml
		data, err = yaml.Marshal(config)
	}

	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	return os.WriteFile(path, data, constants.ModeFileDefault)
}
