package render

import (
	"strings"
	"testing"
)

func TestAgentRendering(t *testing.T) {
	svg := Agent("Build feature", "working", "API")
	if !strings.Contains(svg, "Build feature") {
		t.Error("SVG missing title")
	}
	if !strings.Contains(svg, "WORKING") {
		t.Error("SVG missing WORKING label")
	}
	if !strings.Contains(svg, ColorWorking) {
		t.Error("SVG missing working color")
	}
	if !strings.Contains(svg, "API") {
		t.Error("SVG missing role")
	}
}

func TestAgentWaitingState(t *testing.T) {
	svg := Agent("Fix bug", "waiting", "")
	if !strings.Contains(svg, "WAITING") {
		t.Error("SVG missing WAITING label")
	}
	if !strings.Contains(svg, ColorWaiting) {
		t.Error("SVG missing waiting color")
	}
	// No role should produce no role line.
	if strings.Contains(svg, "sans-serif\" font-size=\"9\"") {
		t.Error("SVG should not have role line when role is empty")
	}
}

func TestAgentIdleState(t *testing.T) {
	svg := Agent("Idle task", "", "")
	if !strings.Contains(svg, "IDLE") {
		t.Error("SVG missing IDLE label")
	}
	if !strings.Contains(svg, ColorIdle) {
		t.Error("SVG missing idle color")
	}
}

func TestAgentTruncation(t *testing.T) {
	longTitle := "This is a very long agent title that should be truncated"
	svg := Agent(longTitle, "working", "")
	if !strings.Contains(svg, "…") {
		t.Error("SVG should contain truncation marker")
	}
	// The title text element should not contain the full long title.
	if strings.Contains(svg, longTitle) {
		t.Error("SVG should not contain the full untruncated title")
	}
}

func TestAttentionRendering(t *testing.T) {
	svg := Attention("plans", "Approve plan X", "normal", 0, 3)
	if !strings.Contains(svg, "PLAN") {
		t.Error("SVG missing PLAN producer label")
	}
	if !strings.Contains(svg, "Approve plan X") {
		t.Error("SVG missing title")
	}
	// First tile with total > 1 should show count.
	if !strings.Contains(svg, "3 ITEMS") {
		t.Error("SVG should show item count on first tile")
	}
}

func TestAttentionUrgent(t *testing.T) {
	svg := Attention("workflows", "Retry step", "urgent", 0, 1)
	if !strings.Contains(svg, ColorUrgent) {
		t.Error("SVG should use urgent color")
	}
	if !strings.Contains(svg, "FLOW") {
		t.Error("SVG missing FLOW producer label")
	}
}

func TestEmpty(t *testing.T) {
	svg := Empty()
	// Should be an empty SVG with no visible elements.
	if strings.Contains(svg, "text") {
		t.Error("empty SVG should not contain text elements")
	}
}

func TestDisconnected(t *testing.T) {
	svg := Disconnected()
	if !strings.Contains(svg, "OFFLINE") {
		t.Error("disconnected SVG should contain OFFLINE text")
	}
}

func TestNoToken(t *testing.T) {
	svg := NoToken()
	if !strings.Contains(svg, "NO TOKEN") {
		t.Error("no-token SVG should contain NO TOKEN text")
	}
}

func TestDataURI(t *testing.T) {
	svg := `<svg>test</svg>`
	uri := DataURI(svg)
	if !strings.HasPrefix(uri, "data:image/svg+xml;base64,") {
		t.Error("data URI should have correct prefix")
	}
}

func TestProducerLabels(t *testing.T) {
	tests := []struct {
		producer, want string
	}{
		{"plans", "PLAN"},
		{"reviews", "REV"},
		{"workflows", "FLOW"},
		{"workflow_triggers", "TRIG"},
		{"unknown", "TASK"},
	}
	for _, tt := range tests {
		got := producerLabel(tt.producer)
		if got != tt.want {
			t.Errorf("producerLabel(%q) = %q, want %q", tt.producer, got, tt.want)
		}
	}
}

func TestActivityColors(t *testing.T) {
	tests := []struct {
		activity, color string
	}{
		{"working", ColorWorking},
		{"waiting", ColorWaiting},
		{"", ColorIdle},
		{"unknown", ColorIdle},
	}
	for _, tt := range tests {
		got := activityColor(tt.activity)
		if got != tt.color {
			t.Errorf("activityColor(%q) = %q, want %q", tt.activity, got, tt.color)
		}
	}
}
