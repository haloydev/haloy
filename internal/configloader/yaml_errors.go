package configloader

import (
	"errors"
	"fmt"
	"os"
	"regexp"
	"strings"
)

// EnhanceConfigError takes a koanf parse error and attempts to provide
// a more helpful error message with context and suggestions.
// Currently only enhances YAML errors - JSON and TOML parsers already provide clear messages.
func EnhanceConfigError(configFile string, format string, originalErr error) error {
	// Only enhance YAML errors - JSON and TOML parsers provide clear errors already
	if format != "yaml" {
		return fmt.Errorf("failed to load config file: %w", originalErr)
	}

	// Try to parse the YAML error to extract line information
	info, ok := parseYAMLError(originalErr)
	if !ok {
		return fmt.Errorf("failed to load config file: %w", originalErr)
	}

	content, err := os.ReadFile(configFile)
	if err != nil {
		return fmt.Errorf("failed to load config file: %w", originalErr)
	}

	lines := strings.Split(string(content), "\n")

	hint, hintLine := detectCommonMistakes(lines, info.Line)

	// If we found a hint, use the hint line for context; otherwise use the error line
	contextLine := info.Line
	if hint != "" && hintLine > 0 {
		contextLine = hintLine
	}

	enhanced := formatErrorWithContext(configFile, lines, contextLine, info, hint)
	return errors.New(enhanced)
}

// yamlErrorInfo holds parsed information from a YAML error
type yamlErrorInfo struct {
	Line    int
	Column  int
	Message string
}

// parseYAMLError extracts line/column from yaml.v3 error messages
// yaml.v3 errors look like: "yaml: line 2: mapping values are not allowed in this context"
// or: "yaml: line 2: column 5: ..."
func parseYAMLError(err error) (*yamlErrorInfo, bool) {
	if err == nil {
		return nil, false
	}

	errStr := err.Error()

	// Pattern: "yaml: line N: ..." or "yaml: line N: column M: ..."
	lineColPattern := regexp.MustCompile(`yaml: line (\d+): column (\d+): (.+)`)
	lineOnlyPattern := regexp.MustCompile(`yaml: line (\d+): (.+)`)

	if matches := lineColPattern.FindStringSubmatch(errStr); matches != nil {
		var line, col int
		fmt.Sscanf(matches[1], "%d", &line)
		fmt.Sscanf(matches[2], "%d", &col)
		return &yamlErrorInfo{
			Line:    line,
			Column:  col,
			Message: matches[3],
		}, true
	}

	if matches := lineOnlyPattern.FindStringSubmatch(errStr); matches != nil {
		var line int
		fmt.Sscanf(matches[1], "%d", &line)
		return &yamlErrorInfo{
			Line:    line,
			Column:  0,
			Message: matches[2],
		}, true
	}

	return nil, false
}

// formatErrorWithContext formats the error with surrounding lines from the file
func formatErrorWithContext(configFile string, lines []string, contextLine int, info *yamlErrorInfo, hint string) string {
	var b strings.Builder

	// Header
	b.WriteString(fmt.Sprintf("YAML syntax error in %s\n\n", configFile))

	// Show context: 1 line before, the error line, and 1 line after
	startLine := max(1, contextLine-1)
	endLine := min(len(lines), contextLine+1)

	// Calculate width for line numbers
	lineNumWidth := len(fmt.Sprintf("%d", endLine))

	for i := startLine; i <= endLine; i++ {
		if i-1 < len(lines) {
			lineContent := lines[i-1]
			// Replace tabs with visible indicator for display
			displayContent := strings.ReplaceAll(lineContent, "\t", "→   ")
			b.WriteString(fmt.Sprintf("  %*d | %s\n", lineNumWidth, i, displayContent))

			// If this is the context line and we have column info, show a caret
			if i == info.Line && info.Column > 0 {
				padding := strings.Repeat(" ", lineNumWidth+4+info.Column-1)
				b.WriteString(fmt.Sprintf("%s^ %s\n", padding, info.Message))
			}
		}
	}

	// Add hint if we have one
	if hint != "" {
		b.WriteString(fmt.Sprintf("\n   Hint: %s\n", hint))
	} else if info.Message != "" && info.Column == 0 {
		// No hint, but we have an error message - show it
		b.WriteString(fmt.Sprintf("\n   Error: %s\n", info.Message))
	}

	return b.String()
}

// detectCommonMistakes runs all heuristics and returns the first matching hint
// Returns the hint message and the line number the hint applies to
func detectCommonMistakes(lines []string, errorLine int) (hint string, hintLine int) {
	if line, ok := detectTabIndentation(lines); ok {
		return "YAML requires spaces for indentation, not tabs. Replace the tab with spaces.", line
	}

	if suggestion, line, ok := detectMissingColon(lines, errorLine); ok {
		return suggestion, line
	}

	// Check for inconsistent indentation
	if suggestion, line, ok := detectInconsistentIndent(lines, errorLine); ok {
		return suggestion, line
	}

	// Check for unquoted special characters
	if suggestion, line, ok := detectUnquotedSpecialChars(lines, errorLine); ok {
		return suggestion, line
	}

	// Check for bad list indentation
	if suggestion, line, ok := detectBadListIndent(lines, errorLine); ok {
		return suggestion, line
	}

	return "", 0
}

// detectMissingColon checks for lines that look like "key value" without a colon
func detectMissingColon(lines []string, errorLine int) (suggestion string, line int, ok bool) {
	// Check both the error line and the line before it, since YAML parsers
	// sometimes report the error on the problematic line itself, and sometimes
	// on the following line when it realizes the previous line was malformed.
	checkLines := []int{errorLine, errorLine - 1}

	for _, checkLine := range checkLines {
		if checkLine < 1 || checkLine > len(lines) {
			continue
		}

		lineContent := lines[checkLine-1]
		trimmed := strings.TrimSpace(lineContent)

		// Skip empty lines, comments, and lines that already have colons
		if trimmed == "" || strings.HasPrefix(trimmed, "#") || strings.Contains(trimmed, ":") {
			continue
		}

		// Pattern: word followed by space and more content (likely "key value" without colon)
		// But not list items (starting with -)
		if strings.HasPrefix(trimmed, "-") {
			continue
		}

		// Check if it looks like "key value" pattern
		parts := strings.SplitN(trimmed, " ", 2)
		if len(parts) == 2 && isValidYAMLKey(parts[0]) && parts[1] != "" {
			suggestion := fmt.Sprintf("Line %d appears to be missing a ':' after '%s'. Did you mean '%s: %s'?",
				checkLine, parts[0], parts[0], parts[1])
			return suggestion, checkLine, true
		}
	}

	return "", 0, false
}

// detectTabIndentation checks for lines that use tabs for indentation
func detectTabIndentation(lines []string) (line int, ok bool) {
	for i, lineContent := range lines {
		// Check if the line starts with a tab (after any leading spaces)
		if strings.HasPrefix(lineContent, "\t") {
			return i + 1, true
		}
		// Also check for tabs after leading spaces
		for j, ch := range lineContent {
			if ch == '\t' {
				return i + 1, true
			}
			if ch != ' ' {
				break
			}
			_ = j
		}
	}
	return 0, false
}

// detectInconsistentIndent checks for inconsistent indentation levels
func detectInconsistentIndent(lines []string, errorLine int) (suggestion string, line int, ok bool) {
	if len(lines) < 2 {
		return "", 0, false
	}

	// Detect the base indentation unit (usually 2 or 4 spaces)
	indentUnit := detectIndentUnit(lines)
	if indentUnit == 0 {
		return "", 0, false
	}

	// Check lines around the error for inconsistent indentation
	startCheck := max(0, errorLine-3)
	endCheck := min(len(lines), errorLine+1)

	for i := startCheck; i < endCheck; i++ {
		lineContent := lines[i]
		if strings.TrimSpace(lineContent) == "" || strings.HasPrefix(strings.TrimSpace(lineContent), "#") {
			continue
		}

		indent := countLeadingSpaces(lineContent)
		if indent > 0 && indent%indentUnit != 0 {
			return fmt.Sprintf("Inconsistent indentation on line %d. Expected indentation to be a multiple of %d spaces.", i+1, indentUnit), i + 1, true
		}
	}

	return "", 0, false
}

// detectUnquotedSpecialChars checks for values with special characters that should be quoted
func detectUnquotedSpecialChars(lines []string, errorLine int) (suggestion string, line int, ok bool) {
	checkLines := []int{errorLine - 1, errorLine}

	specialChars := []string{":", "@", "#", "{", "}", "[", "]", "*", "&", "!", "|", ">", "'", "\"", "%"}

	for _, lineNum := range checkLines {
		if lineNum < 1 || lineNum > len(lines) {
			continue
		}

		lineContent := lines[lineNum-1]
		trimmed := strings.TrimSpace(lineContent)

		// Skip comments and empty lines
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}

		// Look for key: value pattern
		if colonIdx := strings.Index(trimmed, ":"); colonIdx > 0 && colonIdx < len(trimmed)-1 {
			value := strings.TrimSpace(trimmed[colonIdx+1:])

			// Skip if already quoted
			if (strings.HasPrefix(value, "\"") && strings.HasSuffix(value, "\"")) ||
				(strings.HasPrefix(value, "'") && strings.HasSuffix(value, "'")) {
				continue
			}

			// Skip if it's a nested structure (just a colon with nothing after)
			if value == "" {
				continue
			}

			// Check for special characters in the value
			for _, special := range specialChars {
				if strings.Contains(value, special) {
					return fmt.Sprintf("Value on line %d contains special character '%s'. Try quoting the value: '%s'",
						lineNum, special, value), lineNum, true
				}
			}
		}
	}

	return "", 0, false
}

// detectBadListIndent checks for list items with incorrect indentation
func detectBadListIndent(lines []string, errorLine int) (suggestion string, line int, ok bool) {
	if errorLine < 1 || errorLine > len(lines) {
		return "", 0, false
	}

	lineContent := lines[errorLine-1]
	trimmed := strings.TrimSpace(lineContent)

	// Check if this is a list item
	if !strings.HasPrefix(trimmed, "- ") && trimmed != "-" {
		return "", 0, false
	}

	currentIndent := countLeadingSpaces(lineContent)

	// Look backwards for the parent key
	for i := errorLine - 2; i >= 0; i-- {
		prevLine := lines[i]
		prevTrimmed := strings.TrimSpace(prevLine)

		// Skip empty lines and comments
		if prevTrimmed == "" || strings.HasPrefix(prevTrimmed, "#") {
			continue
		}

		prevIndent := countLeadingSpaces(prevLine)

		// If we found a line with less indentation that ends with `:`, that's the parent
		if prevIndent < currentIndent && strings.HasSuffix(prevTrimmed, ":") {
			expectedIndent := prevIndent + detectIndentUnit(lines)
			if expectedIndent == 0 {
				expectedIndent = prevIndent + 2 // default to 2 spaces
			}

			if currentIndent != expectedIndent {
				return fmt.Sprintf("List item on line %d has incorrect indentation. Expected %d spaces, found %d.",
					errorLine, expectedIndent, currentIndent), errorLine, true
			}
			break
		}

		// If we hit a line with less or equal indentation that's not the parent, stop
		if prevIndent <= currentIndent && !strings.HasPrefix(prevTrimmed, "-") {
			break
		}
	}

	return "", 0, false
}

// Helper functions

// isValidYAMLKey checks if a string looks like a valid YAML key
func isValidYAMLKey(s string) bool {
	if s == "" {
		return false
	}
	// Simple check: starts with letter or underscore, contains only alphanumeric, underscore, hyphen
	matched, _ := regexp.MatchString(`^[a-zA-Z_][a-zA-Z0-9_-]*$`, s)
	return matched
}

// countLeadingSpaces counts the number of leading spaces in a string
func countLeadingSpaces(s string) int {
	count := 0
	for _, ch := range s {
		if ch == ' ' {
			count++
		} else {
			break
		}
	}
	return count
}

// detectIndentUnit tries to detect the indentation unit (typically 2 or 4 spaces)
func detectIndentUnit(lines []string) int {
	indents := make(map[int]int)

	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			continue
		}
		indent := countLeadingSpaces(line)
		if indent > 0 {
			indents[indent]++
		}
	}

	// Find the GCD of all non-zero indents, or the smallest indent
	minIndent := 0
	for indent := range indents {
		if indent > 0 && (minIndent == 0 || indent < minIndent) {
			minIndent = indent
		}
	}

	// Common cases: 2 or 4 spaces
	if minIndent == 2 || minIndent == 4 {
		return minIndent
	}

	// Default to 2 if we can't determine
	if minIndent > 0 {
		return minIndent
	}

	return 2
}
