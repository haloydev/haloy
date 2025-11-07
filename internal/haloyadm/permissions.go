package haloyadm

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/haloydev/haloy/internal/config"
)

// RequiredAccess defines what directory access is needed
type RequiredAccess struct {
	Config bool // Need config directory access
	Data   bool // Need data directory access
}

// CheckDirectoryAccess verifies we can access/create required directories
func checkDirectoryAccess(required RequiredAccess) error {
	if required.Config {
		configDir, err := config.ConfigDir()
		if err != nil {
			return fmt.Errorf("failed to determine config directory: %w", err)
		}

		if err := checkDirectoryWritable(configDir, "config"); err != nil {
			return err
		}
	}

	if required.Data {
		dataDir, err := config.DataDir()
		if err != nil {
			return fmt.Errorf("failed to determine data directory: %w", err)
		}

		if err := checkDirectoryWritable(dataDir, "data"); err != nil {
			return err
		}
	}

	return nil
}

// checkDirectoryWritable checks if we can read/write to a directory
func checkDirectoryWritable(dir, dirType string) error {
	// Check if directory exists and is accessible
	info, err := os.Stat(dir)
	if err != nil {
		if os.IsNotExist(err) {
			if err := os.MkdirAll(dir, 0o755); err != nil {
				return formatPermissionError(dir, dirType, err)
			}
			return nil
		}
		// Some other error accessing the directory
		return formatPermissionError(dir, dirType, err)
	}

	// Directory exists - check if it's actually a directory
	if !info.IsDir() {
		return fmt.Errorf("%s path exists but is not a directory: %s", dirType, dir)
	}

	// Check if we can write to it by creating a temporary file
	testFile := filepath.Join(dir, ".haloyadm-access-test")
	if err := os.WriteFile(testFile, []byte("test"), 0o644); err != nil {
		return formatPermissionError(dir, dirType, err)
	}

	// Clean up test file
	_ = os.Remove(testFile)

	return nil
}

// formatPermissionError creates a helpful error message with sudo hint if needed
func formatPermissionError(dir, dirType string, err error) error {
	baseMsg := fmt.Sprintf("cannot access %s directory: %s\nError: %v", dirType, dir, err)

	// Add sudo hint if not running as root
	if os.Geteuid() != 0 {
		return fmt.Errorf("%s\n\nTip: Try running with sudo if this is a system directory", baseMsg)
	}

	return fmt.Errorf("%s", baseMsg)
}
