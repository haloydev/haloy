package healthcheck

import "time"

// Target represents a backend to health check.
type Target struct {
	ID              string // Container ID
	AppName         string
	IP              string
	Port            string
	HealthCheckPath string // e.g., "/health"
}

// Result represents the outcome of a single health check.
type Result struct {
	Target  Target
	Healthy bool
	Err     error
	Latency time.Duration
}

// Config holds the health monitor configuration.
type Config struct {
	Enabled  bool          // Whether health monitoring is enabled
	Interval time.Duration // Check interval (e.g., 15s)
	Fall     int           // Mark unhealthy after N consecutive failures
	Rise     int           // Mark healthy after N consecutive successes
	Timeout  time.Duration // Per-check timeout
}

// DefaultConfig returns the default health monitor configuration.
func DefaultConfig() Config {
	return Config{
		Enabled:  false,
		Interval: 15 * time.Second,
		Fall:     3,
		Rise:     2,
		Timeout:  5 * time.Second,
	}
}

// TargetState represents the current health state of a target.
type TargetState int

const (
	StateHealthy   TargetState = iota // Target is healthy and receiving traffic
	StateUnhealthy                    // Target is unhealthy and not receiving traffic
)

func (s TargetState) String() string {
	switch s {
	case StateHealthy:
		return "healthy"
	case StateUnhealthy:
		return "unhealthy"
	default:
		return "unknown"
	}
}

// TargetProvider is an interface for getting health check targets.
type TargetProvider interface {
	GetHealthCheckTargets() []Target
}

// ConfigUpdater is an interface for updating proxy config when health state changes.
type ConfigUpdater interface {
	OnHealthChange(healthyTargets []Target)
}
