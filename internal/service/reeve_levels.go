package service

import (
	"regexp"
	"strings"

	"github.com/cpave3/legato/config"
)

// ReeveLevel is what a call the Reeve proposes needs before it runs.
type ReeveLevel string

const (
	// LevelFree: a read, which needs nothing.
	LevelFree ReeveLevel = "free"
	// LevelApprove: a tap.
	LevelApprove ReeveLevel = "approve"
	// LevelSigned: a decision signed on an enrolled device.
	LevelSigned ReeveLevel = "signed"
	// LevelVerified: signed, by a device that verified the person (a
	// fingerprint or a PIN), not only a touch.
	LevelVerified ReeveLevel = "verified"
)

var levelRank = map[ReeveLevel]int{LevelFree: 0, LevelApprove: 1, LevelSigned: 2, LevelVerified: 3}

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

// ReeveLevelFor is the level a call of a class needs: the class's level
// (Settings › Reeve), then the last rule whose pattern matches the call.
// A read is free and a change never falls below approve. Risky's auto (the
// default) is signed once a device is enrolled, else approve. A signed or
// verified level with no device enrolled stays as it is: such a change is
// refused, never downgraded to a tap.
func ReeveLevelFor(cfg config.ReeveConfig, class, call string, enrolled bool) ReeveLevel {
	if class == "read" {
		return LevelFree
	}
	level := LevelApprove
	switch class {
	case "change":
		level = parseLevel(cfg.Levels.Change, LevelApprove)
	case "risky", "":
		switch cfg.Levels.Risky {
		case "", "auto":
			if enrolled {
				level = LevelSigned
			}
		default:
			level = parseLevel(cfg.Levels.Risky, LevelSigned)
		}
	}
	for _, rule := range cfg.Rules {
		if matchCall(rule.Pattern, call) {
			level = parseLevel(rule.Level, level)
		}
	}
	if levelRank[level] < levelRank[LevelApprove] {
		level = LevelApprove
	}
	return level
}

func parseLevel(s string, fallback ReeveLevel) ReeveLevel {
	switch ReeveLevel(strings.TrimSpace(s)) {
	case LevelApprove:
		return LevelApprove
	case LevelSigned:
		return LevelSigned
	case LevelVerified:
		return LevelVerified
	}
	return fallback
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
