package helpers

import (
	"sync"
	"time"
)

// DebounceFunc defines the type for the function to be executed after debouncing.
type DebounceFunc func()

// Debouncer manages debouncing calls for different keys.
type Debouncer struct {
	mu     sync.Mutex
	timers map[string]*time.Timer
	delay  time.Duration
}

// NewDebouncer creates a new Debouncer.
func NewDebouncer(delay time.Duration) *Debouncer {
	return &Debouncer{
		timers: make(map[string]*time.Timer),
		delay:  delay,
	}
}

// Debounce schedules or resets the timer for a given key.
// When the delay expires without subsequent calls for the same key, the action function is executed.
func (d *Debouncer) Debounce(key string, action DebounceFunc) {
	d.mu.Lock()
	defer d.mu.Unlock()

	if timer, ok := d.timers[key]; ok {
		timer.Stop()
	}

	d.timers[key] = time.AfterFunc(d.delay, func() {
		d.mu.Lock()
		delete(d.timers, key)
		d.mu.Unlock()

		action()
	})
}

// Stop cancels all pending debounced actions.
func (d *Debouncer) Stop() {
	d.mu.Lock()
	defer d.mu.Unlock()
	for key, timer := range d.timers {
		timer.Stop()
		delete(d.timers, key)
	}
}
