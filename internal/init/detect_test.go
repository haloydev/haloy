package init

import (
	"path/filepath"
	"runtime"
	"testing"
)

// getTestdataPath returns the absolute path to the testdata directory
func getTestdataPath(t *testing.T) string {
	t.Helper()
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("failed to get current file path")
	}
	return filepath.Join(filepath.Dir(filename), "testdata")
}

func TestDetectFramework(t *testing.T) {
	testdataPath := getTestdataPath(t)

	tests := []struct {
		name     string
		fixture  string
		expected Framework
	}{
		{
			name:     "Next.js with pnpm",
			fixture:  "nextjs-pnpm",
			expected: FrameworkNextJS,
		},
		{
			name:     "Next.js with npm, no standalone",
			fixture:  "nextjs-npm-no-standalone",
			expected: FrameworkNextJS,
		},
		{
			name:     "Next.js with mjs config",
			fixture:  "nextjs-standalone-mjs",
			expected: FrameworkNextJS,
		},
		{
			name:     "TanStack Start with pnpm",
			fixture:  "tanstack-pnpm",
			expected: FrameworkTanStackStart,
		},
		{
			name:     "TanStack Start with bun",
			fixture:  "tanstack-bun",
			expected: FrameworkTanStackStart,
		},
		{
			name:     "Django with requirements.txt",
			fixture:  "django-basic",
			expected: FrameworkDjango,
		},
		{
			name:     "Django with pyproject.toml",
			fixture:  "django-pyproject",
			expected: FrameworkDjango,
		},
		{
			name:     "Unknown framework",
			fixture:  "unknown",
			expected: FrameworkUnknown,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			projectDir := filepath.Join(testdataPath, tt.fixture)
			framework, err := DetectFramework(projectDir)
			if err != nil {
				t.Fatalf("DetectFramework() error = %v", err)
			}
			if framework != tt.expected {
				t.Errorf("DetectFramework() = %v, want %v", framework, tt.expected)
			}
		})
	}
}

func TestDetectFramework_NonexistentDir(t *testing.T) {
	framework, err := DetectFramework("/nonexistent/path")
	if err != nil {
		t.Fatalf("DetectFramework() should not error for nonexistent dir, got %v", err)
	}
	if framework != FrameworkUnknown {
		t.Errorf("DetectFramework() = %v, want %v", framework, FrameworkUnknown)
	}
}

func TestCheckNextJSStandalone(t *testing.T) {
	testdataPath := getTestdataPath(t)

	tests := []struct {
		name     string
		fixture  string
		expected bool
	}{
		{
			name:     "Next.js with standalone in .js config",
			fixture:  "nextjs-pnpm",
			expected: true,
		},
		{
			name:     "Next.js without standalone",
			fixture:  "nextjs-npm-no-standalone",
			expected: false,
		},
		{
			name:     "Next.js with standalone in .mjs config (single quotes)",
			fixture:  "nextjs-standalone-mjs",
			expected: true,
		},
		{
			name:     "Non-Next.js project",
			fixture:  "tanstack-pnpm",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			projectDir := filepath.Join(testdataPath, tt.fixture)
			result := CheckNextJSStandalone(projectDir)
			if result != tt.expected {
				t.Errorf("CheckNextJSStandalone() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestFramework_DisplayName(t *testing.T) {
	tests := []struct {
		framework Framework
		expected  string
	}{
		{FrameworkNextJS, "Next.js"},
		{FrameworkTanStackStart, "TanStack Start"},
		{FrameworkDjango, "Django"},
		{FrameworkUnknown, "Unknown"},
	}

	for _, tt := range tests {
		t.Run(string(tt.framework), func(t *testing.T) {
			if got := tt.framework.DisplayName(); got != tt.expected {
				t.Errorf("DisplayName() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestFramework_TemplateFile(t *testing.T) {
	tests := []struct {
		framework Framework
		expected  string
	}{
		{FrameworkNextJS, "nextjs.Dockerfile"},
		{FrameworkTanStackStart, "tanstack-start.Dockerfile"},
		{FrameworkDjango, "django.Dockerfile"},
		{FrameworkUnknown, ""},
	}

	for _, tt := range tests {
		t.Run(string(tt.framework), func(t *testing.T) {
			if got := tt.framework.TemplateFile(); got != tt.expected {
				t.Errorf("TemplateFile() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestFramework_DefaultPort(t *testing.T) {
	tests := []struct {
		framework Framework
		expected  string
	}{
		{FrameworkNextJS, "3000"},
		{FrameworkTanStackStart, "3000"},
		{FrameworkDjango, "8000"},
		{FrameworkUnknown, "3000"},
	}

	for _, tt := range tests {
		t.Run(string(tt.framework), func(t *testing.T) {
			if got := tt.framework.DefaultPort(); got != tt.expected {
				t.Errorf("DefaultPort() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestHasDependency(t *testing.T) {
	testdataPath := getTestdataPath(t)

	tests := []struct {
		name       string
		fixture    string
		dependency string
		expected   bool
	}{
		{
			name:       "has next dependency",
			fixture:    "nextjs-pnpm",
			dependency: "next",
			expected:   true,
		},
		{
			name:       "has react dependency",
			fixture:    "nextjs-pnpm",
			dependency: "react",
			expected:   true,
		},
		{
			name:       "does not have express dependency",
			fixture:    "nextjs-pnpm",
			dependency: "express",
			expected:   false,
		},
		{
			name:       "has @tanstack/start dependency",
			fixture:    "tanstack-pnpm",
			dependency: "@tanstack/start",
			expected:   true,
		},
		{
			name:       "no package.json",
			fixture:    "django-basic",
			dependency: "next",
			expected:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			projectDir := filepath.Join(testdataPath, tt.fixture)
			result := hasDependency(projectDir, tt.dependency)
			if result != tt.expected {
				t.Errorf("hasDependency() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestHasPythonDependency(t *testing.T) {
	testdataPath := getTestdataPath(t)

	tests := []struct {
		name       string
		fixture    string
		dependency string
		expected   bool
	}{
		{
			name:       "has django in requirements.txt",
			fixture:    "django-basic",
			dependency: "django",
			expected:   true,
		},
		{
			name:       "has django in pyproject.toml",
			fixture:    "django-pyproject",
			dependency: "django",
			expected:   true,
		},
		{
			name:       "does not have flask",
			fixture:    "django-basic",
			dependency: "flask",
			expected:   false,
		},
		{
			name:       "no Python files",
			fixture:    "nextjs-pnpm",
			dependency: "django",
			expected:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			projectDir := filepath.Join(testdataPath, tt.fixture)
			result := hasPythonDependency(projectDir, tt.dependency)
			if result != tt.expected {
				t.Errorf("hasPythonDependency() = %v, want %v", result, tt.expected)
			}
		})
	}
}
