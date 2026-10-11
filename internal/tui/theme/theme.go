package theme

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// Surfaces, from the page outward. A panel that sits beside the page is
// darker than it; what is laid on the page is lighter.
var (
	Background = lipgloss.Color("#1f1d27") // the page
	Surface    = lipgloss.Color("#1b1a22") // side panels
	Inset      = lipgloss.Color("#191720") // a command set apart from the text around it
	Card       = lipgloss.Color("#24222d") // a card, and the header of a quiet tile
	CardActive = lipgloss.Color("#2a2735") // the selected card or row
	Bar        = lipgloss.Color("#26232f") // the key bar, and a chip
	Border     = lipgloss.Color("#3a3648")
)

// Text, from what is read first to what only separates.
var (
	TextPrimary   = lipgloss.Color("#dcd9e6")
	TextSecondary = lipgloss.Color("#7d7896")
	TextFaint     = lipgloss.Color("#57536a")

	// TextTertiary is the old name of the quiet text color. Text in it has to
	// stay readable, so it is the secondary color; separators use TextFaint.
	TextTertiary = TextSecondary
)

// Accent and the colors that carry a meaning.
var (
	Accent    = lipgloss.Color("#b79cf7")
	AccentAlt = lipgloss.Color("#d4c4ff")

	Green  = lipgloss.Color("#8fd18b") // running, passed, added
	Amber  = lipgloss.Color("#f0b35a") // needs the person
	Blue   = lipgloss.Color("#7aa7ff") // finished and handed over
	Red    = lipgloss.Color("#f08a8a") // failed, removed
	Yellow = lipgloss.Color("#e6d76a") // in progress, a command
	Cyan   = lipgloss.Color("#74c7d4") // waiting on its own background work

	// What a strip of each meaning is laid on.
	TintGreen = lipgloss.Color("#1f2a24")
	TintAmber = lipgloss.Color("#2e2820")
	TintBlue  = lipgloss.Color("#1f2433")
	TintRed   = lipgloss.Color("#3a2228")
	TintCyan  = lipgloss.Color("#1c2a2e")

	AccentPurple    = Accent
	AccentPurpleAlt = AccentAlt
)

// Sync state colors
var (
	SyncOK      = Green
	SyncActive  = Yellow
	SyncError   = Red
	SyncOffline = TextSecondary
)

// An agent whose turn ended while a shell or subagent it started still runs.
var AgentBackground = Cyan

// Column colors
var (
	ColBacklog = lipgloss.Color("#8a869c")
	ColTriage  = lipgloss.Color("#b0a98a")
	ColReady   = Blue
	ColDoing   = Accent
	ColReview  = lipgloss.Color("#5fcfa4")
	ColDone    = lipgloss.Color("#9bc85a")
	ColOther   = TextFaint
)

// Styles
var (
	KeyHintKey = lipgloss.NewStyle().
			Foreground(TextPrimary)

	KeyHintLabel = lipgloss.NewStyle().
			Foreground(TextSecondary)

	// SectionLabel is the small capital heading of a group.
	SectionLabel = lipgloss.NewStyle().
			Foreground(TextSecondary)
)

// ColumnBorderColor returns the color a board column is marked with.
func ColumnBorderColor(column string) lipgloss.Color {
	switch strings.ToLower(column) {
	case "backlog":
		return ColBacklog
	case "triage":
		return ColTriage
	case "ready":
		return ColReady
	case "doing":
		return ColDoing
	case "review":
		return ColReview
	case "done":
		return ColDone
	default:
		return ColOther
	}
}

// PriorityColor returns the color of a priority's mark. A task with no
// priority has a faint one.
func PriorityColor(priority string) lipgloss.Color {
	switch priority {
	case "High", "Highest":
		return Red
	case "Medium":
		return Amber
	case "Low", "Lowest":
		return Blue
	default:
		return TextFaint
	}
}

// StateColor returns the color of an agent activity, and StateTint the
// background of a strip that reports it.
func StateColor(activity string) lipgloss.Color {
	switch activity {
	case "working":
		return Green
	case "waiting":
		return Amber
	case "background":
		return Cyan
	case "error", "dead":
		return Red
	case "done":
		return Blue
	case "mixed":
		return Accent
	default:
		return TextFaint
	}
}

func StateTint(activity string) lipgloss.Color {
	switch activity {
	case "working":
		return TintGreen
	case "waiting":
		return TintAmber
	case "background":
		return TintCyan
	case "error", "dead":
		return TintRed
	case "done":
		return TintBlue
	default:
		return Card
	}
}
