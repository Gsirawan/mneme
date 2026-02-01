package main

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// Colors
var (
	amber    = lipgloss.Color("#FFB347") // warm amber
	gold     = lipgloss.Color("#FFD700")
	lilac    = lipgloss.Color("#C4A7E7") // soft purple
	dimGray  = lipgloss.Color("#666666")
	softGray = lipgloss.Color("#888888")
	green    = lipgloss.Color("#A6E3A1")
	red      = lipgloss.Color("#F38BA8")
	cyan     = lipgloss.Color("#89DCEB")
	white    = lipgloss.Color("#CDD6F4")
)

// Styles
var (
	// Header / branding
	titleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(gold).
			PaddingBottom(1)

	subtitleStyle = lipgloss.NewStyle().
			Foreground(softGray).
			Italic(true)

	// Session picker
	sessionNumStyle = lipgloss.NewStyle().
			Foreground(amber).
			Bold(true)

	sessionTitleStyle = lipgloss.NewStyle().
				Foreground(white).
				Bold(true)

	sessionSlugStyle = lipgloss.NewStyle().
				Foreground(dimGray)

	sessionDateStyle = lipgloss.NewStyle().
				Foreground(softGray)

	promptStyle = lipgloss.NewStyle().
			Foreground(amber).
			Bold(true)

	// Preflight steps
	stepOK = lipgloss.NewStyle().
		Foreground(green).
		Bold(true).
		Render("  OK ")

	stepFail = lipgloss.NewStyle().
			Foreground(red).
			Bold(true).
			Render(" FAIL")

	stepWait = lipgloss.NewStyle().
			Foreground(amber).
			Render("  .. ")

	stepLabelStyle = lipgloss.NewStyle().
			Foreground(white)

	// Message boxes
	userBoxStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(amber).
			PaddingLeft(1).
			PaddingRight(1).
			Width(72)

	assistantBoxStyle = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(lilac).
				PaddingLeft(1).
				PaddingRight(1).
				Width(72)

	userNameStyle = lipgloss.NewStyle().
			Foreground(amber).
			Bold(true)

	assistantNameStyle = lipgloss.NewStyle().
				Foreground(lilac).
				Bold(true)

	timeStyle = lipgloss.NewStyle().
			Foreground(dimGray)

	// Ingestion
	ingestStyle = lipgloss.NewStyle().
			Foreground(gold).
			Bold(true)

	// Info line (watching status, skip count, etc.)
	infoStyle = lipgloss.NewStyle().
			Foreground(softGray)

	infoHighlightStyle = lipgloss.NewStyle().
				Foreground(cyan).
				Bold(true)
)

// renderHeader prints the mneme watch banner
func renderHeader() string {
	return titleStyle.Render("Mneme Watch") + "\n" +
		subtitleStyle.Render("Live session → Mneme memory")
}

// renderSessionItem formats a single session line for the picker
func renderSessionItem(index int, title, slug, date string) string {
	num := sessionNumStyle.Render(fmt.Sprintf(" %d.", index))
	t := sessionTitleStyle.Render(title)
	s := sessionSlugStyle.Render(slug)
	d := sessionDateStyle.Render(fmt.Sprintf("[%s]", date))
	return fmt.Sprintf("%s %s %s %s", num, t, s, d)
}

// renderMessage formats a message in a colored box
func renderMessage(role, timestamp, text string, isUser bool) string {
	maxPreview := 200
	if len(text) > maxPreview {
		text = text[:maxPreview] + "..."
	}

	var nameStyle lipgloss.Style
	var boxStyle lipgloss.Style
	if isUser {
		nameStyle = userNameStyle
		boxStyle = userBoxStyle
	} else {
		nameStyle = assistantNameStyle
		boxStyle = assistantBoxStyle
	}

	header := nameStyle.Render(role) + " " + timeStyle.Render(timestamp)
	content := header + "\n" + text

	return boxStyle.Render(content)
}

// renderIngest formats the ingestion status line
func renderIngest(count int, batchNum int) string {
	return ingestStyle.Render(fmt.Sprintf("  Ingesting %d messages... done! (batch %d)", count, batchNum))
}

// renderPreflight formats a preflight check step
func renderPreflightStep(status, label string) string {
	var badge string
	switch status {
	case "ok":
		badge = stepOK
	case "fail":
		badge = stepFail
	default:
		badge = stepWait
	}
	return fmt.Sprintf("%s %s", badge, stepLabelStyle.Render(label))
}

// renderWatchStatus formats the "Watching: ..." info block
func renderWatchStatus(title, sessionID string, batchSize, pollSec int, dbPath string) string {
	var b strings.Builder
	b.WriteString(infoHighlightStyle.Render(fmt.Sprintf("  Watching: %s", title)))
	b.WriteString("\n")
	b.WriteString(infoStyle.Render(fmt.Sprintf("  Session:  %s", sessionID)))
	b.WriteString("\n")
	b.WriteString(infoStyle.Render(fmt.Sprintf("  Batch: %d messages | Poll: %ds | DB: %s", batchSize, pollSec, dbPath)))
	b.WriteString("\n")
	b.WriteString(infoStyle.Render("  Ctrl+C to stop."))
	return b.String()
}
