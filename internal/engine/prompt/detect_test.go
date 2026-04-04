package prompt

import (
	"sync"
	"testing"
)

func TestExtractNumberedActions(t *testing.T) {
	tests := []struct {
		name       string
		input      string
		wantLabels []string
		wantKeys   []string
	}{
		{
			name:       "contiguous block",
			input:      "Do you want to run?\n1. Yes\n2. Always\n3. No",
			wantLabels: []string{"Yes", "Always", "No"},
			wantKeys:   []string{"1 Enter", "2 Enter", "3 Enter"},
		},
		{
			name:       "single option",
			input:      "Continue?\n1. OK",
			wantLabels: []string{"OK"},
			wantKeys:   []string{"1 Enter"},
		},
		{
			name:       "many options",
			input:      "Pick one:\n1. Alpha\n2. Bravo\n3. Charlie\n4. Delta\n5. Echo\n6. Foxtrot",
			wantLabels: []string{"Alpha", "Bravo", "Charlie", "Delta", "Echo", "Foxtrot"},
			wantKeys:   []string{"1 Enter", "2 Enter", "3 Enter", "4 Enter", "5 Enter", "6 Enter"},
		},
		{
			name:       "leading whitespace",
			input:      "Allow?\n  1. Yes\n  2. No",
			wantLabels: []string{"Yes", "No"},
			wantKeys:   []string{"1 Enter", "2 Enter"},
		},
		{
			name:       "cursor prefix on selected option",
			input:      "Do you want to proceed?\n❯ 1. Yes\n  2. No",
			wantLabels: []string{"Yes", "No"},
			wantKeys:   []string{"1 Enter", "2 Enter"},
		},
		{
			name:       "angle bracket cursor prefix",
			input:      "Proceed?\n> 1. Yes\n  2. No",
			wantLabels: []string{"Yes", "No"},
			wantKeys:   []string{"1 Enter", "2 Enter"},
		},
		{
			name:  "no matches",
			input: "Just some regular output\nnothing numbered here",
		},
		{
			name:  "empty input",
			input: "",
		},
		{
			name:       "non-contiguous gap breaks extraction",
			input:      "1. First item\nSome text\n1. Yes\n2. No",
			wantLabels: []string{"Yes", "No"},
			wantKeys:   []string{"1 Enter", "2 Enter"},
		},
		{
			name:       "trailing empty lines ignored",
			input:      "Prompt?\n1. Yes\n2. No\n\n  \n",
			wantLabels: []string{"Yes", "No"},
			wantKeys:   []string{"1 Enter", "2 Enter"},
		},
		{
			name:       "hint text after numbered options",
			input:      "Do you want to proceed?\n❯ 1. Yes\n  2. No\n\nEsc to cancel · Tab to amend · ctrl+e to explain",
			wantLabels: []string{"Yes", "No"},
			wantKeys:   []string{"1 Enter", "2 Enter"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			actions := extractNumberedActions(tt.input)
			if tt.wantLabels == nil {
				if actions != nil {
					t.Errorf("got %d actions, want nil", len(actions))
				}
				return
			}
			if len(actions) != len(tt.wantLabels) {
				t.Fatalf("got %d actions, want %d", len(actions), len(tt.wantLabels))
			}
			for i, a := range actions {
				if a.Label != tt.wantLabels[i] {
					t.Errorf("actions[%d].Label = %q, want %q", i, a.Label, tt.wantLabels[i])
				}
				if a.Keys != tt.wantKeys[i] {
					t.Errorf("actions[%d].Keys = %q, want %q", i, a.Keys, tt.wantKeys[i])
				}
			}
		})
	}
}

func TestDetectToolApprovalWithNumberedOptions(t *testing.T) {
	tests := []struct {
		name       string
		input      string
		wantLabels []string
		wantKeys   []string
	}{
		{
			name:       "do you want to run with numbers",
			input:      "Do you want to run this command?\n1. Yes\n2. Yes, and don't ask again\n3. No",
			wantLabels: []string{"Yes", "Yes, and don't ask again", "No"},
			wantKeys:   []string{"1 Enter", "2 Enter", "3 Enter"},
		},
		{
			name:       "allow tool with numbers",
			input:      "Allow Edit on file.go?\n1. Yes\n2. Always\n3. No",
			wantLabels: []string{"Yes", "Always", "No"},
			wantKeys:   []string{"1 Enter", "2 Enter", "3 Enter"},
		},
		{
			name:       "yes no compact with numbers",
			input:      "Some output\n[Y/n]\n1. Yes\n2. No",
			wantLabels: []string{"Yes", "No"},
			wantKeys:   []string{"1 Enter", "2 Enter"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := Detect(tt.input)
			if result.Type != ToolApproval {
				t.Errorf("got type %q, want %q", result.Type, ToolApproval)
			}
			if len(result.Actions) != len(tt.wantLabels) {
				t.Fatalf("got %d actions, want %d", len(result.Actions), len(tt.wantLabels))
			}
			for i, a := range result.Actions {
				if a.Label != tt.wantLabels[i] {
					t.Errorf("actions[%d].Label = %q, want %q", i, a.Label, tt.wantLabels[i])
				}
				if a.Keys != tt.wantKeys[i] {
					t.Errorf("actions[%d].Keys = %q, want %q", i, a.Keys, tt.wantKeys[i])
				}
			}
		})
	}
}

func TestDetectToolApprovalNoNumberedLines(t *testing.T) {
	// Tool approval detected but no numbered lines → no actions (don't guess).
	tests := []struct {
		name  string
		input string
	}{
		{
			name:  "do you want to run without numbers",
			input: "Do you want to run this command?\n[Y/n]",
		},
		{
			name:  "allow tool without numbers",
			input: "Allow Edit on file.go?",
		},
		{
			name:  "yes and dont ask again inline",
			input: "Yes / Yes, and don't ask again / No",
		},
		{
			name:  "simple yes no",
			input: "Do you want to proceed?\nYes / No",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := Detect(tt.input)
			if result.Type != ToolApproval {
				t.Errorf("got type %q, want %q", result.Type, ToolApproval)
			}
			if len(result.Actions) != 0 {
				t.Errorf("got %d actions, want 0 (no numbered lines = no actions)", len(result.Actions))
			}
		})
	}
}

func TestDetectPlanApprovalWithNumberedOptions(t *testing.T) {
	input := "Do you want to proceed with this plan?\n1. Accept\n2. Reject"
	result := Detect(input)
	if result.Type != PlanApproval {
		t.Errorf("got type %q, want %q", result.Type, PlanApproval)
	}
	if len(result.Actions) != 2 {
		t.Fatalf("got %d actions, want 2", len(result.Actions))
	}
	if result.Actions[0].Label != "Accept" || result.Actions[0].Keys != "1 Enter" {
		t.Errorf("action[0] = %+v, want {Accept, 1 Enter}", result.Actions[0])
	}
	if result.Actions[1].Label != "Reject" || result.Actions[1].Keys != "2 Enter" {
		t.Errorf("action[1] = %+v, want {Reject, 2 Enter}", result.Actions[1])
	}
}

func TestDetectPlanApprovalNoNumberedLines(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{
			name:  "accept plan question mark",
			input: "Here's my plan:\n- Do X\n- Do Y\n\nAccept plan?",
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
			if len(result.Actions) != 0 {
				t.Errorf("got %d actions, want 0 (no numbered lines = no actions)", len(result.Actions))
			}
		})
	}
}

func TestDetectFreeTextWithNumberedLinesAbove(t *testing.T) {
	// Free text prompt takes priority — numbered lines above are irrelevant.
	input := "1. Yes\n2. No\n❯ "
	result := Detect(input)
	if result.Type != FreeText {
		t.Errorf("got type %q, want %q", result.Type, FreeText)
	}
	if len(result.Actions) != 0 {
		t.Errorf("got %d actions, want 0 for free text", len(result.Actions))
	}
}

func TestDetectNumberedLinesNonContiguous(t *testing.T) {
	// Gap between numbered blocks — only the bottom block is used.
	input := "Allow Edit on file.go?\n1. First stale option\n2. Second stale option\nSome output\n1. Yes\n2. No"
	result := Detect(input)
	if result.Type != ToolApproval {
		t.Errorf("got type %q, want %q", result.Type, ToolApproval)
	}
	if len(result.Actions) != 2 {
		t.Fatalf("got %d actions, want 2", len(result.Actions))
	}
	if result.Actions[0].Label != "Yes" || result.Actions[0].Keys != "1 Enter" {
		t.Errorf("action[0] = %+v, want {Yes, 1 Enter}", result.Actions[0])
	}
	if result.Actions[1].Label != "No" || result.Actions[1].Keys != "2 Enter" {
		t.Errorf("action[1] = %+v, want {No, 2 Enter}", result.Actions[1])
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
		"Allow Edit on file.go?\n1. Yes\n2. Always\n3. No",
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
