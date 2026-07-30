// Package render produces SVG tile images for Stream Deck keys.
//
// Each tile is a 144×144 SVG rendered to a base64 data URI. The SVG is
// downscaled to 72×72 by the hardware — so design for 72px legibility.
//
// Design philosophy: color and shape only, no text. The keys are glance
// indicators, not text displays. Color encodes state (working/waiting/idle/
// urgent). Shape encodes activity (filled circle / hollow ring / dot). Color
// shift encodes elapsed time (green→yellow→amber→red as an agent works longer).
// Border weight encodes urgency (thin bar = normal, thick border = urgent).
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

// Base colors for activity states.
const (
	Green  = "#22c55e"
	Amber  = "#f59e0b"
	Red    = "#ef4444"
	Gray   = "#52525b"
	DarkGray = "#27272a"
)

// DataURI wraps raw SVG bytes as a data URI.
func DataURI(svg string) string {
	return "data:image/svg+xml;base64," + base64.StdEncoding.EncodeToString([]byte(svg))
}

// elapsedColor shifts green→yellow→amber→red based on seconds working.
// 0-10min: green, 10-30min: yellow-green, 30-60min: amber, 60min+: red.
func elapsedColor(seconds float64) string {
	switch {
	case seconds < 600:   // < 10 min
		return Green
	case seconds < 1800:  // 10-30 min
		return "#84cc16" // lime — green shifting yellow
	case seconds < 3600:  // 30-60 min
		return Amber
	default:              // > 60 min
		return Red
	}
}

// waitingColor shifts amber→red based on seconds waiting.
// 0-5min: amber, 5min+: red.
func waitingColor(seconds float64) string {
	if seconds > 300 {
		return Red
	}
	return Amber
}

// Agent renders a purely visual tile for an agent.
//
// Visual encoding:
// - Working: filled circle, color shifts green→red with elapsed time
// - Waiting: hollow ring, amber (red if >5min)
// - Idle: small dim dot
// - Dead: red X over dark background
func Agent(activity string, workingSeconds, waitingSeconds float64) string {
	var centerSVG string

	switch activity {
	case "working":
		color := elapsedColor(workingSeconds)
		// Filled circle with outer ring
		centerSVG = fmt.Sprintf(
			`<circle cx="72" cy="72" r="44" fill="none" stroke="%s" stroke-width="3" opacity="0.25"/>
  <circle cx="72" cy="72" r="34" fill="%s"/>`, color, color)
	case "waiting":
		color := waitingColor(waitingSeconds)
		// Hollow ring
		centerSVG = fmt.Sprintf(
			`<circle cx="72" cy="72" r="34" fill="none" stroke="%s" stroke-width="12"/>`, color)
	default:
		// Idle: small dim dot
		centerSVG = fmt.Sprintf(
			`<circle cx="72" cy="72" r="14" fill="%s"/>`, Gray)
	}

	return fmt.Sprintf(`<svg xmlns="http://www.w3.org/2000/svg" width="%d" height="%d" viewBox="0 0 %d %d">
  <rect width="%d" height="%d" rx="12" fill="%s"/>
  %s
</svg>`,
		tileSize, tileSize, tileSize, tileSize,
		tileSize, tileSize, bgColor,
		centerSVG)
}

// AgentDead renders a tile for a dead/stopped agent — red X on dark.
func AgentDead() string {
	return fmt.Sprintf(`<svg xmlns="http://www.w3.org/2000/svg" width="%d" height="%d" viewBox="0 0 %d %d">
  <rect width="%d" height="%d" rx="12" fill="%s"/>
  <line x1="48" y1="48" x2="96" y2="96" stroke="%s" stroke-width="10" stroke-linecap="round"/>
  <line x1="96" y1="48" x2="48" y2="96" stroke="%s" stroke-width="10" stroke-linecap="round"/>
</svg>`,
		tileSize, tileSize, tileSize, tileSize,
		tileSize, tileSize, bgColor,
		Red, Red)
}

// Attention renders a purely visual tile for an attention item.
//
// Visual encoding:
// - Normal urgency: thin colored left bar + producer icon
// - Urgent: thick red border around entire tile + producer icon
// - Producer type: distinct geometric icon (no text)
func Attention(producer, urgency string, index, total int) string {
	isUrgent := urgency == "urgent" || urgency == "high"

	var iconSVG string
	switch producer {
	case "plans":
		// Document shape
		iconSVG = `<rect x="50" y="40" width="44" height="56" rx="4" fill="none" stroke="{color}" stroke-width="5"/><path d="M60 58h24M60 72h24M60 86h16" stroke="{color}" stroke-width="4" stroke-linecap="round"/>`
	case "reviews":
		// Eye shape
		iconSVG = `<path d="M72 44C50 44 36 72 36 72s14 28 36 28 36-28 36-28-14-28-36-28z" fill="none" stroke="{color}" stroke-width="5"/><circle cx="72" cy="72" r="12" fill="{color}"/>`
	case "workflows":
		// Connected boxes (flow)
		iconSVG = `<rect x="38" y="40" width="30" height="30" rx="5" fill="none" stroke="{color}" stroke-width="5"/><rect x="76" y="74" width="30" height="30" rx="5" fill="none" stroke="{color}" stroke-width="5"/><path d="M68 55h24v19" stroke="{color}" stroke-width="5" stroke-linecap="round" fill="none"/>`
	case "workflow_triggers":
		// Lightning bolt
		iconSVG = `<path d="M78 38L52 80h20l-8 26 24-42H66z" fill="{color}"/>`
	default:
		// Filled square
		iconSVG = `<rect x="54" y="54" width="36" height="36" rx="8" fill="{color}"/>`
	}

	if isUrgent {
		// Urgent: thick red border + red icon
		iconSVG = strings.ReplaceAll(iconSVG, "{color}", Red)
		return fmt.Sprintf(`<svg xmlns="http://www.w3.org/2000/svg" width="%d" height="%d" viewBox="0 0 %d %d">
  <rect width="%d" height="%d" rx="12" fill="%s"/>
  <rect x="6" y="6" width="%d" height="%d" rx="8" fill="none" stroke="%s" stroke-width="6"/>
  %s
</svg>`,
			tileSize, tileSize, tileSize, tileSize,
			tileSize, tileSize, bgColor,
			tileSize-12, tileSize-12, Red,
			iconSVG)
	}

	// Normal: thin amber left bar + amber icon
	color := Amber
	iconSVG = strings.ReplaceAll(iconSVG, "{color}", color)
	return fmt.Sprintf(`<svg xmlns="http://www.w3.org/2000/svg" width="%d" height="%d" viewBox="0 0 %d %d">
  <rect width="%d" height="%d" rx="12" fill="%s"/>
  <rect x="0" y="0" width="10" height="%d" rx="0" fill="%s"/>
  %s
</svg>`,
		tileSize, tileSize, tileSize, tileSize,
		tileSize, tileSize, bgColor,
		tileSize, color,
		iconSVG)
}

// EmptyAgent renders a subtle dashed circle for an unused agent slot.
func EmptyAgent() string {
	return fmt.Sprintf(`<svg xmlns="http://www.w3.org/2000/svg" width="%d" height="%d" viewBox="0 0 %d %d">
  <rect width="%d" height="%d" rx="12" fill="%s"/>
  <circle cx="72" cy="72" r="34" fill="none" stroke="%s" stroke-width="3" stroke-dasharray="8 6"/>
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
