package render

import (
	"strings"
	"testing"
)

func TestAgentRendering(t *testing.T) {
	svg := Agent("Build feature", "working", "API")
	// Working agent should have a filled circle (dominant visual)
	if !strings.Contains(svg, `<circle cx="72" cy="58" r="22" fill="#22c55e"/>`) {
		t.Error("working agent should have filled green circle")
	}
	// Title may be split across two lines
	if !strings.Contains(svg, "Build") || !strings.Contains(svg, "feature") {
		t.Error("SVG missing title text")
	}
	if !strings.Contains(svg, "API") {
		t.Error("SVG missing role")
	}
}

func TestAgentWaitingState(t *testing.T) {
	svg := Agent("Fix bug", "waiting", "")
	// Waiting agent should have a ring (hollow circle with stroke)
	if !strings.Contains(svg, `fill="none" stroke="#f59e0b" stroke-width="6"`) {
		t.Error("waiting agent should have amber ring")
	}
	// No role should produce no role text
	if strings.Contains(svg, "sans-serif\" font-size=\"8\"") {
		t.Error("SVG should not have role line when role is empty")
	}
}

func TestAgentIdleState(t *testing.T) {
	svg := Agent("Idle task", "", "")
	// Idle agent should have a small dim dot
	if !strings.Contains(svg, `r="10" fill="#52525b"`) {
		t.Error("idle agent should have small gray dot")
	}
}

func TestAgentWorkingHasStatusBar(t *testing.T) {
	svg := Agent("Test", "working", "")
	// Top status bar should be present and colored
	if !strings.Contains(svg, `<rect x="12" y="8" width="120" height="6" rx="3" fill="#22c55e"/>`) {
		t.Error("working agent should have green status bar at top")
	}
}

func TestAgentWaitingHasStatusBar(t *testing.T) {
	svg := Agent("Test", "waiting", "")
	if !strings.Contains(svg, `fill="#f59e0b"/>`) {
		t.Error("waiting agent should have amber status bar")
	}
}

func TestAgentTruncation(t *testing.T) {
	longTitle := "This is a very long agent title that should be truncated"
	svg := Agent(longTitle, "working", "")
	if !strings.Contains(svg, "…") {
		t.Error("SVG should contain truncation marker")
	}
	if strings.Contains(svg, longTitle) {
		t.Error("SVG should not contain the full untruncated title")
	}
}

func TestAgentLongTitleSplits(t *testing.T) {
	svg := Agent("Very long title here", "working", "")
	// Long titles should produce two text lines for the title
	// (plus status bar = 0 text, center circle = 0 text, no role = 0 text)
	textCount := strings.Count(svg, "<text ")
	if textCount != 2 {
		t.Errorf("long title should produce 2 text lines, got %d text elements", textCount)
	}
}

func TestAttentionRendering(t *testing.T) {
	svg := Attention("plans", "Approve plan X", "normal", 0, 3)
	// Should have a plan icon (document shape)
	if !strings.Contains(svg, "rect") {
		t.Error("attention SVG should have an icon")
	}
	// Title may be split across two lines
	if !strings.Contains(svg, "Approve") || !strings.Contains(svg, "plan") {
		t.Error("SVG missing title text")
	}
}

func TestAttentionCountBadge(t *testing.T) {
	svg := Attention("plans", "Test", "normal", 0, 3)
	// First tile with total > 1 should show count badge
	if !strings.Contains(svg, ">3<") {
		t.Error("SVG should show count badge with number 3")
	}
}

func TestAttentionNoCountBadge(t *testing.T) {
	svg := Attention("plans", "Test", "normal", 0, 1)
	// Single item should not show count badge
	if strings.Contains(svg, ">1<") {
		t.Error("SVG should not show count badge for single item")
	}
}

func TestAttentionUrgent(t *testing.T) {
	svg := Attention("workflows", "Retry step", "urgent", 0, 1)
	if !strings.Contains(svg, "#ef4444") {
		t.Error("SVG should use urgent red color")
	}
}

func TestAttentionLeftBar(t *testing.T) {
	svg := Attention("reviews", "Test", "normal", 0, 1)
	// Should have a left edge colored bar
	if !strings.Contains(svg, `width="8" height="144"`) {
		t.Error("attention SVG should have left edge bar")
	}
}

func TestAttentionProducerIcons(t *testing.T) {
	// Each producer should render a distinct icon
	producers := []string{"plans", "reviews", "workflows", "workflow_triggers", "unknown"}
	for _, p := range producers {
		svg := Attention(p, "Test", "normal", 0, 1)
		// Each should have some path or rect (icon element)
		if !strings.Contains(svg, "<path") && !strings.Contains(svg, "<rect") {
			t.Errorf("producer %q should have an icon element", p)
		}
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
	if !strings.Contains(svg, "<path") {
		t.Error("disconnected SVG should have a broken-link icon")
	}
}

func TestNoToken(t *testing.T) {
	svg := NoToken()
	if !strings.Contains(svg, "NO TOKEN") {
		t.Error("no-token SVG should contain NO TOKEN text")
	}
	if !strings.Contains(svg, "NO TOKEN") {
		t.Error("no-token SVG should have lock icon")
	}
}

func TestDataURI(t *testing.T) {
	svg := `<svg>test</svg>`
	uri := DataURI(svg)
	if !strings.HasPrefix(uri, "data:image/svg+xml;base64,") {
		t.Error("data URI should have correct prefix")
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
