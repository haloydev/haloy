package ui

import (
	"fmt"
	"strings"

	"github.com/haloydev/haloy/internal/logging"
)

func DisplayLogEntry(logEntry logging.LogEntry, prefix string) {
	message := logEntry.Message
	isSuccess := logEntry.IsDeploymentSuccess
	domains := logEntry.Domains

	// Handle HTTP request logs specially
	if logEntry.Message == "request" {
		method, _ := logEntry.Fields["method"].(string)
		host, _ := logEntry.Fields["host"].(string)
		path, _ := logEntry.Fields["path"].(string)
		status, _ := logEntry.Fields["status"].(string)
		durationMs, _ := logEntry.Fields["duration_ms"].(string)

		if method != "" && host != "" {
			message = fmt.Sprintf("%s %s%s → %s (%sms)", method, host, path, status, durationMs)
		}
	}

	// Include error details from Fields if available
	if errorMsg, exists := logEntry.Fields["error"]; exists {
		if errorStr, ok := errorMsg.(string); ok && errorStr != "" {
			message = fmt.Sprintf("%s: %s", message, errorStr)
		}
	}

	if prefix != "" {
		message = fmt.Sprintf("%s%s", prefix, message)
	}

	switch strings.ToUpper(logEntry.Level) {
	case "ERROR":
		Error("%s", message)
	case "WARN":
		Warn("%s", message)
	case "INFO":
		if isSuccess {
			if len(domains) > 0 {
				urls := make([]string, len(domains))
				for i, domain := range domains {
					urls[i] = fmt.Sprintf("https://%s", domain)
				}
				message = fmt.Sprintf("%s → %s", message, strings.Join(urls, ", "))
			}
			Success("%s", message)
		} else {
			if len(domains) > 0 {
				message = fmt.Sprintf("%s (domains: %s)", message, strings.Join(domains, ", "))
			}
			Info("%s", message)
		}
	case "DEBUG":
		Debug("%s", message)
	default:
		fmt.Printf("%s\n", message)
	}
}
