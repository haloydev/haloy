package helpers

import (
	"fmt"
	"net"
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
	// Default to HTTPS for API calls, HTTP for localhost
	if IsLocalhost(normalizedURL) {
		return "http://" + normalizedURL
	}
	return "https://" + normalizedURL
}

// IsLocalhost checks if the given server URL refers to the local machine.
// It handles various formats: with/without port, IPv4, IPv6.
func IsLocalhost(serverURL string) bool {
	normalized, err := NormalizeServerURL(serverURL)
	if err != nil {
		return false
	}

	host := normalized
	// Handle host:port format (works for both IPv4 and IPv6)
	if h, _, err := net.SplitHostPort(normalized); err == nil {
		host = h
	}

	// Remove brackets from IPv6 (e.g., "[::1]" -> "::1")
	host = strings.Trim(host, "[]")

	return host == "localhost" || host == "127.0.0.1" || host == "::1"
}

// ValidateServerHost checks a normalized server address — the "host" or
// "host:port" that NormalizeServerURL returns.
//
// This is deliberately looser than IsValidDomain. A haloy server is not a
// site being served; it is an address the CLI dials, and the useful ones
// include an IP address (a box with no name yet) and a port (a daemon
// reached through an SSH tunnel on 127.0.0.1). IsValidDomain rejects both:
// it reads "127.0.0.1:9922" as a domain whose TLD is "1:9922".
func ValidateServerHost(hostPort string) error {
	host := hostPort

	if h, port, err := net.SplitHostPort(hostPort); err == nil {
		host = h
		if err := ValidatePort(port); err != nil {
			return fmt.Errorf("invalid port '%s': %w", port, err)
		}
	}

	// IPv6 arrives bracketed from url.Parse.
	host = strings.Trim(host, "[]")

	if host == "" {
		return fmt.Errorf("server address has no host")
	}

	if net.ParseIP(host) != nil {
		return nil
	}

	if host == "localhost" {
		return nil
	}

	return IsValidDomain(host)
}
