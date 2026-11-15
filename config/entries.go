package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// Lists of named entries that can live in the database beside config.yaml.
// Each list is the file's entries (pinned, matched by name) plus the
// database's; an entry whose name the file has is refused.

// EntryList declares a list of named entries kept in the database.
type EntryList struct {
	Key   string       `json:"key"`
	Title string       `json:"title"`
	Class SettingClass `json:"class"`
	Mode  SettingMode  `json:"mode"`
	// NameField is the yaml field that names an entry; "" for a map, whose
	// keys name its entries.
	NameField string `json:"name_field,omitempty"`
	// OwnRule marks a list of the Reeve's own rules: changed only with a
	// signed approval.
	OwnRule bool `json:"own_rule,omitempty"`
	// Ordered marks a list whose order decides (the Reeve's rules: the
	// last that matches wins). The database's entries are added after the
	// file's, sorted by name, so an entry never moves out of the file by
	// itself: that would change which one wins.
	Ordered bool `json:"ordered,omitempty"`
}

var entryListDefs = []EntryList{
	{Key: "workspaces", Title: "Workspaces", Class: ClassChange, Mode: ModeRestart, NameField: "name"},
	{Key: "macros", Title: "Macros", Class: ClassChange, Mode: ModeRestart, NameField: "name"},
	{Key: "adapters", Title: "Agent tools", Class: ClassChange, Mode: ModeRestart},
	{Key: "credentials", Title: "Credentials", Class: ClassRisky, Mode: ModeRestart, NameField: "name"},
	{Key: "credential_sources", Title: "Credential sources", Class: ClassRisky, Mode: ModeRestart, NameField: "vault"},
	{Key: "agents.tmux_options", Title: "tmux options for agents", Class: ClassChange, Mode: ModeNewAgents},
	{Key: "agents.sandbox.mounts", Title: "Sandbox mounts", Class: ClassRisky, Mode: ModeRestart, NameField: "path"},
	{Key: "swarm.prompts", Title: "Swarm role prompts", Class: ClassChange, Mode: ModeRestart},
	{Key: "reeve.rules", Title: "The Reeve's rules", Class: ClassRisky, Mode: ModeLive, NameField: "pattern", OwnRule: true, Ordered: true},
}

// tabledLists are lists that had tables of their own before the registry
// (repository presets, repository guidance, resources) or live in the
// database already (the board's columns): they keep them, and follow the
// same rule.
var tabledLists = []string{"repositories", "workflows.repositories", "resources", "agents.guidance", "board.columns"}

// EntryListDefs lists the lists of named entries the database keeps here.
func EntryListDefs() []EntryList { return slices.Clone(entryListDefs) }

// LookupEntryList finds one by key.
func LookupEntryList(key string) (EntryList, bool) {
	for _, l := range entryListDefs {
		if l.Key == key {
			return l, true
		}
	}
	return EntryList{}, false
}

// ErrEntryPinned is an entry whose name config.yaml already has.
var ErrEntryPinned = errors.New("config.yaml already has an entry of this name; change it there")

// PinnedEntry reports whether config.yaml has an entry of name in list.
func (c *Config) PinnedEntry(list, name string) bool {
	return slices.ContainsFunc(c.fileEntries[list], func(n string) bool { return strings.EqualFold(n, name) })
}

// FileEntries lists the names of list's entries that config.yaml gives.
func (c *Config) FileEntries(list string) []string { return slices.Clone(c.fileEntries[list]) }

// recordFileEntries keeps the names of every list's entries as the file
// gives them, before the database's are added.
func recordFileEntries(cfg *Config) {
	cfg.fileEntries = map[string][]string{}
	for _, l := range entryListDefs {
		names, _ := entryNames(cfg, l)
		cfg.fileEntries[l.Key] = names
	}
}

// entryNames lists a list's entry names in cfg.
func entryNames(cfg *Config, l EntryList) ([]string, error) {
	field, err := settingField(cfg, l.Key, false)
	if err != nil {
		return nil, err
	}
	var names []string
	switch field.Kind() {
	case reflect.Map:
		for _, k := range field.MapKeys() {
			names = append(names, k.String())
		}
		sort.Strings(names)
	case reflect.Slice:
		for i := 0; i < field.Len(); i++ {
			names = append(names, entryName(field.Index(i), l.NameField))
		}
	}
	return names, nil
}

func entryName(v reflect.Value, nameField string) string {
	t := v.Type()
	for i := 0; i < t.NumField(); i++ {
		if strings.Split(t.Field(i).Tag.Get("yaml"), ",")[0] == nameField {
			return fmt.Sprint(v.Field(i).Interface())
		}
	}
	return ""
}

// EntryValue reads one entry of list from cfg as JSON-able data, by its
// yaml shape; ok is false when there is none.
func EntryValue(cfg *Config, list, name string) (any, bool, error) {
	l, found := LookupEntryList(list)
	if !found {
		return nil, false, fmt.Errorf("%s: %w", list, ErrUnknownSetting)
	}
	field, err := settingField(cfg, l.Key, false)
	if err != nil {
		return nil, false, err
	}
	var entry reflect.Value
	switch field.Kind() {
	case reflect.Map:
		entry = field.MapIndex(reflect.ValueOf(name))
	case reflect.Slice:
		for i := 0; i < field.Len(); i++ {
			if strings.EqualFold(entryName(field.Index(i), l.NameField), name) {
				entry = field.Index(i)
				break
			}
		}
	}
	if !entry.IsValid() {
		return nil, false, nil
	}
	data, err := yaml.Marshal(entry.Interface())
	if err != nil {
		return nil, false, err
	}
	var out any
	if err := yaml.Unmarshal(data, &out); err != nil {
		return nil, false, err
	}
	return out, true, nil
}

// addEntry adds one entry from JSON (read by its yaml tags) to list in cfg.
// The entry's name is name: its name field is set to it, or it is the map
// key.
func addEntry(cfg *Config, l EntryList, name string, raw json.RawMessage) error {
	field, err := settingField(cfg, l.Key, true)
	if err != nil {
		return err
	}
	switch field.Kind() {
	case reflect.Map:
		elem := reflect.New(field.Type().Elem())
		if err := yaml.Unmarshal(raw, elem.Interface()); err != nil {
			return fmt.Errorf("%s %q: %w", l.Key, name, err)
		}
		if field.IsNil() {
			field.Set(reflect.MakeMap(field.Type()))
		}
		field.SetMapIndex(reflect.ValueOf(name), elem.Elem())
	case reflect.Slice:
		elem := reflect.New(field.Type().Elem())
		if err := yaml.Unmarshal(raw, elem.Interface()); err != nil {
			return fmt.Errorf("%s %q: %w", l.Key, name, err)
		}
		if err := setEntryName(elem.Elem(), l.NameField, name); err != nil {
			return fmt.Errorf("%s: %w", l.Key, err)
		}
		field.Set(reflect.Append(field, elem.Elem()))
	default:
		return fmt.Errorf("%s is not a list", l.Key)
	}
	return nil
}

func setEntryName(v reflect.Value, nameField, name string) error {
	t := v.Type()
	for i := 0; i < t.NumField(); i++ {
		if strings.Split(t.Field(i).Tag.Get("yaml"), ",")[0] == nameField && v.Field(i).Kind() == reflect.String {
			v.Field(i).SetString(name)
			return nil
		}
	}
	return fmt.Errorf("entries have no %s", nameField)
}

// OverlayEntries adds the database's entries (by list, then name) to cfg,
// leaving out any whose name config.yaml has, then checks the whole
// configuration with the rules Load uses.
func OverlayEntries(cfg *Config, entries map[string]map[string]json.RawMessage) error {
	return OverlayAll(cfg, nil, entries)
}

func applyEntries(cfg *Config, entries map[string]map[string]json.RawMessage) error {
	lists := make([]string, 0, len(entries))
	for list := range entries {
		lists = append(lists, list)
	}
	sort.Strings(lists)
	for _, list := range lists {
		l, ok := LookupEntryList(list)
		if !ok {
			continue
		}
		names := make([]string, 0, len(entries[list]))
		for name := range entries[list] {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			if cfg.PinnedEntry(list, name) {
				continue
			}
			if err := addEntry(cfg, l, name, entries[list][name]); err != nil {
				return err
			}
		}
	}
	return nil
}
