package haloydcli

import (
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"time"

	"github.com/haloydev/haloy/internal/config"
	"github.com/haloydev/haloy/internal/constants"
	"github.com/haloydev/haloy/internal/ui"
	"github.com/spf13/cobra"
)

func verifyCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "verify",
		Short: "Verify Haloy installation and configuration",
		Long: `Run diagnostic checks to verify Haloy is properly installed and configured.

Checks performed:
  - Configuration files exist and are valid
  - Data directories have correct permissions
  - Docker daemon is accessible
  - Docker network exists
  - API is responding (if service is running)`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runVerify()
		},
	}
	return cmd
}

type checkResult struct {
	name    string
	passed  bool
	message string
}

func runVerify() error {
	ui.Info("Running Haloy verification checks...\n")

	checks := []func() checkResult{
		checkConfigDir,
		checkDataDir,
		checkConfigFiles,
		checkDocker,
		checkDockerNetwork,
		checkAPIHealth,
	}

	passed := 0
	failed := 0

	for _, check := range checks {
		result := check()
		if result.passed {
			ui.Success("%s: %s", result.name, result.message)
			passed++
		} else {
			ui.Error("%s: %s", result.name, result.message)
			failed++
		}
	}

	ui.Info("\nResults: %d passed, %d failed", passed, failed)

	if failed > 0 {
		return fmt.Errorf("%d checks failed", failed)
	}
	return nil
}

func checkConfigDir() checkResult {
	configDir, err := config.ConfigDir()
	if err != nil {
		return checkResult{
			name:    "Config directory",
			passed:  false,
			message: fmt.Sprintf("failed to determine path: %v", err),
		}
	}

	info, err := os.Stat(configDir)
	if os.IsNotExist(err) {
		return checkResult{
			name:    "Config directory",
			passed:  false,
			message: fmt.Sprintf("does not exist: %s", configDir),
		}
	}
	if err != nil {
		return checkResult{
			name:    "Config directory",
			passed:  false,
			message: fmt.Sprintf("cannot access: %v", err),
		}
	}
	if !info.IsDir() {
		return checkResult{
			name:    "Config directory",
			passed:  false,
			message: fmt.Sprintf("not a directory: %s", configDir),
		}
	}

	return checkResult{
		name:    "Config directory",
		passed:  true,
		message: configDir,
	}
}

func checkDataDir() checkResult {
	dataDir, err := config.DataDir()
	if err != nil {
		return checkResult{
			name:    "Data directory",
			passed:  false,
			message: fmt.Sprintf("failed to determine path: %v", err),
		}
	}

	info, err := os.Stat(dataDir)
	if os.IsNotExist(err) {
		return checkResult{
			name:    "Data directory",
			passed:  false,
			message: fmt.Sprintf("does not exist: %s", dataDir),
		}
	}
	if err != nil {
		return checkResult{
			name:    "Data directory",
			passed:  false,
			message: fmt.Sprintf("cannot access: %v", err),
		}
	}
	if !info.IsDir() {
		return checkResult{
			name:    "Data directory",
			passed:  false,
			message: fmt.Sprintf("not a directory: %s", dataDir),
		}
	}

	// Check permissions
	stat, ok := info.Sys().(*syscall.Stat_t)
	if ok {
		mode := info.Mode().Perm()
		if mode != 0700 {
			return checkResult{
				name:    "Data directory",
				passed:  false,
				message: fmt.Sprintf("incorrect permissions %o (expected 700): %s", mode, dataDir),
			}
		}
		// Check owner is haloy user or root
		if stat.Uid != 0 {
			return checkResult{
				name:    "Data directory",
				passed:  true,
				message: fmt.Sprintf("%s (uid=%d)", dataDir, stat.Uid),
			}
		}
	}

	return checkResult{
		name:    "Data directory",
		passed:  true,
		message: dataDir,
	}
}

func checkConfigFiles() checkResult {
	configDir, err := config.ConfigDir()
	if err != nil {
		return checkResult{
			name:    "Config files",
			passed:  false,
			message: fmt.Sprintf("failed to determine config dir: %v", err),
		}
	}

	// Check .env file
	envPath := filepath.Join(configDir, constants.ConfigEnvFileName)
	if _, err := os.Stat(envPath); os.IsNotExist(err) {
		return checkResult{
			name:    "Config files",
			passed:  false,
			message: fmt.Sprintf("missing: %s", envPath),
		}
	}

	// Check haloyd.yaml
	yamlPath := filepath.Join(configDir, constants.HaloydConfigFileName)
	if _, err := os.Stat(yamlPath); os.IsNotExist(err) {
		return checkResult{
			name:    "Config files",
			passed:  false,
			message: fmt.Sprintf("missing: %s", yamlPath),
		}
	}

	// Try to load and validate config
	cfg, err := config.LoadHaloydConfig(yamlPath)
	if err != nil {
		return checkResult{
			name:    "Config files",
			passed:  false,
			message: fmt.Sprintf("invalid config: %v", err),
		}
	}

	if cfg == nil {
		return checkResult{
			name:    "Config files",
			passed:  false,
			message: fmt.Sprintf("could not load: %s", yamlPath),
		}
	}

	if err := cfg.Validate(); err != nil {
		return checkResult{
			name:    "Config files",
			passed:  false,
			message: fmt.Sprintf("validation failed: %v", err),
		}
	}

	return checkResult{
		name:    "Config files",
		passed:  true,
		message: "valid",
	}
}

func checkDocker() checkResult {
	// Check if docker command exists
	if _, err := exec.LookPath("docker"); err != nil {
		return checkResult{
			name:    "Docker",
			passed:  false,
			message: "docker command not found",
		}
	}

	// Check if Docker daemon is running
	cmd := exec.Command("docker", "info")
	if err := cmd.Run(); err != nil {
		return checkResult{
			name:    "Docker",
			passed:  false,
			message: "daemon not running or not accessible",
		}
	}

	// Get Docker version
	versionCmd := exec.Command("docker", "--version")
	output, err := versionCmd.Output()
	if err != nil {
		return checkResult{
			name:    "Docker",
			passed:  true,
			message: "running",
		}
	}

	return checkResult{
		name:    "Docker",
		passed:  true,
		message: string(output[:len(output)-1]), // trim newline
	}
}

func checkDockerNetwork() checkResult {
	cmd := exec.Command("docker", "network", "inspect", constants.DockerNetwork)
	if err := cmd.Run(); err != nil {
		return checkResult{
			name:    "Docker network",
			passed:  false,
			message: fmt.Sprintf("'%s' network does not exist", constants.DockerNetwork),
		}
	}

	return checkResult{
		name:    "Docker network",
		passed:  true,
		message: fmt.Sprintf("'%s' exists", constants.DockerNetwork),
	}
}

func checkAPIHealth() checkResult {
	client := &http.Client{
		Timeout: 2 * time.Second,
	}

	url := fmt.Sprintf("http://localhost:%s/version", constants.APIServerPort)
	resp, err := client.Get(url)
	if err != nil {
		return checkResult{
			name:    "API health",
			passed:  false,
			message: "not responding (service may not be running)",
		}
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return checkResult{
			name:    "API health",
			passed:  false,
			message: fmt.Sprintf("unhealthy (status %d)", resp.StatusCode),
		}
	}

	return checkResult{
		name:    "API health",
		passed:  true,
		message: fmt.Sprintf("responding on port %s", constants.APIServerPort),
	}
}
