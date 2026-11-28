package hooks_test

import (
	"os"
	"path/filepath"
	"regexp"
	"testing"
)

// Claude Code's bundled security mod (cc-plugin-sec-default, 2.1.330 and
// later) is seated outermost on Team and Enterprise plans, among others, and
// passes these events over every mod a person installs. Legato's mods load
// in that tier, so a hook on one of them never runs there, and Claude Code
// says so in the session each time (LEG-684). Its own list is exported as
// PASSED_OVER_EVENTS in the engine's mod API.
var passedOverEvents = regexp.MustCompile(`^(classic\..*|prompt\.(section|context|attachment|compose)|skill\.prompt|attribution\.text|session\.(compact|info|redact|identity)|settings\.read|telemetry\.log)$`)

// hookRegistration is an `on('<event>', ...)` in a mod's module, with the
// matcher object when one follows the event.
var hookRegistration = regexp.MustCompile(`\bon\(\s*'([^']+)'\s*,\s*(\{[^}]*\})?`)

func TestLegatosClaudeModsHookNoEventClaudeCodePassesOver(t *testing.T) {
	modules, err := filepath.Glob(filepath.Join("claudemod", "*", "hooks", "*.ts"))
	if err != nil || len(modules) == 0 {
		t.Fatalf("no mod modules found: %v", err)
	}
	for _, module := range modules {
		source, err := os.ReadFile(module)
		if err != nil {
			t.Fatal(err)
		}
		for _, match := range hookRegistration.FindAllStringSubmatch(string(source), -1) {
			event, matcher := match[1], match[2]
			if passedOverEvents.MatchString(event) {
				t.Errorf("%s hooks %s, which Claude Code passes over: answer it from a command hook in the legato-state plugin", module, event)
			}
			// session.append is passed over by door: attachments, hook
			// context and compaction always.
			if event == "session.append" && !regexp.MustCompile(`\bdoor\s*:`).MatchString(matcher) {
				t.Errorf("%s hooks session.append without a door matcher; name the doors it needs, never attachment, hook-context or compaction", module)
			}
		}
	}
}

// The doors the Legato mod relays leave out those the security mod always
// passes over.
func TestTheLegatoModRelaysNoDoorClaudeCodePassesOver(t *testing.T) {
	source, err := os.ReadFile(filepath.Join("claudemod", "legato", "hooks", "register.ts"))
	if err != nil {
		t.Fatal(err)
	}
	doors := regexp.MustCompile(`const RELAYED_DOORS = \[([^\]]*)\]`).FindSubmatch(source)
	if doors == nil {
		t.Fatal("register.ts has no RELAYED_DOORS")
	}
	for _, door := range []string{"attachment", "hook-context", "compaction"} {
		if regexp.MustCompile(`'` + door + `'`).Match(doors[1]) {
			t.Errorf("RELAYED_DOORS names %s, which Claude Code passes over", door)
		}
	}
}
