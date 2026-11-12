package service

import (
	"net/url"
	"os"
	"regexp"
	"strings"

	"github.com/cpave3/legato/config"
)

// ReeveLevel is what a call the Reeve proposes needs before it runs.
type ReeveLevel string

const (
	// LevelFree: a read, which needs nothing.
	LevelFree ReeveLevel = "free"
	// LevelAuto: a change that runs at once, with no approval asked. The
	// person chose it for a class, a rule or host operations; the set is
	// still recorded, shown and audited.
	LevelAuto ReeveLevel = "auto"
	// LevelApprove: a tap.
	LevelApprove ReeveLevel = "approve"
	// LevelSigned: a decision signed on an enrolled device.
	LevelSigned ReeveLevel = "signed"
	// LevelVerified: signed, by a device that verified the person (a
	// fingerprint or a PIN), not only a touch.
	LevelVerified ReeveLevel = "verified"
)

var levelRank = map[ReeveLevel]int{LevelFree: 0, LevelAuto: 1, LevelApprove: 2, LevelSigned: 3, LevelVerified: 4}

// StrictestLevel is the strictest of levels: a change set's level.
func StrictestLevel(levels ...ReeveLevel) ReeveLevel {
	out := LevelFree
	for _, l := range levels {
		if levelRank[l] > levelRank[out] {
			out = l
		}
	}
	return out
}

// LevelCall is a call whose level is resolved.
type LevelCall struct {
	// Class is the call's class: read, change or risky.
	Class string
	// Calls are the forms of the call rules are matched against (METHOD
	// /path, legato <args>, host_shell:<command>, …): a rule matches when
	// it matches any of them.
	Calls []string
	// Host: a host operation, which reeve.host_operations_auto covers when
	// it is not as root.
	Host   bool
	AsRoot bool
	// Compound: a host call a narrow rule cannot make auto, because what
	// it runs is more than the rule names (a shell command with ;, |, &&,
	// $(…) or a redirect; a file path that is not in its clean form).
	Compound bool
	// Floor is the least the call needs, whatever the levels and rules
	// say; FloorWhy says why.
	Floor    ReeveLevel
	FloorWhy string
}

// LevelDecision is a call's level and what set it: the class's level
// (reeve.levels.change), a rule (rule <pattern>), the host auto setting,
// a floor (its reason), or "as root".
type LevelDecision struct {
	Level ReeveLevel `json:"level"`
	By    string     `json:"by,omitempty"`
}

// ReeveLevelFor is the level a call of a class needs (see
// ResolveReeveLevel).
func ReeveLevelFor(cfg config.ReeveConfig, class, call string, enrolled bool) ReeveLevel {
	return ResolveReeveLevel(cfg, LevelCall{Class: class, Calls: []string{call}}, enrolled).Level
}

// ResolveReeveLevel is the level a call needs, in this order:
//   - a read is free;
//   - the class's level (Settings › Reeve): change is reeve.levels.change
//     (approve when empty); risky is reeve.levels.risky, whose default is
//     signed once a device is enrolled, else approve;
//   - a host operation not as root is auto while
//     reeve.host_operations_auto is on;
//   - the last rule whose pattern matches the call sets it, up or down
//     (a narrow rule never makes a compound host call auto);
//   - the call's floor, which nothing lowers;
//   - a host call as root is verified, always.
//
// A signed or verified level with no device enrolled stays as it is: such
// a change is refused, never downgraded to a tap.
func ResolveReeveLevel(cfg config.ReeveConfig, call LevelCall, enrolled bool) LevelDecision {
	if call.Class == "read" {
		return LevelDecision{Level: LevelFree, By: "read"}
	}
	d := LevelDecision{Level: LevelApprove, By: "reeve.levels.risky"}
	switch call.Class {
	case "change":
		d = LevelDecision{Level: parseLevel(cfg.Levels.Change, LevelApprove), By: "reeve.levels.change"}
	default:
		switch strings.TrimSpace(cfg.Levels.Risky) {
		case "", "default":
			if enrolled {
				d.Level = LevelSigned
			}
		default:
			d.Level = parseLevel(cfg.Levels.Risky, LevelSigned)
		}
	}
	if call.Host && !call.AsRoot && cfg.HostOperationsAutoOn() {
		d = LevelDecision{Level: LevelAuto, By: "reeve.host_operations_auto"}
	}
	for _, rule := range cfg.Rules {
		if !matchesAny(rule.Pattern, call.Calls) {
			continue
		}
		level := parseLevel(rule.Level, d.Level)
		if level == LevelAuto && call.Compound {
			continue
		}
		d = LevelDecision{Level: level, By: "rule " + strings.TrimSpace(rule.Pattern)}
	}
	if levelRank[d.Level] < levelRank[LevelAuto] {
		d.Level = LevelAuto
	}
	if call.Floor != "" && levelRank[d.Level] < levelRank[call.Floor] {
		d = LevelDecision{Level: call.Floor, By: call.FloorWhy}
	}
	if call.AsRoot && d.Level != LevelVerified {
		d = LevelDecision{Level: LevelVerified, By: "as root"}
	}
	return d
}

func parseLevel(s string, fallback ReeveLevel) ReeveLevel {
	switch l := ReeveLevel(strings.TrimSpace(s)); l {
	case LevelAuto, LevelApprove, LevelSigned, LevelVerified:
		return l
	}
	return fallback
}

func matchesAny(pattern string, calls []string) bool {
	// ~ in a host rule (edit_file:~/Projects/**) is the home folder.
	if kind, rest, ok := strings.Cut(strings.TrimSpace(pattern), ":~"); ok && !strings.Contains(kind, " ") {
		if home, err := os.UserHomeDir(); err == nil {
			pattern = kind + ":" + home + rest
		}
	}
	for _, call := range calls {
		if matchCall(pattern, call) {
			return true
		}
	}
	return false
}

// matchCall reports whether a rule's pattern matches a call: * stands for
// any run of characters, case ignored.
func matchCall(pattern, call string) bool {
	parts := strings.Split(strings.TrimSpace(pattern), "*")
	for i, p := range parts {
		parts[i] = regexp.QuoteMeta(p)
	}
	re, err := regexp.Compile("(?i)^" + strings.Join(parts, ".*") + "$")
	return err == nil && re.MatchString(strings.TrimSpace(call))
}

// floorSettingPrefixes are the settings and lists that decide what Legato
// guards: credentials, the sandbox, what runs on the host and with which
// arguments, and approvals. A change to one is never automatic.
var floorSettingPrefixes = []string{
	"credentials", "credential_sources", "credential_channels.", "agents.sandbox.", "ssh_agent_socket",
	"github.auth", "github.token", "notifications.approvals", "review_passes.github_credential",
	"adapters", "integrations.", "learning.embedding.api_key_env",
	"reeve.voice.command", "reeve.voice.credential", "reeve.voice.player", "reeve.launch_args",
}

// floorRoutePrefixes are routes that pair, connect or hand out access.
var floorRoutePrefixes = []string{
	"/api/credentials", "/api/pairing", "/api/pair/", "/api/devices", "/api/settings/workers",
	"/api/settings/mains", "/api/settings/integrations", "/api/reeve/peers", "/api/integrations/handoff/pairing",
}

// apiFloor is the least an API call needs, whatever is set to auto:
//   - a deletion (DELETE) is never automatic;
//   - a change to one of the Reeve's own rules (which still needs its own
//     signature when it runs) or its seed;
//   - a security setting or a route that pairs or connects.
func apiFloor(method, target string) (ReeveLevel, string) {
	if method == "DELETE" {
		return LevelApprove, "a deletion"
	}
	if target == "/api/reeve/engram/seed" {
		return LevelApprove, "one of the Reeve's own rules (its seed)"
	}
	for _, prefix := range []string{"/api/config/settings/", "/api/config/lists/"} {
		rest, ok := strings.CutPrefix(target, prefix)
		if !ok {
			continue
		}
		key, _, _ := strings.Cut(rest, "/")
		if k, err := url.PathUnescape(key); err == nil {
			key = k
		}
		if isOwnRule(key) {
			return LevelApprove, "one of the Reeve's own rules (" + key + ")"
		}
		if s, ok := config.LookupSetting(key); ok && s.Secret {
			return LevelApprove, "a security setting (" + key + ")"
		}
		for _, p := range floorSettingPrefixes {
			if key == strings.TrimSuffix(p, ".") || strings.HasPrefix(key, p) {
				return LevelApprove, "a security setting (" + key + ")"
			}
		}
	}
	for _, p := range floorRoutePrefixes {
		if strings.HasPrefix(target, p) {
			return LevelApprove, "pairing, connecting or access"
		}
	}
	return "", ""
}

// legatoFloor is the least a legato command needs: secrets and
// integrations (tokens, connections) are never automatic, nor is removing
// or discarding anything.
func legatoFloor(args []string) (ReeveLevel, string) {
	if len(args) == 0 {
		return "", ""
	}
	switch args[0] {
	case "secret", "integration", "credential", "auth", "device", "pair":
		return LevelApprove, "secrets, tokens and connections"
	}
	for _, a := range args[1:] {
		if a == "remove" || a == "discard" || a == "delete" {
			return LevelApprove, "a deletion"
		}
	}
	return "", ""
}
