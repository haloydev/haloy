package haloyd

import (
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/docker/docker/api/types/events"
	"github.com/haloydev/haloy/internal/config"
)

func testEvent(action events.Action, deploymentID string) ContainerEvent {
	return ContainerEvent{
		Event: events.Message{Action: action},
		Labels: &config.ContainerLabels{
			AppName:      "test-app",
			DeploymentID: deploymentID,
		},
	}
}

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestAppDebouncerMergesEvents(t *testing.T) {
	output := make(chan debouncedAppEvent, 1)
	d := newAppDebouncer(50*time.Millisecond, time.Second, output, discardLogger())
	defer d.stop()

	d.captureEvent("test-app", testEvent(events.ActionDie, "01aaa"))
	d.captureEvent("test-app", testEvent(events.ActionStart, "01bbb"))

	select {
	case de := <-output:
		if de.DeploymentID != "01bbb" {
			t.Errorf("expected latest deployment ID 01bbb, got %s", de.DeploymentID)
		}
		if !de.CapturedStartEvent {
			t.Error("expected CapturedStartEvent to be true")
		}
		if de.AppName != "test-app" {
			t.Errorf("expected app name test-app, got %s", de.AppName)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for debounced event")
	}
}

// TestAppDebouncerMaxWait verifies that a steady stream of events (e.g. a
// crash-looping container) cannot postpone the debounced event forever.
func TestAppDebouncerMaxWait(t *testing.T) {
	output := make(chan debouncedAppEvent, 1)
	d := newAppDebouncer(50*time.Millisecond, 250*time.Millisecond, output, discardLogger())
	defer d.stop()

	stopSending := make(chan struct{})
	defer close(stopSending)
	go func() {
		ticker := time.NewTicker(25 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-stopSending:
				return
			case <-ticker.C:
				d.captureEvent("test-app", testEvent(events.ActionDie, "01aaa"))
			}
		}
	}()

	d.captureEvent("test-app", testEvent(events.ActionDie, "01aaa"))

	select {
	case <-output:
		// Fired despite events arriving faster than the debounce delay.
	case <-time.After(time.Second):
		t.Fatal("debounced event never fired while events kept arriving")
	}
}

// TestAppDebouncerNoDeadlock verifies that a signalDone blocked on an
// undrained output channel does not hold the mutex, so captureEvent stays
// non-blocking.
func TestAppDebouncerNoDeadlock(t *testing.T) {
	output := make(chan debouncedAppEvent) // unbuffered, never drained
	d := newAppDebouncer(10*time.Millisecond, time.Second, output, discardLogger())
	defer d.stop()

	d.captureEvent("app-a", testEvent(events.ActionDie, "01aaa"))

	// Wait until signalDone has consumed app-a's events, after which it is
	// blocked sending on output. TryLock keeps this loop from hanging if the
	// send holds the lock; the captureEvent check below then reports it.
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if d.mu.TryLock() {
			_, pending := d.capturedEvents["app-a"]
			d.mu.Unlock()
			if !pending {
				break
			}
		}
		time.Sleep(time.Millisecond)
	}

	captured := make(chan struct{})
	go func() {
		d.captureEvent("app-b", testEvent(events.ActionDie, "01bbb"))
		close(captured)
	}()

	select {
	case <-captured:
	case <-time.After(time.Second):
		t.Fatal("captureEvent blocked while a debounced send was pending")
	}
}

func TestStartMustBelongToSelectedDeployment(t *testing.T) {
	for _, tc := range []struct {
		name   string
		events []ContainerEvent
		want   bool
	}{
		{"stop only", []ContainerEvent{testEvent(events.ActionStop, "002")}, false},
		{"older start", []ContainerEvent{testEvent(events.ActionStart, "001"), testEvent(events.ActionStop, "002")}, false},
		{"same id later start", []ContainerEvent{testEvent(events.ActionStop, "002"), testEvent(events.ActionStart, "002")}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			output := make(chan debouncedAppEvent, 1)
			d := newAppDebouncer(time.Hour, time.Hour, output, discardLogger())
			defer d.stop()
			for _, e := range tc.events {
				d.capturedEvents["test-app"] = append(d.capturedEvents["test-app"], e)
			}
			d.signalDone("test-app")
			if got := (<-output).StartedSelected; got != tc.want {
				t.Errorf("StartedSelected = %v", got)
			}
		})
	}
}
