package init

import (
	"fmt"
	"path/filepath"

	"github.com/charmbracelet/huh"
	"github.com/haloydev/haloy/internal/config"
	"github.com/haloydev/haloy/internal/constants"
)

// ServerSelection represents the result of server selection
type ServerSelection struct {
	Server   string
	Selected bool // true if user selected, false if auto-selected (single server)
}

// SelectServer handles server selection based on client config
// Returns the selected server URL, or an error if no servers are configured
func SelectServer() (*ServerSelection, error) {
	// Load client config
	configDir, err := config.ConfigDir()
	if err != nil {
		return nil, fmt.Errorf("failed to get config directory: %w", err)
	}

	clientConfigPath := filepath.Join(configDir, constants.ClientConfigFileName)
	clientConfig, err := config.LoadClientConfig(clientConfigPath)
	if err != nil {
		return nil, fmt.Errorf("failed to load client config: %w", err)
	}

	if clientConfig == nil || len(clientConfig.Servers) == 0 {
		return nil, fmt.Errorf("no servers configured. Run 'haloy server add <url> <token>' first")
	}

	servers := clientConfig.ListServers()

	return SelectServerFrom(servers, runInteractiveSelect)
}

// SelectServerFrom is a testable function that selects a server from a list.
// The promptFn is used for interactive selection when multiple servers exist.
func SelectServerFrom(servers []string, promptFn func([]string) (string, error)) (*ServerSelection, error) {
	if len(servers) == 0 {
		return nil, fmt.Errorf("no servers configured. Run 'haloy server add <url> <token>' first")
	}

	// If only one server, auto-select it
	if len(servers) == 1 {
		return &ServerSelection{
			Server:   servers[0],
			Selected: false,
		}, nil
	}

	// Multiple servers - use the prompt function
	selected, err := promptFn(servers)
	if err != nil {
		return nil, fmt.Errorf("server selection cancelled: %w", err)
	}

	return &ServerSelection{
		Server:   selected,
		Selected: true,
	}, nil
}

// runInteractiveSelect runs the interactive server selection prompt
func runInteractiveSelect(servers []string) (string, error) {
	var selected string

	options := make([]huh.Option[string], len(servers))
	for i, server := range servers {
		options[i] = huh.NewOption(server, server)
	}

	form := huh.NewForm(
		huh.NewGroup(
			huh.NewSelect[string]().
				Title("Select a server").
				Description("Multiple servers are configured. Please select one.").
				Options(options...).
				Value(&selected),
		),
	)

	err := form.Run()
	if err != nil {
		return "", err
	}

	return selected, nil
}

// GetServerCount returns the number of configured servers
func GetServerCount() (int, error) {
	configDir, err := config.ConfigDir()
	if err != nil {
		return 0, err
	}

	clientConfigPath := filepath.Join(configDir, constants.ClientConfigFileName)
	clientConfig, err := config.LoadClientConfig(clientConfigPath)
	if err != nil {
		return 0, err
	}

	if clientConfig == nil {
		return 0, nil
	}

	return len(clientConfig.Servers), nil
}
