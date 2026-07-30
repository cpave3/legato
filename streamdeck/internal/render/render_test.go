package render

import (
	"strings"
	"testing"
)

func TestAgentWorkingGreen(t *testing.T) {
	svg := Agent("working", "5hptcdnx", 120, 0) // 2 min working
	if !strings.Contains(svg, `fill="#22c55e"`) {
		t.Error("working agent <30min should be green")
	}
}

func TestAgentWorkingLime(t *testing.T) {
	svg := Agent("working", "AG-9632", 2400, 0) // 40 min working
	if !strings.Contains(svg, `fill="#84cc16"`) {
		t.Error("working agent 30-60min should be lime")
	}
}

func TestAgentWorkingAmber(t *testing.T) {
	svg := Agent("working", "AG-9645", 5400, 0) // 90 min working
	if !strings.Contains(svg, `fill="#f59e0b"`) {
		t.Error("working agent 1-2hr should be amber")
	}
}

func TestAgentWorkingRed(t *testing.T) {
	svg := Agent("working", "dyxsx3i8", 10800, 0) // 3 hours working
	if !strings.Contains(svg, `fill="#ef4444"`) {
		t.Error("working agent >2hr should be red")
	}
}

func TestAgentWaiting(t *testing.T) {
	svg := Agent("waiting", "AG-9632", 0, 60) // 1 min waiting
	if !strings.Contains(svg, `fill="none"`) {
		t.Error("waiting agent should have hollow ring")
	}
	if !strings.Contains(svg, `stroke="#f59e0b"`) {
		t.Error("waiting agent <5min should be amber stroke")
	}
}

func TestAgentWaitingLong(t *testing.T) {
	svg := Agent("waiting", "AG-9632", 0, 600) // 10 min waiting
	if !strings.Contains(svg, `stroke="#ef4444"`) {
		t.Error("waiting agent >5min should be red stroke")
	}
}

func TestAgentIdle(t *testing.T) {
	svg := Agent("", "AG-9632", 0, 0)
	if !strings.Contains(svg, `r="12"`) {
		t.Error("idle agent should have small circle")
	}
	if !strings.Contains(svg, `fill="#52525b"`) {
		t.Error("idle agent should be gray")
	}
}

func TestAgentShowsID(t *testing.T) {
	svg := Agent("working", "5hptcdnx", 0, 0)
	if !strings.Contains(svg, "cdnx") {
		t.Error("agent tile should show last 4 chars of task ID")
	}
}

func TestAgentDead(t *testing.T) {
	svg := AgentDead("AG-9632")
	if !strings.Contains(svg, `stroke="#ef4444"`) {
		t.Error("dead agent should have red lines")
	}
	if strings.Count(svg, "<line") != 2 {
		t.Error("dead agent should have two lines (X shape)")
	}
	if !strings.Contains(svg, "9632") {
		t.Error("dead agent should show task ID")
	}
}

func TestAttentionNormal(t *testing.T) {
	svg := Attention("plans", "normal", "att-abc123", 0, 1)
	if !strings.Contains(svg, `fill="#f59e0b"`) {
		t.Error("normal attention should use amber")
	}
	if !strings.Contains(svg, "c123") {
		t.Error("attention tile should show last 4 chars of item ID")
	}
}

func TestAttentionUrgent(t *testing.T) {
	svg := Attention("plans", "urgent", "att-abc123", 0, 1)
	if !strings.Contains(svg, `stroke="#ef4444"`) {
		t.Error("urgent attention should have red border")
	}
	if !strings.Contains(svg, `stroke-width="6"`) {
		t.Error("urgent attention should have thick border")
	}
}

func TestAttentionProducerIcons(t *testing.T) {
	producers := []string{"plans", "reviews", "workflows", "workflow_triggers", "unknown"}
	for _, p := range producers {
		svg := Attention(p, "normal", "att-test", 0, 1)
		if !strings.Contains(svg, "<path") && !strings.Contains(svg, "<rect") {
			t.Errorf("producer %q should have an icon element", p)
		}
	}
}

func TestEmptyAgent(t *testing.T) {
	svg := EmptyAgent()
	if !strings.Contains(svg, "stroke-dasharray") {
		t.Error("empty agent should have dashed circle")
	}
	if strings.Contains(svg, "<text") {
		t.Error("empty agent should have no text")
	}
}

func TestEmptyAttention(t *testing.T) {
	svg := EmptyAttention()
	if !strings.Contains(svg, "stroke-dasharray") {
		t.Error("empty attention should have dashed square")
	}
	if strings.Contains(svg, "<text") {
		t.Error("empty attention should have no text")
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

func TestLastChars(t *testing.T) {
	tests := []struct {
		input string
		n     int
		want  string
	}{
		{"5hptcdnx", 4, "cdnx"},
		{"AG-9632", 4, "9632"},
		{"ab", 4, "ab"},
		{"", 4, ""},
	}
	for _, tt := range tests {
		got := lastChars(tt.input, tt.n)
		if got != tt.want {
			t.Errorf("lastChars(%q, %d) = %q, want %q", tt.input, tt.n, got, tt.want)
		}
	}
}

func TestElapsedColor(t *testing.T) {
	tests := []struct {
		seconds float64
		want    string
	}{
		{0, "#22c55e"},
		{1799, "#22c55e"},
		{1800, "#84cc16"},
		{3599, "#84cc16"},
		{3600, "#f59e0b"},
		{7199, "#f59e0b"},
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
