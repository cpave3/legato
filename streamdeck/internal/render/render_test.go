package render

import (
	"strings"
	"testing"
)

func TestAgentWorkingGreen(t *testing.T) {
	svg := Agent("working", 120, 0) // 2 min working
	// Fresh working: filled green circle
	if !strings.Contains(svg, `fill="#22c55e"`) {
		t.Error("working agent <10min should be green")
	}
	// No text elements
	if strings.Contains(svg, "<text") {
		t.Error("agent tile should have no text")
	}
}

func TestAgentWorkingYellowGreen(t *testing.T) {
	svg := Agent("working", 900, 0) // 15 min working
	if !strings.Contains(svg, `fill="#84cc16"`) {
		t.Error("working agent 10-30min should be lime")
	}
}

func TestAgentWorkingAmber(t *testing.T) {
	svg := Agent("working", 2400, 0) // 40 min working
	if !strings.Contains(svg, `fill="#f59e0b"`) {
		t.Error("working agent 30-60min should be amber")
	}
}

func TestAgentWorkingRed(t *testing.T) {
	svg := Agent("working", 7200, 0) // 2 hours working
	if !strings.Contains(svg, `fill="#ef4444"`) {
		t.Error("working agent >60min should be red")
	}
}

func TestAgentWaiting(t *testing.T) {
	svg := Agent("waiting", 0, 60) // 1 min waiting
	// Waiting: hollow ring (stroke, no fill)
	if !strings.Contains(svg, `fill="none"`) {
		t.Error("waiting agent should have hollow ring")
	}
	if !strings.Contains(svg, `stroke="#f59e0b"`) {
		t.Error("waiting agent <5min should be amber stroke")
	}
}

func TestAgentWaitingLong(t *testing.T) {
	svg := Agent("waiting", 0, 600) // 10 min waiting
	if !strings.Contains(svg, `stroke="#ef4444"`) {
		t.Error("waiting agent >5min should be red stroke")
	}
}

func TestAgentIdle(t *testing.T) {
	svg := Agent("", 0, 0)
	// Idle: small dim dot
	if !strings.Contains(svg, `r="14"`) {
		t.Error("idle agent should have small circle")
	}
	if !strings.Contains(svg, `fill="#52525b"`) {
		t.Error("idle agent should be gray")
	}
}

func TestAgentDead(t *testing.T) {
	svg := AgentDead()
	// Red X — two diagonal lines
	if !strings.Contains(svg, `stroke="#ef4444"`) {
		t.Error("dead agent should have red lines")
	}
	if strings.Count(svg, "<line") != 2 {
		t.Error("dead agent should have two lines (X shape)")
	}
}

func TestAgentNoText(t *testing.T) {
	// All agent variants should be text-free
	for _, svg := range []string{
		Agent("working", 0, 0),
		Agent("waiting", 0, 0),
		Agent("", 0, 0),
		AgentDead(),
		EmptyAgent(),
	} {
		if strings.Contains(svg, "<text") {
			t.Error("agent tile should have no text elements")
		}
	}
}

func TestAttentionNormal(t *testing.T) {
	svg := Attention("plans", "normal", 0, 1)
	// Normal: thin left bar, amber
	if !strings.Contains(svg, `fill="#f59e0b"`) {
		t.Error("normal attention should use amber")
	}
	if strings.Contains(svg, "<text") {
		t.Error("attention tile should have no text")
	}
}

func TestAttentionUrgent(t *testing.T) {
	svg := Attention("plans", "urgent", 0, 1)
	// Urgent: thick red border
	if !strings.Contains(svg, `stroke="#ef4444"`) {
		t.Error("urgent attention should have red border")
	}
	if !strings.Contains(svg, `stroke-width="6"`) {
		t.Error("urgent attention should have thick border")
	}
}

func TestAttentionProducerIcons(t *testing.T) {
	// Each producer should render a distinct shape
	producers := []string{"plans", "reviews", "workflows", "workflow_triggers", "unknown"}
	for _, p := range producers {
		svg := Attention(p, "normal", 0, 1)
		if !strings.Contains(svg, "<path") && !strings.Contains(svg, "<rect") {
			t.Errorf("producer %q should have an icon element", p)
		}
		if strings.Contains(svg, "<text") {
			t.Errorf("producer %q should have no text", p)
		}
	}
}

func TestAttentionNoText(t *testing.T) {
	for _, svg := range []string{
		Attention("plans", "normal", 0, 1),
		Attention("reviews", "urgent", 0, 1),
		EmptyAttention(),
	} {
		if strings.Contains(svg, "<text") {
			t.Error("attention tile should have no text elements")
		}
	}
}

func TestEmptyAgent(t *testing.T) {
	svg := EmptyAgent()
	if !strings.Contains(svg, "stroke-dasharray") {
		t.Error("empty agent should have dashed circle")
	}
}

func TestEmptyAttention(t *testing.T) {
	svg := EmptyAttention()
	if !strings.Contains(svg, "stroke-dasharray") {
		t.Error("empty attention should have dashed square")
	}
}

func TestDisconnected(t *testing.T) {
	svg := Disconnected()
	if strings.Contains(svg, "<text") {
		t.Error("disconnected should have no text")
	}
	if !strings.Contains(svg, "<path") {
		t.Error("disconnected should have broken-link icon")
	}
}

func TestNoToken(t *testing.T) {
	svg := NoToken()
	if strings.Contains(svg, "<text") {
		t.Error("no-token should have no text")
	}
	// Lock icon: rect + path (arc) + circle
	if !strings.Contains(svg, "<rect") || !strings.Contains(svg, "<path") {
		t.Error("no-token should have lock icon")
	}
}

func TestDataURI(t *testing.T) {
	svg := `<svg>test</svg>`
	uri := DataURI(svg)
	if !strings.HasPrefix(uri, "data:image/svg+xml;base64,") {
		t.Error("data URI should have correct prefix")
	}
}

func TestElapsedColor(t *testing.T) {
	tests := []struct {
		seconds float64
		want    string
	}{
		{0, "#22c55e"},
		{599, "#22c55e"},
		{600, "#84cc16"},
		{1799, "#84cc16"},
		{1800, "#f59e0b"},
		{3599, "#f59e0b"},
		{3600, "#ef4444"},
		{7200, "#ef4444"},
	}
	for _, tt := range tests {
		got := elapsedColor(tt.seconds)
		if got != tt.want {
			t.Errorf("elapsedColor(%.0f) = %q, want %q", tt.seconds, got, tt.want)
		}
	}
}

func TestWaitingColor(t *testing.T) {
	tests := []struct {
		seconds float64
		want    string
	}{
		{0, "#f59e0b"},
		{299, "#f59e0b"},
		{301, "#ef4444"},
		{600, "#ef4444"},
	}
	for _, tt := range tests {
		got := waitingColor(tt.seconds)
		if got != tt.want {
			t.Errorf("waitingColor(%.0f) = %q, want %q", tt.seconds, got, tt.want)
		}
	}
}
