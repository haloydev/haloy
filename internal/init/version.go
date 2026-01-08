package init

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// PackageManager represents a Node.js package manager
type PackageManager string

const (
	PackageManagerNPM  PackageManager = "npm"
	PackageManagerYarn PackageManager = "yarn"
	PackageManagerPNPM PackageManager = "pnpm"
	PackageManagerBun  PackageManager = "bun"
)

// PackageManagerInfo contains information about a detected package manager
type PackageManagerInfo struct {
	Name       PackageManager
	LockFile   string
	InstallCmd string
	BuildCmd   string
	StartCmd   string
}

// DetectPackageManager detects the package manager used in a Node.js project
func DetectPackageManager(projectDir string) PackageManagerInfo {
	// Check for bun (check both old and new lockfile names)
	if fileExists(filepath.Join(projectDir, "bun.lockb")) || fileExists(filepath.Join(projectDir, "bun.lock")) {
		lockFile := "bun.lockb"
		if fileExists(filepath.Join(projectDir, "bun.lock")) {
			lockFile = "bun.lock"
		}
		return PackageManagerInfo{
			Name:       PackageManagerBun,
			LockFile:   lockFile,
			InstallCmd: "bun install --frozen-lockfile",
			BuildCmd:   "bun run build",
			StartCmd:   "bun start",
		}
	}

	// Check for pnpm
	if fileExists(filepath.Join(projectDir, "pnpm-lock.yaml")) {
		return PackageManagerInfo{
			Name:       PackageManagerPNPM,
			LockFile:   "pnpm-lock.yaml",
			InstallCmd: "pnpm install --frozen-lockfile",
			BuildCmd:   "pnpm run build",
			StartCmd:   "pnpm start",
		}
	}

	// Check for yarn
	if fileExists(filepath.Join(projectDir, "yarn.lock")) {
		return PackageManagerInfo{
			Name:       PackageManagerYarn,
			LockFile:   "yarn.lock",
			InstallCmd: "yarn --frozen-lockfile",
			BuildCmd:   "yarn build",
			StartCmd:   "yarn start",
		}
	}

	// Default to npm
	return PackageManagerInfo{
		Name:       PackageManagerNPM,
		LockFile:   "package-lock.json",
		InstallCmd: "npm ci",
		BuildCmd:   "npm run build",
		StartCmd:   "npm start",
	}
}

// DetectNodeVersion detects the Node.js version from project files
func DetectNodeVersion(projectDir string) string {
	// Check .nvmrc
	nvmrcPath := filepath.Join(projectDir, ".nvmrc")
	if data, err := os.ReadFile(nvmrcPath); err == nil {
		version := strings.TrimSpace(string(data))
		version = strings.TrimPrefix(version, "v")
		// Extract major version
		if major := extractMajorVersion(version); major != "" {
			return major
		}
	}

	// Check .node-version
	nodeVersionPath := filepath.Join(projectDir, ".node-version")
	if data, err := os.ReadFile(nodeVersionPath); err == nil {
		version := strings.TrimSpace(string(data))
		version = strings.TrimPrefix(version, "v")
		if major := extractMajorVersion(version); major != "" {
			return major
		}
	}

	// Check package.json engines.node
	packageJSONPath := filepath.Join(projectDir, "package.json")
	if data, err := os.ReadFile(packageJSONPath); err == nil {
		var packageJSON struct {
			Engines struct {
				Node string `json:"node"`
			} `json:"engines"`
		}
		if err := json.Unmarshal(data, &packageJSON); err == nil && packageJSON.Engines.Node != "" {
			// Parse version constraints like ">=18", "^20", "22.x", etc.
			if major := extractMajorFromConstraint(packageJSON.Engines.Node); major != "" {
				return major
			}
		}
	}

	// Default to Node 22 (current LTS)
	return "22"
}

// DetectPythonVersion detects the Python version from project files
func DetectPythonVersion(projectDir string) string {
	// Check .python-version
	pythonVersionPath := filepath.Join(projectDir, ".python-version")
	if data, err := os.ReadFile(pythonVersionPath); err == nil {
		version := strings.TrimSpace(string(data))
		// Return major.minor (e.g., "3.12")
		parts := strings.Split(version, ".")
		if len(parts) >= 2 {
			return parts[0] + "." + parts[1]
		}
		return version
	}

	// Check runtime.txt (Heroku-style)
	runtimePath := filepath.Join(projectDir, "runtime.txt")
	if data, err := os.ReadFile(runtimePath); err == nil {
		content := strings.TrimSpace(string(data))
		// Format: python-3.12.0
		if strings.HasPrefix(content, "python-") {
			version := strings.TrimPrefix(content, "python-")
			parts := strings.Split(version, ".")
			if len(parts) >= 2 {
				return parts[0] + "." + parts[1]
			}
		}
	}

	// Check pyproject.toml for requires-python
	pyprojectPath := filepath.Join(projectDir, "pyproject.toml")
	if data, err := os.ReadFile(pyprojectPath); err == nil {
		content := string(data)
		// Look for requires-python = ">=3.12" or similar
		re := regexp.MustCompile(`requires-python\s*=\s*["']>=?(\d+\.\d+)`)
		if matches := re.FindStringSubmatch(content); len(matches) > 1 {
			return matches[1]
		}
	}

	// Default to Python 3.12
	return "3.12"
}

// DetectDjangoProject detects the Django project name (for wsgi module)
func DetectDjangoProject(projectDir string) string {
	// Look for directories containing wsgi.py
	entries, err := os.ReadDir(projectDir)
	if err != nil {
		return "app"
	}

	for _, entry := range entries {
		if entry.IsDir() {
			wsgiPath := filepath.Join(projectDir, entry.Name(), "wsgi.py")
			if fileExists(wsgiPath) {
				return entry.Name()
			}
		}
	}

	// Fallback: parse manage.py for DJANGO_SETTINGS_MODULE
	managePyPath := filepath.Join(projectDir, "manage.py")
	if data, err := os.ReadFile(managePyPath); err == nil {
		content := string(data)
		// Look for: os.environ.setdefault('DJANGO_SETTINGS_MODULE', 'myproject.settings')
		re := regexp.MustCompile(`DJANGO_SETTINGS_MODULE['"]\s*,\s*['"]([^.]+)\.settings`)
		if matches := re.FindStringSubmatch(content); len(matches) > 1 {
			return matches[1]
		}
	}

	return "app"
}

// extractMajorVersion extracts the major version from a version string
func extractMajorVersion(version string) string {
	parts := strings.Split(version, ".")
	if len(parts) > 0 && parts[0] != "" {
		// Ensure it's numeric
		if regexp.MustCompile(`^\d+$`).MatchString(parts[0]) {
			return parts[0]
		}
	}
	return ""
}

// extractMajorFromConstraint extracts the major version from a version constraint
func extractMajorFromConstraint(constraint string) string {
	// Remove common constraint prefixes
	constraint = strings.TrimLeft(constraint, ">=^~<> ")

	// Handle ranges like "18 - 22" or ">=18 <23"
	// Just take the first version number we find
	re := regexp.MustCompile(`(\d+)`)
	if matches := re.FindStringSubmatch(constraint); len(matches) > 1 {
		return matches[1]
	}

	return ""
}
