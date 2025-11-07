package helpers

import "strings"

// SanitizeString takes a string and sanitizes it for use as a safe identifier.
// Suitable for HAProxy identifiers (backend names, ACL names), Docker container names,
// and filenames (when extensions are added separately).
// Allows alphanumeric characters, hyphens, and underscores. Consecutive disallowed
// characters are replaced by a single underscore.
func SanitizeString(input string) string {
	if input == "" {
		return ""
	}
	var result strings.Builder
	result.Grow(len(input))
	lastCharWasUnderscore := false

	for _, r := range input {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			result.WriteRune(r)
			lastCharWasUnderscore = false
		} else {
			if !lastCharWasUnderscore {
				result.WriteRune('_')
				lastCharWasUnderscore = true
			}
		}
	}
	return result.String()
}

func SafeIDPrefix(id string) string {
	if len(id) > 12 {
		return id[:12]
	}
	return id
}
