package haloy

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/haloydev/haloy/internal/config"
	"github.com/haloydev/haloy/internal/constants"
	"github.com/haloydev/haloy/internal/helpers"
	"github.com/haloydev/haloy/internal/ui"
	"github.com/joho/godotenv"
	"github.com/spf13/cobra"
)

func ServerCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "server",
		Short: "Manage Haloy servers",
		Long:  "Add, remove, and manage connections to Haloy servers",
	}

	cmd.AddCommand(ServerAddCmd())
	cmd.AddCommand(ServerDeleteCmd())
	cmd.AddCommand(ServerListCmd())

	return cmd
}

func ServerAddCmd() *cobra.Command {
	var force bool
	cmd := &cobra.Command{
		Use:   "add <url> <token>",
		Short: "Add a new Haloy server",
		Args: func(cmd *cobra.Command, args []string) error {
			if len(args) < 2 {
				ui.Error("Error: You must provide a <url> and a <token> to add a server.\n")
				ui.Info("%s", cmd.UsageString())
				return fmt.Errorf("requires at least 2 arg(s), only received %d", len(args))
			}
			return nil
		},
		Run: func(cmd *cobra.Command, args []string) {
			url := args[0]
			token := strings.Join(args[1:], " ")

			if url == "" {
				ui.Error("URL is required")
				return
			}

			if token == "" {
				ui.Error("Token is required")
				return
			}

			normalizedURL, err := helpers.NormalizeServerURL(url)
			if err != nil {
				ui.Error("Invalid URL: %v", err)
				return
			}

			if err := helpers.IsValidDomain(normalizedURL); err != nil {
				ui.Error("Invalid domain: %v", err)
				return
			}

			configDir, err := config.ConfigDir()
			if err != nil {
				ui.Error("Failed to get config dir: %v", err)
				return
			}

			if err = helpers.EnsureDir(configDir); err != nil {
				ui.Error("Failed to create config dir: %v", err)
				return
			}

			envFile := filepath.Join(configDir, constants.ConfigEnvFileName)

			tokenEnv := generateTokenEnvName(normalizedURL)

			env, err := godotenv.Read(envFile)
			if err != nil {
				if os.IsNotExist(err) {
					// Create empty map if file doesn't exist
					env = make(map[string]string)
				} else {
					ui.Error("Failed to read env file: %v", err)
					return
				}
			}
			env[tokenEnv] = token
			if err := godotenv.Write(env, envFile); err != nil {
				ui.Error("Failed to write env file: %v", err)
				return
			}

			clientConfigPath := filepath.Join(configDir, constants.ClientConfigFileName)
			clientConfig, err := config.LoadClientConfig(clientConfigPath)
			if err != nil {
				ui.Error("Failed to load client config: %v", err)
				return
			}

			if clientConfig == nil {
				clientConfig = &config.ClientConfig{}
			}

			if err := clientConfig.AddServer(normalizedURL, tokenEnv, force); err != nil {
				ui.Error("Failed to add server: %v", err)
				return
			}

			if err := config.SaveClientConfig(clientConfig, clientConfigPath); err != nil {
				ui.Error("Failed to save client config: %v", err)
				return
			}

			ui.Success("Server %s added successfully", normalizedURL)
			ui.Info("API token stored as: %s", tokenEnv)
		},
	}

	cmd.Flags().BoolVarP(&force, "force", "f", false, "Force overwrite if server already exists")

	return cmd
}

func generateTokenEnvName(url string) string {
	return fmt.Sprintf("HALOY_API_TOKEN_%s", strings.ToUpper(helpers.SanitizeString(url)))
}

func ServerDeleteCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "delete <url>",
		Short: "Delete a Haloy server",
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			url := args[0]

			if url == "" {
				ui.Error("URL is required")
				return
			}

			normalizedURL, err := helpers.NormalizeServerURL(url)
			if err != nil {
				ui.Error("Invalid URL: %v", err)
				return
			}

			configDir, err := config.ConfigDir()
			if err != nil {
				ui.Error("Failed to get config dir: %v", err)
				return
			}

			clientConfigPath := filepath.Join(configDir, constants.ClientConfigFileName)
			clientConfig, err := config.LoadClientConfig(clientConfigPath)
			if err != nil {
				ui.Error("Failed to load client config: %v", err)
				return
			}

			if clientConfig == nil {
				ui.Error("No config file found in %s", clientConfigPath)
				return
			}

			if len(clientConfig.Servers) == 0 {
				ui.Error("No servers found in client config")
				return
			}

			serverConfig, exists := clientConfig.Servers[normalizedURL]
			if !exists {
				ui.Error("Server %s not found in config", normalizedURL)
				return
			}

			envFile := filepath.Join(configDir, constants.ConfigEnvFileName)
			env, _ := godotenv.Read(envFile)
			if _, exists := env[serverConfig.TokenEnv]; exists {
				delete(env, serverConfig.TokenEnv)
				if err := godotenv.Write(env, envFile); err != nil {
					ui.Warn("Failed to write env file: %v", err)
					ui.Info("Please remove the token %s from %s manually", serverConfig.TokenEnv, envFile)
				}
			}

			if err := clientConfig.DeleteServer(normalizedURL); err != nil {
				ui.Error("Failed to delete server: %v", err)
				return
			}

			if err := config.SaveClientConfig(clientConfig, clientConfigPath); err != nil {
				ui.Error("Failed to save client config: %v", err)
				return
			}

			ui.Success("Server %s deleted successfully", normalizedURL)
		},
	}
	return cmd
}

func ServerListCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List all Haloy servers",
		Run: func(cmd *cobra.Command, args []string) {
			configDir, err := config.ConfigDir()
			if err != nil {
				ui.Error("Failed to get config dir: %v", err)
				return
			}

			clientConfigPath := filepath.Join(configDir, constants.ClientConfigFileName)
			clientConfig, err := config.LoadClientConfig(clientConfigPath)
			if err != nil {
				ui.Error("Failed to load client config: %v", err)
				return
			}

			if clientConfig == nil {
				ui.Error("No config file found in %s", clientConfigPath)
				return
			}

			servers := clientConfig.Servers
			if len(servers) == 0 {
				ui.Info("No Haloy servers found")
				return
			}

			ui.Info("List of servers:")
			headers := []string{"URL", "ENV VAR", "ENV VAR EXISTS"}
			rows := make([][]string, 0, len(servers))
			for url, config := range servers {
				tokenExists := "⚠️ no"
				token := os.Getenv(config.TokenEnv)
				if token != "" {
					tokenExists = "✅ yes"
				}
				rows = append(rows, []string{url, config.TokenEnv, tokenExists})
			}

			ui.Table(headers, rows)
		},
	}
	return cmd
}
