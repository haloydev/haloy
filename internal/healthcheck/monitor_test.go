package healthcheck

import (
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// mockTargetProvider implements TargetProvider for testing.
type mockTargetProvider struct {
	mu      sync.Mutex
	targets []Target
}

func (m *mockTargetProvider) GetHealthCheckTargets() []Target {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.targets
}

func (m *mockTargetProvider) SetTargets(targets []Target) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.targets = targets
}

// mockConfigUpdater implements ConfigUpdater for testing.
type mockConfigUpdater struct {
	mu             sync.Mutex
	calls          int
	lastHealthy    []Target
	onHealthChange func([]Target)
}

func (m *mockConfigUpdater) OnHealthChange(healthyTargets []Target) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calls++
	m.lastHealthy = healthyTargets
	if m.onHealthChange != nil {
		m.onHealthChange(healthyTargets)
	}
}

func (m *mockConfigUpdater) GetCalls() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.calls
}

func (m *mockConfigUpdater) GetLastHealthy() []Target {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.lastHealthy
}

func newTestLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
}

func TestNewHealthMonitor(t *testing.T) {
	config := DefaultConfig()
	provider := &mockTargetProvider{}
	updater := &mockConfigUpdater{}
	logger := newTestLogger()

	monitor := NewHealthMonitor(config, provider, updater, logger)

	if monitor == nil {
		t.Fatal("NewHealthMonitor returned nil")
	}
	if monitor.checker == nil {
		t.Error("monitor.checker is nil")
	}
	if monitor.stateTracker == nil {
		t.Error("monitor.stateTracker is nil")
	}
}

func TestHealthMonitor_StartStop(t *testing.T) {
	config := Config{
		Enabled:  true,
		Interval: 100 * time.Millisecond,
		Fall:     1,
		Rise:     1,
		Timeout:  1 * time.Second,
	}

	provider := &mockTargetProvider{}
	updater := &mockConfigUpdater{}
	logger := newTestLogger()

	monitor := NewHealthMonitor(config, provider, updater, logger)

	// Start should work
	monitor.Start()

	// Double start should be no-op
	monitor.Start()

	// Give it time to run a check
	time.Sleep(150 * time.Millisecond)

	// Stop should work
	monitor.Stop()

	// Double stop should be no-op
	monitor.Stop()
}

func TestHealthMonitor_RunsChecks(t *testing.T) {
	var checkCount int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&checkCount, 1)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	addr := strings.TrimPrefix(server.URL, "http://")
	parts := strings.Split(addr, ":")

	config := Config{
		Enabled:  true,
		Interval: 50 * time.Millisecond,
		Fall:     1,
		Rise:     1,
		Timeout:  1 * time.Second,
	}

	provider := &mockTargetProvider{
		targets: []Target{
			{ID: "a", IP: parts[0], Port: parts[1], HealthCheckPath: "/health"},
		},
	}
	updater := &mockConfigUpdater{}
	logger := newTestLogger()

	monitor := NewHealthMonitor(config, provider, updater, logger)
	monitor.Start()

	// Wait for at least 2 check cycles
	time.Sleep(130 * time.Millisecond)

	monitor.Stop()

	count := atomic.LoadInt32(&checkCount)
	if count < 2 {
		t.Errorf("Health check ran %d times, want >= 2", count)
	}
}

func TestHealthMonitor_DetectsUnhealthy(t *testing.T) {
	var healthy int32 = 1

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if atomic.LoadInt32(&healthy) == 1 {
			w.WriteHeader(http.StatusOK)
		} else {
			w.WriteHeader(http.StatusServiceUnavailable)
		}
	}))
	defer server.Close()

	addr := strings.TrimPrefix(server.URL, "http://")
	parts := strings.Split(addr, ":")

	config := Config{
		Enabled:  true,
		Interval: 30 * time.Millisecond,
		Fall:     2,
		Rise:     1,
		Timeout:  1 * time.Second,
	}

	provider := &mockTargetProvider{
		targets: []Target{
			{ID: "a", AppName: "testapp", IP: parts[0], Port: parts[1], HealthCheckPath: "/health"},
		},
	}

	stateChanged := make(chan []Target, 10)
	updater := &mockConfigUpdater{
		onHealthChange: func(targets []Target) {
			stateChanged <- targets
		},
	}
	logger := newTestLogger()

	monitor := NewHealthMonitor(config, provider, updater, logger)
	monitor.Start()

	// Wait for initial check to complete
	time.Sleep(50 * time.Millisecond)

	// Make server unhealthy
	atomic.StoreInt32(&healthy, 0)

	// Wait for fall threshold (2 failures)
	time.Sleep(100 * time.Millisecond)

	monitor.Stop()

	// Check that updater was called with state change
	select {
	case targets := <-stateChanged:
		// After becoming unhealthy, there should be no healthy targets
		if len(targets) != 0 {
			t.Errorf("Expected 0 healthy targets after failure, got %d", len(targets))
		}
	case <-time.After(100 * time.Millisecond):
		t.Error("OnHealthChange was not called after backend became unhealthy")
	}
}

func TestHealthMonitor_DetectsRecovery(t *testing.T) {
	var healthy int32 = 0 // Start unhealthy

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if atomic.LoadInt32(&healthy) == 1 {
			w.WriteHeader(http.StatusOK)
		} else {
			w.WriteHeader(http.StatusServiceUnavailable)
		}
	}))
	defer server.Close()

	addr := strings.TrimPrefix(server.URL, "http://")
	parts := strings.Split(addr, ":")

	config := Config{
		Enabled:  true,
		Interval: 30 * time.Millisecond,
		Fall:     1,
		Rise:     2,
		Timeout:  1 * time.Second,
	}

	provider := &mockTargetProvider{
		targets: []Target{
			{ID: "a", AppName: "testapp", IP: parts[0], Port: parts[1], HealthCheckPath: "/health"},
		},
	}

	var recoveryDetected int32
	updater := &mockConfigUpdater{
		onHealthChange: func(targets []Target) {
			if len(targets) == 1 {
				atomic.StoreInt32(&recoveryDetected, 1)
			}
		},
	}
	logger := newTestLogger()

	monitor := NewHealthMonitor(config, provider, updater, logger)
	monitor.Start()

	// Wait for initial failure
	time.Sleep(50 * time.Millisecond)

	// Make server healthy
	atomic.StoreInt32(&healthy, 1)

	// Wait for rise threshold (2 successes)
	time.Sleep(100 * time.Millisecond)

	monitor.Stop()

	if atomic.LoadInt32(&recoveryDetected) != 1 {
		t.Error("Recovery was not detected")
	}
}

func TestHealthMonitor_ForceCheck(t *testing.T) {
	var checkCount int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&checkCount, 1)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	addr := strings.TrimPrefix(server.URL, "http://")
	parts := strings.Split(addr, ":")

	config := Config{
		Enabled:  true,
		Interval: 10 * time.Second, // Long interval
		Fall:     1,
		Rise:     1,
		Timeout:  1 * time.Second,
	}

	provider := &mockTargetProvider{
		targets: []Target{
			{ID: "a", IP: parts[0], Port: parts[1], HealthCheckPath: "/health"},
		},
	}
	updater := &mockConfigUpdater{}
	logger := newTestLogger()

	monitor := NewHealthMonitor(config, provider, updater, logger)
	monitor.Start()

	// Wait for initial check
	time.Sleep(50 * time.Millisecond)

	initialCount := atomic.LoadInt32(&checkCount)

	// Force an immediate check
	monitor.ForceCheck()

	time.Sleep(50 * time.Millisecond)

	finalCount := atomic.LoadInt32(&checkCount)

	monitor.Stop()

	if finalCount <= initialCount {
		t.Errorf("ForceCheck did not trigger additional check: before=%d, after=%d", initialCount, finalCount)
	}
}

func TestHealthMonitor_ForceCheck_NotRunning(t *testing.T) {
	config := DefaultConfig()
	provider := &mockTargetProvider{}
	updater := &mockConfigUpdater{}
	logger := newTestLogger()

	monitor := NewHealthMonitor(config, provider, updater, logger)

	// ForceCheck on stopped monitor should not panic
	monitor.ForceCheck()
}

func TestHealthMonitor_GetHealthyTargets(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	addr := strings.TrimPrefix(server.URL, "http://")
	parts := strings.Split(addr, ":")

	config := Config{
		Enabled:  true,
		Interval: 50 * time.Millisecond,
		Fall:     1,
		Rise:     1,
		Timeout:  1 * time.Second,
	}

	provider := &mockTargetProvider{
		targets: []Target{
			{ID: "a", IP: parts[0], Port: parts[1], HealthCheckPath: "/health"},
			{ID: "b", IP: parts[0], Port: parts[1], HealthCheckPath: "/health"},
		},
	}
	updater := &mockConfigUpdater{}
	logger := newTestLogger()

	monitor := NewHealthMonitor(config, provider, updater, logger)
	monitor.Start()

	// Wait for initial check
	time.Sleep(100 * time.Millisecond)

	healthy := monitor.GetHealthyTargets()

	monitor.Stop()

	if len(healthy) != 2 {
		t.Errorf("GetHealthyTargets returned %d targets, want 2", len(healthy))
	}
}

func TestHealthMonitor_GetStats(t *testing.T) {
	var callCount int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count := atomic.AddInt32(&callCount, 1)
		// First target always healthy, second fails after first check
		if r.URL.Query().Get("id") == "b" && count > 2 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	addr := strings.TrimPrefix(server.URL, "http://")
	parts := strings.Split(addr, ":")

	config := Config{
		Enabled:  true,
		Interval: 30 * time.Millisecond,
		Fall:     1,
		Rise:     1,
		Timeout:  1 * time.Second,
	}

	provider := &mockTargetProvider{
		targets: []Target{
			{ID: "a", IP: parts[0], Port: parts[1], HealthCheckPath: "/health?id=a"},
			{ID: "b", IP: parts[0], Port: parts[1], HealthCheckPath: "/health?id=b"},
		},
	}
	updater := &mockConfigUpdater{}
	logger := newTestLogger()

	monitor := NewHealthMonitor(config, provider, updater, logger)
	monitor.Start()

	// Wait for checks to run
	time.Sleep(100 * time.Millisecond)

	total, healthy, unhealthy := monitor.GetStats()

	monitor.Stop()

	if total != 2 {
		t.Errorf("total = %d, want 2", total)
	}
	if healthy+unhealthy != total {
		t.Errorf("healthy(%d) + unhealthy(%d) != total(%d)", healthy, unhealthy, total)
	}
}

func TestHealthMonitor_NoTargets(t *testing.T) {
	config := Config{
		Enabled:  true,
		Interval: 50 * time.Millisecond,
		Fall:     1,
		Rise:     1,
		Timeout:  1 * time.Second,
	}

	provider := &mockTargetProvider{targets: []Target{}}
	updater := &mockConfigUpdater{}
	logger := newTestLogger()

	monitor := NewHealthMonitor(config, provider, updater, logger)
	monitor.Start()

	// Should not panic with no targets
	time.Sleep(100 * time.Millisecond)

	monitor.Stop()

	// Updater should not be called when there are no targets
	if updater.GetCalls() > 0 {
		t.Errorf("OnHealthChange called %d times with no targets", updater.GetCalls())
	}
}

func TestHealthMonitor_DynamicTargets(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	addr := strings.TrimPrefix(server.URL, "http://")
	parts := strings.Split(addr, ":")

	config := Config{
		Enabled:  true,
		Interval: 30 * time.Millisecond,
		Fall:     1,
		Rise:     1,
		Timeout:  1 * time.Second,
	}

	provider := &mockTargetProvider{
		targets: []Target{
			{ID: "a", IP: parts[0], Port: parts[1], HealthCheckPath: "/health"},
		},
	}
	updater := &mockConfigUpdater{}
	logger := newTestLogger()

	monitor := NewHealthMonitor(config, provider, updater, logger)
	monitor.Start()

	// Wait for initial check
	time.Sleep(50 * time.Millisecond)

	// Add a new target
	provider.SetTargets([]Target{
		{ID: "a", IP: parts[0], Port: parts[1], HealthCheckPath: "/health"},
		{ID: "b", IP: parts[0], Port: parts[1], HealthCheckPath: "/health"},
	})

	// Wait for next check cycle
	time.Sleep(50 * time.Millisecond)

	healthy := monitor.GetHealthyTargets()

	monitor.Stop()

	if len(healthy) != 2 {
		t.Errorf("GetHealthyTargets returned %d targets after adding, want 2", len(healthy))
	}
}
