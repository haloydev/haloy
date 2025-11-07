package logging

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"
)

// LogEntry represents a structured log entry for streaming logs
type LogEntry struct {
	Level                string         `json:"level"`
	Message              string         `json:"message"`
	Timestamp            time.Time      `json:"timestamp"`
	DeploymentID         string         `json:"deploymentID,omitempty"`
	AppName              string         `json:"appName,omitempty"`
	Domains              []string       `json:"domains,omitempty"`
	Fields               map[string]any `json:"fields,omitempty"`
	IsDeploymentComplete bool           `json:"isDeploymentComplete,omitempty"`
	IsDeploymentFailed   bool           `json:"isDeploymentFailed,omitempty"`
	IsDeploymentSuccess  bool           `json:"isDeploymentSuccess,omitempty"`
	IsHaloydInitComplete bool           `json:"isHaloydInitComplete,omitempty"`
}

// StreamPublisher defines the interface for publishing log entries to streams
type StreamPublisher interface {
	Publish(entry LogEntry)

	SubscribeGeneral() (<-chan LogEntry, string)
	UnsubscribeGeneral(subscriberID string)

	SubscribeDeployment(deploymentID string) <-chan LogEntry
	UnsubscribeDeployment(deploymentID string)

	Close()
}

// LogBroker manages log streams for different deployment IDs
type LogBroker struct {
	streams map[string]chan LogEntry // subscriberID -> channel
	buffer  []LogEntry               // Buffer for historical logs

	deploymentStreams map[string]chan LogEntry // One channel per deployment ID
	deploymentBuffer  map[string][]LogEntry

	maxBuffer        int // Maximum buffered logs
	subscriberIDSeed int
	mutex            sync.RWMutex
	closed           bool
}

// NewLogBroker creates a new log broker
func NewLogBroker() StreamPublisher {
	return &LogBroker{
		streams:           make(map[string]chan LogEntry),
		buffer:            make([]LogEntry, 0),
		deploymentStreams: make(map[string]chan LogEntry),
		deploymentBuffer:  make(map[string][]LogEntry),
		maxBuffer:         100,
		subscriberIDSeed:  1,
	}
}

// Publish publishes a log entry to the general stream and deployment-specific streams
func (lb *LogBroker) Publish(entry LogEntry) {
	lb.mutex.Lock()
	defer lb.mutex.Unlock()

	if lb.closed {
		return
	}

	lb.buffer = append(lb.buffer, entry)
	if len(lb.buffer) > lb.maxBuffer {
		lb.buffer = lb.buffer[len(lb.buffer)-lb.maxBuffer:]
	}

	// Send to all general subscribers
	for subscriberID, ch := range lb.streams {
		select {
		case ch <- entry:
		default:
			// Channel full, close slow subscriber
			close(ch)
			delete(lb.streams, subscriberID)
		}
	}

	if entry.DeploymentID != "" {
		lb.publishToDeployment(entry.DeploymentID, entry)
	}
}

// publishToDeployment is a private helper for deployment-specific publishing
func (lb *LogBroker) publishToDeployment(deploymentID string, entry LogEntry) {
	buffer := lb.deploymentBuffer[deploymentID]
	buffer = append(buffer, entry)
	if len(buffer) > lb.maxBuffer {
		buffer = buffer[len(buffer)-lb.maxBuffer:]
	}
	lb.deploymentBuffer[deploymentID] = buffer

	// Send to deployment subscriber if exists
	if ch, exists := lb.deploymentStreams[deploymentID]; exists {
		select {
		case ch <- entry:
		default:
			// Channel full, close slow subscriber
			close(ch)
			delete(lb.deploymentStreams, deploymentID)
			delete(lb.deploymentBuffer, deploymentID)
		}
	}
}

// SubscribeGeneral creates a subscription for all logs and returns the channel and subscriber ID
func (lb *LogBroker) SubscribeGeneral() (<-chan LogEntry, string) {
	lb.mutex.Lock()
	defer lb.mutex.Unlock()

	if lb.closed {
		ch := make(chan LogEntry)
		close(ch)
		return ch, ""
	}

	// Generate unique subscriber ID
	subscriberID := lb.generateSubscriberID()

	// Create new general subscription
	ch := make(chan LogEntry, 100)

	// Copy buffered general logs
	var bufferCopy []LogEntry
	if len(lb.buffer) > 0 {
		bufferCopy = make([]LogEntry, len(lb.buffer))
		copy(bufferCopy, lb.buffer)
	}

	// Store the channel
	lb.streams[subscriberID] = ch

	// Send buffered logs in a separate goroutine
	if len(bufferCopy) > 0 {
		go func() {
			for _, entry := range bufferCopy {
				select {
				case ch <- entry:
				case <-time.After(2 * time.Second):
					// Timeout, subscriber is too slow
					lb.mutex.Lock()
					if existingCh, exists := lb.streams[subscriberID]; exists && existingCh == ch {
						close(ch)
						delete(lb.streams, subscriberID)
					}
					lb.mutex.Unlock()
					return
				}
			}
		}()
	}

	return ch, subscriberID
}

// UnsubscribeGeneral removes a specific general subscriber
func (lb *LogBroker) UnsubscribeGeneral(subscriberID string) {
	lb.mutex.Lock()
	defer lb.mutex.Unlock()

	if ch, exists := lb.streams[subscriberID]; exists {
		close(ch)
		delete(lb.streams, subscriberID)
	}
}

// SubscribeDeployment creates a new subscription for a deployment ID
func (lb *LogBroker) SubscribeDeployment(deploymentID string) <-chan LogEntry {
	lb.mutex.Lock()
	defer lb.mutex.Unlock()

	if lb.closed {
		ch := make(chan LogEntry)
		close(ch)
		return ch
	}

	// Check if already subscribed
	if existingCh, exists := lb.deploymentStreams[deploymentID]; exists {
		return existingCh
	}

	// Create new subscription
	ch := make(chan LogEntry, 100)

	// Copy buffered logs
	var bufferCopy []LogEntry
	if buffer, exists := lb.deploymentBuffer[deploymentID]; exists && len(buffer) > 0 {
		bufferCopy = make([]LogEntry, len(buffer))
		copy(bufferCopy, buffer)
	}

	// Store the channel
	lb.deploymentStreams[deploymentID] = ch

	// Send buffered logs in a separate goroutine
	if len(bufferCopy) > 0 {
		go func() {
			for _, entry := range bufferCopy {
				select {
				case ch <- entry:
				case <-time.After(2 * time.Second):
					// Timeout, close the channel
					lb.mutex.Lock()
					if existingCh, exists := lb.deploymentStreams[deploymentID]; exists && existingCh == ch {
						close(ch)
						delete(lb.deploymentStreams, deploymentID)
						delete(lb.deploymentBuffer, deploymentID)
					}
					lb.mutex.Unlock()
					return
				}
			}
		}()
	}

	return ch
}

// UnsubscribeDeployment removes a deployment subscriber
func (lb *LogBroker) UnsubscribeDeployment(deploymentID string) {
	lb.mutex.Lock()
	defer lb.mutex.Unlock()

	if ch, exists := lb.deploymentStreams[deploymentID]; exists {
		close(ch)
		delete(lb.deploymentStreams, deploymentID)
	}
	delete(lb.deploymentBuffer, deploymentID)
}

// Close shuts down the log broker and closes all channels
func (lb *LogBroker) Close() {
	lb.mutex.Lock()
	defer lb.mutex.Unlock()

	if lb.closed {
		return
	}

	lb.closed = true

	// Close all general streams
	for subscriberID, ch := range lb.streams {
		close(ch)
		delete(lb.streams, subscriberID)
	}

	// Close all deployment streams
	for deploymentID, ch := range lb.deploymentStreams {
		close(ch)
		delete(lb.deploymentStreams, deploymentID)
	}

	// Clear buffers
	lb.buffer = nil
	lb.deploymentBuffer = nil
}

// generateSubscriberID creates a unique subscriber ID
func (lb *LogBroker) generateSubscriberID() string {
	id := lb.subscriberIDSeed
	lb.subscriberIDSeed++
	return fmt.Sprintf("general_%d", id)
}

// StreamHandler wraps another slog.Handler and publishes logs to streams
type StreamHandler struct {
	publisher       StreamPublisher
	next            slog.Handler
	persistentAttrs []slog.Attr
}

// NewStreamHandler creates a new streaming handler
func NewStreamHandler(publisher StreamPublisher, next slog.Handler) slog.Handler {
	return &StreamHandler{
		publisher:       publisher,
		next:            next,
		persistentAttrs: []slog.Attr{},
	}
}

// Handle processes log records and publishes them to streams
func (sh *StreamHandler) Handle(ctx context.Context, rec slog.Record) error {
	// Extract deployment ID and other fields
	var deploymentID, appName string
	var isDeploymentComplete, isDeploymentFailed, isDeploymentSuccess, isHaloydInitComplete bool
	var domains []string
	fields := make(map[string]any)

	// Process persistent attributes from With() calls
	for _, attr := range sh.persistentAttrs {
		switch attr.Key {
		case AttrDeploymentID:
			deploymentID = attr.Value.String()
		case AttrDeploymentComplete:
			isDeploymentComplete = attr.Value.Bool()
		case AttrDeploymentFailed:
			isDeploymentFailed = attr.Value.Bool()
		case AttrDeploymentSuccess:
			isDeploymentSuccess = attr.Value.Bool()
		case AttrHaloydInitComplete:
			isHaloydInitComplete = attr.Value.Bool()
		case AttrAppName, AttrApp: // Handle both "appName" and "app"
			appName = attr.Value.String()
		case AttrDomains:
			if arr, ok := attr.Value.Any().([]string); ok {
				domains = arr
			}
		case AttrError:
			if err, ok := attr.Value.Any().(error); ok && err != nil {
				fields[attr.Key] = err.Error()
			} else {
				fields[attr.Key] = attr.Value.String()
			}
		default:
			fields[attr.Key] = attr.Value.String()
		}
	}

	// Process record attributes (these can override persistent ones)
	rec.Attrs(func(a slog.Attr) bool {
		switch a.Key {
		case AttrDeploymentID:
			deploymentID = a.Value.String()
		case AttrDeploymentComplete:
			isDeploymentComplete = a.Value.Bool()
		case AttrDeploymentFailed:
			isDeploymentFailed = a.Value.Bool()
		case AttrDeploymentSuccess:
			isDeploymentSuccess = a.Value.Bool()
		case AttrHaloydInitComplete:
			isHaloydInitComplete = a.Value.Bool()
		case AttrAppName, AttrApp: // Handle both "appName" and "app"
			appName = a.Value.String()
		case AttrDomains:
			if arr, ok := a.Value.Any().([]string); ok {
				domains = arr
			}
		case AttrError:
			if err, ok := a.Value.Any().(error); ok {
				fields[a.Key] = err.Error()
			} else {
				fields[a.Key] = a.Value.String()
			}
		default:
			fields[a.Key] = a.Value.String()
		}
		return true
	})

	// Create single log entry that routes automatically
	entry := LogEntry{
		Level:                rec.Level.String(),
		Message:              rec.Message,
		Timestamp:            rec.Time,
		DeploymentID:         deploymentID,
		AppName:              appName,
		Domains:              domains,
		Fields:               fields,
		IsDeploymentComplete: isDeploymentComplete,
		IsDeploymentFailed:   isDeploymentFailed,
		IsDeploymentSuccess:  isDeploymentSuccess,
		IsHaloydInitComplete: isHaloydInitComplete,
	}

	// Single publish call handles all routing
	if sh.publisher != nil {
		sh.publisher.Publish(entry)
	}

	// Pass to next handler (console output)
	if sh.next != nil {
		return sh.next.Handle(ctx, rec)
	}
	return nil
}

// WithAttrs creates a new handler with additional persistent attributes
func (sh *StreamHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	// Combine existing persistent attributes with new ones
	newAttrs := make([]slog.Attr, len(sh.persistentAttrs)+len(attrs))
	copy(newAttrs, sh.persistentAttrs)
	copy(newAttrs[len(sh.persistentAttrs):], attrs)

	newHandler := &StreamHandler{
		publisher:       sh.publisher,
		persistentAttrs: newAttrs,
	}

	// Also call WithAttrs on the next handler
	if sh.next != nil {
		newHandler.next = sh.next.WithAttrs(attrs)
	}

	return newHandler
}

// Enabled delegates to the next handler
func (sh *StreamHandler) Enabled(ctx context.Context, level slog.Level) bool {
	if sh.next != nil {
		return sh.next.Enabled(ctx, level)
	}
	return true
}

// WithGroup creates a new handler with a group
func (sh *StreamHandler) WithGroup(name string) slog.Handler {
	newHandler := &StreamHandler{
		publisher:       sh.publisher,
		persistentAttrs: sh.persistentAttrs, // Keep persistent attrs through groups
	}

	if sh.next != nil {
		newHandler.next = sh.next.WithGroup(name)
	}

	return newHandler
}
