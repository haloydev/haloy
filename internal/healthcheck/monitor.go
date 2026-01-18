package healthcheck

import (
	"context"
	"log/slog"
	"sync"
	"time"
)

const (
	maxConcurrentChecks = 10 // Maximum concurrent health checks
)

// HealthMonitor runs continuous health checks in the background.
type HealthMonitor struct {
	config         Config
	targetProvider TargetProvider
	configUpdater  ConfigUpdater
	checker        *HTTPChecker
	stateTracker   *StateTracker
	logger         *slog.Logger

	mu        sync.Mutex
	running   bool
	stopCh    chan struct{}
	stoppedCh chan struct{}
}

// NewHealthMonitor creates a new health monitor.
func NewHealthMonitor(
	config Config,
	targetProvider TargetProvider,
	configUpdater ConfigUpdater,
	logger *slog.Logger,
) *HealthMonitor {
	return &HealthMonitor{
		config:         config,
		targetProvider: targetProvider,
		configUpdater:  configUpdater,
		checker:        NewHTTPChecker(config.Timeout),
		stateTracker:   NewStateTracker(config.Fall, config.Rise),
		logger:         logger,
	}
}

// Start begins the health monitoring loop.
// It is safe to call Start multiple times; subsequent calls are no-ops.
func (m *HealthMonitor) Start() {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.running {
		return
	}

	m.running = true
	m.stopCh = make(chan struct{})
	m.stoppedCh = make(chan struct{})

	go m.run()
}

// Stop stops the health monitoring loop and waits for it to finish.
func (m *HealthMonitor) Stop() {
	m.mu.Lock()
	if !m.running {
		m.mu.Unlock()
		return
	}
	close(m.stopCh)
	m.running = false
	stoppedCh := m.stoppedCh
	m.mu.Unlock()

	// Wait for the run loop to finish
	<-stoppedCh
}

// run is the main monitoring loop.
func (m *HealthMonitor) run() {
	defer close(m.stoppedCh)

	m.logger.Info("Health monitor started",
		"interval", m.config.Interval,
		"fall", m.config.Fall,
		"rise", m.config.Rise,
		"timeout", m.config.Timeout)

	// Run initial check immediately
	m.runCheck()

	ticker := time.NewTicker(m.config.Interval)
	defer ticker.Stop()

	for {
		select {
		case <-m.stopCh:
			m.logger.Info("Health monitor stopped")
			return
		case <-ticker.C:
			m.runCheck()
		}
	}
}

// runCheck performs a single round of health checks on all targets.
func (m *HealthMonitor) runCheck() {
	// Get current targets from provider
	targets := m.targetProvider.GetHealthCheckTargets()
	if len(targets) == 0 {
		m.logger.Debug("Health check: no targets to check")
		return
	}

	// Sync state tracker with current targets
	m.stateTracker.SyncTargets(targets)

	// Create a context with timeout for the entire check round
	ctx, cancel := context.WithTimeout(context.Background(), m.config.Interval)
	defer cancel()

	// Run health checks concurrently
	results := m.checker.CheckAll(ctx, targets, maxConcurrentChecks)

	// Process results and track state changes
	var stateChanged bool
	for _, result := range results {
		if m.stateTracker.RecordResult(result) {
			stateChanged = true
			m.logStateChange(result)
		}
	}

	// Log check summary at debug level
	total, healthy, unhealthy := m.stateTracker.GetStats()
	m.logger.Debug("Health check completed",
		"total", total,
		"healthy", healthy,
		"unhealthy", unhealthy)

	// If any state changed, notify the config updater
	if stateChanged {
		healthyTargets := m.stateTracker.GetHealthyTargets()
		m.configUpdater.OnHealthChange(healthyTargets)
	}
}

// logStateChange logs a health state transition.
func (m *HealthMonitor) logStateChange(result Result) {
	state := m.stateTracker.GetState(result.Target.ID)
	if state == StateHealthy {
		m.logger.Info("Backend recovered",
			"app", result.Target.AppName,
			"ip", result.Target.IP,
			"port", result.Target.Port,
			"latency_ms", result.Latency.Milliseconds())
	} else {
		errMsg := ""
		if result.Err != nil {
			errMsg = result.Err.Error()
		}
		m.logger.Warn("Backend marked unhealthy",
			"app", result.Target.AppName,
			"ip", result.Target.IP,
			"port", result.Target.Port,
			"error", errMsg)
	}
}

// ForceCheck triggers an immediate health check round, bypassing the ticker.
// This is useful after deployment changes to quickly update health state.
func (m *HealthMonitor) ForceCheck() {
	m.mu.Lock()
	running := m.running
	m.mu.Unlock()

	if running {
		m.runCheck()
	}
}

// GetHealthyTargets returns the current list of healthy targets.
func (m *HealthMonitor) GetHealthyTargets() []Target {
	return m.stateTracker.GetHealthyTargets()
}

// GetStats returns current health statistics.
func (m *HealthMonitor) GetStats() (total, healthy, unhealthy int) {
	return m.stateTracker.GetStats()
}
