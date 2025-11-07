package apiclient

import (
	"bufio"
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/haloydev/haloy/internal/constants"
	"github.com/haloydev/haloy/internal/helpers"
)

// APIClient handles communication with the haloy API
type APIClient struct {
	client   *http.Client
	baseURL  string
	apiToken string
}

func New(url, token string) (*APIClient, error) {
	return NewWithTimeout(url, token, 30*time.Second)
}

func NewWithTimeout(url, token string, timeout time.Duration) (*APIClient, error) {
	normalizedUrl, err := helpers.NormalizeServerURL(url)
	if err != nil {
		return nil, fmt.Errorf("failed to normalize url: %w", err)
	}
	serverUrl := helpers.BuildServerURL(normalizedUrl)

	cli := &APIClient{
		client: &http.Client{
			Timeout: timeout,
		},
		baseURL:  serverUrl,
		apiToken: token,
	}

	return cli, nil
}

func (c *APIClient) setAuthHeader(req *http.Request) {
	if c.apiToken != "" {
		req.Header.Set("Authorization", "Bearer "+c.apiToken)
	}
}

func (c *APIClient) HealthCheck(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, "GET", c.baseURL+"/health", nil)
	if err != nil {
		return fmt.Errorf("failed to create health check request: %w", err)
	}

	// Health endpoint doesn't require auth
	resp, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("server not reachable: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return fmt.Errorf("server returned status %d", resp.StatusCode)
	}

	return nil
}

func (c *APIClient) Get(ctx context.Context, path string, v any) error {
	if err := c.HealthCheck(ctx); err != nil {
		return fmt.Errorf("server not available at %s: %w", c.baseURL, err)
	}

	url := fmt.Sprintf("%s/v1/%s", c.baseURL, path)
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return fmt.Errorf("failed to create GET request: %w", err)
	}
	c.setAuthHeader(req)

	resp, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		if resp.StatusCode == http.StatusUnauthorized {
			return fmt.Errorf("authentication failed - check your %s", constants.EnvVarAPIToken)
		}
		return fmt.Errorf("GET request failed with status %d", resp.StatusCode)
	}

	if err := json.NewDecoder(resp.Body).Decode(v); err != nil {
		return fmt.Errorf("failed to decode response: %w", err)
	}

	return nil
}

func (c *APIClient) Post(ctx context.Context, path string, request, response any) error {
	if err := c.HealthCheck(ctx); err != nil {
		return fmt.Errorf("server not available at %s: %w", c.baseURL, err)
	}

	var jsonData []byte
	var err error

	// Handle nil request for endpoints that don't need request body
	if request != nil {
		jsonData, err = json.Marshal(request)
		if err != nil {
			return fmt.Errorf("failed to marshal request: %w", err)
		}
	}

	url := fmt.Sprintf("%s/v1/%s", c.baseURL, path)
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(jsonData))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	// Only set Content-Type if we have a request body
	if request != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	c.setAuthHeader(req)

	resp, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		bodyBytes, readErr := io.ReadAll(resp.Body)
		if readErr != nil {
			return fmt.Errorf("POST request failed with status %d (unable to read error details: %v)", resp.StatusCode, readErr)
		}

		errorMessage := strings.TrimSpace(string(bodyBytes))
		if errorMessage == "" {
			errorMessage = "no error details provided"
		}
		if resp.StatusCode == http.StatusUnauthorized {
			return fmt.Errorf("authentication failed - check your %s", constants.EnvVarAPIToken)
		}
		return fmt.Errorf("POST request failed with status %d: %s", resp.StatusCode, errorMessage)
	}

	if response != nil {
		if err := json.NewDecoder(resp.Body).Decode(response); err != nil {
			return fmt.Errorf("failed to decode response: %w", err)
		}
	}

	return nil
}

// PostFile uploads a file using multipart form data
func (c *APIClient) PostFile(ctx context.Context, path, fieldName, filePath string) error {
	if err := c.HealthCheck(ctx); err != nil {
		return fmt.Errorf("server not available at %s: %w", c.baseURL, err)
	}

	file, err := os.Open(filePath)
	if err != nil {
		return fmt.Errorf("failed to open file: %w", err)
	}
	defer file.Close()

	// Create multipart form
	var requestBody bytes.Buffer
	writer := multipart.NewWriter(&requestBody)

	part, err := writer.CreateFormFile(fieldName, filepath.Base(filePath))
	if err != nil {
		return fmt.Errorf("failed to create form file: %w", err)
	}

	if _, err := io.Copy(part, file); err != nil {
		return fmt.Errorf("failed to copy file data: %w", err)
	}

	if err := writer.Close(); err != nil {
		return fmt.Errorf("failed to close multipart writer: %w", err)
	}

	url := fmt.Sprintf("%s/v1/%s", c.baseURL, path)
	req, err := http.NewRequestWithContext(ctx, "POST", url, &requestBody)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", writer.FormDataContentType())
	c.setAuthHeader(req)

	resp, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		bodyBytes, readErr := io.ReadAll(resp.Body)
		if readErr != nil {
			return fmt.Errorf("file upload failed with status %d (unable to read error details: %v)", resp.StatusCode, readErr)
		}

		errorMessage := strings.TrimSpace(string(bodyBytes))
		if errorMessage == "" {
			errorMessage = "no error details provided"
		}
		if resp.StatusCode == http.StatusUnauthorized {
			return fmt.Errorf("authentication failed - check your %s", constants.EnvVarAPIToken)
		}
		return fmt.Errorf("file upload failed with status %d: %s", resp.StatusCode, errorMessage)
	}

	return nil
}

// Generic streaming method that handles any SSE endpoint
func (c *APIClient) Stream(ctx context.Context, path string, handler func(data string) bool) error {
	// Create transport that forces HTTP/1.1 to avoid HTTP/2 stream cancellation
	streamingTransport := &http.Transport{
		ForceAttemptHTTP2: false, // Force HTTP/1.1
		TLSNextProto:      make(map[string]func(authority string, c *tls.Conn) http.RoundTripper),
		IdleConnTimeout:   0,     // No idle timeout
		DisableKeepAlives: false, // Keep connections alive
	}
	streamingClient := &http.Client{Timeout: 0, Transport: streamingTransport}

	url := fmt.Sprintf("%s/v1/%s", c.baseURL, path)
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return fmt.Errorf("failed to create SSE request: %w", err)
	}

	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("Cache-Control", "no-cache")
	req.Header.Set("Connection", "keep-alive")
	c.setAuthHeader(req)

	resp, err := streamingClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to connect to stream: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		if resp.StatusCode == http.StatusUnauthorized {
			return fmt.Errorf("authentication failed for stream - check your %s", constants.EnvVarAPIToken)
		}
		return fmt.Errorf("stream returned status %d", resp.StatusCode)
	}

	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		line := scanner.Text()

		// Skip empty lines and SSE comment lines
		if line == "" || strings.HasPrefix(line, ":") {
			continue
		}

		// Parse SSE data lines
		if data, ok := strings.CutPrefix(line, "data: "); ok {

			// Call the handler function to process the data
			shouldStop := handler(data)

			// If handler returns true, stop streaming
			if shouldStop {
				return nil
			}
		}
	}

	if err := scanner.Err(); err != nil {
		return fmt.Errorf("error reading stream: %w", err)
	}

	return nil
}
