package logging

import (
	"log/slog"
	"os"
)

// Log attribute keys used for structured logging and streaming
const (
	// Deployment attributes
	AttrDeploymentID       = "deploymentID"
	AttrDeploymentComplete = "deploymentComplete"
	AttrDeploymentFailed   = "deploymentFailed"
	AttrDeploymentSuccess  = "deploymentSuccess"

	// haloyd attributes
	AttrHaloydInitComplete = "haloydInitComplete"

	// App attributes
	AttrAppName = "appName"
	AttrApp     = "app"
	AttrDomains = "domains"

	// General attributes
	AttrError = "error"
)

// NewLogger creates a new slog.Logger with optional streaming
func NewLogger(level slog.Level, publisher StreamPublisher) *slog.Logger {
	// Create base handler (console output)
	opts := &slog.HandlerOptions{Level: level}
	baseHandler := slog.NewTextHandler(os.Stdout, opts)

	if publisher != nil {
		handler := NewStreamHandler(publisher, baseHandler)
		return slog.New(handler)
	}

	return slog.New(baseHandler)
}

// NewDeploymentLogger creates a logger with persistent deploymentID
func NewDeploymentLogger(deploymentID string, level slog.Level, publisher StreamPublisher) *slog.Logger {
	logger := NewLogger(level, publisher)
	if deploymentID != "" {
		return logger.With("deploymentID", deploymentID)
	}
	return logger
}

// LogFatal logs an error and exits the program
func LogFatal(logger *slog.Logger, message string, args ...any) {
	logger.Error(message, args...)
	os.Exit(1)
}

// LogDeploymentComplete marks a deployment as successfully completed
// This sends the completion signal that tells CLI clients to stop streaming
func LogDeploymentComplete(logger *slog.Logger, domains []string, deploymentID, appName, message string) {
	logger.Info(message,
		AttrApp, appName,
		AttrDeploymentID, deploymentID,
		AttrDomains, domains,
		AttrDeploymentComplete, true,
		AttrDeploymentSuccess, true,
	)
}

// LogDeploymentFailed marks a deployment as failed
// This sends the failure signal that tells CLI clients to stop streaming with error
func LogDeploymentFailed(logger *slog.Logger, deploymentID, appName, message string, err error) {
	logger.Error(message,
		AttrApp, appName,
		AttrDeploymentID, deploymentID,
		AttrError, err,
		AttrDeploymentComplete, true, // Also end stream on failure
		AttrDeploymentFailed, true,
	)
}
