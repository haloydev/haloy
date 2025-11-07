package helpers

import (
	"fmt"
	"net/url"
	"strings"
)

// NormalizeServerURL strips protocol and normalizes the server URL for storage
func NormalizeServerURL(rawURL string) (string, error) {
	// If no protocol specified, assume https://
	if !strings.Contains(rawURL, "://") {
		rawURL = "https://" + rawURL
	}

	parsed, err := url.Parse(rawURL)
	if err != nil {
		return "", fmt.Errorf("invalid URL: %w", err)
	}

	return parsed.Host, nil
}

// BuildServerURL constructs the full URL for API calls
func BuildServerURL(normalizedURL string) string {
	// Default to HTTPS for API calls
	if strings.Contains(normalizedURL, "localhost") || strings.Contains(normalizedURL, "127.0.0.1") {
		return "http://" + normalizedURL
	}
	return "https://" + normalizedURL
}
