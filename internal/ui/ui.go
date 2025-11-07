package ui

import (
	"fmt"
	"os"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/lipgloss/table"
)

// Colors
var (
	Amber     = lipgloss.Color("#F59E0B")
	Blue      = lipgloss.Color("#3B82F6")
	White     = lipgloss.Color("#fafafa")
	Gray      = lipgloss.Color("#6B7280")
	Green     = lipgloss.Color("#10B981")
	LightGray = lipgloss.Color("241")
	Red       = lipgloss.Color("#f87171")
	Purple    = lipgloss.Color("#8B5CF6")
)

// Styles
var (
	s          = lipgloss.NewStyle()
	titleStyle = s.Bold(true).Foreground(White)
)

func Basic(format string, a ...any) {
	printStyledLines(os.Stdout, "", s.Foreground(White), format, a...)
}

func Info(format string, a ...any) {
	printStyledLines(os.Stdout, s.Foreground(Blue).Render("●"), s.Foreground(White), format, a...)
}

func Success(format string, a ...any) {
	printStyledLines(os.Stdout, s.Foreground(Green).Render("●"), s.Foreground(White).Bold(true), format, a...)
}

func Debug(format string, a ...any) {
	printStyledLines(os.Stdout, s.Foreground(Purple).Render("◆"), s.Foreground(White), format, a...)
}

func Warn(format string, a ...any) {
	printStyledLines(os.Stderr, s.Foreground(Amber).Render("⚠"), s.Foreground(White), format, a...)
}

func Error(format string, a ...any) {
	printStyledLines(os.Stderr, s.Foreground(Red).Render("✖"), s.Foreground(White), format, a...)
}

var lineStyle = lipgloss.NewStyle().
	Foreground(White).
	TabWidth(5)

func Section(title string, textLines []string) {
	fmt.Println(titleStyle.BorderStyle(lipgloss.NormalBorder()).BorderForeground(White).BorderBottom(true).Render(title))
	for _, line := range textLines {
		fmt.Println(lineStyle.Render(line))
	}
}

func Table(headers []string, rows [][]string) {
	cellStyle := lipgloss.NewStyle().Padding(0, 1)

	t := table.New().
		Border(lipgloss.NormalBorder()).
		BorderStyle(lipgloss.NewStyle().Foreground(Gray)).
		StyleFunc(func(row, col int) lipgloss.Style {
			switch {
			case row == table.HeaderRow:
				return titleStyle.Align(lipgloss.Center)
			case row%2 == 0:
				return cellStyle.Foreground(LightGray)
			default:
				return cellStyle.Foreground(Gray)
			}
		}).
		Headers(headers...).
		Rows(rows...)
	fmt.Println(t)
}

func printStyledLines(output *os.File, prefix string, style lipgloss.Style, format string, a ...any) {
	msg := fmt.Sprintf(format, a...)
	lines := strings.SplitSeq(msg, "\n")
	for line := range lines {
		if line != "" {
			prefixedLine := fmt.Sprintf("%s %s", prefix, style.Render(line))
			fmt.Fprintln(output, prefixedLine)
		}
	}
}

type PrefixedUI struct {
	Prefix string
}

func (p *PrefixedUI) Info(format string, a ...any) {
	if p.Prefix != "" {
		format = "%s" + format
		a = append([]any{p.Prefix}, a...)
	}
	Info(format, a...)
}

func (p *PrefixedUI) Error(format string, a ...any) {
	if p.Prefix != "" {
		format = "%s" + format
		a = append([]any{p.Prefix}, a...)
	}
	Error(format, a...)
}

func (p *PrefixedUI) Success(format string, a ...any) {
	if p.Prefix != "" {
		format = "%s" + format
		a = append([]any{p.Prefix}, a...)
	}
	Success(format, a...)
}
