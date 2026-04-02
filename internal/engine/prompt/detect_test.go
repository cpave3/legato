package prompt

import (
	"sync"
	"testing"
)

func TestDetectToolApproval(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{
			name:  "do you want to run",
			input: "Do you want to run this command?\n[Y/n]",
		},
		{
			name:  "allow tool",
			input: "Allow Edit on file.go?",
		},
		{
			name:  "yes no compact",
			input: "Some output\n[Y/n]",
		},
		{
			name:  "yes and dont ask again",
			input: "Yes / Yes, and don't ask again / No",
		},
		{
			name:  "simple yes no",
			input: "Do you want to proceed?\nYes / No",
		},
		{
			name:  "allow with tool name",
			input: "Allow Run on bash command?",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := Detect(tt.input)
			if result.Type != ToolApproval {
				t.Errorf("got type %q, want %q", result.Type, ToolApproval)
			}
			if len(result.Actions) != 3 {
				t.Errorf("got %d actions, want 3", len(result.Actions))
			}
		})
	}
}

func TestDetectPlanApproval(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{
			name:  "accept plan question mark",
			input: "Here's my plan:\n1. Do X\n2. Do Y\n\nAccept plan?",
		},
		{
			name:  "do you want to proceed with plan",
			input: "Do you want to proceed with this plan?",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := Detect(tt.input)
			if result.Type != PlanApproval {
				t.Errorf("got type %q, want %q", result.Type, PlanApproval)
			}
			if len(result.Actions) != 2 {
				t.Errorf("got %d actions, want 2", len(result.Actions))
			}
			if result.Actions[0].Label != "Accept" {
				t.Errorf("first action label = %q, want Accept", result.Actions[0].Label)
			}
		})
	}
}

func TestDetectFreeText(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{
			name:  "arrow prompt",
			input: "Welcome to Claude Code\n❯ ",
		},
		{
			name:  "angle bracket prompt",
			input: "Ready for input\n> ",
		},
		{
			name:  "prompt at end with trailing space",
			input: "Some output here\n❯  ",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := Detect(tt.input)
			if result.Type != FreeText {
				t.Errorf("got type %q, want %q", result.Type, FreeText)
			}
			if len(result.Actions) != 0 {
				t.Errorf("got %d actions, want 0 for free text", len(result.Actions))
			}
		})
	}
}

func TestDetectDefaultsToFreeText(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{
			name:  "unrecognized output",
			input: "Reading file src/main.go...\nAnalyzing code structure...",
		},
		{
			name:  "empty output",
			input: "",
		},
		{
			name:  "whitespace only",
			input: "   \n\n  ",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := Detect(tt.input)
			if result.Type != FreeText {
				t.Errorf("got type %q, want %q", result.Type, FreeText)
			}
		})
	}
}

func TestDetectWithANSICodes(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		wantType PromptType
	}{
		{
			name:     "colored tool approval",
			input:    "\x1b[1;33mAllow\x1b[0m \x1b[36mEdit\x1b[0m on file.go?",
			wantType: ToolApproval,
		},
		{
			name:     "colored plan approval",
			input:    "\x1b[1mAccept plan?\x1b[0m",
			wantType: PlanApproval,
		},
		{
			name:     "colored free text prompt",
			input:    "output\n\x1b[32m❯\x1b[0m ",
			wantType: FreeText,
		},
		{
			name:     "colored unrecognized output defaults to free text",
			input:    "\x1b[2mReading files...\x1b[0m",
			wantType: FreeText,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := Detect(tt.input)
			if result.Type != tt.wantType {
				t.Errorf("got type %q, want %q", result.Type, tt.wantType)
			}
		})
	}
}

func TestDetectConcurrentSafety(t *testing.T) {
	inputs := []string{
		"Allow Edit on file.go?",
		"Accept plan?",
		"❯ ",
		"Working on task...",
	}

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			_ = Detect(inputs[idx%len(inputs)])
		}(i)
	}
	wg.Wait()
}

func TestStripANSI(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"\x1b[31mred\x1b[0m", "red"},
		{"\x1b[1;33mbold yellow\x1b[0m", "bold yellow"},
		{"no codes", "no codes"},
		{"", ""},
	}

	for _, tt := range tests {
		got := stripANSI(tt.input)
		if got != tt.want {
			t.Errorf("stripANSI(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}
