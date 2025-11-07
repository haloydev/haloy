package helpers

import (
	"fmt"
	"time"

	"github.com/oklog/ulid"
)

// GetTimestampFromDeploymentID extracts time.Time from an ULID
func GetTimestampFromDeploymentID(deploymentID string) (time.Time, error) {
	parsedULID, err := ulid.Parse(deploymentID)
	if err != nil {
		return time.Time{}, fmt.Errorf("invalid deployment ID: %v", err)
	}

	return ulid.Time(parsedULID.Time()), nil
}

// FormatTime formats a time.Time in a simple, CLI-friendly format
// similar to Docker and Kubernetes tools (e.g., "2 minutes ago", "3 hours ago", "2 days ago")
func FormatTime(t time.Time) string {
	return FormatTimeWithLocation(t, time.Local)
}

// FormatDateString formats a date string in a simple, CLI-friendly format
// similar to Docker and Kubernetes tools (e.g., "2 minutes ago", "3 hours ago", "2 days ago")
func FormatDateString(dateString string) (string, error) {
	return FormatDateStringWithLocation(dateString, time.Local)
}

// FormatTimeWithLocation formats a time.Time for the specified timezone
func FormatTimeWithLocation(t time.Time, loc *time.Location) string {
	// Convert to specified location
	tInLoc := t.In(loc)
	nowInLoc := time.Now().In(loc)
	elapsed := nowInLoc.Sub(tInLoc)

	// Handle future dates
	if elapsed < 0 {
		elapsed = -elapsed
		return formatDuration(elapsed) + " from now"
	}

	// Format like Docker/Kubernetes
	return formatDuration(elapsed) + " ago"
}

// FormatDateStringWithLocation formats a date string for the specified timezone
func FormatDateStringWithLocation(dateString string, loc *time.Location) (string, error) {
	var t time.Time
	var err error

	switch len(dateString) {
	case 14:
		t, err = time.ParseInLocation("20060102150405", dateString, loc)
	case 16: // with centiseconds
		t, err = time.ParseInLocation("20060102150405", dateString[:14], loc)
	default:
		// Try RFC3339 and other formats
		layouts := []string{time.RFC3339, time.RFC3339Nano}
		for _, layout := range layouts {
			t, err = time.Parse(layout, dateString)
			if err == nil {
				break
			}
		}
	}
	if err != nil {
		return "", fmt.Errorf("failed to parse date string %q: %w", dateString, err)
	}

	// Delegate to FormatTimeWithLocation for the actual formatting
	return FormatTimeWithLocation(t, loc), nil
}

func formatDuration(d time.Duration) string {
	if d < time.Minute {
		seconds := int(d.Seconds())
		if seconds <= 1 {
			return "1 second"
		}
		return fmt.Sprintf("%d seconds", seconds)
	}

	if d < time.Hour {
		minutes := int(d.Minutes())
		if minutes == 1 {
			return "1 minute"
		}
		return fmt.Sprintf("%d minutes", minutes)
	}

	if d < 24*time.Hour {
		hours := int(d.Hours())
		if hours == 1 {
			return "1 hour"
		}
		return fmt.Sprintf("%d hours", hours)
	}

	if d < 30*24*time.Hour { // Less than ~30 days
		days := int(d.Hours() / 24)
		if days == 1 {
			return "1 day"
		}
		return fmt.Sprintf("%d days", days)
	}

	if d < 365*24*time.Hour { // Less than a year
		months := int(d.Hours() / (24 * 30)) // Rough approximation
		if months == 1 {
			return "1 month"
		}
		return fmt.Sprintf("%d months", months)
	}

	years := int(d.Hours() / (24 * 365))
	if years == 1 {
		return "1 year"
	}
	return fmt.Sprintf("%d years", years)
}
