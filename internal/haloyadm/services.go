package haloyadm

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/haloydev/haloy/internal/apiclient"
	"github.com/haloydev/haloy/internal/config"
	"github.com/haloydev/haloy/internal/constants"
	"github.com/haloydev/haloy/internal/logging"
	"github.com/haloydev/haloy/internal/ui"
	"github.com/joho/godotenv"
)

// startHaloyd runs the docker command to start haloyd.
func startHaloyd(ctx context.Context, dataDir, configDir string, devMode bool, debug bool) error {
	var image string
	if devMode {
		image = "haloyd:dev"
	} else {
		image = fmt.Sprintf("ghcr.io/haloydev/haloy-haloyd:%s", constants.Version)
	}

	uid := os.Getuid()
	gid := os.Getgid()
	dockerGID := getDockerGroupID()

	args := []string{
		"run",
		"--detach",
		"--name", constants.HaloydContainerName,
		"--publish", fmt.Sprintf("127.0.0.1:%s:%s", constants.CertificatesHTTPProviderPort, constants.CertificatesHTTPProviderPort),
		"--publish", fmt.Sprintf("127.0.0.1:%s:%s", constants.APIServerPort, constants.APIServerPort),
		"--volume", fmt.Sprintf("%s:%s:ro", configDir, configDir), // /etc/haloy or ~/.config/haloy
		"--volume", fmt.Sprintf("%s:%s:rw", dataDir, dataDir), // /var/lib/haloy or ~/.local/share/haloy
		"--volume", "/var/run/docker.sock:/var/run/docker.sock:rw",
		"--user", fmt.Sprintf("%d:%d", uid, gid),
		"--group-add", dockerGID,
		"--label", fmt.Sprintf("%s=%s", config.LabelRole, config.HaloydLabelRole),
		"--restart", "unless-stopped",
		"--network", constants.DockerNetwork,
		// Path environment variables so we can use paths functions and get the same results as on the host system.
		"--env", fmt.Sprintf("%s=%s", constants.EnvVarDataDir, dataDir),
		"--env", fmt.Sprintf("%s=%s", constants.EnvVarConfigDir, configDir),
		"--env", fmt.Sprintf("%s=%s", constants.EnvVarSystemInstall, fmt.Sprintf("%t", config.IsSystemMode())),
	}

	// using godotenv to add env variables from .env because --env-file does not support quotes in values.
	envFile := filepath.Join(configDir, constants.ConfigEnvFileName)
	env, err := godotenv.Read(envFile)
	if err != nil {
		return fmt.Errorf("failed to read env file: %w", err)
	}
	for key, value := range env {
		args = append(args[:2], append([]string{"--env", fmt.Sprintf("%s=%s", key, value)}, args[2:]...)...)
	}

	if debug {
		args = append(args[:2], append([]string{"--env", fmt.Sprintf("%s=true", constants.EnvVarDebug)}, args[2:]...)...)
	}

	args = append(args, image)

	cmd := exec.CommandContext(ctx, "docker", args...)

	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		if stderr.Len() > 0 {
			return fmt.Errorf("failed to start haloyd: %s", stderr.String())
		}
		return fmt.Errorf("failed to start haloyd: %w", err)
	}
	return nil
}

// Helper function to get Docker group ID dynamically
func getDockerGroupID() string {
	// First try environment variable
	if gid := os.Getenv("DOCKER_GID"); gid != "" {
		return gid
	}

	// Try to get it from getent command
	cmd := exec.Command("getent", "group", "docker")
	output, err := cmd.Output()
	if err == nil {
		// Parse output like "docker:x:999:user1,user2"
		parts := strings.Split(strings.TrimSpace(string(output)), ":")
		if len(parts) >= 3 {
			return parts[2] // The GID
		}
	}

	// Fall back to common default
	return "999"
}

// startHAProxy runs the docker command to start HAProxy.
func startHAProxy(ctx context.Context, dataDir string) error {
	cmd := exec.CommandContext(ctx, "docker", "run",
		"--detach",
		"--name", constants.HAProxyContainerName,
		"--publish", "80:80",
		"--publish", "443:443",
		"--volume", fmt.Sprintf("%s/%s:/usr/local/etc/haproxy:ro", dataDir, constants.HAProxyConfigDir),
		"--volume", fmt.Sprintf("%s/%s:/usr/local/etc/haproxy-certs:rw", dataDir, constants.CertStorageDir),
		"--volume", fmt.Sprintf("%s/error-pages:/usr/local/etc/haproxy-errors:ro", dataDir),
		"--label", fmt.Sprintf("%s=%s", config.LabelRole, config.HAProxyLabelRole),
		// Running as root is necessary for privileged ports 80 and 443.
		"--user", "root",
		"--restart", "unless-stopped",
		"--network", constants.DockerNetwork,
		fmt.Sprintf("haproxy:%s", constants.HAProxyVersion),
	)

	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		if stderr.Len() > 0 {
			return fmt.Errorf("failed to start haproxy: %s", stderr.String())
		}
		return fmt.Errorf("failed to start haproxy: %w", err)
	}

	return nil
}

// containerExists checks if a haloy container with the given role exists (running or stopped).
func containerExists(ctx context.Context, role string) (bool, error) {
	cmd := exec.CommandContext(ctx, "docker", "ps", "-a",
		"--filter", fmt.Sprintf("label=%s=%s", config.LabelRole, role),
		"--format", "{{.Names}}")

	var out bytes.Buffer
	cmd.Stdout = &out

	if err := cmd.Run(); err != nil {
		return false, fmt.Errorf("failed to list containers: %w", err)
	}

	// If there's any output, a container with this role exists
	output := strings.TrimSpace(out.String())
	return output != "", nil
}

func startServices(ctx context.Context, dataDir, configDir string, devMode, restart, debug bool) error {
	haloydExists, err := containerExists(ctx, config.HaloydLabelRole)
	if err != nil {
		return fmt.Errorf("failed to check haloyd container: %w", err)
	}

	haproxyExists, err := containerExists(ctx, config.HAProxyLabelRole)
	if err != nil {
		return fmt.Errorf("failed to check haloy-haproxy container: %w", err)
	}

	if !restart {
		if haproxyExists {
			return fmt.Errorf("haloy-haproxy container already exists, use haloyadm restart instead")
		}
		if haloydExists {
			return fmt.Errorf("haloyd container already exists, use haloyadm restart instead")
		}
	}

	if restart {
		if haproxyExists {
			if err := stopContainer(ctx, config.HAProxyLabelRole); err != nil {
				return fmt.Errorf("failed to stop existing haloy-haproxy: %w", err)
			}
		}

		if haloydExists {
			if err := stopContainer(ctx, config.HaloydLabelRole); err != nil {
				return fmt.Errorf("failed to stop existing haloyd: %w", err)
			}
		}
	}

	// Start haloyd first so that when HAProxy starts, it can resolve the haloyd
	// hostname via Docker DNS. This is important for ACME challenge routing.
	if err := startHaloyd(ctx, dataDir, configDir, devMode, debug); err != nil {
		return err
	}

	if err := startHAProxy(ctx, dataDir); err != nil {
		return err
	}

	return nil
}

func stopContainer(ctx context.Context, role string) error {
	cmd := exec.CommandContext(ctx, "docker", "ps", "-a",
		"--filter", fmt.Sprintf("label=%s=%s", config.LabelRole, role),
		"--format", "{{.Names}}")

	var out bytes.Buffer
	cmd.Stdout = &out

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to list containers with role %s: %w", role, err)
	}

	output := strings.TrimSpace(out.String())
	if output == "" {
		return nil // No container to stop
	}

	// Get the first container name (should only be one per role)
	containerName := strings.Split(output, "\n")[0]
	containerName = strings.TrimSpace(containerName)

	cmd = exec.CommandContext(ctx, "docker", "rm", "-f", containerName)

	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		if stderr.Len() > 0 {
			return fmt.Errorf("failed to stop and remove container %s: %s", containerName, stderr.String())
		}
		return fmt.Errorf("failed to stop and remove container %s: %w", containerName, err)
	}

	return nil
}

// EnsureNetworkCmd checks for the existence of the specified Docker network and creates it if it doesn't exist.
func ensureNetwork(ctx context.Context) error {
	// List networks filtering by name
	// The --format option outputs only the network names.
	cmdList := exec.CommandContext(ctx, "docker", "network", "ls", "--filter", fmt.Sprintf("name=%s", constants.DockerNetwork), "--format", "{{.Name}}")
	var out bytes.Buffer
	cmdList.Stdout = &out
	if err := cmdList.Run(); err != nil {
		return fmt.Errorf("failed to list Docker networks: %w", err)
	}

	networks := strings.Split(strings.TrimSpace(out.String()), "\n")
	networkExists := slices.Contains(networks, constants.DockerNetwork)

	if networkExists {
		return nil
	}

	cmdCreate := exec.CommandContext(ctx, "docker", "network", "create",
		"--driver", "bridge",
		"--attachable",
		"--label", "created-by=haloy",
		constants.DockerNetwork,
	)
	if output, err := cmdCreate.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to create Docker network: %w - output: %s", err, output)
	}

	return nil
}

// streamHaloydInitLogs waits for the API to become available and streams initialization logs
func streamHaloydInitLogs(ctx context.Context, api *apiclient.APIClient) error {
	streamHandler := func(data string) bool {
		var logEntry logging.LogEntry
		if err := json.Unmarshal([]byte(data), &logEntry); err != nil {
			// Skip malformed log entries and continue streaming
			return false
		}

		ui.DisplayLogEntry(logEntry, "")

		return logEntry.IsHaloydInitComplete
	}
	return api.Stream(ctx, "logs", streamHandler)
}

// waitForAPI polls the API health endpoint until it's available
func waitForAPI(ctx context.Context, api *apiclient.APIClient) error {
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("timeout waiting for API to become ready: %w", ctx.Err())
		case <-ticker.C:
			// Try to check API health (without auth since health endpoint is public)
			healthCtx, healthCancel := context.WithTimeout(ctx, 2*time.Second)
			err := api.HealthCheck(healthCtx)
			healthCancel()

			if err == nil {
				return nil // API is available
			}

			// Continue polling if API is not ready yet
		}
	}
}

// waitForHAProxy polls HAProxy until it's accepting connections.
// It checks the root path "/" to ensure the container is up and the port is bound.
func waitForHAProxy(ctx context.Context) error {
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	portReady := false

	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("timeout waiting for HAProxy to become ready: %w", ctx.Err())
		case <-ticker.C:
			// Check if HAProxy container is running
			cmd := exec.CommandContext(ctx, "docker", "ps",
				"--filter", fmt.Sprintf("label=%s=%s", config.LabelRole, config.HAProxyLabelRole),
				"--filter", "status=running",
				"--format", "{{.ID}}")

			var out bytes.Buffer
			cmd.Stdout = &out
			if err := cmd.Run(); err != nil || out.Len() == 0 {
				continue // Container not running yet
			}

			// Check if port 80 is accepting connections
			if !portReady {
				conn, err := net.DialTimeout("tcp", "127.0.0.1:80", 2*time.Second)
				if err != nil {
					continue // Port not ready yet
				}
				conn.Close()
				portReady = true
			}

			// Verify HAProxy is responding to HTTP requests.
			// We check the root path which should return 404 (default backend) or 503
			// if something is misconfigured, but either way it proves HAProxy is up and listening.
			// We avoid checking the ACME path because the backend (haloyd) might not be listening
			// on that port yet, causing connection timeouts from HAProxy.
			client := &http.Client{Timeout: 2 * time.Second}
			resp, err := client.Get("http://127.0.0.1:80/")
			if err != nil {
				continue // Routing not ready yet
			}
			resp.Body.Close()

			// HAProxy is fully ready
			return nil
		}
	}
}

// generateAPIToken creates a secure random API token
func generateAPIToken() (string, error) {
	tokenBytes := make([]byte, apiTokenLength)
	if _, err := rand.Read(tokenBytes); err != nil {
		return "", fmt.Errorf("failed to generate random token: %w", err)
	}
	token := hex.EncodeToString(tokenBytes)

	// Validate generated token
	if len(token) != apiTokenLength*2 {
		return "", fmt.Errorf("generated token has unexpected length: got %d, expected %d",
			len(token), apiTokenLength*2)
	}

	return token, nil
}
