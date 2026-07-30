// Package render produces SVG tile images for Stream Deck keys.
//
// Each tile is a 144×144 SVG rendered to a base64 data URI. The SVG is
// downscaled to 72×72 by the hardware — so design for 72px legibility.
//
// Design philosophy: color and shape as primary signals, minimal text.
// Agent tiles use a rectangular layout:
// - Top: state bar (green=working, amber=waiting, gray=idle)
// - Middle: context pressure bar (green→amber→red as context fills)
// - Bottom: last 4 chars of task ID
// - Corner: purple dot if notify enabled
package render

import (
	"encoding/base64"
	"fmt"
	"strings"
)

const (
	tileSize = 144
	bgColor  = "#18181b" // zinc-900
)

// Base colors.
const (
	Green  = "#22c55e"
	Amber  = "#f59e0b"
	Red    = "#ef4444"
	Gray   = "#52525b"
	DarkGray = "#27272a"
	Purple = "#a855f7"
)

// DataURI wraps raw SVG bytes as a data URI.
func DataURI(svg string) string {
	return "data:image/svg+xml;base64," + base64.StdEncoding.EncodeToString([]byte(svg))
}

// lastChars returns the last n characters of s, or s if shorter.
func lastChars(s string, n int) string {
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	return string(runes[len(runes)-n:])
}

func escapeXML(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	s = strings.ReplaceAll(s, "\"", "&quot;")
	s = strings.ReplaceAll(s, "'", "&apos;")
	return s
}

// pressureColor returns a color for context pressure (0-1).
// 0-0.5: green, 0.5-0.75: amber, 0.75-1: red.
func pressureColor(pressure float64) string {
	if pressure < 0 {
		return DarkGray // unknown
	}
	switch {
	case pressure < 0.5:
		return Green
	case pressure < 0.75:
		return Amber
	default:
		return Red
	}
}

// Agent renders a rectangular tile for an agent.
//
// Layout (144×144):
// - Top bar (y=8-14): state color — always green for working, amber for waiting, gray for idle
// - Middle (y=30-110): context pressure bar — fills left-to-right, colored by pressure
// - Bottom (y=126): last 4 chars of task ID in monospace
// - Top-right corner: purple dot if notify enabled
func Agent(activity, taskID string, workingSeconds, waitingSeconds, contextPressure float64, notifyEnabled bool) string {
	// State color — working is always green regardless of elapsed time
	var stateColor string
	switch activity {
	case "working":
		stateColor = Green
	case "waiting":
		stateColor = Amber
	default:
		stateColor = Gray
	}

	// Top state bar
	stateBar := fmt.Sprintf(`<rect x="12" y="8" width="120" height="6" rx="3" fill="%s"/>`, stateColor)

	// Context pressure bar
	var pressureBar string
	if contextPressure < 0 {
		// Unknown — show a dim dashed bar
		pressureBar = `<rect x="20" y="40" width="104" height="60" rx="6" fill="none" stroke="#27272a" stroke-width="3" stroke-dasharray="6 4"/>`
	} else {
		// Fill proportionally
		barWidth := int(104 * contextPressure)
		if barWidth < 2 {
			barWidth = 2 // minimum visible
		}
		color := pressureColor(contextPressure)
		pressureBar = fmt.Sprintf(
			`<rect x="20" y="40" width="104" height="60" rx="6" fill="none" stroke="#27272a" stroke-width="3"/>
  <rect x="23" y="43" width="%d" height="54" rx="4" fill="%s" opacity="0.8"/>`,
			barWidth, color)
	}

	// ID text
	idText := escapeXML(lastChars(taskID, 4))
	idBar := fmt.Sprintf(`<text x="72" y="130" fill="#a1a1aa" font-family="monospace" font-size="18" font-weight="700" text-anchor="middle">%s</text>`,
		idText)

	// Notify dot (top-right corner)
	var notifyDot string
	if notifyEnabled {
		notifyDot = `<circle cx="128" cy="22" r="8" fill="#a855f7"/>`
	}

	return fmt.Sprintf(`<svg xmlns="http://www.w3.org/2000/svg" width="%d" height="%d" viewBox="0 0 %d %d">
  <rect width="%d" height="%d" rx="12" fill="%s"/>
  %s
  %s
  %s
  %s
</svg>`,
		tileSize, tileSize, tileSize, tileSize,
		tileSize, tileSize, bgColor,
		stateBar,
		pressureBar,
		notifyDot,
		idBar)
}

// AgentDead renders a tile for a dead/stopped agent — red X + task ID.
func AgentDead(taskID string) string {
	idText := escapeXML(lastChars(taskID, 4))
	return fmt.Sprintf(`<svg xmlns="http://www.w3.org/2000/svg" width="%d" height="%d" viewBox="0 0 %d %d">
  <rect width="%d" height="%d" rx="12" fill="%s"/>
  <line x1="40" y1="40" x2="80" y2="80" stroke="%s" stroke-width="9" stroke-linecap="round"/>
  <line x1="80" y1="40" x2="40" y2="80" stroke="%s" stroke-width="9" stroke-linecap="round"/>
  <text x="72" y="126" fill="#ef4444" font-family="monospace" font-size="18" font-weight="700" text-anchor="middle">%s</text>
</svg>`,
		tileSize, tileSize, tileSize, tileSize,
		tileSize, tileSize, bgColor,
		Red, Red, idText)
}

// Attention renders a purely visual tile for an attention item.
//
// Visual encoding:
// - Normal urgency: thin colored left bar + producer icon
// - Urgent: thick red border around entire tile + producer icon
// - Producer type: distinct geometric icon (document/eye/flow/bolt)
// - Last 4 chars of the related item's ID (from navigation.target) at bottom
func Attention(producer, urgency, navTarget string, index, total int) string {
	isUrgent := urgency == "urgent" || urgency == "high"

	var iconSVG string
	switch producer {
	case "plans":
		// Document shape
		iconSVG = `<rect x="50" y="30" width="44" height="56" rx="4" fill="none" stroke="{color}" stroke-width="5"/><path d="M60 48h24M60 62h24M60 76h16" stroke="{color}" stroke-width="4" stroke-linecap="round"/>`
	case "reviews":
		// Eye shape
		iconSVG = `<path d="M72 34C50 34 36 62 36 62s14 28 36 28 36-28 36-28-14-28-36-28z" fill="none" stroke="{color}" stroke-width="5"/><circle cx="72" cy="62" r="12" fill="{color}"/>`
	case "workflows":
		// Connected boxes (flow)
		iconSVG = `<rect x="38" y="30" width="30" height="30" rx="5" fill="none" stroke="{color}" stroke-width="5"/><rect x="76" y="64" width="30" height="30" rx="5" fill="none" stroke="{color}" stroke-width="5"/><path d="M68 45h24v19" stroke="{color}" stroke-width="5" stroke-linecap="round" fill="none"/>`
	case "workflow_triggers":
		// Lightning bolt
		iconSVG = `<path d="M78 28L52 70h20l-8 26 24-42H66z" fill="{color}"/>`
	default:
		// Filled square
		iconSVG = `<rect x="54" y="44" width="36" height="36" rx="8" fill="{color}"/>`
	}

	idText := escapeXML(lastChars(navTarget, 4))
	textColor := "#a1a1aa"

	if isUrgent {
		// Urgent: thick red border + red icon
		iconSVG = strings.ReplaceAll(iconSVG, "{color}", Red)
		textColor = "#ef4444"
		return fmt.Sprintf(`<svg xmlns="http://www.w3.org/2000/svg" width="%d" height="%d" viewBox="0 0 %d %d">
  <rect width="%d" height="%d" rx="12" fill="%s"/>
  <rect x="6" y="6" width="%d" height="%d" rx="8" fill="none" stroke="%s" stroke-width="6"/>
  %s
  <text x="72" y="130" fill="%s" font-family="monospace" font-size="20" font-weight="700" text-anchor="middle">%s</text>
</svg>`,
			tileSize, tileSize, tileSize, tileSize,
			tileSize, tileSize, bgColor,
			tileSize-12, tileSize-12, Red,
			iconSVG, textColor, idText)
	}

	// Normal: thin amber left bar + amber icon
	color := Amber
	iconSVG = strings.ReplaceAll(iconSVG, "{color}", color)
	return fmt.Sprintf(`<svg xmlns="http://www.w3.org/2000/svg" width="%d" height="%d" viewBox="0 0 %d %d">
  <rect width="%d" height="%d" rx="12" fill="%s"/>
  <rect x="0" y="0" width="10" height="%d" rx="0" fill="%s"/>
  %s
  <text x="72" y="130" fill="%s" font-family="monospace" font-size="20" font-weight="700" text-anchor="middle">%s</text>
</svg>`,
		tileSize, tileSize, tileSize, tileSize,
		tileSize, tileSize, bgColor,
		tileSize, color,
		iconSVG, textColor, idText)
}

// EmptyAgent renders a subtle dashed rectangle for an unused agent slot.
func EmptyAgent() string {
	return fmt.Sprintf(`<svg xmlns="http://www.w3.org/2000/svg" width="%d" height="%d" viewBox="0 0 %d %d">
  <rect width="%d" height="%d" rx="12" fill="%s"/>
  <rect x="20" y="20" width="104" height="104" rx="8" fill="none" stroke="%s" stroke-width="3" stroke-dasharray="8 6"/>
</svg>`,
		tileSize, tileSize, tileSize, tileSize,
		tileSize, tileSize, bgColor, DarkGray)
}

// EmptyAttention renders a subtle dashed square for an unused attention slot.
func EmptyAttention() string {
	return fmt.Sprintf(`<svg xmlns="http://www.w3.org/2000/svg" width="%d" height="%d" viewBox="0 0 %d %d">
  <rect width="%d" height="%d" rx="12" fill="%s"/>
  <rect x="38" y="38" width="68" height="68" rx="8" fill="none" stroke="%s" stroke-width="3" stroke-dasharray="8 6"/>
</svg>`,
		tileSize, tileSize, tileSize, tileSize,
		tileSize, tileSize, bgColor, DarkGray)
}

// Disconnected renders a broken-link icon (no text).
func Disconnected() string {
	return fmt.Sprintf(`<svg xmlns="http://www.w3.org/2000/svg" width="%d" height="%d" viewBox="0 0 %d %d">
  <rect width="%d" height="%d" rx="12" fill="%s"/>
  <path d="M56 64h-8v-8a16 16 0 0 1 16-16h8M88 80h8v8a16 16 0 0 1-16 16h-8" stroke="%s" stroke-width="6" stroke-linecap="round" fill="none"/>
  <line x1="58" y1="86" x2="86" y2="58" stroke="%s" stroke-width="6" stroke-linecap="round"/>
</svg>`,
		tileSize, tileSize, tileSize, tileSize,
		tileSize, tileSize, bgColor,
		Gray, Gray)
}

// NoToken renders a lock icon (no text).
func NoToken() string {
	return fmt.Sprintf(`<svg xmlns="http://www.w3.org/2000/svg" width="%d" height="%d" viewBox="0 0 %d %d">
  <rect width="%d" height="%d" rx="12" fill="%s"/>
  <rect x="54" y="60" width="36" height="32" rx="5" fill="none" stroke="%s" stroke-width="5"/>
  <path d="M60 60v-8a12 12 0 0 1 24 0v8" fill="none" stroke="%s" stroke-width="5"/>
  <circle cx="72" cy="74" r="5" fill="%s"/>
</svg>`,
		tileSize, tileSize, tileSize, tileSize,
		tileSize, tileSize, bgColor,
		Red, Red, Red)
}

// Workflow renders a tile for a workflow run.
//
// Visual encoding (mirrors agent tile layout):
// - Top bar: status color (blue=running, amber=waiting, green=succeeded, red=failed)
// - Middle: flow icon (connected boxes) in the status color
// - Bottom: last 4 chars of run ID
func Workflow(status, runID string) string {
	var color string
	switch status {
	case "running", "pending":
		color = "#3b82f6" // blue
	case "waiting":
		color = Amber
	case "succeeded":
		color = Green
	case "failed":
		color = Red
	default:
		color = Gray
	}

	// Flow icon — connected boxes
	iconSVG := fmt.Sprintf(`<rect x="38" y="32" width="28" height="28" rx="5" fill="none" stroke="%s" stroke-width="5"/><rect x="78" y="62" width="28" height="28" rx="5" fill="none" stroke="%s" stroke-width="5"/><path d="M66 46h22v16" stroke="%s" stroke-width="5" stroke-linecap="round" fill="none"/>`,
		color, color, color)

	idText := escapeXML(lastChars(runID, 4))

	return fmt.Sprintf(`<svg xmlns="http://www.w3.org/2000/svg" width="%d" height="%d" viewBox="0 0 %d %d">
  <rect width="%d" height="%d" rx="12" fill="%s"/>
  <rect x="12" y="8" width="120" height="6" rx="3" fill="%s"/>
  %s
  <text x="72" y="128" fill="#a1a1aa" font-family="monospace" font-size="18" font-weight="700" text-anchor="middle">%s</text>
</svg>`,
		tileSize, tileSize, tileSize, tileSize,
		tileSize, tileSize, bgColor,
		color,
		iconSVG,
		idText)
}

// EmptyWorkflow renders a subtle dashed rectangle for an unused workflow slot.
func EmptyWorkflow() string {
	return fmt.Sprintf(`<svg xmlns="http://www.w3.org/2000/svg" width="%d" height="%d" viewBox="0 0 %d %d">
  <rect width="%d" height="%d" rx="12" fill="%s"/>
  <rect x="20" y="20" width="104" height="104" rx="8" fill="none" stroke="%s" stroke-width="3" stroke-dasharray="8 6"/>
</svg>`,
		tileSize, tileSize, tileSize, tileSize,
		tileSize, tileSize, bgColor, DarkGray)
}
