package healthcheck

import (
	"testing"
	"time"
)

func TestDefaultConfig(t *testing.T) {
	config := DefaultConfig()

	if !config.Enabled {
		t.Error("DefaultConfig().Enabled should be true (enabled by default)")
	}
	if config.Interval != 15*time.Second {
		t.Errorf("DefaultConfig().Interval = %v, want %v", config.Interval, 15*time.Second)
	}
	if config.Fall != 3 {
		t.Errorf("DefaultConfig().Fall = %d, want 3", config.Fall)
	}
	if config.Rise != 2 {
		t.Errorf("DefaultConfig().Rise = %d, want 2", config.Rise)
	}
	if config.Timeout != 5*time.Second {
		t.Errorf("DefaultConfig().Timeout = %v, want %v", config.Timeout, 5*time.Second)
	}
}

func TestTargetState_String(t *testing.T) {
	tests := []struct {
		state TargetState
		want  string
	}{
		{StateHealthy, "healthy"},
		{StateUnhealthy, "unhealthy"},
		{TargetState(99), "unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			if got := tt.state.String(); got != tt.want {
				t.Errorf("TargetState.String() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestTarget_Fields(t *testing.T) {
	target := Target{
		ID:              "container-123",
		AppName:         "myapp",
		IP:              "172.17.0.2",
		Port:            "8080",
		HealthCheckPath: "/health",
	}

	if target.ID != "container-123" {
		t.Errorf("Target.ID = %q, want %q", target.ID, "container-123")
	}
	if target.AppName != "myapp" {
		t.Errorf("Target.AppName = %q, want %q", target.AppName, "myapp")
	}
	if target.IP != "172.17.0.2" {
		t.Errorf("Target.IP = %q, want %q", target.IP, "172.17.0.2")
	}
	if target.Port != "8080" {
		t.Errorf("Target.Port = %q, want %q", target.Port, "8080")
	}
	if target.HealthCheckPath != "/health" {
		t.Errorf("Target.HealthCheckPath = %q, want %q", target.HealthCheckPath, "/health")
	}
}

func TestResult_Fields(t *testing.T) {
	target := Target{ID: "test-id"}
	result := Result{
		Target:  target,
		Healthy: true,
		Err:     nil,
		Latency: 100 * time.Millisecond,
	}

	if result.Target.ID != "test-id" {
		t.Errorf("Result.Target.ID = %q, want %q", result.Target.ID, "test-id")
	}
	if !result.Healthy {
		t.Error("Result.Healthy should be true")
	}
	if result.Err != nil {
		t.Errorf("Result.Err = %v, want nil", result.Err)
	}
	if result.Latency != 100*time.Millisecond {
		t.Errorf("Result.Latency = %v, want %v", result.Latency, 100*time.Millisecond)
	}
}
