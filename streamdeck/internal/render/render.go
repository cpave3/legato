// Package render produces SVG tile images for Stream Deck keys.
//
// Each tile is a 144×144 SVG rendered to a base64 data URI suitable for
// OpenDeck's setImage command. All rendering is pure string templating —
// no external SVG library is needed.
//
// Design philosophy: visual-first, minimal text. The key should be
// glanceable from across the desk — a colored status indicator dominates
// the tile, with just enough text to identify the agent or item.
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
	ColorIdle    = "#52525b" // dark gray
	ColorUrgent  = "#ef4444" // red
	ColorNormal  = "#f59e0b" // amber
	ColorOffline = "#52525b" // dark gray
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

// urgencyColor returns the color for an attention item's urgency.
func urgencyColor(urgency string) string {
	if urgency == "urgent" || urgency == "high" {
		return ColorUrgent
	}
	return ColorNormal
}

// producerIcon returns an SVG path for an attention item's producer.
// Returns a simple geometric icon — no text needed.
func producerIcon(producer string) (iconSVG, label string) {
	switch producer {
	case "plans":
		// Document/checkmark icon
		return `<rect x="52" y="40" width="40" height="52" rx="4" fill="none" stroke="{color}" stroke-width="4"/><path d="M60 60h24M60 70h24M60 80h16" stroke="{color}" stroke-width="3" stroke-linecap="round"/>`, "PLAN"
	case "reviews":
		// Eye icon (review)
		return `<path d="M72 50C55 50 42 72 42 72s13 22 30 22 30-22 30-22-13-22-30-22z" fill="none" stroke="{color}" stroke-width="4"/><circle cx="72" cy="72" r="8" fill="{color}"/>`, "REVIEW"
	case "workflows":
		// Flow icon (connected boxes)
		return `<rect x="40" y="44" width="26" height="26" rx="4" fill="none" stroke="{color}" stroke-width="4"/><rect x="78" y="74" width="26" height="26" rx="4" fill="none" stroke="{color}" stroke-width="4"/><path d="M66 57h24v17" stroke="{color}" stroke-width="4" stroke-linecap="round" fill="none"/>`, "FLOW"
	case "workflow_triggers":
		// Lightning bolt (trigger)
		return `<path d="M76 42L54 78h18l-8 24 22-36H68z" fill="{color}"/>`, "TRIG"
	default:
		// Square dot (generic task)
		return `<rect x="58" y="58" width="28" height="28" rx="6" fill="{color}"/>`, "TASK"
	}
}

// Agent renders an SVG tile for an agent.
//
// Visual layout:
// - Top: large colored status bar (full width, ~8px thick)
// - Center: big colored circle/ring showing activity (the dominant visual)
// - Bottom: short title text (max ~16 chars, 2 lines)
// - Bottom edge: role badge if present
func Agent(title, activity, role string) string {
	color := activityColor(activity)
	titleXML := escapeXML(truncate(title, 16))

	// The center circle is the main visual — filled for working, ring for waiting, dim for idle
	var centerSVG string
	switch activity {
	case "working":
		// Filled circle with a subtle pulse ring
		centerSVG = fmt.Sprintf(
			`<circle cx="72" cy="58" r="28" fill="none" stroke="%s" stroke-width="3" opacity="0.3"/>
  <circle cx="72" cy="58" r="22" fill="%s"/>`,
			color, color)
	case "waiting":
		// Ring (hollow circle)
		centerSVG = fmt.Sprintf(
			`<circle cx="72" cy="58" r="22" fill="none" stroke="%s" stroke-width="6"/>`, color)
	default:
		// Small dim dot
		centerSVG = fmt.Sprintf(
			`<circle cx="72" cy="58" r="10" fill="%s"/>`, color)
	}

	// Split title into two lines if long
	var titleLines string
	runes := []rune(titleXML)
	if len(runes) > 10 {
		mid := len(runes) / 2
		// Try to split at a space near the middle
		for i := mid; i < len(runes) && i < mid+8; i++ {
			if runes[i] == ' ' {
				mid = i + 1
				break
			}
		}
		line1 := escapeXML(truncate(string(runes[:mid-1]), 14))
		line2 := escapeXML(truncate(string(runes[mid:]), 14))
		titleLines = fmt.Sprintf(
			`<text x="72" y="108" fill="#e4e4e7" font-family="sans-serif" font-size="11" font-weight="600" text-anchor="middle">%s</text>
  <text x="72" y="122" fill="#a1a1aa" font-family="sans-serif" font-size="10" font-weight="500" text-anchor="middle">%s</text>`,
			line1, line2)
	} else {
		titleLines = fmt.Sprintf(
			`<text x="72" y="115" fill="#e4e4e7" font-family="sans-serif" font-size="12" font-weight="600" text-anchor="middle">%s</text>`,
			titleXML)
	}

	// Role badge at bottom
	var roleLine string
	if role != "" {
		roleLine = fmt.Sprintf(
			`<text x="72" y="136" fill="#71717a" font-family="sans-serif" font-size="8" font-weight="600" text-anchor="middle">%s</text>`,
			escapeXML(truncate(role, 10)))
	}

	return fmt.Sprintf(`<svg xmlns="http://www.w3.org/2000/svg" width="%d" height="%d" viewBox="0 0 %d %d">
  <rect width="%d" height="%d" rx="12" fill="%s"/>
  <rect x="12" y="8" width="120" height="6" rx="3" fill="%s"/>
  %s
  %s
  %s
</svg>`,
		tileSize, tileSize, tileSize, tileSize,
		tileSize, tileSize, bgColor,
		color,
		centerSVG,
		titleLines,
		roleLine)
}

// Attention renders an SVG tile for an attention item.
//
// Visual layout:
// - Left edge: thick colored urgency bar (full height)
// - Center: large producer icon (dominant visual)
// - Bottom: short title text (max ~14 chars, 2 lines)
// - Top-right: count badge if multiple items
func Attention(producer, title, urgency string, index, total int) string {
	color := urgencyColor(urgency)
	iconSVG, _ := producerIcon(producer)
	// Replace {color} placeholder in icon
	iconSVG = strings.ReplaceAll(iconSVG, "{color}", color)
	titleXML := escapeXML(truncate(title, 14))

	// Count badge (top-right corner) on the first tile when multiple items
	var badge string
	if index == 0 && total > 1 {
		badge = fmt.Sprintf(
			`<circle cx="124" cy="20" r="14" fill="%s"/>
  <text x="124" y="25" fill="#18181b" font-family="sans-serif" font-size="14" font-weight="800" text-anchor="middle">%d</text>`,
			color, total)
	}

	// Split title into two lines if long
	var titleLines string
	runes := []rune(titleXML)
	if len(runes) > 8 {
		mid := len(runes) / 2
		for i := mid; i < len(runes) && i < mid+6; i++ {
			if runes[i] == ' ' {
				mid = i + 1
				break
			}
		}
		line1 := escapeXML(truncate(string(runes[:mid-1]), 12))
		line2 := escapeXML(truncate(string(runes[mid:]), 12))
		titleLines = fmt.Sprintf(
			`<text x="72" y="116" fill="#e4e4e7" font-family="sans-serif" font-size="10" font-weight="600" text-anchor="middle">%s</text>
  <text x="72" y="128" fill="#a1a1aa" font-family="sans-serif" font-size="9" font-weight="500" text-anchor="middle">%s</text>`,
			line1, line2)
	} else {
		titleLines = fmt.Sprintf(
			`<text x="72" y="122" fill="#e4e4e7" font-family="sans-serif" font-size="11" font-weight="600" text-anchor="middle">%s</text>`,
			titleXML)
	}

	return fmt.Sprintf(`<svg xmlns="http://www.w3.org/2000/svg" width="%d" height="%d" viewBox="0 0 %d %d">
  <rect width="%d" height="%d" rx="12" fill="%s"/>
  <rect x="0" y="0" width="8" height="%d" rx="0" fill="%s"/>
  %s
  %s
  %s
</svg>`,
		tileSize, tileSize, tileSize, tileSize,
		tileSize, tileSize, bgColor,
		tileSize, color,
		badge,
		iconSVG,
		titleLines)
}

// Empty renders a transparent/blank tile (for unused slots).
func Empty() string {
	return `<svg xmlns="http://www.w3.org/2000/svg" width="144" height="144" viewBox="0 0 144 144"></svg>`
}

// EmptyAgent renders a subtle placeholder for an agent tile with no agent.
// Shows a dim circle outline so the slot reads as "available" not "broken".
func EmptyAgent() string {
	return fmt.Sprintf(`<svg xmlns="http://www.w3.org/2000/svg" width="%d" height="%d" viewBox="0 0 %d %d">
  <rect width="%d" height="%d" rx="12" fill="%s"/>
  <circle cx="72" cy="58" r="22" fill="none" stroke="#27272a" stroke-width="4" stroke-dasharray="6 4"/>
</svg>`,
		tileSize, tileSize, tileSize, tileSize,
		tileSize, tileSize, bgColor)
}

// EmptyAttention renders a subtle placeholder for an attention tile with no item.
// Shows a dim dashed left bar so the slot reads as "available" not "broken".
func EmptyAttention() string {
	return fmt.Sprintf(`<svg xmlns="http://www.w3.org/2000/svg" width="%d" height="%d" viewBox="0 0 %d %d">
  <rect width="%d" height="%d" rx="12" fill="%s"/>
  <rect x="0" y="0" width="8" height="%d" rx="0" fill="#27272a"/>
  <circle cx="72" cy="58" r="22" fill="none" stroke="#27272a" stroke-width="4" stroke-dasharray="6 4"/>
</svg>`,
		tileSize, tileSize, tileSize, tileSize,
		tileSize, tileSize, bgColor,
		tileSize)
}

// Disconnected renders an "OFFLINE" tile when Legato is unreachable.
//
// Visual: dim background with a broken-link icon.
func Disconnected() string {
	return fmt.Sprintf(`<svg xmlns="http://www.w3.org/2000/svg" width="%d" height="%d" viewBox="0 0 %d %d">
  <rect width="%d" height="%d" rx="12" fill="%s"/>
  <path d="M56 64h-8v-8a16 16 0 0 1 16-16h8M88 80h8v8a16 16 0 0 1-16 16h-8" stroke="%s" stroke-width="5" stroke-linecap="round" fill="none"/>
  <line x1="58" y1="86" x2="86" y2="58" stroke="%s" stroke-width="5" stroke-linecap="round"/>
  <text x="72" y="120" fill="%s" font-family="sans-serif" font-size="11" font-weight="600" text-anchor="middle">OFFLINE</text>
</svg>`,
		tileSize, tileSize, tileSize, tileSize,
		tileSize, tileSize, bgColor,
		ColorOffline, ColorOffline, "#52525b")
}

// NoToken renders a "NO TOKEN" tile when the auth token can't be found.
//
// Visual: lock icon with warning text.
func NoToken() string {
	return fmt.Sprintf(`<svg xmlns="http://www.w3.org/2000/svg" width="%d" height="%d" viewBox="0 0 %d %d">
  <rect width="%d" height="%d" rx="12" fill="%s"/>
  <rect x="56" y="58" width="32" height="28" rx="4" fill="none" stroke="%s" stroke-width="4"/>
  <path d="M62 58v-6a10 10 0 0 1 20 0v6" fill="none" stroke="%s" stroke-width="4"/>
  <circle cx="72" cy="70" r="4" fill="%s"/>
  <text x="72" y="112" fill="%s" font-family="sans-serif" font-size="10" font-weight="700" text-anchor="middle">NO TOKEN</text>
  <text x="72" y="126" fill="#52525b" font-family="sans-serif" font-size="8" text-anchor="middle">check config</text>
</svg>`,
		tileSize, tileSize, tileSize, tileSize,
		tileSize, tileSize, bgColor,
		ColorUrgent, ColorUrgent, ColorUrgent,
		"#ef4444")
}
