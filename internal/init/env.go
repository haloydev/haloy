package init

import (
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/joho/godotenv"
)

// EnvFile represents a parsed .env file
type EnvFile struct {
	Path string
	Name string
	Vars []string // Just the variable names, not values
}

// ParseEnvFiles finds and parses .env files in the project directory
// Returns a list of environment variable names found across all .env files
func ParseEnvFiles(projectDir string) ([]EnvFile, error) {
	var envFiles []EnvFile

	// Look for common .env file patterns
	patterns := []string{
		".env",
		".env.local",
		".env.development",
		".env.production",
		".env.staging",
		".env.test",
		".env.example",
		".env.sample",
	}

	for _, pattern := range patterns {
		envPath := filepath.Join(projectDir, pattern)
		if !fileExists(envPath) {
			continue
		}

		vars, err := parseEnvFile(envPath)
		if err != nil {
			// Skip files that can't be parsed
			continue
		}

		if len(vars) > 0 {
			envFiles = append(envFiles, EnvFile{
				Path: envPath,
				Name: pattern,
				Vars: vars,
			})
		}
	}

	return envFiles, nil
}

// parseEnvFile reads a .env file and returns the variable names
func parseEnvFile(path string) ([]string, error) {
	env, err := godotenv.Read(path)
	if err != nil {
		return nil, err
	}

	var vars []string
	for key := range env {
		vars = append(vars, key)
	}
	sort.Strings(vars)
	return vars, nil
}

// GetUniqueEnvVars returns a deduplicated, sorted list of env var names
// from multiple .env files, optionally filtering by patterns
func GetUniqueEnvVars(envFiles []EnvFile, excludePatterns []string) []string {
	seen := make(map[string]bool)
	var vars []string

	for _, ef := range envFiles {
		for _, v := range ef.Vars {
			if seen[v] {
				continue
			}
			if shouldExcludeVar(v, excludePatterns) {
				continue
			}
			seen[v] = true
			vars = append(vars, v)
		}
	}

	sort.Strings(vars)
	return vars
}

// shouldExcludeVar checks if a variable name matches any exclusion pattern
func shouldExcludeVar(varName string, patterns []string) bool {
	upperVar := strings.ToUpper(varName)
	for _, pattern := range patterns {
		upperPattern := strings.ToUpper(pattern)
		if strings.HasPrefix(upperVar, upperPattern) {
			return true
		}
		if strings.HasSuffix(upperVar, upperPattern) {
			return true
		}
		if upperVar == upperPattern {
			return true
		}
	}
	return false
}

// DefaultExcludePatterns returns patterns for env vars that typically shouldn't
// be included in haloy.yaml (build-time only, or framework internals)
func DefaultExcludePatterns() []string {
	return []string{
		// Next.js build-time vars
		"NEXT_PUBLIC_",
		// Vite build-time vars
		"VITE_",
		// Create React App build-time vars
		"REACT_APP_",
		// Common dev-only vars
		"DEBUG",
		"VERBOSE",
		// Editor/IDE vars
		"EDITOR",
		"VISUAL",
	}
}

// GetEnvFileSummary returns a summary string of found .env files
func GetEnvFileSummary(envFiles []EnvFile) string {
	if len(envFiles) == 0 {
		return "No .env files found"
	}

	var names []string
	for _, ef := range envFiles {
		names = append(names, ef.Name)
	}
	return strings.Join(names, ", ")
}

// ReadEnvValue reads the actual value of an environment variable from .env files
// Returns empty string if not found
func ReadEnvValue(projectDir, varName string) string {
	// Check in order of precedence
	files := []string{
		".env.production",
		".env.local",
		".env",
	}

	for _, file := range files {
		envPath := filepath.Join(projectDir, file)
		if !fileExists(envPath) {
			continue
		}

		env, err := godotenv.Read(envPath)
		if err != nil {
			continue
		}

		if val, ok := env[varName]; ok {
			return val
		}
	}

	// Also check current environment
	return os.Getenv(varName)
}
