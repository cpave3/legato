// Package render produces SVG tile images for Stream Deck keys.
//
// Each tile is a 144×144 SVG rendered to a base64 data URI suitable for
// OpenDeck's setImage command. All rendering is pure string templating —
// no external SVG library is needed.
package render

import (
	"encoding/base64"
	"fmt"
	"strings"
)

const (
	tileSize = 144
	bgColor  = "#18181b" // zinc-900, matches Legato web UI
)

// Color constants for activity states.
const (
	ColorWorking = "#22c55e" // green
	ColorWaiting = "#f59e0b" // amber
	ColorIdle    = "#71717a" // gray
	ColorUrgent  = "#ef4444" // red
	ColorNormal  = "#f59e0b" // amber
	ColorOffline = "#71717a" // gray
)

// DataURI wraps raw SVG bytes as a data URI.
func DataURI(svg string) string {
	return "data:image/svg+xml;base64," + base64.StdEncoding.EncodeToString([]byte(svg))
}

func escapeXML(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	s = strings.ReplaceAll(s, "\"", "&quot;")
	s = strings.ReplaceAll(s, "'", "&apos;")
	return s
}

// truncate shortens a string to maxLen characters, appending "…" if truncated.
func truncate(s string, maxLen int) string {
	runes := []rune(s)
	if len(runes) <= maxLen {
		return s
	}
	return string(runes[:maxLen]) + "…"
}

// activityColor returns the color for an agent's activity state.
func activityColor(activity string) string {
	switch activity {
	case "working":
		return ColorWorking
	case "waiting":
		return ColorWaiting
	default:
		return ColorIdle
	}
}

// activityLabel returns the uppercase label for an agent's activity state.
func activityLabel(activity string) string {
	switch activity {
	case "working":
		return "WORKING"
	case "waiting":
		return "WAITING"
	default:
		return "IDLE"
	}
}

// urgencyColor returns the color for an attention item's urgency.
func urgencyColor(urgency string) string {
	if urgency == "urgent" || urgency == "high" {
		return ColorUrgent
	}
	return ColorNormal
}

// producerLabel returns a short label for an attention item's producer.
func producerLabel(producer string) string {
	switch producer {
	case "plans":
		return "PLAN"
	case "reviews":
		return "REV"
	case "workflows":
		return "FLOW"
	case "workflow_triggers":
		return "TRIG"
	default:
		return "TASK"
	}
}

// Agent renders an SVG tile for an agent.
func Agent(title, activity, role string) string {
	color := activityColor(activity)
	label := activityLabel(activity)
	displayTitle := truncate(title, 20)

	var roleLine string
	if role != "" {
		roleLine = fmt.Sprintf(`<text x="72" y="128" fill="#71717a" font-family="sans-serif" font-size="9" font-weight="600" text-anchor="middle">%s</text>`,
			escapeXML(truncate(role, 12)))
	}

	return fmt.Sprintf(`<svg xmlns="http://www.w3.org/2000/svg" width="%d" height="%d" viewBox="0 0 %d %d">
  <rect width="%d" height="%d" rx="12" fill="%s"/>
  <circle cx="72" cy="32" r="17" fill="none" stroke="%s" stroke-width="5"/>
  <path d="M62 32h20M72 22v20" stroke="%s" stroke-width="4" stroke-linecap="round"/>
  <text x="72" y="72" fill="#e4e4e7" font-family="sans-serif" font-size="12" font-weight="700" text-anchor="middle">%s</text>
  <text x="72" y="100" fill="%s" font-family="sans-serif" font-size="18" font-weight="800" text-anchor="middle">%s</text>
  %s
</svg>`,
		tileSize, tileSize, tileSize, tileSize,
		tileSize, tileSize, bgColor,
		color, color,
		escapeXML(displayTitle),
		color, label,
		roleLine)
}

// Attention renders an SVG tile for an attention item.
func Attention(producer, title, urgency string, index, total int) string {
	color := urgencyColor(urgency)
	label := producerLabel(producer)
	displayTitle := truncate(title, 18)

	var countLine string
	if index == 0 && total > 1 {
		countLine = fmt.Sprintf(`<text x="72" y="128" fill="#71717a" font-family="sans-serif" font-size="10" font-weight="600" text-anchor="middle">%d ITEMS</text>`,
			total)
	}

	return fmt.Sprintf(`<svg xmlns="http://www.w3.org/2000/svg" width="%d" height="%d" viewBox="0 0 %d %d">
  <rect width="%d" height="%d" rx="12" fill="%s"/>
  <rect x="0" y="0" width="6" height="%d" rx="3" fill="%s"/>
  <text x="72" y="40" fill="%s" font-family="sans-serif" font-size="14" font-weight="800" text-anchor="middle">%s</text>
  <text x="72" y="72" fill="#e4e4e7" font-family="sans-serif" font-size="12" font-weight="600" text-anchor="middle">%s</text>
  <text x="72" y="100" fill="%s" font-family="sans-serif" font-size="11" font-weight="500" text-anchor="middle">%s</text>
  %s
</svg>`,
		tileSize, tileSize, tileSize, tileSize,
		tileSize, tileSize, bgColor,
		tileSize, color,
		color, label,
		escapeXML(displayTitle),
		"#a1a1aa", escapeXML(truncate(urgency, 10)),
		countLine)
}

// Empty renders a transparent/blank tile (for unused slots).
func Empty() string {
	return `<svg xmlns="http://www.w3.org/2000/svg" width="144" height="144" viewBox="0 0 144 144"></svg>`
}

// Disconnected renders an "OFFLINE" tile when Legato is unreachable.
func Disconnected() string {
	return fmt.Sprintf(`<svg xmlns="http://www.w3.org/2000/svg" width="%d" height="%d" viewBox="0 0 %d %d">
  <rect width="%d" height="%d" rx="12" fill="%s"/>
  <text x="72" y="72" fill="%s" font-family="sans-serif" font-size="14" font-weight="700" text-anchor="middle">OFFLINE</text>
  <circle cx="72" cy="100" r="3" fill="%s"/>
</svg>`,
		tileSize, tileSize, tileSize, tileSize,
		tileSize, tileSize, bgColor,
		ColorOffline, ColorOffline)
}

// NoToken renders a "NO TOKEN" tile when the auth token can't be found.
func NoToken() string {
	return fmt.Sprintf(`<svg xmlns="http://www.w3.org/2000/svg" width="%d" height="%d" viewBox="0 0 %d %d">
  <rect width="%d" height="%d" rx="12" fill="%s"/>
  <text x="72" y="68" fill="%s" font-family="sans-serif" font-size="12" font-weight="700" text-anchor="middle">NO TOKEN</text>
  <text x="72" y="88" fill="#52525b" font-family="sans-serif" font-size="9" text-anchor="middle">Check auth-token file</text>
</svg>`,
		tileSize, tileSize, tileSize, tileSize,
		tileSize, tileSize, bgColor,
		ColorUrgent)
}
