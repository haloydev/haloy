package haloyd

import (
	"log/slog"
	"sync"
	"time"

	"github.com/haloydev/haloy/internal/config"
	"github.com/docker/docker/api/types/events"
)

type debouncedAppEvent struct {
	AppName            string
	DeploymentID       string
	Domains            []config.Domain
	EventAction        events.Action
	CapturedStartEvent bool
}

type appDebouncer struct {
	mu             sync.Mutex
	timers         map[string]*time.Timer
	delay          time.Duration
	capturedEvents map[string][]ContainerEvent
	output         chan<- debouncedAppEvent
	logger         *slog.Logger
}

func newAppDebouncer(delay time.Duration, output chan<- debouncedAppEvent, logger *slog.Logger) *appDebouncer {
	return &appDebouncer{
		timers:         make(map[string]*time.Timer),
		delay:          delay,
		capturedEvents: make(map[string][]ContainerEvent),
		output:         output,
		logger:         logger,
	}
}

func (d *appDebouncer) captureEvent(appName string, event ContainerEvent) {
	d.mu.Lock()
	defer d.mu.Unlock()

	d.logger.Debug("Captured event for debouncing", "app", appName, "action", event.Event.Action, "deploymentID", event.Labels.DeploymentID)

	d.capturedEvents[appName] = append(d.capturedEvents[appName], event)

	// Reset timer
	if timer, ok := d.timers[appName]; ok {
		timer.Stop()
	}

	// Create new timer
	d.timers[appName] = time.AfterFunc(d.delay, func() {
		d.signalDone(appName)
	})
}

func (d *appDebouncer) signalDone(appName string) {
	d.mu.Lock()
	defer d.mu.Unlock()

	capturedEvents := d.capturedEvents[appName]
	if len(capturedEvents) == 0 {
		return
	}

	latestEvent := capturedEvents[0]
	var capturedStartEvent bool
	for _, event := range capturedEvents {

		if event.Labels.DeploymentID > latestEvent.Labels.DeploymentID {
			latestEvent = event
		}

		if event.Event.Action == events.ActionStart {
			capturedStartEvent = true
		}
	}

	debouncedEvent := debouncedAppEvent{
		AppName:            appName,
		DeploymentID:       latestEvent.Labels.DeploymentID,
		Domains:            latestEvent.Labels.Domains,
		EventAction:        latestEvent.Event.Action,
		CapturedStartEvent: capturedStartEvent,
	}

	d.output <- debouncedEvent

	// Cleanup
	delete(d.timers, appName)
	delete(d.capturedEvents, appName)
}

func (d *appDebouncer) stop() {
	d.mu.Lock()
	defer d.mu.Unlock()

	for _, timer := range d.timers {
		timer.Stop()
	}
	d.timers = make(map[string]*time.Timer)
	d.capturedEvents = make(map[string][]ContainerEvent)
}
