package service

import (
	"context"
	"encoding/json"

	"github.com/cpave3/legato/config"
)

// ReeveName is what this instance calls its Reeve (reeve.name), "" for
// none. Every surface that names the Reeve to a person asks it, so an
// instance with no name still says "the Reeve" and one with a name says it
// in the same places. The role keeps its name: settings keys, routes, IDs
// and the manual still say Reeve.
type ReeveName string

// ReeveNameOf is the Reeve's name in a configuration: reeve.name trimmed,
// or "" when it is empty, invalid, or there is no configuration.
func ReeveNameOf(cfg *config.Config) ReeveName {
	if cfg == nil {
		return ""
	}
	return ReeveName(cfg.Reeve.DisplayName())
}

// ReeveNameSource reads the Reeve's name now, so a change in Settings shows
// without a restart. A nil source is a Reeve with no name.
type ReeveNameSource func(ctx context.Context) ReeveName

// Of is the name the source gives now; a nil source gives none.
func (s ReeveNameSource) Of(ctx context.Context) ReeveName {
	if s == nil {
		return ""
	}
	return s(ctx)
}

// Label is the name on its own, as a title or a tag: "Reeve" when unnamed.
func (n ReeveName) Label() string {
	if n == "" {
		return "Reeve"
	}
	return string(n)
}

// Start is the name at the start of a sentence: "The Reeve" when unnamed.
func (n ReeveName) Start() string {
	if n == "" {
		return "The Reeve"
	}
	return string(n)
}

// Mid is the name inside a sentence: "the Reeve" when unnamed.
func (n ReeveName) Mid() string {
	if n == "" {
		return "the Reeve"
	}
	return string(n)
}

// ReeveNamePrompt is the line of the Reeve's launch prompt that tells it
// its name on this instance, "" when it has none.
func ReeveNamePrompt(name ReeveName) string {
	if name == "" {
		return ""
	}
	return "## Your name\n\nOn this instance you go by " + string(name) + ": Legato shows that name wherever it would say \"the Reeve\", and the operator may call you by it. Your role is still the Reeve, and the manual, settings and routes still say Reeve."
}

// IsReeveApprovalSource says whether approvals of a source are the
// Reeve's: they carry its name and say it.
func IsReeveApprovalSource(source string) bool {
	switch source {
	case ApprovalSourceReeveChangeSet, ApprovalSourceReeveRules, ApprovalSourceReeveSecret, ApprovalSourceReeveSeed:
		return true
	}
	return false
}

// withReeveName adds reeve_name to an approval's details object, so the
// tray, the desktop widget and a paired instance show the name of the
// Reeve that asked, as it was then. Unnamed, or details that are not an
// object, are left as they are.
func withReeveName(details []byte, name ReeveName) []byte {
	if name == "" {
		return details
	}
	var obj map[string]json.RawMessage
	if json.Unmarshal(details, &obj) != nil || obj == nil {
		return details
	}
	obj["reeve_name"], _ = json.Marshal(string(name))
	out, err := json.Marshal(obj)
	if err != nil {
		return details
	}
	return out
}
