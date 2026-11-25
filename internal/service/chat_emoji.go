package service

import (
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"
)

// The emoji that chat steps know by name, shared by both platforms: Slack
// reacts by name and Discord by the emoji itself, so a workflow can name
// either on either. The first name of each emoji is Slack's own. It is the
// common ones only; Discord takes any emoji given as itself, and Slack any
// name it knows (its custom emoji and skin tones included).
var chatEmoji = []struct {
	emoji string
	names []string
}{
	{"👀", []string{"eyes"}},
	{"✅", []string{"white_check_mark"}},
	{"✔️", []string{"heavy_check_mark"}},
	{"❌", []string{"x"}},
	{"⚠️", []string{"warning"}},
	{"⌛", []string{"hourglass"}},
	{"⏳", []string{"hourglass_flowing_sand"}},
	{"🚀", []string{"rocket"}},
	{"🎉", []string{"tada"}},
	{"👍", []string{"+1", "thumbsup"}},
	{"👎", []string{"-1", "thumbsdown"}},
	{"👌", []string{"ok_hand"}},
	{"👋", []string{"wave"}},
	{"🙌", []string{"raised_hands"}},
	{"🙏", []string{"pray"}},
	{"🔥", []string{"fire"}},
	{"🐛", []string{"bug"}},
	{"📝", []string{"memo"}},
	{"🚨", []string{"rotating_light"}},
	{"🚧", []string{"construction"}},
	{"⛔", []string{"no_entry"}},
	{"❓", []string{"question"}},
	{"❗", []string{"exclamation"}},
	{"❤️", []string{"heart"}},
	{"💯", []string{"100"}},
	{"⚡", []string{"zap"}},
	{"✨", []string{"sparkles"}},
	{"🤔", []string{"thinking_face"}},
	{"🤖", []string{"robot_face"}},
	{"💬", []string{"speech_balloon"}},
}

var (
	chatEmojiByName = map[string]string{}
	// chatEmojiNames is keyed by the emoji without variation selectors.
	chatEmojiNames = map[string]string{}
)

func init() {
	for _, e := range chatEmoji {
		chatEmojiNames[withoutVariation(e.emoji)] = e.names[0]
		for _, name := range e.names {
			chatEmojiByName[name] = e.emoji
		}
	}
}

func withoutVariation(s string) string { return strings.ReplaceAll(s, "️", "") }

func isASCII(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] >= utf8.RuneSelf {
			return false
		}
	}
	return true
}

// trimEmoji is what a workflow gave as an emoji, without spaces and the
// colons around a name.
func trimEmoji(emoji string) string { return strings.Trim(strings.TrimSpace(emoji), ":") }

// slackReactionName is the name reactions.add takes for emoji: a name, with
// or without colons, as it is; an emoji Legato knows by its name.
func slackReactionName(emoji string) (string, error) {
	e := trimEmoji(emoji)
	switch {
	case e == "":
		return "", fmt.Errorf("no emoji given")
	case isASCII(e):
		return e, nil
	}
	if name, ok := chatEmojiNames[withoutVariation(e)]; ok {
		return name, nil
	}
	return "", fmt.Errorf("reactions on Slack take an emoji's name, such as eyes; %s is not one Legato knows the name of", e)
}

var discordCustomEmoji = regexp.MustCompile(`^[A-Za-z0-9_~-]+:[0-9]+$`)

// discordReactionEmoji is the emoji Discord's reaction routes take: a name
// Legato knows as its emoji, an emoji as itself, a custom emoji as name:id.
func discordReactionEmoji(emoji string) (string, error) {
	e := trimEmoji(emoji)
	if e == "" {
		return "", fmt.Errorf("no emoji given")
	}
	if u, ok := chatEmojiByName[e]; ok {
		return u, nil
	}
	if !isASCII(e) || discordCustomEmoji.MatchString(e) {
		return e, nil
	}
	return "", fmt.Errorf("unknown emoji name %q on Discord: give the emoji itself (👀) or a custom emoji as name:id", e)
}

var emojiShortcode = regexp.MustCompile(`:[a-z0-9_+-]+:`)

// expandEmojiShortcodes turns the :names: Legato knows into their emoji, for
// Discord, which shows a bot's :eyes: as text. Unknown names and anything in
// a code span or block stay as they are.
func expandEmojiShortcodes(text string) string {
	if !strings.Contains(text, ":") {
		return text
	}
	var out strings.Builder
	expand := func(s string) {
		out.WriteString(emojiShortcode.ReplaceAllStringFunc(s, func(code string) string {
			if u, ok := chatEmojiByName[code[1:len(code)-1]]; ok {
				return u
			}
			return code
		}))
	}
	for text != "" {
		start := strings.IndexByte(text, '`')
		if start < 0 {
			expand(text)
			break
		}
		expand(text[:start])
		text = text[start:]
		fence := len(text) - len(strings.TrimLeft(text, "`"))
		end := strings.Index(text[fence:], text[:fence])
		if end < 0 {
			// An unmatched run of backticks is only backticks.
			out.WriteString(text[:fence])
			text = text[fence:]
			continue
		}
		stop := fence + end + fence
		out.WriteString(text[:stop])
		text = text[stop:]
	}
	return out.String()
}
