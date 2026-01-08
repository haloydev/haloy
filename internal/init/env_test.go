package init

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseEnvFiles(t *testing.T) {
	testdataPath := getTestdataPath(t)
	envFilesDir := filepath.Join(testdataPath, "envfiles")

	envFiles, err := ParseEnvFiles(envFilesDir)
	if err != nil {
		t.Fatalf("ParseEnvFiles() error = %v", err)
	}

	if len(envFiles) != 3 {
		t.Errorf("len(envFiles) = %v, want 3", len(envFiles))
	}

	// Check that we found the expected files
	names := make(map[string]bool)
	for _, ef := range envFiles {
		names[ef.Name] = true
	}

	expectedNames := []string{".env", ".env.production", ".env.local"}
	for _, name := range expectedNames {
		if !names[name] {
			t.Errorf("Expected to find %s", name)
		}
	}
}

func TestParseEnvFiles_EmptyDir(t *testing.T) {
	tmpDir := t.TempDir()

	envFiles, err := ParseEnvFiles(tmpDir)
	if err != nil {
		t.Fatalf("ParseEnvFiles() error = %v", err)
	}

	if len(envFiles) != 0 {
		t.Errorf("len(envFiles) = %v, want 0", len(envFiles))
	}
}

func TestParseEnvFiles_NonexistentDir(t *testing.T) {
	envFiles, err := ParseEnvFiles("/nonexistent/path")
	if err != nil {
		t.Fatalf("ParseEnvFiles() should not error for nonexistent path, got %v", err)
	}

	if len(envFiles) != 0 {
		t.Errorf("len(envFiles) = %v, want 0", len(envFiles))
	}
}

func TestGetUniqueEnvVars(t *testing.T) {
	testdataPath := getTestdataPath(t)
	envFilesDir := filepath.Join(testdataPath, "envfiles")

	envFiles, err := ParseEnvFiles(envFilesDir)
	if err != nil {
		t.Fatalf("ParseEnvFiles() error = %v", err)
	}

	// Get all vars without exclusions
	allVars := GetUniqueEnvVars(envFiles, nil)

	// Should have: DATABASE_URL, API_KEY, DEBUG, REDIS_URL, NEXT_PUBLIC_API_URL, SECRET_KEY, VITE_APP_TITLE
	if len(allVars) != 7 {
		t.Errorf("len(allVars) = %v, want 7", len(allVars))
	}

	// Check that vars are sorted
	for i := 1; i < len(allVars); i++ {
		if allVars[i] < allVars[i-1] {
			t.Error("Vars should be sorted alphabetically")
		}
	}
}

func TestGetUniqueEnvVars_WithExclusions(t *testing.T) {
	testdataPath := getTestdataPath(t)
	envFilesDir := filepath.Join(testdataPath, "envfiles")

	envFiles, err := ParseEnvFiles(envFilesDir)
	if err != nil {
		t.Fatalf("ParseEnvFiles() error = %v", err)
	}

	// Exclude NEXT_PUBLIC_ and VITE_ and DEBUG
	excludePatterns := []string{"NEXT_PUBLIC_", "VITE_", "DEBUG"}
	filteredVars := GetUniqueEnvVars(envFiles, excludePatterns)

	// Should NOT contain excluded vars
	for _, v := range filteredVars {
		if v == "DEBUG" || v == "NEXT_PUBLIC_API_URL" || v == "VITE_APP_TITLE" {
			t.Errorf("Should not contain excluded var: %s", v)
		}
	}

	// Should still contain non-excluded vars
	containsDBURL := false
	containsAPIKey := false
	for _, v := range filteredVars {
		if v == "DATABASE_URL" {
			containsDBURL = true
		}
		if v == "API_KEY" {
			containsAPIKey = true
		}
	}
	if !containsDBURL {
		t.Error("Should contain DATABASE_URL")
	}
	if !containsAPIKey {
		t.Error("Should contain API_KEY")
	}
}

func TestGetUniqueEnvVars_DefaultExclusions(t *testing.T) {
	testdataPath := getTestdataPath(t)
	envFilesDir := filepath.Join(testdataPath, "envfiles")

	envFiles, err := ParseEnvFiles(envFilesDir)
	if err != nil {
		t.Fatalf("ParseEnvFiles() error = %v", err)
	}

	filteredVars := GetUniqueEnvVars(envFiles, DefaultExcludePatterns())

	// NEXT_PUBLIC_, VITE_, and DEBUG should be excluded
	for _, v := range filteredVars {
		if v == "DEBUG" || v == "NEXT_PUBLIC_API_URL" || v == "VITE_APP_TITLE" {
			t.Errorf("Default exclusions should filter out: %s", v)
		}
	}
}

func TestShouldExcludeVar(t *testing.T) {
	tests := []struct {
		varName  string
		patterns []string
		expected bool
	}{
		{"NEXT_PUBLIC_API_URL", []string{"NEXT_PUBLIC_"}, true},
		{"DATABASE_URL", []string{"NEXT_PUBLIC_"}, false},
		{"VITE_APP_TITLE", []string{"VITE_"}, true},
		{"DEBUG", []string{"DEBUG"}, true},
		{"DEBUG_MODE", []string{"DEBUG"}, true},      // prefix match
		{"MY_DEBUG", []string{"DEBUG"}, true},        // suffix match
		{"DEBUGGING", []string{"DEBUG"}, true},       // prefix match
		{"database_url", []string{"DATABASE"}, true}, // case insensitive
		{"API_KEY", []string{}, false},
		{"API_KEY", nil, false},
	}

	for _, tt := range tests {
		t.Run(tt.varName, func(t *testing.T) {
			result := shouldExcludeVar(tt.varName, tt.patterns)
			if result != tt.expected {
				t.Errorf("shouldExcludeVar(%q, %v) = %v, want %v", tt.varName, tt.patterns, result, tt.expected)
			}
		})
	}
}

func TestDefaultExcludePatterns(t *testing.T) {
	patterns := DefaultExcludePatterns()

	if len(patterns) == 0 {
		t.Error("DefaultExcludePatterns() should return non-empty slice")
	}

	// Check that common build-time vars are included
	expectedPatterns := []string{"NEXT_PUBLIC_", "VITE_", "REACT_APP_", "DEBUG"}
	for _, expected := range expectedPatterns {
		found := false
		for _, p := range patterns {
			if p == expected {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("DefaultExcludePatterns() should include %q", expected)
		}
	}
}

func TestGetEnvFileSummary(t *testing.T) {
	tests := []struct {
		name     string
		envFiles []EnvFile
		expected string
	}{
		{
			name:     "empty",
			envFiles: nil,
			expected: "No .env files found",
		},
		{
			name: "single file",
			envFiles: []EnvFile{
				{Name: ".env"},
			},
			expected: ".env",
		},
		{
			name: "multiple files",
			envFiles: []EnvFile{
				{Name: ".env"},
				{Name: ".env.production"},
			},
			expected: ".env, .env.production",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := GetEnvFileSummary(tt.envFiles)
			if result != tt.expected {
				t.Errorf("GetEnvFileSummary() = %q, want %q", result, tt.expected)
			}
		})
	}
}

func TestReadEnvValue(t *testing.T) {
	tmpDir := t.TempDir()

	// Create .env file
	envContent := "DATABASE_URL=dev-db\nAPI_KEY=devkey"
	if err := os.WriteFile(filepath.Join(tmpDir, ".env"), []byte(envContent), 0644); err != nil {
		t.Fatal(err)
	}

	// Create .env.production file with different value
	prodContent := "DATABASE_URL=prod-db\nPROD_ONLY=prodvalue"
	if err := os.WriteFile(filepath.Join(tmpDir, ".env.production"), []byte(prodContent), 0644); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		varName  string
		expected string
	}{
		// .env.production takes precedence
		{"DATABASE_URL", "prod-db"},
		// Only in .env
		{"API_KEY", "devkey"},
		// Only in .env.production
		{"PROD_ONLY", "prodvalue"},
		// Not found
		{"NONEXISTENT", ""},
	}

	for _, tt := range tests {
		t.Run(tt.varName, func(t *testing.T) {
			result := ReadEnvValue(tmpDir, tt.varName)
			if result != tt.expected {
				t.Errorf("ReadEnvValue(%q) = %q, want %q", tt.varName, result, tt.expected)
			}
		})
	}
}
