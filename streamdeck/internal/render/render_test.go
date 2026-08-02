package render

import (
	"strings"
	"testing"
)

func TestAgentWorkingGreen(t *testing.T) {
	svg := Agent("working", "5hptcdnx", 0, 0, 0.3, false)
	if !strings.Contains(svg, `fill="#22c55e"`) {
		t.Error("working agent should always be green")
	}
}

func TestAgentWaiting(t *testing.T) {
	svg := Agent("waiting", "AG-9632", 0, 60, -1, false)
	if !strings.Contains(svg, `fill="#f59e0b"`) {
		t.Error("waiting agent should be amber")
	}
}

func TestAgentIdle(t *testing.T) {
	svg := Agent("", "AG-9632", 0, 0, -1, false)
	if !strings.Contains(svg, `fill="#52525b"`) {
		t.Error("idle agent should be gray")
	}
}

func TestAgentShowsID(t *testing.T) {
	svg := Agent("working", "5hptcdnx", 0, 0, 0.3, false)
	if !strings.Contains(svg, "cdnx") {
		t.Error("agent tile should show last 4 chars of task ID")
	}
}

func TestAgentNotifyDot(t *testing.T) {
	svg := Agent("working", "AG-9632", 0, 0, 0.3, true)
	if !strings.Contains(svg, `#a855f7`) {
		t.Error("agent with notify enabled should have purple dot")
	}
}

func TestAgentNoNotifyDot(t *testing.T) {
	svg := Agent("working", "AG-9632", 0, 0, 0.3, false)
	if strings.Contains(svg, "#a855f7") {
		t.Error("agent with notify disabled should not have purple dot")
	}
}

func TestAgentContextPressureGreen(t *testing.T) {
	svg := Agent("working", "AG-9632", 0, 0, 0.2, false)
	if !strings.Contains(svg, `fill="#22c55e"`) {
		t.Error("low context pressure should show green bar")
	}
}

func TestAgentContextPressureAmber(t *testing.T) {
	svg := Agent("working", "AG-9632", 0, 0, 0.6, false)
	if !strings.Contains(svg, `fill="#f59e0b"`) {
		t.Error("medium context pressure should show amber bar")
	}
}

func TestAgentContextPressureRed(t *testing.T) {
	svg := Agent("working", "AG-9632", 0, 0, 0.9, false)
	if !strings.Contains(svg, `fill="#ef4444"`) {
		t.Error("high context pressure should show red bar")
	}
}

func TestAgentContextPressureUnknown(t *testing.T) {
	svg := Agent("", "AG-9632", 0, 0, -1, false)
	if !strings.Contains(svg, "stroke-dasharray") {
		t.Error("unknown context pressure should show dashed bar")
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

func TestAttentionShowsNavTarget(t *testing.T) {
	svg := Attention("reviews", "normal", "rt-AG-9645", 0, 1)
	if !strings.Contains(svg, "9645") {
		t.Error("attention tile should show last 4 chars of navigation target")
	}
	if strings.Contains(svg, "a5d2") {
		t.Error("attention tile should NOT show the attention entry ID")
	}
}

func TestAttentionNormal(t *testing.T) {
	svg := Attention("plans", "normal", "pl-abc123", 0, 1)
	if !strings.Contains(svg, `fill="#f59e0b"`) {
		t.Error("normal attention should use amber")
	}
	if !strings.Contains(svg, "c123") {
		t.Error("attention tile should show nav target last 4")
	}
}

func TestAttentionUrgent(t *testing.T) {
	svg := Attention("plans", "urgent", "pl-abc123", 0, 1)
	if !strings.Contains(svg, `stroke="#ef4444"`) {
		t.Error("urgent attention should have red border")
	}
	if !strings.Contains(svg, `stroke-width="6"`) {
		t.Error("urgent attention should have thick border")
	}
}

func TestAttentionLearningUsesLightbulbIcon(t *testing.T) {
	svg := Attention("learnings", "normal", "learn-1234", 0, 1)
	if !strings.Contains(svg, `data-icon="learning-lightbulb"`) {
		t.Fatalf("learning attention should use lightbulb icon: %s", svg)
	}
}

func TestAttentionProducerIcons(t *testing.T) {
	producers := []string{"plans", "reviews", "workflows", "workflow_triggers", "learnings", "unknown"}
	for _, p := range producers {
		svg := Attention(p, "normal", "test1234", 0, 1)
		if !strings.Contains(svg, "<path") && !strings.Contains(svg, "<rect") {
			t.Errorf("producer %q should have an icon element", p)
		}
	}
}

func TestEmptyAgent(t *testing.T) {
	svg := EmptyAgent()
	if !strings.Contains(svg, "stroke-dasharray") {
		t.Error("empty agent should have dashed border")
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
		{"rt-AG-9645", 4, "9645"},
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

func TestPressureColor(t *testing.T) {
	tests := []struct {
		pressure float64
		want     string
	}{
		{-1, DarkGray},
		{0.0, Green},
		{0.3, Green},
		{0.49, Green},
		{0.5, Amber},
		{0.7, Amber},
		{0.74, Amber},
		{0.75, Red},
		{0.9, Red},
		{1.0, Red},
	}
	for _, tt := range tests {
		got := pressureColor(tt.pressure)
		if got != tt.want {
			t.Errorf("pressureColor(%.2f) = %q, want %q", tt.pressure, got, tt.want)
		}
	}
}
