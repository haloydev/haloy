package healthcheck

import (
	"sync"
)

// targetEntry tracks the health state and check history for a single target.
type targetEntry struct {
	Target               Target
	State                TargetState
	ConsecutiveFailures  int
	ConsecutiveSuccesses int
}

// StateTracker tracks health state for multiple targets with fall/rise thresholds.
// It follows the HAProxy/Traefik pattern:
// - fall: Mark unhealthy after N consecutive failures
// - rise: Mark healthy after N consecutive successes
type StateTracker struct {
	mu      sync.RWMutex
	entries map[string]*targetEntry // keyed by target ID (container ID)
	fall    int                     // failures needed to mark unhealthy
	rise    int                     // successes needed to mark healthy
}

// NewStateTracker creates a new state tracker with the given thresholds.
func NewStateTracker(fall, rise int) *StateTracker {
	if fall < 1 {
		fall = 1
	}
	if rise < 1 {
		rise = 1
	}
	return &StateTracker{
		entries: make(map[string]*targetEntry),
		fall:    fall,
		rise:    rise,
	}
}

// SyncTargets updates the tracked targets to match the provided list.
// New targets are added with initial healthy state, stale targets are removed.
// Returns true if any targets were added or removed.
func (st *StateTracker) SyncTargets(targets []Target) bool {
	st.mu.Lock()
	defer st.mu.Unlock()

	currentIDs := make(map[string]struct{}, len(targets))
	changed := false

	// Add new targets
	for _, t := range targets {
		currentIDs[t.ID] = struct{}{}
		if _, exists := st.entries[t.ID]; !exists {
			st.entries[t.ID] = &targetEntry{
				Target: t,
				State:  StateHealthy, // Assume healthy initially
			}
			changed = true
		} else {
			// Update target info (IP/port might change on restart)
			st.entries[t.ID].Target = t
		}
	}

	// Remove stale targets
	for id := range st.entries {
		if _, exists := currentIDs[id]; !exists {
			delete(st.entries, id)
			changed = true
		}
	}

	return changed
}

// RecordResult updates the state based on a health check result.
// Returns true if the target's state changed (healthy->unhealthy or vice versa).
func (st *StateTracker) RecordResult(result Result) (stateChanged bool) {
	st.mu.Lock()
	defer st.mu.Unlock()

	entry, exists := st.entries[result.Target.ID]
	if !exists {
		// Target not tracked (probably removed between check and result)
		return false
	}

	oldState := entry.State

	if result.Healthy {
		// Reset failure count, increment success count
		entry.ConsecutiveFailures = 0
		entry.ConsecutiveSuccesses++

		// Check if we should mark as healthy
		if entry.State == StateUnhealthy && entry.ConsecutiveSuccesses >= st.rise {
			entry.State = StateHealthy
		}
	} else {
		// Reset success count, increment failure count
		entry.ConsecutiveSuccesses = 0
		entry.ConsecutiveFailures++

		// Check if we should mark as unhealthy
		if entry.State == StateHealthy && entry.ConsecutiveFailures >= st.fall {
			entry.State = StateUnhealthy
		}
	}

	return entry.State != oldState
}

// GetHealthyTargets returns all targets currently in healthy state.
func (st *StateTracker) GetHealthyTargets() []Target {
	st.mu.RLock()
	defer st.mu.RUnlock()

	var healthy []Target
	for _, entry := range st.entries {
		if entry.State == StateHealthy {
			healthy = append(healthy, entry.Target)
		}
	}
	return healthy
}

// GetUnhealthyTargets returns all targets currently in unhealthy state.
func (st *StateTracker) GetUnhealthyTargets() []Target {
	st.mu.RLock()
	defer st.mu.RUnlock()

	var unhealthy []Target
	for _, entry := range st.entries {
		if entry.State == StateUnhealthy {
			unhealthy = append(unhealthy, entry.Target)
		}
	}
	return unhealthy
}

// GetAllTargets returns all tracked targets regardless of state.
func (st *StateTracker) GetAllTargets() []Target {
	st.mu.RLock()
	defer st.mu.RUnlock()

	targets := make([]Target, 0, len(st.entries))
	for _, entry := range st.entries {
		targets = append(targets, entry.Target)
	}
	return targets
}

// GetState returns the current state of a target by ID.
// Returns StateUnhealthy if the target is not found.
func (st *StateTracker) GetState(targetID string) TargetState {
	st.mu.RLock()
	defer st.mu.RUnlock()

	if entry, exists := st.entries[targetID]; exists {
		return entry.State
	}
	return StateUnhealthy
}

// GetStats returns statistics about the tracked targets.
func (st *StateTracker) GetStats() (total, healthy, unhealthy int) {
	st.mu.RLock()
	defer st.mu.RUnlock()

	total = len(st.entries)
	for _, entry := range st.entries {
		if entry.State == StateHealthy {
			healthy++
		} else {
			unhealthy++
		}
	}
	return total, healthy, unhealthy
}
