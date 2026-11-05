package theme

import "testing"

func TestUnicodeIcons(t *testing.T) {
	icons := NewIcons("unicode")
	if icons.Jira == "" {
		t.Error("Jira icon should not be empty")
	}
	if icons.Local == "" {
		t.Error("Local icon should not be empty")
	}
	if icons.Terminal == "" {
		t.Error("Terminal icon should not be empty")
	}
	if icons.Worktree == "" {
		t.Error("Worktree icon should not be empty")
	}
}

func TestNerdFontIcons(t *testing.T) {
	icons := NewIcons("nerdfonts")
	if icons.Jira == icons.Local {
		t.Error("Jira and Local icons should differ")
	}
	// Nerd font glyphs are multi-byte
	if len(icons.Jira) < 2 {
		t.Error("Nerd font Jira icon should be a multi-byte glyph")
	}
}

func TestDefaultIconsAreUnicode(t *testing.T) {
	unicode := NewIcons("unicode")
	fallback := NewIcons("")
	if unicode.Jira != fallback.Jira {
		t.Error("empty mode should default to unicode")
	}
}

func TestBurnliteIconInBothModes(t *testing.T) {
	for _, mode := range []string{"unicode", "nerdfonts"} {
		icons := NewIcons(mode)
		if icons.Burnlite == "" {
			t.Errorf("%s: Burnlite icon should not be empty", mode)
		}
		for name, other := range map[string]string{"Jira": icons.Jira, "GitHub": icons.GitHub, "Local": icons.Local} {
			if icons.Burnlite == other {
				t.Errorf("%s: Burnlite icon should differ from %s", mode, name)
			}
		}
	}
}

func TestProviderIcon(t *testing.T) {
	icons := NewIcons("unicode")
	// A worker's copy of a handed-off Jira task shows as Jira's.
	cases := map[string]string{"jira": icons.Jira, "jira-copy": icons.Jira, "github": icons.GitHub, "burnlite": icons.Burnlite, "linear": "", "": ""}
	for provider, want := range cases {
		if got := icons.Provider(provider); got != want {
			t.Errorf("Provider(%q) = %q, want %q", provider, got, want)
		}
	}
}

func TestProviderName(t *testing.T) {
	cases := map[string]string{"jira": "Jira", "jira-copy": "Jira", "github": "GitHub", "burnlite": "Burnlite", "linear": "linear", "": ""}
	for provider, want := range cases {
		if got := ProviderName(provider); got != want {
			t.Errorf("ProviderName(%q) = %q, want %q", provider, got, want)
		}
	}
}
