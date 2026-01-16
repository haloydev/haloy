package healthcheck

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"time"
)

// HTTPChecker performs HTTP health checks on targets.
type HTTPChecker struct {
	client *http.Client
}

// NewHTTPChecker creates a new HTTP health checker with the given timeout.
func NewHTTPChecker(timeout time.Duration) *HTTPChecker {
	return &HTTPChecker{
		client: &http.Client{
			Timeout: timeout,
			Transport: &http.Transport{
				DialContext: (&net.Dialer{
					Timeout: timeout,
				}).DialContext,
				DisableKeepAlives:     true, // Don't reuse connections for health checks
				MaxIdleConns:          0,
				IdleConnTimeout:       0,
				TLSHandshakeTimeout:   timeout,
				ResponseHeaderTimeout: timeout,
			},
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				// Don't follow redirects for health checks
				return http.ErrUseLastResponse
			},
		},
	}
}

// Check performs a health check on the given target.
// A target is considered healthy if the HTTP request succeeds with a 2xx or 3xx status code.
func (c *HTTPChecker) Check(ctx context.Context, target Target) Result {
	start := time.Now()

	url := fmt.Sprintf("http://%s:%s%s", target.IP, target.Port, target.HealthCheckPath)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return Result{
			Target:  target,
			Healthy: false,
			Err:     fmt.Errorf("failed to create request: %w", err),
			Latency: time.Since(start),
		}
	}

	resp, err := c.client.Do(req)
	latency := time.Since(start)

	if err != nil {
		return Result{
			Target:  target,
			Healthy: false,
			Err:     fmt.Errorf("request failed: %w", err),
			Latency: latency,
		}
	}
	defer resp.Body.Close()

	// Consider 2xx and 3xx status codes as healthy
	healthy := resp.StatusCode >= 200 && resp.StatusCode < 400
	var checkErr error
	if !healthy {
		checkErr = fmt.Errorf("unhealthy status code: %d", resp.StatusCode)
	}

	return Result{
		Target:  target,
		Healthy: healthy,
		Err:     checkErr,
		Latency: latency,
	}
}

// RetryConfig holds configuration for retry behavior.
type RetryConfig struct {
	MaxRetries     int           // Maximum number of retry attempts
	InitialBackoff time.Duration // Initial backoff duration
	MaxBackoff     time.Duration // Maximum backoff duration
}

// DefaultRetryConfig returns the default retry configuration for initial deployment.
func DefaultRetryConfig() RetryConfig {
	return RetryConfig{
		MaxRetries:     5,
		InitialBackoff: 500 * time.Millisecond,
		MaxBackoff:     8 * time.Second,
	}
}

// CheckWithRetry performs a health check with exponential backoff retries.
// This is used during initial deployment when containers may take time to start.
// The onRetry callback is called before each retry attempt (can be nil).
func (c *HTTPChecker) CheckWithRetry(ctx context.Context, target Target, config RetryConfig, onRetry func(attempt int, backoff time.Duration)) Result {
	var lastResult Result
	backoff := config.InitialBackoff

	for attempt := 0; attempt <= config.MaxRetries; attempt++ {
		if attempt > 0 {
			if onRetry != nil {
				onRetry(attempt, backoff)
			}

			select {
			case <-ctx.Done():
				return Result{
					Target:  target,
					Healthy: false,
					Err:     ctx.Err(),
					Latency: lastResult.Latency,
				}
			case <-time.After(backoff):
			}

			// Exponential backoff with cap
			backoff *= 2
			if backoff > config.MaxBackoff {
				backoff = config.MaxBackoff
			}
		}

		lastResult = c.Check(ctx, target)
		if lastResult.Healthy {
			return lastResult
		}
	}

	// All retries exhausted
	if lastResult.Err != nil {
		lastResult.Err = fmt.Errorf("health check failed after %d attempts: %w", config.MaxRetries+1, lastResult.Err)
	} else {
		lastResult.Err = fmt.Errorf("health check failed after %d attempts", config.MaxRetries+1)
	}
	return lastResult
}

// CheckAll performs health checks on all targets concurrently.
// It limits concurrency to maxConcurrent to avoid overwhelming the system.
func (c *HTTPChecker) CheckAll(ctx context.Context, targets []Target, maxConcurrent int) []Result {
	if len(targets) == 0 {
		return nil
	}

	if maxConcurrent <= 0 {
		maxConcurrent = 10
	}

	results := make([]Result, len(targets))
	sem := make(chan struct{}, maxConcurrent)
	done := make(chan struct{})

	go func() {
		for i, target := range targets {
			select {
			case <-ctx.Done():
				// Fill remaining results with context error
				for j := i; j < len(targets); j++ {
					results[j] = Result{
						Target:  targets[j],
						Healthy: false,
						Err:     ctx.Err(),
					}
				}
				close(done)
				return
			case sem <- struct{}{}:
			}

			go func(idx int, t Target) {
				defer func() { <-sem }()
				results[idx] = c.Check(ctx, t)
			}(i, target)
		}

		// Wait for all goroutines to finish
		for i := 0; i < maxConcurrent; i++ {
			sem <- struct{}{}
		}
		close(done)
	}()

	<-done
	return results
}
