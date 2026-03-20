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
