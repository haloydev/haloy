package helpers

import (
	"os"
	"os/exec"
)

// InitSystem represents the detected init system type
type InitSystem string

const (
	InitSystemd  InitSystem = "systemd"
	InitOpenRC   InitSystem = "openrc"
	InitSysVInit InitSystem = "sysvinit"
	InitUnknown  InitSystem = "unknown"
)

// DetectInitSystem returns the init system used on the current machine
func DetectInitSystem() InitSystem {
	// Check for systemd: directory must exist AND systemctl must be available
	if _, err := os.Stat("/run/systemd/system"); err == nil {
		if _, err := exec.LookPath("systemctl"); err == nil {
			return InitSystemd
		}
	}
	// Check for OpenRC
	if _, err := os.Stat("/sbin/openrc-run"); err == nil {
		return InitOpenRC
	}
	// Fallback to sysvinit if init.d exists
	if _, err := os.Stat("/etc/init.d"); err == nil {
		return InitSysVInit
	}
	return InitUnknown
}

// RestartCommand returns the command to restart haloyd for the detected init system
func RestartCommand() string {
	switch DetectInitSystem() {
	case InitSystemd:
		return "systemctl restart haloyd"
	case InitOpenRC:
		return "rc-service haloyd restart"
	default:
		return "/etc/init.d/haloyd restart"
	}
}

// RestartServiceArgs returns the command and arguments to restart haloyd
func RestartServiceArgs() (string, []string) {
	switch DetectInitSystem() {
	case InitSystemd:
		return "systemctl", []string{"restart", "haloyd"}
	case InitOpenRC:
		return "rc-service", []string{"haloyd", "restart"}
	default:
		return "/etc/init.d/haloyd", []string{"restart"}
	}
}

// RestartService executes the service restart command
func RestartService() error {
	cmd, args := RestartServiceArgs()
	return exec.Command(cmd, args...).Run()
}
