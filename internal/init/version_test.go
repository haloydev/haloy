package init

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDetectPackageManager(t *testing.T) {
	testdataPath := getTestdataPath(t)

	tests := []struct {
		name            string
		fixture         string
		expectedName    PackageManager
		expectedLock    string
		expectedInstall string
	}{
		{
			name:            "pnpm from pnpm-lock.yaml",
			fixture:         "nextjs-pnpm",
			expectedName:    PackageManagerPNPM,
			expectedLock:    "pnpm-lock.yaml",
			expectedInstall: "pnpm install --frozen-lockfile",
		},
		{
			name:            "npm from package-lock.json",
			fixture:         "nextjs-npm-no-standalone",
			expectedName:    PackageManagerNPM,
			expectedLock:    "package-lock.json",
			expectedInstall: "npm ci",
		},
		{
			name:            "yarn from yarn.lock",
			fixture:         "nextjs-standalone-mjs",
			expectedName:    PackageManagerYarn,
			expectedLock:    "yarn.lock",
			expectedInstall: "yarn --frozen-lockfile",
		},
		{
			name:            "bun from bun.lockb",
			fixture:         "tanstack-bun",
			expectedName:    PackageManagerBun,
			expectedLock:    "bun.lockb",
			expectedInstall: "bun install --frozen-lockfile",
		},
		{
			name:            "defaults to npm when no lockfile",
			fixture:         "unknown",
			expectedName:    PackageManagerNPM,
			expectedLock:    "package-lock.json",
			expectedInstall: "npm ci",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			projectDir := filepath.Join(testdataPath, tt.fixture)
			result := DetectPackageManager(projectDir)

			if result.Name != tt.expectedName {
				t.Errorf("Name = %v, want %v", result.Name, tt.expectedName)
			}
			if result.LockFile != tt.expectedLock {
				t.Errorf("LockFile = %v, want %v", result.LockFile, tt.expectedLock)
			}
			if result.InstallCmd != tt.expectedInstall {
				t.Errorf("InstallCmd = %v, want %v", result.InstallCmd, tt.expectedInstall)
			}
		})
	}
}

func TestDetectPackageManager_BunLockVariants(t *testing.T) {
	// Test that both bun.lockb and bun.lock are detected
	tmpDir := t.TempDir()

	// Test bun.lock (newer format)
	bunLockPath := filepath.Join(tmpDir, "bun.lock")
	if err := os.WriteFile(bunLockPath, []byte{}, 0644); err != nil {
		t.Fatal(err)
	}

	result := DetectPackageManager(tmpDir)
	if result.Name != PackageManagerBun {
		t.Errorf("Expected bun for bun.lock, got %v", result.Name)
	}
	if result.LockFile != "bun.lock" {
		t.Errorf("Expected lockfile bun.lock, got %v", result.LockFile)
	}
}

func TestDetectNodeVersion(t *testing.T) {
	testdataPath := getTestdataPath(t)

	tests := []struct {
		name     string
		fixture  string
		expected string
	}{
		{
			name:     "from .nvmrc",
			fixture:  "nextjs-pnpm",
			expected: "20",
		},
		{
			name:     "from .node-version",
			fixture:  "tanstack-pnpm",
			expected: "22",
		},
		{
			name:     "defaults to 22 when no version file",
			fixture:  "tanstack-bun",
			expected: "22",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			projectDir := filepath.Join(testdataPath, tt.fixture)
			result := DetectNodeVersion(projectDir)
			if result != tt.expected {
				t.Errorf("DetectNodeVersion() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestDetectNodeVersion_FromPackageJson(t *testing.T) {
	tmpDir := t.TempDir()

	// Create package.json with engines.node
	packageJSON := `{
		"name": "test",
		"engines": {
			"node": ">=18"
		}
	}`
	if err := os.WriteFile(filepath.Join(tmpDir, "package.json"), []byte(packageJSON), 0644); err != nil {
		t.Fatal(err)
	}

	result := DetectNodeVersion(tmpDir)
	if result != "18" {
		t.Errorf("DetectNodeVersion() = %v, want 18", result)
	}
}

func TestDetectNodeVersion_NvmrcWithV(t *testing.T) {
	tmpDir := t.TempDir()

	// Create .nvmrc with v prefix
	if err := os.WriteFile(filepath.Join(tmpDir, ".nvmrc"), []byte("v18.17.0"), 0644); err != nil {
		t.Fatal(err)
	}

	result := DetectNodeVersion(tmpDir)
	if result != "18" {
		t.Errorf("DetectNodeVersion() = %v, want 18", result)
	}
}

func TestDetectPythonVersion(t *testing.T) {
	testdataPath := getTestdataPath(t)

	tests := []struct {
		name     string
		fixture  string
		expected string
	}{
		{
			name:     "from .python-version",
			fixture:  "django-basic",
			expected: "3.12",
		},
		{
			name:     "from pyproject.toml requires-python",
			fixture:  "django-pyproject",
			expected: "3.11",
		},
		{
			name:     "defaults to 3.12 when no version file",
			fixture:  "nextjs-pnpm",
			expected: "3.12",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			projectDir := filepath.Join(testdataPath, tt.fixture)
			result := DetectPythonVersion(projectDir)
			if result != tt.expected {
				t.Errorf("DetectPythonVersion() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestDetectPythonVersion_RuntimeTxt(t *testing.T) {
	tmpDir := t.TempDir()

	// Create runtime.txt (Heroku-style)
	if err := os.WriteFile(filepath.Join(tmpDir, "runtime.txt"), []byte("python-3.10.5"), 0644); err != nil {
		t.Fatal(err)
	}

	result := DetectPythonVersion(tmpDir)
	if result != "3.10" {
		t.Errorf("DetectPythonVersion() = %v, want 3.10", result)
	}
}

func TestDetectDjangoProject(t *testing.T) {
	testdataPath := getTestdataPath(t)

	tests := []struct {
		name     string
		fixture  string
		expected string
	}{
		{
			name:     "finds wsgi.py in myproject",
			fixture:  "django-basic",
			expected: "myproject",
		},
		{
			name:     "finds wsgi.py in webapp",
			fixture:  "django-pyproject",
			expected: "webapp",
		},
		{
			name:     "defaults to app when no wsgi.py",
			fixture:  "nextjs-pnpm",
			expected: "app",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			projectDir := filepath.Join(testdataPath, tt.fixture)
			result := DetectDjangoProject(projectDir)
			if result != tt.expected {
				t.Errorf("DetectDjangoProject() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestExtractMajorVersion(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"20.10.0", "20"},
		{"18", "18"},
		{"22.0", "22"},
		{"", ""},
		{"lts/hydrogen", ""},
		{"18.17.0", "18"}, // v prefix should be stripped before calling this function
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := extractMajorVersion(tt.input)
			if result != tt.expected {
				t.Errorf("extractMajorVersion(%q) = %v, want %v", tt.input, result, tt.expected)
			}
		})
	}
}

func TestExtractMajorFromConstraint(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{">=18", "18"},
		{"^20", "20"},
		{"~22", "22"},
		{">=18 <23", "18"},
		{"18 - 22", "18"},
		{">= 20.0.0", "20"},
		{"", ""},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := extractMajorFromConstraint(tt.input)
			if result != tt.expected {
				t.Errorf("extractMajorFromConstraint(%q) = %v, want %v", tt.input, result, tt.expected)
			}
		})
	}
}
