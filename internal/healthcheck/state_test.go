package healthcheck

import (
	"errors"
	"testing"
)

func TestNewStateTracker(t *testing.T) {
	tests := []struct {
		name     string
		fall     int
		rise     int
		wantFall int
		wantRise int
	}{
		{"normal values", 3, 2, 3, 2},
		{"zero fall defaults to 1", 0, 2, 1, 2},
		{"zero rise defaults to 1", 3, 0, 3, 1},
		{"negative values default to 1", -1, -5, 1, 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := NewStateTracker(tt.fall, tt.rise)
			if st.fall != tt.wantFall {
				t.Errorf("fall = %d, want %d", st.fall, tt.wantFall)
			}
			if st.rise != tt.wantRise {
				t.Errorf("rise = %d, want %d", st.rise, tt.wantRise)
			}
		})
	}
}

func TestStateTracker_SyncTargets_AddNew(t *testing.T) {
	st := NewStateTracker(3, 2)

	targets := []Target{
		{ID: "a", AppName: "app1", IP: "10.0.0.1", Port: "8080"},
		{ID: "b", AppName: "app1", IP: "10.0.0.2", Port: "8080"},
	}

	changed := st.SyncTargets(targets)

	if !changed {
		t.Error("SyncTargets should return true when adding new targets")
	}

	all := st.GetAllTargets()
	if len(all) != 2 {
		t.Errorf("GetAllTargets returned %d targets, want 2", len(all))
	}

	// New targets should start as healthy
	healthy := st.GetHealthyTargets()
	if len(healthy) != 2 {
		t.Errorf("GetHealthyTargets returned %d targets, want 2", len(healthy))
	}
}

func TestStateTracker_SyncTargets_RemoveStale(t *testing.T) {
	st := NewStateTracker(3, 2)

	// Add initial targets
	st.SyncTargets([]Target{
		{ID: "a"},
		{ID: "b"},
		{ID: "c"},
	})

	// Remove one target
	changed := st.SyncTargets([]Target{
		{ID: "a"},
		{ID: "c"},
	})

	if !changed {
		t.Error("SyncTargets should return true when removing targets")
	}

	all := st.GetAllTargets()
	if len(all) != 2 {
		t.Errorf("GetAllTargets returned %d targets, want 2", len(all))
	}

	// Verify 'b' was removed
	state := st.GetState("b")
	if state != StateUnhealthy {
		t.Error("Removed target should return StateUnhealthy")
	}
}

func TestStateTracker_SyncTargets_UpdateExisting(t *testing.T) {
	st := NewStateTracker(3, 2)

	st.SyncTargets([]Target{
		{ID: "a", IP: "10.0.0.1"},
	})

	// Update IP
	changed := st.SyncTargets([]Target{
		{ID: "a", IP: "10.0.0.2"},
	})

	// Updating existing target info doesn't count as "changed" for the return value
	if changed {
		t.Error("SyncTargets should return false when only updating target info")
	}

	targets := st.GetAllTargets()
	if len(targets) != 1 {
		t.Fatalf("Expected 1 target, got %d", len(targets))
	}
	if targets[0].IP != "10.0.0.2" {
		t.Errorf("Target IP = %q, want %q", targets[0].IP, "10.0.0.2")
	}
}

func TestStateTracker_SyncTargets_NoChange(t *testing.T) {
	st := NewStateTracker(3, 2)

	targets := []Target{{ID: "a"}, {ID: "b"}}
	st.SyncTargets(targets)

	changed := st.SyncTargets(targets)

	if changed {
		t.Error("SyncTargets should return false when no targets added or removed")
	}
}

func TestStateTracker_RecordResult_HealthyToUnhealthy(t *testing.T) {
	st := NewStateTracker(3, 2) // fall=3, rise=2

	target := Target{ID: "a"}
	st.SyncTargets([]Target{target})

	// Record 2 failures - should still be healthy
	st.RecordResult(Result{Target: target, Healthy: false, Err: errors.New("fail")})
	st.RecordResult(Result{Target: target, Healthy: false, Err: errors.New("fail")})

	if st.GetState("a") != StateHealthy {
		t.Error("Target should still be healthy after 2 failures (fall=3)")
	}

	// Third failure should mark unhealthy
	changed := st.RecordResult(Result{Target: target, Healthy: false, Err: errors.New("fail")})

	if !changed {
		t.Error("RecordResult should return true when state changes")
	}
	if st.GetState("a") != StateUnhealthy {
		t.Error("Target should be unhealthy after 3 failures")
	}
}

func TestStateTracker_RecordResult_UnhealthyToHealthy(t *testing.T) {
	st := NewStateTracker(1, 2) // fall=1 (quick fail), rise=2

	target := Target{ID: "a"}
	st.SyncTargets([]Target{target})

	// Make unhealthy
	st.RecordResult(Result{Target: target, Healthy: false})

	if st.GetState("a") != StateUnhealthy {
		t.Fatal("Target should be unhealthy")
	}

	// One success - should still be unhealthy
	st.RecordResult(Result{Target: target, Healthy: true})

	if st.GetState("a") != StateUnhealthy {
		t.Error("Target should still be unhealthy after 1 success (rise=2)")
	}

	// Second success should mark healthy
	changed := st.RecordResult(Result{Target: target, Healthy: true})

	if !changed {
		t.Error("RecordResult should return true when state changes")
	}
	if st.GetState("a") != StateHealthy {
		t.Error("Target should be healthy after 2 successes")
	}
}

func TestStateTracker_RecordResult_ResetsCounters(t *testing.T) {
	st := NewStateTracker(3, 2)

	target := Target{ID: "a"}
	st.SyncTargets([]Target{target})

	// 2 failures
	st.RecordResult(Result{Target: target, Healthy: false})
	st.RecordResult(Result{Target: target, Healthy: false})

	// 1 success resets failure counter
	st.RecordResult(Result{Target: target, Healthy: true})

	// 2 more failures - still healthy because counter was reset
	st.RecordResult(Result{Target: target, Healthy: false})
	st.RecordResult(Result{Target: target, Healthy: false})

	if st.GetState("a") != StateHealthy {
		t.Error("Target should be healthy - failure counter should have been reset")
	}

	// Third failure now marks unhealthy
	st.RecordResult(Result{Target: target, Healthy: false})

	if st.GetState("a") != StateUnhealthy {
		t.Error("Target should be unhealthy after 3 consecutive failures")
	}
}

func TestStateTracker_RecordResult_UnknownTarget(t *testing.T) {
	st := NewStateTracker(3, 2)

	// Record result for unknown target
	changed := st.RecordResult(Result{Target: Target{ID: "unknown"}, Healthy: true})

	if changed {
		t.Error("RecordResult should return false for unknown target")
	}
}

func TestStateTracker_GetHealthyTargets(t *testing.T) {
	st := NewStateTracker(1, 1)

	st.SyncTargets([]Target{
		{ID: "a"},
		{ID: "b"},
		{ID: "c"},
	})

	// Make 'b' unhealthy
	st.RecordResult(Result{Target: Target{ID: "b"}, Healthy: false})

	healthy := st.GetHealthyTargets()

	if len(healthy) != 2 {
		t.Errorf("GetHealthyTargets returned %d targets, want 2", len(healthy))
	}

	// Check that 'b' is not in healthy list
	for _, h := range healthy {
		if h.ID == "b" {
			t.Error("Unhealthy target 'b' should not be in healthy list")
		}
	}
}

func TestStateTracker_GetUnhealthyTargets(t *testing.T) {
	st := NewStateTracker(1, 1)

	st.SyncTargets([]Target{
		{ID: "a"},
		{ID: "b"},
		{ID: "c"},
	})

	// Make 'a' and 'c' unhealthy
	st.RecordResult(Result{Target: Target{ID: "a"}, Healthy: false})
	st.RecordResult(Result{Target: Target{ID: "c"}, Healthy: false})

	unhealthy := st.GetUnhealthyTargets()

	if len(unhealthy) != 2 {
		t.Errorf("GetUnhealthyTargets returned %d targets, want 2", len(unhealthy))
	}
}

func TestStateTracker_GetStats(t *testing.T) {
	st := NewStateTracker(1, 1)

	st.SyncTargets([]Target{
		{ID: "a"},
		{ID: "b"},
		{ID: "c"},
		{ID: "d"},
	})

	// Make 2 unhealthy
	st.RecordResult(Result{Target: Target{ID: "a"}, Healthy: false})
	st.RecordResult(Result{Target: Target{ID: "c"}, Healthy: false})

	total, healthy, unhealthy := st.GetStats()

	if total != 4 {
		t.Errorf("total = %d, want 4", total)
	}
	if healthy != 2 {
		t.Errorf("healthy = %d, want 2", healthy)
	}
	if unhealthy != 2 {
		t.Errorf("unhealthy = %d, want 2", unhealthy)
	}
}

func TestStateTracker_GetState_UnknownTarget(t *testing.T) {
	st := NewStateTracker(3, 2)

	state := st.GetState("nonexistent")

	if state != StateUnhealthy {
		t.Errorf("GetState for unknown target = %v, want StateUnhealthy", state)
	}
}

func TestStateTracker_ConcurrentAccess(t *testing.T) {
	st := NewStateTracker(3, 2)

	targets := []Target{
		{ID: "a"},
		{ID: "b"},
		{ID: "c"},
	}
	st.SyncTargets(targets)

	done := make(chan bool)

	// Concurrent reads and writes
	for i := 0; i < 10; i++ {
		go func() {
			for j := 0; j < 100; j++ {
				st.RecordResult(Result{Target: targets[j%3], Healthy: j%2 == 0})
				st.GetHealthyTargets()
				st.GetStats()
				st.GetState(targets[j%3].ID)
			}
			done <- true
		}()
	}

	// Wait for all goroutines
	for i := 0; i < 10; i++ {
		<-done
	}
}
