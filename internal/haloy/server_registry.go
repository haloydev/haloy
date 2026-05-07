package haloy

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/haloydev/haloy/internal/apiclient"
	"github.com/haloydev/haloy/internal/apitypes"
	"github.com/haloydev/haloy/internal/config"
	"github.com/haloydev/haloy/internal/helpers"
	"github.com/haloydev/haloy/internal/ui"
	"github.com/mattn/go-isatty"
	"github.com/spf13/cobra"
)

var isTerminal = isatty.IsTerminal

type registryTarget struct {
	Server       string
	TargetConfig *config.TargetConfig
}

type registryListResult struct {
	Server   string
	Response *apitypes.RegistriesResponse
}

func ServerRegistryCmd(configPath *string, flags *appCmdFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "registry",
		Short: "Manage server registry credentials",
		Long:  "Manage registry credentials haloyd uses when pulling images.",
	}

	cmd.AddCommand(
		ServerRegistryLoginCmd(configPath, flags),
		ServerRegistryLogoutCmd(configPath, flags),
		ServerRegistryListCmd(configPath, flags),
	)

	return cmd
}

func ServerRegistryLoginCmd(configPath *string, flags *appCmdFlags) *cobra.Command {
	var serverFlag, username, password string
	var passwordStdin bool

	cmd := &cobra.Command{
		Use:   "login <registry>",
		Short: "Store registry credentials on a Haloy server",
		Args:  cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			if username == "" {
				return fmt.Errorf("--username is required")
			}
			if password != "" && passwordStdin {
				return fmt.Errorf("use either --password or --password-stdin, not both")
			}
			if passwordStdin {
				stdin := cmd.InOrStdin()
				if file, ok := stdin.(*os.File); ok && isTerminal(file.Fd()) {
					return fmt.Errorf("--password-stdin requires piped input. Try: echo \"$REGISTRY_TOKEN\" | haloy server registry login docker.io --username <user> --password-stdin. For a direct server, add --server <url>")
				}
				data, err := io.ReadAll(stdin)
				if err != nil {
					return fmt.Errorf("failed to read password from stdin: %w", err)
				}
				password = strings.TrimRight(string(data), "\r\n")
			}
			if password == "" {
				return fmt.Errorf("--password or --password-stdin is required")
			}

			registry, serverOverride, err := parseRegistryCommandArgs(args, serverFlag, "login")
			if err != nil {
				return err
			}

			targets, err := resolveRegistryTargets(cmd.Context(), cmd, registryConfigPath(configPath), flags, serverOverride)
			if err != nil {
				return err
			}

			for _, target := range targets {
				response, err := registryLogin(cmd.Context(), target.TargetConfig, target.Server, apitypes.RegistryLoginRequest{
					Server:   registry,
					Username: username,
					Password: password,
				})
				if err != nil {
					return err
				}
				ui.Success("Registry credentials stored on %s for %s", target.Server, response.Server)
			}
			ui.Info("haloyd will use these credentials on future deploys")
			return nil
		},
	}

	addRegistryTargetFlags(cmd, flags, &serverFlag)
	cmd.Flags().StringVarP(&username, "username", "u", "", "Registry username")
	cmd.Flags().StringVarP(&password, "password", "p", "", "Registry password or access token")
	cmd.Flags().BoolVar(&passwordStdin, "password-stdin", false, "Read registry password or access token from piped stdin")

	return cmd
}

func ServerRegistryLogoutCmd(configPath *string, flags *appCmdFlags) *cobra.Command {
	var serverFlag string

	cmd := &cobra.Command{
		Use:   "logout <registry>",
		Short: "Remove registry credentials from a Haloy server",
		Args:  cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			registry, serverOverride, err := parseRegistryCommandArgs(args, serverFlag, "logout")
			if err != nil {
				return err
			}

			targets, err := resolveRegistryTargets(cmd.Context(), cmd, registryConfigPath(configPath), flags, serverOverride)
			if err != nil {
				return err
			}

			for _, target := range targets {
				if err := registryLogout(cmd.Context(), target.TargetConfig, target.Server, registry); err != nil {
					return err
				}
				ui.Success("Registry credentials removed from %s for %s", target.Server, registry)
			}
			return nil
		},
	}

	addRegistryTargetFlags(cmd, flags, &serverFlag)

	return cmd
}

func ServerRegistryListCmd(configPath *string, flags *appCmdFlags) *cobra.Command {
	var serverFlag string

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List registry credentials configured on a Haloy server",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			serverOverride, err := parseRegistryListArgs(args, serverFlag)
			if err != nil {
				return err
			}

			targets, err := resolveRegistryTargets(cmd.Context(), cmd, registryConfigPath(configPath), flags, serverOverride)
			if err != nil {
				return err
			}

			results := make([]registryListResult, 0, len(targets))
			for _, target := range targets {
				response, err := registryList(cmd.Context(), target.TargetConfig, target.Server)
				if err != nil {
					return err
				}
				results = append(results, registryListResult{
					Server:   target.Server,
					Response: response,
				})
			}
			displayRegistryListResults(results)
			return nil
		},
	}

	addRegistryTargetFlags(cmd, flags, &serverFlag)

	return cmd
}

func addRegistryTargetFlags(cmd *cobra.Command, flags *appCmdFlags, serverFlag *string) {
	cmd.Flags().StringVarP(serverFlag, "server", "s", "", "Server URL (overrides config file)")
	if flags != nil {
		cmd.Flags().StringVarP(&flags.configPath, "config", "c", "", "Path to config file or directory (default: .)")
		cmd.Flags().StringSliceVarP(&flags.targets, "targets", "t", nil, "Apply to specific targets (comma-separated)")
		cmd.Flags().BoolVarP(&flags.all, "all", "a", false, "Apply to all targets")
		cmd.RegisterFlagCompletionFunc("targets", completeTargetNames)
	}
}

func parseRegistryCommandArgs(args []string, serverFlag, command string) (registry string, serverOverride string, err error) {
	switch len(args) {
	case 1:
		return config.NormalizeRegistryServer(args[0]), serverFlag, nil
	case 2:
		if serverFlag != "" {
			return "", "", fmt.Errorf("use either --server or the legacy '%s <server-url> <registry>' form, not both", command)
		}
		return config.NormalizeRegistryServer(args[1]), args[0], nil
	default:
		return "", "", fmt.Errorf("requires a registry")
	}
}

func parseRegistryListArgs(args []string, serverFlag string) (serverOverride string, err error) {
	switch len(args) {
	case 0:
		return serverFlag, nil
	case 1:
		if serverFlag != "" {
			return "", fmt.Errorf("use either --server or the legacy 'list <server-url>' form, not both")
		}
		return args[0], nil
	default:
		return "", fmt.Errorf("too many arguments")
	}
}

func registryConfigPath(configPath *string) string {
	if configPath == nil || *configPath == "" {
		return "."
	}
	return *configPath
}

func resolveRegistryTargets(ctx context.Context, cmd *cobra.Command, configPath string, flags *appCmdFlags, serverOverride string) ([]registryTarget, error) {
	if serverOverride != "" {
		normalized, err := helpers.NormalizeServerURL(serverOverride)
		if err != nil {
			return nil, fmt.Errorf("invalid server URL: %w", err)
		}
		return []registryTarget{{Server: normalized}}, nil
	}

	serverTargets, err := resolveServerTargets(ctx, cmd, configPath, flags)
	if err != nil {
		return nil, err
	}
	if !serverTargetSelectorsChanged(cmd) && len(serverTargets) > 1 {
		return nil, fmt.Errorf("multiple servers available, please specify targets with --targets or use --all")
	}

	registryTargets := make([]registryTarget, 0, len(serverTargets))
	for _, target := range serverTargets {
		registryTargets = append(registryTargets, registryTarget{
			Server:       target.Server,
			TargetConfig: target.TargetConfig,
		})
	}
	return registryTargets, nil
}

func displayRegistryListResults(results []registryListResult) {
	multipleServers := len(results) > 1
	rows := make([][]string, 0)
	for _, result := range results {
		if len(result.Response.Registries) == 0 {
			ui.Info("No registry credentials configured on %s", result.Server)
			continue
		}
		for _, registry := range result.Response.Registries {
			if multipleServers {
				rows = append(rows, []string{result.Server, registry.Server, registry.Username})
			} else {
				rows = append(rows, []string{registry.Server, registry.Username})
			}
		}
	}

	if len(rows) == 0 {
		return
	}
	if multipleServers {
		ui.Table([]string{"Server", "Registry", "Username"}, rows)
		return
	}
	ui.Table([]string{"Registry", "Username"}, rows)
}

func registryAPI(ctx context.Context, targetConfig *config.TargetConfig, serverURL string) (*apiclient.APIClient, error) {
	token, err := getToken(targetConfig, serverURL)
	if err != nil {
		return nil, fmt.Errorf("unable to get token: %w", err)
	}

	api, err := apiclient.New(serverURL, token)
	if err != nil {
		return nil, fmt.Errorf("unable to create API client: %w", err)
	}
	return api, nil
}

func registryLogin(ctx context.Context, targetConfig *config.TargetConfig, serverURL string, request apitypes.RegistryLoginRequest) (*apitypes.RegistryEntry, error) {
	api, err := registryAPI(ctx, targetConfig, serverURL)
	if err != nil {
		return nil, err
	}

	var response apitypes.RegistryEntry
	if err := api.Post(ctx, "registries/login", request, &response); err != nil {
		return nil, fmt.Errorf("failed to store registry credentials: %w", err)
	}
	return &response, nil
}

func registryLogout(ctx context.Context, targetConfig *config.TargetConfig, serverURL, registry string) error {
	api, err := registryAPI(ctx, targetConfig, serverURL)
	if err != nil {
		return err
	}

	if err := api.Post(ctx, "registries/logout", apitypes.RegistryLogoutRequest{Server: registry}, nil); err != nil {
		return fmt.Errorf("failed to remove registry credentials: %w", err)
	}
	return nil
}

func registryList(ctx context.Context, targetConfig *config.TargetConfig, serverURL string) (*apitypes.RegistriesResponse, error) {
	api, err := registryAPI(ctx, targetConfig, serverURL)
	if err != nil {
		return nil, err
	}

	var response apitypes.RegistriesResponse
	if err := api.Get(ctx, "registries", &response); err != nil {
		return nil, fmt.Errorf("failed to list registry credentials: %w", err)
	}
	return &response, nil
}
