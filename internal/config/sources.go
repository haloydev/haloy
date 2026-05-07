package config

import (
	"errors"
	"fmt"
	"os"
)

// SourceReference defines a reference to a value from an external source.
// Only one of its fields should be set.
type SourceReference struct {
	Env    string `json:"env,omitempty" yaml:"env,omitempty" toml:"env,omitempty"`
	Secret string `json:"secret,omitempty" yaml:"secret,omitempty" toml:"secret,omitempty"`
}

// Validate ensures that exactly one source type is specified in the reference.
func (sr *SourceReference) Validate() error {
	hasEnv := sr.Env != ""
	hasSecret := sr.Secret != ""

	if !hasEnv && !hasSecret {
		return errors.New("a source reference (e.g., 'env' or 'secret') must be specified")
	}

	if hasEnv && hasSecret {
		return errors.New("only one source reference ('env' or 'secret') can be specified at a time")
	}

	return nil
}

// ValueSource represents a value that can be a plaintext literal or a sourced reference.
// Only one of its fields should be set.
type ValueSource struct {
	Value string           `json:"value,omitempty" yaml:"value,omitempty" toml:"value,omitempty"`
	From  *SourceReference `json:"from,omitempty" yaml:"from,omitempty" toml:"from,omitempty"`
}

// Validate ensures that the value is provided either as a literal or a 'From' reference, but not both.
func (vs *ValueSource) Validate() error {
	hasValue := vs.Value != ""
	hasFrom := vs.From != nil

	if hasValue && hasFrom {
		return errors.New("cannot provide both 'value' and 'from'")
	}
	if !hasValue && !hasFrom {
		return errors.New("must provide either 'value' or 'from'")
	}

	if hasFrom {
		if err := vs.From.Validate(); err != nil {
			return fmt.Errorf("invalid 'from' block: %w", err)
		}
	}

	return nil
}

func (vs *ValueSource) ResolveEnvOnly() (string, error) {
	if vs.Value != "" {
		return vs.Value, nil
	}
	if vs.From == nil {
		return "", errors.New("must provide either 'value' or 'from'")
	}
	if vs.From.Env == "" {
		return "", errors.New("only environment sources are supported here")
	}
	value, ok := os.LookupEnv(vs.From.Env)
	if !ok {
		return "", fmt.Errorf("environment variable '%s' is not set", vs.From.Env)
	}
	if value == "" {
		return "", fmt.Errorf("environment variable '%s' is empty", vs.From.Env)
	}
	return value, nil
}

func ResolveRegistryAuth(auth RegistryAuth) (*RegistryAuth, error) {
	username, err := auth.Username.ResolveEnvOnly()
	if err != nil {
		return nil, fmt.Errorf("registry username: %w", err)
	}
	password, err := auth.Password.ResolveEnvOnly()
	if err != nil {
		return nil, fmt.Errorf("registry password: %w", err)
	}

	return &RegistryAuth{
		Server:   auth.Server,
		Username: ValueSource{Value: username},
		Password: ValueSource{Value: password},
	}, nil
}
