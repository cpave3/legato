package service

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"strings"

	"github.com/cpave3/legato/config"
	"github.com/cpave3/legato/internal/engine/store"
	"gopkg.in/yaml.v3"
)

// A config edit that takes a pinned setting out of config.yaml would let the
// database's value of it take over, which can be an older one the file had
// shadowed (LEG-164: a default agent tool changed, mounts and launch flags
// undone). So such an edit is a move: the file's value, the one Legato is
// running with, is written into the database before the file changes, and
// the edit says what it kept.

// KeptSetting is a value config.yaml pinned that an edit takes out of the
// file and that is kept in the database.
type KeptSetting struct {
	// List is set for an entry of a list of named entries; Key is then the
	// entry's name.
	List  string          `json:"list,omitempty"`
	Key   string          `json:"key"`
	Value json.RawMessage `json:"value"`
	// Replaced is the database's value it replaces, when it had another.
	Replaced json.RawMessage `json:"replaced,omitempty"`
}

// String says it in a line: `key: value (the database had old)`.
func (k KeptSetting) String() string {
	name := k.Key
	if k.List != "" {
		name = k.List + " " + k.Key
	}
	s := name + ": " + string(k.Value)
	if len(k.Replaced) > 0 {
		s += " (the database had " + string(k.Replaced) + ")"
	}
	return s
}

// ConfigEditKeep is what an edit takes out of config.yaml: Keep is kept in
// the database; Lost are settings it cannot keep there (a secret, or a value
// that reads an environment variable), which go back to the database's value
// or their default.
type ConfigEditKeep struct {
	Keep []KeptSetting `json:"keep,omitempty"`
	Lost []string      `json:"lost,omitempty"`
}

// Lines says it for a person, one line each.
func (k ConfigEditKeep) Lines() []string {
	var out []string
	for _, kept := range k.Keep {
		out = append(out, "kept in the database: "+kept.String())
	}
	for _, lost := range k.Lost {
		out = append(out, "not kept (a secret, or it reads an environment variable): "+lost)
	}
	return out
}

// ConfigEditKeeps is what the edits to the config.yaml at path would take
// out of the file and keep in db.
func ConfigEditKeeps(ctx context.Context, db *store.Store, path string, edits []ConfigEdit) (ConfigEditKeep, error) {
	old, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return ConfigEditKeep{}, err
	}
	edited, err := config.EditBytes(old, applyConfigEdits(edits))
	if err != nil {
		return ConfigEditKeep{}, err
	}
	return pinsLeaving(ctx, db, old, edited, edits)
}

// WriteConfigEditKeeping writes edits to the config.yaml at path, as
// WriteConfigEdit does, after keeping in db the values it takes out of the
// file. A file that cannot be written puts the database back.
func WriteConfigEditKeeping(ctx context.Context, db *store.Store, path string, edits []ConfigEdit, by string) (string, ConfigEditKeep, error) {
	keep, err := ConfigEditKeeps(ctx, db, path, edits)
	if err != nil {
		return "", keep, err
	}
	var olds []RestartSetting
	for _, k := range keep.Keep {
		if k.List != "" {
			err = db.SetSettingEntry(ctx, k.List, k.Key, k.Value, by)
		} else {
			err = db.SetSetting(ctx, k.Key, k.Value, by)
		}
		if err != nil {
			_ = NewSettingsService(db, nil).RestoreSettings(ctx, olds)
			return "", keep, err
		}
		olds = append(olds, RestartSetting{List: k.List, Key: k.Key, Old: k.Replaced})
	}
	backup, err := config.EditFile(path, applyConfigEdits(edits))
	if err != nil {
		_ = NewSettingsService(db, nil).RestoreSettings(ctx, olds)
		return "", keep, err
	}
	return backup, keep, nil
}

// pinsLeaving compares config.yaml before and after edits: each registered
// setting it pinned and no longer does, and each entry of a list it had that
// an edit removes whole, with the file's value and the database's.
func pinsLeaving(ctx context.Context, db *store.Store, old, edited []byte, edits []ConfigEdit) (ConfigEditKeep, error) {
	out := ConfigEditKeep{}
	before, err := config.LoadBytes(old)
	if err != nil {
		// A file that does not load pins nothing Legato runs with.
		return out, nil
	}
	after, err := config.LoadBytes(edited)
	if err != nil {
		return out, nil
	}
	var doc yaml.Node
	_ = yaml.Unmarshal(old, &doc)
	for _, s := range config.Settings() {
		if !before.Pinned(s.Key) || after.Pinned(s.Key) || !removesWhole(edits, s.Key) {
			continue
		}
		if s.Secret || readsEnv(lookupNode(&doc, s.Key)) {
			out.Lost = append(out.Lost, s.Key)
			continue
		}
		value, err := config.SettingValue(before, s.Key)
		if err != nil {
			return out, err
		}
		raw, _ := json.Marshal(value)
		k := KeptSetting{Key: s.Key, Value: raw}
		if stored, had, err := db.GetSetting(ctx, s.Key); err != nil {
			return out, err
		} else if had && !sameJSON(stored, raw) {
			k.Replaced = stored
		}
		out.Keep = append(out.Keep, k)
	}
	stored, err := db.SettingEntries(ctx)
	if err != nil {
		return out, err
	}
	for _, l := range config.EntryListDefs() {
		for _, name := range before.FileEntries(l.Key) {
			if after.PinnedEntry(l.Key, name) || !removesEntry(edits, l, name) {
				continue
			}
			if readsEnv(lookupNode(&doc, l.Key)) {
				out.Lost = append(out.Lost, l.Key+" "+name)
				continue
			}
			value, ok, err := config.EntryValue(before, l.Key, name)
			if err != nil {
				return out, err
			}
			if !ok {
				continue
			}
			raw, _ := json.Marshal(value)
			at := storedEntryName(stored[l.Key], name)
			k := KeptSetting{List: l.Key, Key: at, Value: raw}
			if old, had := stored[l.Key][at]; had && !sameJSON(old, raw) {
				k.Replaced = old
			}
			out.Keep = append(out.Keep, k)
		}
	}
	return out, nil
}

// removesWhole reports whether an edit removes key itself or a section
// holding it. Taking a part out of a value (an entry of a list setting) is
// a change to it, not a move.
func removesWhole(edits []ConfigEdit, key string) bool {
	for _, e := range edits {
		k := strings.TrimSpace(e.Key)
		if !e.Remove || strings.Contains(k, "[") {
			continue
		}
		if k == key || strings.HasPrefix(key, k+".") {
			return true
		}
	}
	return false
}

// removesEntry reports whether an edit removes the list, a section holding
// it, or an entry of it whole (list[...] or, for a map, list.name). Taking a
// field out of an entry is a change to it, even when it leaves it empty.
func removesEntry(edits []ConfigEdit, l config.EntryList, name string) bool {
	if removesWhole(edits, l.Key) {
		return true
	}
	for _, e := range edits {
		k := strings.TrimSpace(e.Key)
		if !e.Remove {
			continue
		}
		if l.NameField == "" && strings.EqualFold(k, l.Key+"."+name) {
			return true
		}
		if strings.HasPrefix(k, l.Key+"[") && strings.HasSuffix(k, "]") && config.PlainKey(k) == l.Key {
			return true
		}
	}
	return false
}

// sameJSON reports whether two JSON values are equal as values.
func sameJSON(a, b []byte) bool {
	var x, y any
	if json.Unmarshal(a, &x) != nil || json.Unmarshal(b, &y) != nil {
		return string(a) == string(b)
	}
	return reflect.DeepEqual(x, y)
}

// keptReport is a config edit's result detail with what it kept.
func keptReport(backup string, keep ConfigEditKeep) string {
	detail := "config.yaml written; the old file is " + backup
	if lines := keep.Lines(); len(lines) > 0 {
		detail += fmt.Sprintf("; %s", strings.Join(lines, "; "))
	}
	return detail
}
