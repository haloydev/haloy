package ui

import (
	"fmt"
	"os"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/charmbracelet/lipgloss"
)

// ProgressBar displays an aggregate progress bar that can be updated from multiple goroutines
type ProgressBar struct {
	total        int64
	current      atomic.Int64
	completed    atomic.Int32
	totalItems   int32
	description  string
	mu           sync.Mutex
	lastLineLen  int
	showBytes    bool
}

// ProgressBarConfig configures the progress bar display
type ProgressBarConfig struct {
	Description string
	TotalBytes  int64
	TotalItems  int
	ShowBytes   bool // if true, shows bytes; if false, shows items only
}

// NewProgressBar creates a new aggregate progress bar
func NewProgressBar(cfg ProgressBarConfig) *ProgressBar {
	pb := &ProgressBar{
		total:       cfg.TotalBytes,
		totalItems:  int32(cfg.TotalItems),
		description: cfg.Description,
		showBytes:   cfg.ShowBytes,
	}
	pb.render()
	return pb
}

// Add adds bytes to the progress (thread-safe)
func (p *ProgressBar) Add(n int64) {
	p.current.Add(n)
	p.render()
}

// CompleteItem marks one item as complete (thread-safe)
func (p *ProgressBar) CompleteItem() {
	p.completed.Add(1)
	p.render()
}

// Finish completes the progress bar and moves to next line
func (p *ProgressBar) Finish() {
	p.mu.Lock()
	defer p.mu.Unlock()
	// Clear the line and print final state
	fmt.Fprint(os.Stdout, "\r"+strings.Repeat(" ", p.lastLineLen)+"\r")
}

func (p *ProgressBar) render() {
	p.mu.Lock()
	defer p.mu.Unlock()

	current := p.current.Load()
	completed := p.completed.Load()

	// Calculate percentage
	var percent float64
	if p.total > 0 {
		percent = float64(current) / float64(p.total) * 100
	}
	if percent > 100 {
		percent = 100
	}

	// Build progress bar
	barWidth := 20
	filledWidth := min(int(percent/100*float64(barWidth)), barWidth)

	filled := strings.Repeat("█", filledWidth)
	empty := strings.Repeat("░", barWidth-filledWidth)
	bar := filled + empty

	// Build status text
	var status string
	if p.showBytes && p.total > 0 {
		status = fmt.Sprintf("%d/%d (%s / %s)",
			completed, p.totalItems,
			formatBytes(current), formatBytes(p.total))
	} else {
		status = fmt.Sprintf("%d/%d", completed, p.totalItems)
	}

	// Format the line
	prefix := lipgloss.NewStyle().Foreground(Blue).Render("●")
	line := fmt.Sprintf("\r%s %s [%s] %s", prefix, p.description, bar, status)

	// Pad to clear previous content
	if len(line) < p.lastLineLen {
		line += strings.Repeat(" ", p.lastLineLen-len(line))
	}
	p.lastLineLen = len(line)

	fmt.Fprint(os.Stdout, line)
}

// formatBytes formats bytes into human-readable format
func formatBytes(b int64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(b)/float64(div), "KMGTPE"[exp])
}
