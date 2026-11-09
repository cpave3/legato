package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"

	"github.com/cpave3/legato/config"
	"github.com/cpave3/legato/internal/engine/store"
	"gopkg.in/yaml.v3"
)

// EntryRef names one entry of a list of named entries.
type EntryRef struct {
	List string `json:"list"`
	Name string `json:"name"`
}

// SettingsMove is what a move takes from config.yaml into the database:
// pinned settings by key, and pinned list entries.
type SettingsMove struct {
	Settings []string   `json:"settings"`
	Entries  []EntryRef `json:"entries"`
}

// Empty reports whether the move takes nothing.
func (m SettingsMove) Empty() bool { return len(m.Settings) == 0 && len(m.Entries) == 0 }

// SettingsMover moves settings config.yaml pins into the database: their
// values are copied, the file is rewritten without them (comments and order
// kept, a backup beside it), and the restart that follows is recorded so a
// failed start puts both back.
type SettingsMover struct {
	Settings *SettingsService
	Rollback *RestartRollback
	// ConfigPath is the instance's config.yaml (config.ResolveConfigPath
	// when nil).
	ConfigPath func() string
}

// ErrNothingToMove is a move of nothing movable.
var ErrNothingToMove = errors.New("nothing to move: name settings and entries that config.yaml pins")

func (m *SettingsMover) path() string {
	if m.ConfigPath != nil {
		return m.ConfigPath()
	}
	return config.ResolveConfigPath()
}

// Movable is every setting and list entry config.yaml pins that can move:
// not one holding a secret, and not one whose value in the file reads an
// environment variable (moving it would freeze the variable's value).
func (m *SettingsMover) Movable(ctx context.Context) (SettingsMove, error) {
	cfg, err := m.Settings.load()
	if err != nil {
		return SettingsMove{}, err
	}
	raw, _ := os.ReadFile(m.path())
	var doc yaml.Node
	_ = yaml.Unmarshal(raw, &doc)
	out := SettingsMove{Settings: []string{}, Entries: []EntryRef{}}
	for _, s := range config.Settings() {
		if s.Secret || !cfg.Pinned(s.Key) || readsEnv(lookupNode(&doc, s.Key)) {
			continue
		}
		out.Settings = append(out.Settings, s.Key)
	}
	for _, l := range config.EntryListDefs() {
		for _, name := range cfg.FileEntries(l.Key) {
			if readsEnv(lookupNode(&doc, l.Key)) {
				continue
			}
			out.Entries = append(out.Entries, EntryRef{List: l.Key, Name: name})
		}
	}
	return out, nil
}

// Move copies what req names from config.yaml into the database, rewrites
// the file without it, and records the restart (to roll back). The caller
// restarts Legato.
func (m *SettingsMover) Move(ctx context.Context, req SettingsMove, by string) (store.RestartRecord, error) {
	if req.Empty() {
		return store.RestartRecord{}, ErrNothingToMove
	}
	path := m.path()
	if _, pending := ReadRestartPointer(path); pending {
		return store.RestartRecord{}, ErrRestartPending
	}
	movable, err := m.Movable(ctx)
	if err != nil {
		return store.RestartRecord{}, err
	}
	for _, key := range req.Settings {
		if !slices.Contains(movable.Settings, key) {
			return store.RestartRecord{}, fmt.Errorf("%s cannot move: config.yaml does not pin it, or it holds a secret or reads an environment variable", key)
		}
	}
	for _, e := range req.Entries {
		if !containsEntry(movable.Entries, e) {
			return store.RestartRecord{}, fmt.Errorf("%s %q cannot move: config.yaml does not have it, or it reads an environment variable", e.List, e.Name)
		}
	}
	cfg, err := m.Settings.load()
	if err != nil {
		return store.RestartRecord{}, err
	}
	db := m.Settings.db
	var olds []RestartSetting
	for _, key := range req.Settings {
		value, err := config.SettingValue(cfg, key)
		if err != nil {
			return store.RestartRecord{}, err
		}
		raw, _ := json.Marshal(value)
		old, had, err := db.GetSetting(ctx, key)
		if err != nil {
			return store.RestartRecord{}, err
		}
		rs := RestartSetting{Key: key}
		if had {
			rs.Old = old
		}
		olds = append(olds, rs)
		if err := db.SetSetting(ctx, key, raw, by); err != nil {
			return store.RestartRecord{}, err
		}
	}
	for _, e := range req.Entries {
		value, _, err := config.EntryValue(cfg, e.List, e.Name)
		if err != nil {
			return store.RestartRecord{}, err
		}
		raw, _ := json.Marshal(value)
		olds = append(olds, RestartSetting{List: e.List, Key: e.Name})
		if err := db.SetSettingEntry(ctx, e.List, e.Name, raw, by); err != nil {
			return store.RestartRecord{}, err
		}
	}
	backup, err := config.EditFile(path, func(doc *yaml.Node) error {
		for _, key := range req.Settings {
			config.RemoveKey(doc, key)
		}
		for _, e := range req.Entries {
			l, _ := config.LookupEntryList(e.List)
			config.RemoveListEntry(doc, e.List, l.NameField, e.Name)
		}
		return nil
	})
	if err != nil {
		_ = m.Settings.RestoreSettings(ctx, olds)
		return store.RestartRecord{}, err
	}
	rec, err := m.Rollback.Begin(ctx, RestartPlan{
		Reason: fmt.Sprintf("move %d settings into the database", len(req.Settings)+len(req.Entries)), RequestedBy: by,
		ConfigPath: path, ConfigBackup: backup, Settings: olds,
	})
	if err != nil {
		return rec, err
	}
	m.Settings.changed()
	return rec, nil
}

func containsEntry(list []EntryRef, e EntryRef) bool {
	for _, v := range list {
		if v.List == e.List && strings.EqualFold(v.Name, e.Name) {
			return true
		}
	}
	return false
}

// lookupNode is the node at a dotted key of a parsed document, or nil.
func lookupNode(doc *yaml.Node, key string) *yaml.Node {
	if doc == nil || len(doc.Content) == 0 {
		return nil
	}
	n := doc.Content[0]
	for _, part := range strings.Split(key, ".") {
		if n.Kind != yaml.MappingNode {
			return nil
		}
		var next *yaml.Node
		for i := 0; i+1 < len(n.Content); i += 2 {
			if n.Content[i].Value == part {
				next = n.Content[i+1]
			}
		}
		if next == nil {
			return nil
		}
		n = next
	}
	return n
}

// readsEnv reports whether a node's text names an environment variable.
func readsEnv(n *yaml.Node) bool {
	if n == nil {
		return false
	}
	if n.Kind == yaml.ScalarNode {
		return strings.Contains(n.Value, "$")
	}
	for _, c := range n.Content {
		if readsEnv(c) {
			return true
		}
	}
	return false
}
