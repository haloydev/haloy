package config

type SecretProviders struct {
	OnePassword map[string]OnePasswordSourceConfig `json:"onepassword,omitempty" yaml:"onepassword,omitempty" toml:"onepassword,omitempty"`
	SOPS        map[string]SOPSSourceConfig        `json:"sops,omitempty" yaml:"sops,omitempty" toml:"sops,omitempty"`
}

type OnePasswordSourceConfig struct {
	Account string `json:"account,omitempty" yaml:"account,omitempty" toml:"account,omitempty"`
	Vault   string `json:"vault,omitempty" yaml:"vault,omitempty" toml:"vault,omitempty"`
	Item    string `json:"item,omitempty" yaml:"item,omitempty" toml:"item,omitempty"`
}

type SOPSSourceConfig struct {
	File   string `json:"file,omitempty" yaml:"file,omitempty" toml:"file,omitempty"`
	Format string `json:"format,omitempty" yaml:"format,omitempty" toml:"format,omitempty"`
}
