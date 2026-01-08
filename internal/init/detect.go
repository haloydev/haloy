package init

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

// Framework represents a detected framework type
type Framework string

const (
	FrameworkNextJS        Framework = "nextjs"
	FrameworkTanStackStart Framework = "tanstack-start"
	FrameworkDjango        Framework = "django"
	FrameworkUnknown       Framework = "unknown"
)

// DisplayName returns a human-readable name for the framework
func (f Framework) DisplayName() string {
	switch f {
	case FrameworkNextJS:
		return "Next.js"
	case FrameworkTanStackStart:
		return "TanStack Start"
	case FrameworkDjango:
		return "Django"
	default:
		return "Unknown"
	}
}

// TemplateFile returns the Dockerfile template filename for the framework
func (f Framework) TemplateFile() string {
	switch f {
	case FrameworkNextJS:
		return "nextjs.Dockerfile"
	case FrameworkTanStackStart:
		return "tanstack-start.Dockerfile"
	case FrameworkDjango:
		return "django.Dockerfile"
	default:
		return ""
	}
}

// DefaultPort returns the default port for the framework
func (f Framework) DefaultPort() string {
	switch f {
	case FrameworkNextJS:
		return "3000"
	case FrameworkTanStackStart:
		return "3000"
	case FrameworkDjango:
		return "8000"
	default:
		return "3000"
	}
}

// DetectFramework detects the framework used in the given project directory
func DetectFramework(projectDir string) (Framework, error) {
	// Check for Next.js
	if isNextJS(projectDir) {
		return FrameworkNextJS, nil
	}

	// Check for TanStack Start
	if isTanStackStart(projectDir) {
		return FrameworkTanStackStart, nil
	}

	// Check for Django
	if isDjango(projectDir) {
		return FrameworkDjango, nil
	}

	return FrameworkUnknown, nil
}

// isNextJS checks if the project is a Next.js project
func isNextJS(projectDir string) bool {
	// Check for next.config.* files
	configFiles := []string{
		"next.config.js",
		"next.config.mjs",
		"next.config.ts",
	}

	for _, configFile := range configFiles {
		if fileExists(filepath.Join(projectDir, configFile)) {
			return true
		}
	}

	// Check package.json for "next" dependency
	return hasDependency(projectDir, "next")
}

// isTanStackStart checks if the project is a TanStack Start project
func isTanStackStart(projectDir string) bool {
	return hasDependency(projectDir, "@tanstack/start")
}

// isDjango checks if the project is a Django project
func isDjango(projectDir string) bool {
	// Check for manage.py
	if !fileExists(filepath.Join(projectDir, "manage.py")) {
		return false
	}

	// Check for django in requirements.txt
	if hasPythonDependency(projectDir, "django") {
		return true
	}

	return false
}

// hasDependency checks if a package.json has a specific dependency
func hasDependency(projectDir string, pkg string) bool {
	packageJSONPath := filepath.Join(projectDir, "package.json")
	data, err := os.ReadFile(packageJSONPath)
	if err != nil {
		return false
	}

	var packageJSON struct {
		Dependencies    map[string]string `json:"dependencies"`
		DevDependencies map[string]string `json:"devDependencies"`
	}

	if err := json.Unmarshal(data, &packageJSON); err != nil {
		return false
	}

	if _, ok := packageJSON.Dependencies[pkg]; ok {
		return true
	}
	if _, ok := packageJSON.DevDependencies[pkg]; ok {
		return true
	}

	return false
}

// hasPythonDependency checks if the project has a Python dependency
func hasPythonDependency(projectDir string, pkg string) bool {
	// Check requirements.txt
	reqPath := filepath.Join(projectDir, "requirements.txt")
	if data, err := os.ReadFile(reqPath); err == nil {
		content := strings.ToLower(string(data))
		if strings.Contains(content, strings.ToLower(pkg)) {
			return true
		}
	}

	// Check pyproject.toml
	pyprojectPath := filepath.Join(projectDir, "pyproject.toml")
	if data, err := os.ReadFile(pyprojectPath); err == nil {
		content := strings.ToLower(string(data))
		if strings.Contains(content, strings.ToLower(pkg)) {
			return true
		}
	}

	return false
}

// fileExists checks if a file exists
func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// CheckNextJSStandalone checks if Next.js is configured for standalone output
func CheckNextJSStandalone(projectDir string) bool {
	configFiles := []string{
		"next.config.js",
		"next.config.mjs",
		"next.config.ts",
	}

	for _, configFile := range configFiles {
		configPath := filepath.Join(projectDir, configFile)
		data, err := os.ReadFile(configPath)
		if err != nil {
			continue
		}

		content := string(data)
		// Simple check for output: "standalone" or output: 'standalone'
		if strings.Contains(content, `output: "standalone"`) ||
			strings.Contains(content, `output: 'standalone'`) ||
			strings.Contains(content, `output:"standalone"`) ||
			strings.Contains(content, `output:'standalone'`) {
			return true
		}
	}

	return false
}
