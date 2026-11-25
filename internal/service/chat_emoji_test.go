package service

import (
	"strings"
	"testing"
)

func TestSlackReactionNames(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"eyes", "eyes"},
		{":eyes:", "eyes"},
		{" eyes ", "eyes"},
		{"👀", "eyes"},
		{"✅", "white_check_mark"},
		{"+1", "+1"},
		// Slack knows its own custom emoji and skin tones.
		{"partyparrot", "partyparrot"},
		{"thumbsup::skin-tone-2", "thumbsup::skin-tone-2"},
	} {
		got, err := slackReactionName(tc.in)
		if err != nil || got != tc.want {
			t.Errorf("slackReactionName(%q) = %q, %v; want %q", tc.in, got, err, tc.want)
		}
	}
	for _, in := range []string{"", "  ", "::", "🦩"} {
		if got, err := slackReactionName(in); err == nil {
			t.Errorf("slackReactionName(%q) = %q, want an error", in, got)
		}
	}
	_, err := slackReactionName("🦩")
	if err == nil || !strings.Contains(err.Error(), "name") {
		t.Errorf("error for Unicode Legato does not know = %v", err)
	}
}

func TestDiscordReactionEmoji(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"eyes", "👀"},
		{":eyes:", "👀"},
		{"white_check_mark", "✅"},
		{"thumbsup", "👍"},
		{"+1", "👍"},
		{"👀", "👀"},
		{"🦩", "🦩"},
		{"party:123456", "party:123456"},
		{":party:123456:", "party:123456"},
	} {
		got, err := discordReactionEmoji(tc.in)
		if err != nil || got != tc.want {
			t.Errorf("discordReactionEmoji(%q) = %q, %v; want %q", tc.in, got, err, tc.want)
		}
	}
	for _, in := range []string{"", "partyparrot", "party:abc"} {
		if got, err := discordReactionEmoji(in); err == nil {
			t.Errorf("discordReactionEmoji(%q) = %q, want an error", in, got)
		}
	}
	_, err := discordReactionEmoji("partyparrot")
	if err == nil || !strings.Contains(err.Error(), "name:id") {
		t.Errorf("error for an unknown name = %v", err)
	}
}

func TestDiscordTextExpandsKnownShortcodes(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"Looking :eyes:", "Looking 👀"},
		{":white_check_mark: done :tada:", "✅ done 🎉"},
		{"Already 👀", "Already 👀"},
		{"unknown :partyparrot: stays", "unknown :partyparrot: stays"},
		{"time 10:30:00 stays", "time 10:30:00 stays"},
		{"code `:eyes:` stays, :eyes: goes", "code `:eyes:` stays, 👀 goes"},
		{"```\n:eyes:\n```\n:eyes:", "```\n:eyes:\n```\n👀"},
		{"an unclosed `:eyes:", "an unclosed `👀"},
		{"", ""},
	} {
		if got := expandEmojiShortcodes(tc.in); got != tc.want {
			t.Errorf("expandEmojiShortcodes(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
