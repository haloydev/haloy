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
// non-blocking and stop returns promptly.
func TestAppDebouncerNoDeadlock(t *testing.T) {
	output := make(chan debouncedAppEvent) // unbuffered, never drained
	d := newAppDebouncer(10*time.Millisecond, time.Second, output, discardLogger())

	d.captureEvent("app-a", testEvent(events.ActionDie, "01aaa"))

	// Wait for the timer to fire so signalDone is blocked sending on output.
	time.Sleep(100 * time.Millisecond)

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

	stopped := make(chan struct{})
	go func() {
		d.stop()
		close(stopped)
	}()

	select {
	case <-stopped:
	case <-time.After(time.Second):
		t.Fatal("stop blocked while a debounced send was pending")
	}
}
