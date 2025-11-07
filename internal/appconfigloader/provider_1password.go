package appconfigloader

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/haloydev/haloy/internal/cmdexec"
	"github.com/haloydev/haloy/internal/config"
)

func fetchFrom1Password(ctx context.Context, config config.OnePasswordSourceConfig) (map[string]string, error) {
	if config.Item == "" || config.Vault == "" {
		return nil, fmt.Errorf("1Password source requires 'vault' and 'item' to be set")
	}

	args := []string{"item", "get", config.Item, "--vault", config.Vault, "--format", "json"}
	if config.Account != "" {
		args = append(args, "--account", config.Account)
	}

	// This struct matches the JSON output of 'op item get'
	type opItem struct {
		Fields []struct {
			Label string `json:"label"`
			Value string `json:"value"`
		} `json:"fields"`
	}

	output, err := cmdexec.RunCLICommand(ctx, "op", args...)
	if err != nil {
		return nil, err
	}

	var item opItem
	if err := json.Unmarshal([]byte(output), &item); err != nil {
		return nil, fmt.Errorf("failed to parse JSON output from 1Password CLI: %w", err)
	}

	secrets := make(map[string]string)
	for _, field := range item.Fields {
		// The key is the field label from 1Password (e.g., "username", "password", "api-key")
		secrets[field.Label] = field.Value
	}

	return secrets, nil
}
