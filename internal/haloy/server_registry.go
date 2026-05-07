package haloy

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/haloydev/haloy/internal/apiclient"
	"github.com/haloydev/haloy/internal/apitypes"
	"github.com/haloydev/haloy/internal/config"
	"github.com/haloydev/haloy/internal/ui"
	"github.com/spf13/cobra"
)

func ServerRegistryCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "registry",
		Short: "Manage server registry credentials",
		Long:  "Manage registry credentials haloyd uses when pulling images.",
	}

	cmd.AddCommand(
		ServerRegistryLoginCmd(),
		ServerRegistryLogoutCmd(),
		ServerRegistryListCmd(),
	)

	return cmd
}

func ServerRegistryLoginCmd() *cobra.Command {
	var username, password string
	var passwordStdin bool

	cmd := &cobra.Command{
		Use:   "login <server-url> <registry>",
		Short: "Store registry credentials on a Haloy server",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			if username == "" {
				return fmt.Errorf("--username is required")
			}
			if password != "" && passwordStdin {
				return fmt.Errorf("use either --password or --password-stdin, not both")
			}
			if passwordStdin {
				data, err := io.ReadAll(cmd.InOrStdin())
				if err != nil {
					return fmt.Errorf("failed to read password from stdin: %w", err)
				}
				password = strings.TrimRight(string(data), "\r\n")
			}
			if password == "" {
				return fmt.Errorf("--password or --password-stdin is required")
			}

			serverURL := args[0]
			registry := config.NormalizeRegistryServer(args[1])
			response, err := registryLogin(cmd.Context(), serverURL, apitypes.RegistryLoginRequest{
				Server:   registry,
				Username: username,
				Password: password,
			})
			if err != nil {
				return err
			}

			ui.Success("Registry credentials stored on %s for %s", serverURL, response.Server)
			ui.Info("haloyd will use these credentials on future deploys")
			return nil
		},
	}

	cmd.Flags().StringVarP(&username, "username", "u", "", "Registry username")
	cmd.Flags().StringVarP(&password, "password", "p", "", "Registry password or access token")
	cmd.Flags().BoolVar(&passwordStdin, "password-stdin", false, "Read registry password or access token from stdin")

	return cmd
}

func ServerRegistryLogoutCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "logout <server-url> <registry>",
		Short: "Remove registry credentials from a Haloy server",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			serverURL := args[0]
			registry := config.NormalizeRegistryServer(args[1])
			if err := registryLogout(cmd.Context(), serverURL, registry); err != nil {
				return err
			}

			ui.Success("Registry credentials removed from %s for %s", serverURL, registry)
			return nil
		},
	}
}

func ServerRegistryListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list <server-url>",
		Short: "List registry credentials configured on a Haloy server",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			serverURL := args[0]
			response, err := registryList(cmd.Context(), serverURL)
			if err != nil {
				return err
			}

			if len(response.Registries) == 0 {
				ui.Info("No registry credentials configured on %s", serverURL)
				return nil
			}

			rows := make([][]string, 0, len(response.Registries))
			for _, registry := range response.Registries {
				rows = append(rows, []string{registry.Server, registry.Username})
			}
			ui.Table([]string{"Registry", "Username"}, rows)
			return nil
		},
	}
}

func registryAPI(ctx context.Context, serverURL string) (*apiclient.APIClient, error) {
	token, err := getToken(nil, serverURL)
	if err != nil {
		return nil, fmt.Errorf("unable to get token: %w", err)
	}

	api, err := apiclient.New(serverURL, token)
	if err != nil {
		return nil, fmt.Errorf("unable to create API client: %w", err)
	}
	return api, nil
}

func registryLogin(ctx context.Context, serverURL string, request apitypes.RegistryLoginRequest) (*apitypes.RegistryEntry, error) {
	api, err := registryAPI(ctx, serverURL)
	if err != nil {
		return nil, err
	}

	var response apitypes.RegistryEntry
	if err := api.Post(ctx, "registries/login", request, &response); err != nil {
		return nil, fmt.Errorf("failed to store registry credentials: %w", err)
	}
	return &response, nil
}

func registryLogout(ctx context.Context, serverURL, registry string) error {
	api, err := registryAPI(ctx, serverURL)
	if err != nil {
		return err
	}

	if err := api.Post(ctx, "registries/logout", apitypes.RegistryLogoutRequest{Server: registry}, nil); err != nil {
		return fmt.Errorf("failed to remove registry credentials: %w", err)
	}
	return nil
}

func registryList(ctx context.Context, serverURL string) (*apitypes.RegistriesResponse, error) {
	api, err := registryAPI(ctx, serverURL)
	if err != nil {
		return nil, err
	}

	var response apitypes.RegistriesResponse
	if err := api.Get(ctx, "registries", &response); err != nil {
		return nil, fmt.Errorf("failed to list registry credentials: %w", err)
	}
	return &response, nil
}
