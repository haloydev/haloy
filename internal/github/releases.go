package github

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/haloydev/haloy/internal/ui"
)

type Release struct {
	TagName string `json:"tag_name"`
}

// FetchLatestVersion fetches the latest release version from GitHub.
// It first tries to get the latest stable release, and if none exists,
// falls back to the most recent release (including prereleases/betas).
func FetchLatestVersion(ctx context.Context) (string, error) {
	client := &http.Client{Timeout: 30 * time.Second}

	// First, try to get the latest stable release
	version, err := fetchReleaseVersion(ctx, client, "https://api.github.com/repos/haloydev/haloy/releases/latest")
	if err == nil && version != "" {
		return version, nil
	}

	// If no stable release found (404), try to get the most recent release including prereleases
	ui.Info("No stable release found, checking for prereleases...")
	version, err = fetchLatestPrerelease(ctx, client)
	if err != nil {
		return "", err
	}

	if version == "" {
		return "", fmt.Errorf("no releases found on GitHub")
	}

	return version, nil
}

// fetchReleaseVersion fetches version from a specific GitHub releases endpoint
func fetchReleaseVersion(ctx context.Context, client *http.Client, url string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to fetch release: %w", err)
	}
	defer resp.Body.Close()

	// Return empty string for 404 (no stable release exists)
	if resp.StatusCode == http.StatusNotFound {
		return "", nil
	}

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("GitHub API returned status %d: %s", resp.StatusCode, string(body))
	}

	var release Release
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return "", fmt.Errorf("failed to decode GitHub response: %w", err)
	}

	return release.TagName, nil
}

// fetchLatestPrerelease fetches the most recent release including prereleases
func fetchLatestPrerelease(ctx context.Context, client *http.Client) (string, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", "https://api.github.com/repos/haloydev/haloy/releases", nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to fetch releases: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("GitHub API returned status %d: %s", resp.StatusCode, string(body))
	}

	var releases []Release
	if err := json.NewDecoder(resp.Body).Decode(&releases); err != nil {
		return "", fmt.Errorf("failed to decode GitHub response: %w", err)
	}

	if len(releases) == 0 {
		return "", nil
	}

	// Return the first (most recent) release
	return releases[0].TagName, nil
}
