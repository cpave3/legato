package theme

import "github.com/charmbracelet/lipgloss"

// Color palette
var (
	Background    = lipgloss.Color("#1E1E2E")
	TextPrimary   = lipgloss.Color("#CDD6F4")
	TextSecondary = lipgloss.Color("#A6ADC8")
	TextTertiary  = lipgloss.Color("#6C7086")

	AccentPurple    = lipgloss.Color("#7F77DD")
	AccentPurpleAlt = lipgloss.Color("#AFA9EC")

	// Priority colors
	PriorityHighBg = lipgloss.Color("#FAECE7")
	PriorityHighFg = lipgloss.Color("#993C1D")
	PriorityMedBg  = lipgloss.Color("#FAEEDA")
	PriorityMedFg  = lipgloss.Color("#854F0B")
	PriorityLowBg  = lipgloss.Color("#E1F5EE")
	PriorityLowFg  = lipgloss.Color("#0F6E56")
	PriorityNone   = lipgloss.Color("#B4B2A9")

	// Sync state colors
	SyncOK      = lipgloss.Color("#1D9E75")
	SyncActive  = lipgloss.Color("#E5C07B")
	SyncError   = lipgloss.Color("#E06C75")
	SyncOffline = lipgloss.Color("#6C7086")

	// Column border colors
	ColBacklog = lipgloss.Color("#B4B2A9")
	ColReady   = lipgloss.Color("#85B7EB")
	ColDoing   = lipgloss.Color("#7F77DD")
	ColReview  = lipgloss.Color("#5DCAA5")
	ColDone    = lipgloss.Color("#97C459")
)

// Styles
var (
	CardBase = lipgloss.NewStyle().
			Background(lipgloss.Color("#252540")).
			Foreground(TextPrimary).
			Padding(0, 1).
			MarginBottom(1).
			BorderStyle(lipgloss.RoundedBorder()).
			BorderForeground(TextTertiary)

	CardSelected = lipgloss.NewStyle().
			Background(lipgloss.Color("#EEEDFE")).
			Foreground(lipgloss.Color("#1E1E2E")).
			Padding(0, 1).
			MarginBottom(1).
			BorderStyle(lipgloss.RoundedBorder()).
			BorderForeground(AccentPurple)

	ColumnHeader = lipgloss.NewStyle().
			Foreground(TextSecondary).
			Bold(true).
			PaddingLeft(1)

	ColumnHeaderActive = lipgloss.NewStyle().
				Foreground(AccentPurple).
				Bold(true).
				PaddingLeft(1)

	StatusBar = lipgloss.NewStyle().
			Foreground(TextSecondary).
			Background(Background).
			BorderTop(true).
			BorderStyle(lipgloss.NormalBorder()).
			BorderForeground(TextTertiary).
			PaddingLeft(1).
			PaddingRight(1)

	KeyHintKey = lipgloss.NewStyle().
			Foreground(AccentPurple).
			Bold(true)

	KeyHintLabel = lipgloss.NewStyle().
			Foreground(TextTertiary)

	DoneMuted = lipgloss.NewStyle().
			Foreground(TextTertiary).
			Strikethrough(true)

	// Priority badge styles — subtle tinted text, no background
	PriorityBadgeHigh = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#E06C75"))

	PriorityBadgeMed = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#E5C07B"))

	PriorityBadgeLow = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#5DCAA5"))

	TypeBadge = lipgloss.NewStyle().
			Foreground(TextTertiary)
)

// ColumnBorderColor returns the border color for a given column name.
func ColumnBorderColor(column string) lipgloss.Color {
	switch column {
	case "Backlog":
		return ColBacklog
	case "Ready":
		return ColReady
	case "Doing":
		return ColDoing
	case "Review":
		return ColReview
	case "Done":
		return ColDone
	default:
		return PriorityNone
	}
}

// PriorityBorderColor returns the border color for a given priority level.
func PriorityBorderColor(priority string) lipgloss.Color {
	switch priority {
	case "High", "Highest":
		return PriorityHighFg
	case "Medium":
		return PriorityMedFg
	case "Low", "Lowest":
		return PriorityLowFg
	default:
		return PriorityNone
	}
}
