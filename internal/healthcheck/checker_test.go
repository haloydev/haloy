package healthcheck

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestNewHTTPChecker(t *testing.T) {
	checker := NewHTTPChecker(5 * time.Second)
	if checker == nil {
		t.Fatal("NewHTTPChecker returned nil")
	}
	if checker.client == nil {
		t.Error("HTTPChecker.client is nil")
	}
}

func TestHTTPChecker_Check_Healthy(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/health" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	// Parse server URL to get host and port
	addr := strings.TrimPrefix(server.URL, "http://")
	parts := strings.Split(addr, ":")
	host, port := parts[0], parts[1]

	checker := NewHTTPChecker(5 * time.Second)
	target := Target{
		ID:              "test-container",
		AppName:         "testapp",
		IP:              host,
		Port:            port,
		HealthCheckPath: "/health",
	}

	result := checker.Check(context.Background(), target)

	if !result.Healthy {
		t.Errorf("Check returned unhealthy, want healthy: %v", result.Err)
	}
	if result.Err != nil {
		t.Errorf("Check returned error: %v", result.Err)
	}
	if result.Latency <= 0 {
		t.Error("Check returned zero or negative latency")
	}
}

func TestHTTPChecker_Check_Unhealthy_StatusCode(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		wantErr    bool
	}{
		{"200 OK", http.StatusOK, false},
		{"201 Created", http.StatusCreated, false},
		{"301 Redirect", http.StatusMovedPermanently, false},
		{"400 Bad Request", http.StatusBadRequest, true},
		{"404 Not Found", http.StatusNotFound, true},
		{"500 Internal Server Error", http.StatusInternalServerError, true},
		{"503 Service Unavailable", http.StatusServiceUnavailable, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.statusCode)
			}))
			defer server.Close()

			addr := strings.TrimPrefix(server.URL, "http://")
			parts := strings.Split(addr, ":")

			checker := NewHTTPChecker(5 * time.Second)
			target := Target{
				ID:              "test",
				IP:              parts[0],
				Port:            parts[1],
				HealthCheckPath: "/health",
			}

			result := checker.Check(context.Background(), target)

			if result.Healthy == tt.wantErr {
				t.Errorf("Check healthy = %v, wantErr = %v", result.Healthy, tt.wantErr)
			}
		})
	}
}

func TestHTTPChecker_Check_ConnectionRefused(t *testing.T) {
	checker := NewHTTPChecker(1 * time.Second)
	target := Target{
		ID:              "test",
		IP:              "127.0.0.1",
		Port:            "59999", // Unlikely to have anything listening
		HealthCheckPath: "/health",
	}

	result := checker.Check(context.Background(), target)

	if result.Healthy {
		t.Error("Check returned healthy for unreachable server")
	}
	if result.Err == nil {
		t.Error("Check should return error for unreachable server")
	}
}

func TestHTTPChecker_Check_Timeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(300 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	addr := strings.TrimPrefix(server.URL, "http://")
	parts := strings.Split(addr, ":")

	checker := NewHTTPChecker(100 * time.Millisecond)
	target := Target{
		ID:              "test",
		IP:              parts[0],
		Port:            parts[1],
		HealthCheckPath: "/health",
	}

	result := checker.Check(context.Background(), target)

	if result.Healthy {
		t.Error("Check returned healthy for slow server")
	}
	if result.Err == nil {
		t.Error("Check should return error for timeout")
	}
	if !strings.Contains(result.Err.Error(), "request failed") {
		t.Errorf("timeout error = %v, expected wrapped request failure", result.Err)
	}
}

func TestHTTPChecker_Check_ContextCanceled(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(500 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	addr := strings.TrimPrefix(server.URL, "http://")
	parts := strings.Split(addr, ":")

	checker := NewHTTPChecker(10 * time.Second)
	target := Target{
		ID:              "test",
		IP:              parts[0],
		Port:            parts[1],
		HealthCheckPath: "/health",
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	result := checker.Check(ctx, target)

	if result.Healthy {
		t.Error("Check returned healthy for canceled context")
	}
	if result.Err == nil {
		t.Fatal("Check should return error for canceled context")
	}
	if !errors.Is(result.Err, context.Canceled) {
		t.Errorf("Check error = %v, expected context canceled", result.Err)
	}
}

func TestDefaultRetryConfig(t *testing.T) {
	config := DefaultRetryConfig()

	if config.MaxRetries != 5 {
		t.Errorf("MaxRetries = %d, want 5", config.MaxRetries)
	}
	if config.InitialBackoff != 500*time.Millisecond {
		t.Errorf("InitialBackoff = %v, want %v", config.InitialBackoff, 500*time.Millisecond)
	}
	if config.MaxBackoff != 8*time.Second {
		t.Errorf("MaxBackoff = %v, want %v", config.MaxBackoff, 8*time.Second)
	}
}

func TestHTTPChecker_CheckWithRetry_ImmediateSuccess(t *testing.T) {
	var callCount int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&callCount, 1)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	addr := strings.TrimPrefix(server.URL, "http://")
	parts := strings.Split(addr, ":")

	checker := NewHTTPChecker(5 * time.Second)
	target := Target{
		ID:              "test",
		IP:              parts[0],
		Port:            parts[1],
		HealthCheckPath: "/health",
	}

	retryConfig := RetryConfig{
		MaxRetries:     3,
		InitialBackoff: 10 * time.Millisecond,
		MaxBackoff:     100 * time.Millisecond,
	}

	result := checker.CheckWithRetry(context.Background(), target, retryConfig, nil)

	if !result.Healthy {
		t.Errorf("CheckWithRetry returned unhealthy: %v", result.Err)
	}
	if atomic.LoadInt32(&callCount) != 1 {
		t.Errorf("Server called %d times, want 1", callCount)
	}
}

func TestHTTPChecker_CheckWithRetry_SuccessAfterRetries(t *testing.T) {
	var callCount int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count := atomic.AddInt32(&callCount, 1)
		if count < 3 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	addr := strings.TrimPrefix(server.URL, "http://")
	parts := strings.Split(addr, ":")

	checker := NewHTTPChecker(5 * time.Second)
	target := Target{
		ID:              "test",
		IP:              parts[0],
		Port:            parts[1],
		HealthCheckPath: "/health",
	}

	retryConfig := RetryConfig{
		MaxRetries:     5,
		InitialBackoff: 10 * time.Millisecond,
		MaxBackoff:     50 * time.Millisecond,
	}

	var retryAttempts int
	result := checker.CheckWithRetry(context.Background(), target, retryConfig, func(attempt int, backoff time.Duration) {
		retryAttempts++
	})

	if !result.Healthy {
		t.Errorf("CheckWithRetry returned unhealthy: %v", result.Err)
	}
	if atomic.LoadInt32(&callCount) != 3 {
		t.Errorf("Server called %d times, want 3", callCount)
	}
	if retryAttempts != 2 {
		t.Errorf("Retry callback called %d times, want 2", retryAttempts)
	}
}

func TestHTTPChecker_CheckWithRetry_AllRetriesFail(t *testing.T) {
	var callCount int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&callCount, 1)
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()

	addr := strings.TrimPrefix(server.URL, "http://")
	parts := strings.Split(addr, ":")

	checker := NewHTTPChecker(5 * time.Second)
	target := Target{
		ID:              "test",
		IP:              parts[0],
		Port:            parts[1],
		HealthCheckPath: "/health",
	}

	retryConfig := RetryConfig{
		MaxRetries:     2,
		InitialBackoff: 10 * time.Millisecond,
		MaxBackoff:     50 * time.Millisecond,
	}

	result := checker.CheckWithRetry(context.Background(), target, retryConfig, nil)

	if result.Healthy {
		t.Error("CheckWithRetry returned healthy after all failures")
	}
	if result.Err == nil {
		t.Error("CheckWithRetry should return error after all failures")
	}
	// Initial attempt + MaxRetries = 3 total calls
	if atomic.LoadInt32(&callCount) != 3 {
		t.Errorf("Server called %d times, want 3", callCount)
	}
}

func TestHTTPChecker_CheckWithRetry_ContextCanceled(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()

	addr := strings.TrimPrefix(server.URL, "http://")
	parts := strings.Split(addr, ":")

	checker := NewHTTPChecker(5 * time.Second)
	target := Target{
		ID:              "test",
		IP:              parts[0],
		Port:            parts[1],
		HealthCheckPath: "/health",
	}

	retryConfig := RetryConfig{
		MaxRetries:     10,
		InitialBackoff: 100 * time.Millisecond,
		MaxBackoff:     1 * time.Second,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	result := checker.CheckWithRetry(ctx, target, retryConfig, nil)

	if result.Healthy {
		t.Error("CheckWithRetry returned healthy after context canceled")
	}
	if result.Err == nil {
		t.Fatal("CheckWithRetry should return context error when canceled")
	}
	if !errors.Is(result.Err, context.DeadlineExceeded) {
		t.Errorf("CheckWithRetry error = %v, expected context deadline exceeded", result.Err)
	}
}

func TestHTTPChecker_CheckAll(t *testing.T) {
	var callCount int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&callCount, 1)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	addr := strings.TrimPrefix(server.URL, "http://")
	parts := strings.Split(addr, ":")

	checker := NewHTTPChecker(5 * time.Second)
	targets := []Target{
		{ID: "1", IP: parts[0], Port: parts[1], HealthCheckPath: "/health"},
		{ID: "2", IP: parts[0], Port: parts[1], HealthCheckPath: "/health"},
		{ID: "3", IP: parts[0], Port: parts[1], HealthCheckPath: "/health"},
	}

	results := checker.CheckAll(context.Background(), targets, 10)

	if len(results) != 3 {
		t.Errorf("CheckAll returned %d results, want 3", len(results))
	}
	for i, result := range results {
		if !result.Healthy {
			t.Errorf("Result %d is unhealthy: %v", i, result.Err)
		}
	}
	if atomic.LoadInt32(&callCount) != 3 {
		t.Errorf("Server called %d times, want 3", callCount)
	}
}

func TestHTTPChecker_CheckAll_EmptyTargets(t *testing.T) {
	checker := NewHTTPChecker(5 * time.Second)
	results := checker.CheckAll(context.Background(), nil, 10)

	if results != nil {
		t.Errorf("CheckAll returned %v for empty targets, want nil", results)
	}
}

func TestHTTPChecker_CheckAll_ConcurrencyLimit(t *testing.T) {
	var concurrent int32
	var maxConcurrent int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		current := atomic.AddInt32(&concurrent, 1)
		for {
			old := atomic.LoadInt32(&maxConcurrent)
			if current <= old || atomic.CompareAndSwapInt32(&maxConcurrent, old, current) {
				break
			}
		}
		time.Sleep(50 * time.Millisecond)
		atomic.AddInt32(&concurrent, -1)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	addr := strings.TrimPrefix(server.URL, "http://")
	parts := strings.Split(addr, ":")

	checker := NewHTTPChecker(5 * time.Second)
	targets := make([]Target, 20)
	for i := range targets {
		targets[i] = Target{
			ID:              string(rune('A' + i)),
			IP:              parts[0],
			Port:            parts[1],
			HealthCheckPath: "/health",
		}
	}

	maxAllowed := 5
	checker.CheckAll(context.Background(), targets, maxAllowed)

	if atomic.LoadInt32(&maxConcurrent) > int32(maxAllowed) {
		t.Errorf("Max concurrent requests = %d, want <= %d", maxConcurrent, maxAllowed)
	}
}
