package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/cpave3/legato/config"
	"github.com/cpave3/legato/internal/engine/macros"
	"github.com/cpave3/legato/internal/engine/store"
)

// Where a macro comes from.
const (
	MacroBuiltin      = "builtin"     // shipped in code, unedited
	MacroOverride     = "override"    // a built-in edited here (a database entry of its name)
	MacroFromFile     = "config.yaml" // config.yaml's, pinned
	MacroFromDatabase = "database"    // added here
)

var (
	// ErrMacroNotFound is a name no macro has.
	ErrMacroNotFound = errors.New("no macro of that name")
	// ErrBuiltinMacro refuses to delete a built-in macro: reset it instead.
	ErrBuiltinMacro = errors.New("a built-in macro cannot be deleted; reset it instead")
	// ErrBuiltinMacroName refuses to rename a built-in macro.
	ErrBuiltinMacroName = errors.New("a built-in macro's name cannot change")
	// ErrMacroNameTaken refuses a name another macro has.
	ErrMacroNameTaken = errors.New("another macro has that name")
	// ErrNotBuiltinMacro refuses to reset a macro that is not built in.
	ErrNotBuiltinMacro = errors.New("only a built-in macro can be reset")
)

// The built-in macros. Every picker sends a macro as a message, so none
// needs a final new line. They are the operator's words, not Legato's, so
// they carry no [legato <kind>] tag (see docs/ai/dev-notes.md).
var builtinMacros = []macros.Macro{
	{
		Name:      "Plan",
		AppliesTo: macros.AppliesToTask,
		Keys: "Plan this task before you change any code. Read the task and the code it touches, then write a plan: " +
			"what you will change and where, how you will test it, and what you are unsure of. " +
			"Wait for me to agree the plan before you start.",
	},
	{
		Name:      "Implement",
		AppliesTo: macros.AppliesToTask,
		Keys: "Implement this task. Work test first where the project has tests, keep the change to what the task asks, " +
			"run the project's checks and commit when it is done. Then tell me what you changed and anything left to do.",
	},
	{
		Name:      "Review",
		AppliesTo: macros.AppliesToPR,
		Keys: "Review this pull request. If it is not checked out, check it out first (gh pr checkout). Read the change set and the code around it, and report what you find by severity, " +
			"each with its file and line: bugs, missing tests, and anything that does not do what the pull request says. " +
			"Do not change the code. Stay open for my questions about it.",
	},
	{
		Name:      "Address feedback",
		AppliesTo: macros.AppliesToPR,
		Keys: "Address the review feedback on this pull request. Read every unresolved review comment and decide whether it is right; " +
			"fix the ones that are, run the checks, commit and push. Then tell me what you changed, " +
			"and which comments you did not act on and why.",
	},
}

// BuiltinMacros lists the macros Legato ships, as shipped.
func BuiltinMacros() []macros.Macro {
	out := make([]macros.Macro, len(builtinMacros))
	copy(out, builtinMacros)
	return out
}

func builtinMacro(name string) (macros.Macro, bool) {
	for _, m := range builtinMacros {
		if strings.EqualFold(m.Name, name) {
			return m, true
		}
	}
	return macros.Macro{}, false
}

// MacroEntry is one macro as the operator sees it, with where it comes from.
type MacroEntry struct {
	macros.Macro
	Source string `json:"source"`
	// Builtin is true for a built-in, edited or not.
	Builtin bool `json:"builtin,omitempty"`
	// Overridden is true for a built-in edited here or in config.yaml.
	Overridden bool `json:"overridden,omitempty"`
	// Pinned is true when config.yaml gives it.
	Pinned bool `json:"pinned,omitempty"`
}

// UnmarshalJSON reads an entry whole: without it, the embedded Macro's
// own UnmarshalJSON would read the macro's fields and drop the rest.
func (e *MacroEntry) UnmarshalJSON(data []byte) error {
	var rest struct {
		Source     string `json:"source"`
		Builtin    bool   `json:"builtin"`
		Overridden bool   `json:"overridden"`
		Pinned     bool   `json:"pinned"`
	}
	if err := json.Unmarshal(data, &rest); err != nil {
		return err
	}
	if err := json.Unmarshal(data, &e.Macro); err != nil {
		return err
	}
	e.Source, e.Builtin, e.Overridden, e.Pinned = rest.Source, rest.Builtin, rest.Overridden, rest.Pinned
	return nil
}

// MacroService gives the macros, read at each use: the built-ins (each
// replaced by an entry of its name, from config.yaml or the database),
// then config.yaml's other entries, then the database's.
type MacroService struct {
	settings *SettingsService
	agents   MacroAgents
}

// NewMacroService reads and keeps macros through settings.
func NewMacroService(settings *SettingsService) *MacroService {
	return &MacroService{settings: settings}
}

// List is every macro, in order.
func (s *MacroService) List(ctx context.Context) ([]MacroEntry, error) {
	cfg, err := s.settings.Effective(ctx)
	if err != nil {
		return nil, err
	}
	return macroEntries(cfg), nil
}

// macroEntries merges the built-ins with cfg's macros.
func macroEntries(cfg *config.Config) []MacroEntry {
	var out []MacroEntry
	used := make([]bool, len(cfg.Macros))
	for _, b := range builtinMacros {
		entry := MacroEntry{Macro: b, Source: MacroBuiltin, Builtin: true}
		for i, m := range cfg.Macros {
			if used[i] || !strings.EqualFold(m.Name, b.Name) {
				continue
			}
			used[i] = true
			m.Name = b.Name
			entry.Macro = m
			entry.Overridden = true
			if cfg.PinnedEntry("macros", m.Name) {
				entry.Source, entry.Pinned = MacroFromFile, true
			} else {
				entry.Source = MacroOverride
			}
			break
		}
		out = append(out, entry)
	}
	for i, m := range cfg.Macros {
		if used[i] {
			continue
		}
		entry := MacroEntry{Macro: m, Source: MacroFromDatabase}
		if cfg.PinnedEntry("macros", m.Name) {
			entry.Source, entry.Pinned = MacroFromFile, true
		}
		out = append(out, entry)
	}
	return out
}

// macroSettingEntries is the macros list as Settings shows it: every
// macro, built-ins included, in the order List gives.
func macroSettingEntries(cfg *config.Config, rows []store.SettingEntry) []SettingEntryValue {
	var out []SettingEntryValue
	for _, m := range macroEntries(cfg) {
		value := map[string]any{"keys": m.Keys}
		for k, v := range map[string]string{"applies_to": m.AppliesTo, "agent": m.Agent, "tier": m.Tier} {
			if v != "" {
				value[k] = v
			}
		}
		e := SettingEntryValue{Name: m.Name, Value: value, Source: m.Source, Pinned: m.Pinned, Builtin: m.Builtin, Overridden: m.Overridden}
		if m.Source == MacroBuiltin {
			e.Source = SettingFromDefault
		} else if m.Source == MacroOverride {
			e.Source = SettingFromDatabase
		}
		for _, r := range rows {
			if r.List == "macros" && strings.EqualFold(r.Name, m.Name) && !m.Pinned {
				e.UpdatedAt, e.UpdatedBy = r.UpdatedAt, r.UpdatedBy
			}
		}
		out = append(out, e)
	}
	return out
}

// Macros is every macro without where it comes from.
func (s *MacroService) Macros(ctx context.Context) ([]macros.Macro, error) {
	list, err := s.List(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]macros.Macro, len(list))
	for i, m := range list {
		out[i] = m.Macro
	}
	return out, nil
}

// Get finds a macro by name, ignoring case.
func (s *MacroService) Get(ctx context.Context, name string) (MacroEntry, error) {
	list, err := s.List(ctx)
	if err != nil {
		return MacroEntry{}, err
	}
	for _, m := range list {
		if strings.EqualFold(m.Name, strings.TrimSpace(name)) {
			return m, nil
		}
	}
	return MacroEntry{}, fmt.Errorf("%q: %w", name, ErrMacroNotFound)
}

// Save keeps a macro in the database: a new one, an edit of one added
// here, or an override of a built-in. One config.yaml gives is refused.
func (s *MacroService) Save(ctx context.Context, name string, m macros.Macro, by string) error {
	name = strings.TrimSpace(name)
	if b, ok := builtinMacro(name); ok {
		name = b.Name
	}
	m.Name = name
	if strings.TrimSpace(m.Keys) == "" {
		return fmt.Errorf("macro %q: it needs text", name)
	}
	if err := m.Validate(); err != nil {
		return err
	}
	raw, err := json.Marshal(map[string]string{"keys": m.Keys, "applies_to": m.AppliesTo, "agent": m.Agent, "tier": m.Tier})
	if err != nil {
		return err
	}
	return s.settings.SetEntry(ctx, "macros", s.storedName(ctx, name), dropEmpty(raw), by)
}

// dropEmpty leaves out the fields a macro leaves empty, as config.yaml would.
func dropEmpty(raw json.RawMessage) json.RawMessage {
	var m map[string]string
	if json.Unmarshal(raw, &m) != nil {
		return raw
	}
	for k, v := range m {
		if v == "" && k != "keys" {
			delete(m, k)
		}
	}
	out, _ := json.Marshal(m)
	return out
}

// Delete removes a macro added here. A built-in is refused.
func (s *MacroService) Delete(ctx context.Context, name string) error {
	if _, ok := builtinMacro(name); ok {
		return fmt.Errorf("%q: %w", name, ErrBuiltinMacro)
	}
	entry, err := s.Get(ctx, name)
	if err != nil {
		return err
	}
	if entry.Pinned {
		return fmt.Errorf("macro %q: %w", entry.Name, config.ErrEntryPinned)
	}
	return s.settings.DeleteEntry(ctx, "macros", entry.Name)
}

// Rename gives a macro added here a new name. A built-in, one config.yaml
// gives, and a name another macro has (ignoring case) are refused.
func (s *MacroService) Rename(ctx context.Context, from, to, by string) error {
	to = strings.TrimSpace(to)
	if to == "" {
		return errors.New("a macro needs a name")
	}
	if _, ok := builtinMacro(from); ok {
		return fmt.Errorf("%q: %w", from, ErrBuiltinMacroName)
	}
	entry, err := s.Get(ctx, from)
	if err != nil {
		return err
	}
	if entry.Pinned {
		return fmt.Errorf("macro %q: %w", entry.Name, config.ErrEntryPinned)
	}
	if _, ok := builtinMacro(to); ok {
		return fmt.Errorf("%q: %w", to, ErrMacroNameTaken)
	}
	list, err := s.List(ctx)
	if err != nil {
		return err
	}
	for _, m := range list {
		if strings.EqualFold(m.Name, to) && !strings.EqualFold(m.Name, entry.Name) {
			return fmt.Errorf("%q: %w", to, ErrMacroNameTaken)
		}
	}
	return s.settings.RenameEntry(ctx, "macros", s.storedName(ctx, entry.Name), to, by)
}

// AgentTier is one of an agent tool's tiers (adapters.<tool>.tiers): a named
// launch profile, usually choosing the model, that a macro may start with.
type AgentTier struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

// Tiers is each agent tool's tiers, by name; a tool with none is left out.
func (s *MacroService) Tiers(ctx context.Context) (map[string][]AgentTier, error) {
	cfg, err := s.settings.Effective(ctx)
	if err != nil {
		return nil, err
	}
	out := map[string][]AgentTier{}
	for kind, adapter := range cfg.Adapters {
		for name, tier := range adapter.Tiers {
			out[kind] = append(out[kind], AgentTier{Name: name, Description: tier.Description})
		}
		sort.Slice(out[kind], func(i, j int) bool { return out[kind][i].Name < out[kind][j].Name })
	}
	return out, nil
}

// Reset puts a built-in back as shipped, removing its edit; one with no
// edit is left as it is. One config.yaml edits is refused.
func (s *MacroService) Reset(ctx context.Context, name string) error {
	if _, ok := builtinMacro(name); !ok {
		return fmt.Errorf("%q: %w", name, ErrNotBuiltinMacro)
	}
	entry, err := s.Get(ctx, name)
	if err != nil {
		return err
	}
	if entry.Pinned {
		return fmt.Errorf("macro %q: %w", entry.Name, config.ErrEntryPinned)
	}
	if !entry.Overridden {
		return nil
	}
	if err := s.settings.DeleteEntry(ctx, "macros", s.storedName(ctx, entry.Name)); err != nil && !errors.Is(err, store.ErrNotFound) {
		return err
	}
	return nil
}

// storedName is the name the database keeps an entry by, which may differ
// in case from the built-in's.
func (s *MacroService) storedName(ctx context.Context, name string) string {
	rows, err := s.settings.db.ListSettingEntries(ctx)
	if err != nil {
		return name
	}
	for _, r := range rows {
		if r.List == "macros" && strings.EqualFold(r.Name, name) {
			return r.Name
		}
	}
	return name
}
