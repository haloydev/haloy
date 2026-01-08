package configloader

import (
	"errors"
	"testing"
)

func TestParseYAMLError(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		wantLine int
		wantCol  int
		wantMsg  string
		wantOK   bool
	}{
		{
			name:     "line only error",
			err:      errors.New("yaml: line 2: mapping values are not allowed in this context"),
			wantLine: 2,
			wantCol:  0,
			wantMsg:  "mapping values are not allowed in this context",
			wantOK:   true,
		},
		{
			name:     "line and column error",
			err:      errors.New("yaml: line 5: column 10: did not find expected key"),
			wantLine: 5,
			wantCol:  10,
			wantMsg:  "did not find expected key",
			wantOK:   true,
		},
		{
			name:   "non-yaml error",
			err:    errors.New("some other error"),
			wantOK: false,
		},
		{
			name:   "nil error",
			err:    nil,
			wantOK: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			info, ok := parseYAMLError(tt.err)
			if ok != tt.wantOK {
				t.Errorf("parseYAMLError() ok = %v, want %v", ok, tt.wantOK)
				return
			}
			if !ok {
				return
			}
			if info.Line != tt.wantLine {
				t.Errorf("parseYAMLError() line = %v, want %v", info.Line, tt.wantLine)
			}
			if info.Column != tt.wantCol {
				t.Errorf("parseYAMLError() column = %v, want %v", info.Column, tt.wantCol)
			}
			if info.Message != tt.wantMsg {
				t.Errorf("parseYAMLError() message = %v, want %v", info.Message, tt.wantMsg)
			}
		})
	}
}

func TestDetectMissingColon(t *testing.T) {
	tests := []struct {
		name      string
		lines     []string
		errorLine int
		wantOK    bool
		wantLine  int
	}{
		{
			name: "missing colon on line 1",
			lines: []string{
				"server artemis.haloy.dev",
				"api_token:",
			},
			errorLine: 2,
			wantOK:    true,
			wantLine:  1,
		},
		{
			name: "valid yaml",
			lines: []string{
				"server: artemis.haloy.dev",
				"api_token:",
			},
			errorLine: 2,
			wantOK:    false,
		},
		{
			name: "comment line",
			lines: []string{
				"# this is a comment",
				"api_token:",
			},
			errorLine: 2,
			wantOK:    false,
		},
		{
			name: "list item",
			lines: []string{
				"- item",
				"api_token:",
			},
			errorLine: 2,
			wantOK:    false,
		},
		{
			name: "missing colon with simple value",
			lines: []string{
				"name myapp",
				"port: 8080",
			},
			errorLine: 2,
			wantOK:    true,
			wantLine:  1,
		},
		{
			name: "line with colon in value is not missing colon",
			lines: []string{
				"database: postgres://localhost:5432/db",
				"name: myapp",
			},
			errorLine: 2,
			wantOK:    false,
		},
		{
			name: "missing colon detected on same line as error",
			lines: []string{
				"name: course-platform",
				"server artemis.haloy.dev",
				"api_token:",
			},
			errorLine: 2,
			wantOK:    true,
			wantLine:  2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			suggestion, line, ok := detectMissingColon(tt.lines, tt.errorLine)
			if ok != tt.wantOK {
				t.Errorf("detectMissingColon() ok = %v, want %v", ok, tt.wantOK)
				return
			}
			if ok {
				if line != tt.wantLine {
					t.Errorf("detectMissingColon() line = %v, want %v", line, tt.wantLine)
				}
				if suggestion == "" {
					t.Error("detectMissingColon() suggestion should not be empty when ok=true")
				}
			}
		})
	}
}

func TestDetectTabIndentation(t *testing.T) {
	tests := []struct {
		name     string
		lines    []string
		wantOK   bool
		wantLine int
	}{
		{
			name: "tab at start of line",
			lines: []string{
				"server: localhost",
				"\tport: 8080",
			},
			wantOK:   true,
			wantLine: 2,
		},
		{
			name: "spaces only",
			lines: []string{
				"server: localhost",
				"  port: 8080",
			},
			wantOK: false,
		},
		{
			name: "tab on first line",
			lines: []string{
				"\tserver: localhost",
				"port: 8080",
			},
			wantOK:   true,
			wantLine: 1,
		},
		{
			name:   "empty lines",
			lines:  []string{},
			wantOK: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			line, ok := detectTabIndentation(tt.lines)
			if ok != tt.wantOK {
				t.Errorf("detectTabIndentation() ok = %v, want %v", ok, tt.wantOK)
				return
			}
			if ok && line != tt.wantLine {
				t.Errorf("detectTabIndentation() line = %v, want %v", line, tt.wantLine)
			}
		})
	}
}

func TestDetectInconsistentIndent(t *testing.T) {
	tests := []struct {
		name      string
		lines     []string
		errorLine int
		wantOK    bool
		wantLine  int
	}{
		{
			name: "inconsistent indent (3 spaces instead of 2)",
			lines: []string{
				"server:",
				"  host: localhost",
				"   port: 8080",
			},
			errorLine: 3,
			wantOK:    true,
			wantLine:  3,
		},
		{
			name: "consistent 2-space indent",
			lines: []string{
				"server:",
				"  host: localhost",
				"  port: 8080",
			},
			errorLine: 3,
			wantOK:    false,
		},
		{
			name: "consistent 4-space indent",
			lines: []string{
				"server:",
				"    host: localhost",
				"    port: 8080",
			},
			errorLine: 3,
			wantOK:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			suggestion, line, ok := detectInconsistentIndent(tt.lines, tt.errorLine)
			if ok != tt.wantOK {
				t.Errorf("detectInconsistentIndent() ok = %v, want %v (suggestion: %s)", ok, tt.wantOK, suggestion)
				return
			}
			if ok && line != tt.wantLine {
				t.Errorf("detectInconsistentIndent() line = %v, want %v", line, tt.wantLine)
			}
		})
	}
}

func TestDetectUnquotedSpecialChars(t *testing.T) {
	tests := []struct {
		name      string
		lines     []string
		errorLine int
		wantOK    bool
		wantLine  int
	}{
		{
			name: "unquoted colon in value",
			lines: []string{
				"url: http://localhost:8080",
			},
			errorLine: 1,
			wantOK:    true,
			wantLine:  1,
		},
		{
			name: "unquoted @ in value",
			lines: []string{
				"email: user@example.com",
			},
			errorLine: 1,
			wantOK:    true,
			wantLine:  1,
		},
		{
			name: "quoted value with special chars",
			lines: []string{
				`url: "http://localhost:8080"`,
			},
			errorLine: 1,
			wantOK:    false,
		},
		{
			name: "single quoted value",
			lines: []string{
				`email: 'user@example.com'`,
			},
			errorLine: 1,
			wantOK:    false,
		},
		{
			name: "no special chars",
			lines: []string{
				"name: myapp",
			},
			errorLine: 1,
			wantOK:    false,
		},
		{
			name: "nested structure (colon with no value)",
			lines: []string{
				"server:",
				"  host: localhost",
			},
			errorLine: 1,
			wantOK:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			suggestion, line, ok := detectUnquotedSpecialChars(tt.lines, tt.errorLine)
			if ok != tt.wantOK {
				t.Errorf("detectUnquotedSpecialChars() ok = %v, want %v (suggestion: %s)", ok, tt.wantOK, suggestion)
				return
			}
			if ok && line != tt.wantLine {
				t.Errorf("detectUnquotedSpecialChars() line = %v, want %v", line, tt.wantLine)
			}
		})
	}
}

func TestDetectBadListIndent(t *testing.T) {
	tests := []struct {
		name      string
		lines     []string
		errorLine int
		wantOK    bool
		wantLine  int
	}{
		{
			name: "list item with wrong indent (4 instead of 2)",
			lines: []string{
				"config:",
				"  name: test",
				"items:",
				"    - item1",
			},
			errorLine: 4,
			wantOK:    true,
			wantLine:  4,
		},
		{
			name: "list item with correct indent",
			lines: []string{
				"items:",
				"  - item1",
				"  - item2",
			},
			errorLine: 2,
			wantOK:    false,
		},
		{
			name: "not a list item",
			lines: []string{
				"server: localhost",
			},
			errorLine: 1,
			wantOK:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			suggestion, line, ok := detectBadListIndent(tt.lines, tt.errorLine)
			if ok != tt.wantOK {
				t.Errorf("detectBadListIndent() ok = %v, want %v (suggestion: %s)", ok, tt.wantOK, suggestion)
				return
			}
			if ok && line != tt.wantLine {
				t.Errorf("detectBadListIndent() line = %v, want %v", line, tt.wantLine)
			}
		})
	}
}

func TestCountLeadingSpaces(t *testing.T) {
	tests := []struct {
		input string
		want  int
	}{
		{"no spaces", 0},
		{"  two spaces", 2},
		{"    four spaces", 4},
		{"", 0},
		{"   ", 3},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := countLeadingSpaces(tt.input)
			if got != tt.want {
				t.Errorf("countLeadingSpaces(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestDetectIndentUnit(t *testing.T) {
	tests := []struct {
		name  string
		lines []string
		want  int
	}{
		{
			name: "2-space indent",
			lines: []string{
				"root:",
				"  child:",
				"    grandchild: value",
			},
			want: 2,
		},
		{
			name: "4-space indent",
			lines: []string{
				"root:",
				"    child:",
				"        grandchild: value",
			},
			want: 4,
		},
		{
			name: "no indentation",
			lines: []string{
				"key1: value1",
				"key2: value2",
			},
			want: 2, // defaults to 2
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := detectIndentUnit(tt.lines)
			if got != tt.want {
				t.Errorf("detectIndentUnit() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestIsValidYAMLKey(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		{"server", true},
		{"api_token", true},
		{"my-key", true},
		{"Key123", true},
		{"_private", true},
		{"123key", false},
		{"-invalid", false},
		{"", false},
		{"has space", false},
		{"has:colon", false},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := isValidYAMLKey(tt.input)
			if got != tt.want {
				t.Errorf("isValidYAMLKey(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}
