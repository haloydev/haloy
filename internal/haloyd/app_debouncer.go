package haloyd

import (
	"log/slog"
	"sync"
	"time"

	"github.com/docker/docker/api/types/events"
	"github.com/haloydev/haloy/internal/config"
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
	deadlines      map[string]time.Time
	delay          time.Duration
	maxWait        time.Duration
	capturedEvents map[string][]ContainerEvent
	output         chan<- debouncedAppEvent
	done           chan struct{}
	stopOnce       sync.Once
	logger         *slog.Logger
}

func newAppDebouncer(delay, maxWait time.Duration, output chan<- debouncedAppEvent, logger *slog.Logger) *appDebouncer {
	return &appDebouncer{
		timers:         make(map[string]*time.Timer),
		deadlines:      make(map[string]time.Time),
		delay:          delay,
		maxWait:        maxWait,
		capturedEvents: make(map[string][]ContainerEvent),
		output:         output,
		done:           make(chan struct{}),
		logger:         logger,
	}
}

func (d *appDebouncer) captureEvent(appName string, event ContainerEvent) {
	d.mu.Lock()
	defer d.mu.Unlock()

	d.logger.Debug("Captured event for debouncing", "app", appName, "action", event.Event.Action, "deploymentID", event.Labels.DeploymentID)

	d.capturedEvents[appName] = append(d.capturedEvents[appName], event)

	// A steady stream of events (e.g. a crash-looping container) keeps resetting
	// the timer, so cap the total wait at maxWait from the first captured event.
	deadline, ok := d.deadlines[appName]
	if !ok {
		deadline = time.Now().Add(d.maxWait)
		d.deadlines[appName] = deadline
	}
	wait := d.delay
	if remaining := time.Until(deadline); remaining < wait {
		wait = max(remaining, 0)
	}

	// Reset timer
	if timer, ok := d.timers[appName]; ok {
		timer.Stop()
	}

	d.timers[appName] = time.AfterFunc(wait, func() {
		d.signalDone(appName)
	})
}

func (d *appDebouncer) signalDone(appName string) {
	d.mu.Lock()

	capturedEvents := d.capturedEvents[appName]
	if len(capturedEvents) == 0 {
		d.mu.Unlock()
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

	// Cleanup
	delete(d.timers, appName)
	delete(d.deadlines, appName)
	delete(d.capturedEvents, appName)

	// Send after releasing the lock: a blocked send while holding the lock
	// would deadlock the main loop if it is calling captureEvent at the same
	// time, since the main loop is also the only receiver of output.
	d.mu.Unlock()

	select {
	case d.output <- debouncedEvent:
	case <-d.done:
	}
}

func (d *appDebouncer) stop() {
	d.stopOnce.Do(func() {
		close(d.done)
	})

	d.mu.Lock()
	defer d.mu.Unlock()

	for _, timer := range d.timers {
		timer.Stop()
	}
	d.timers = make(map[string]*time.Timer)
	d.deadlines = make(map[string]time.Time)
	d.capturedEvents = make(map[string][]ContainerEvent)
}
