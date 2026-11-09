package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"regexp"
	"slices"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// The settings registry: every setting that can live in the database as
// well as in config.yaml, by the same dotted key. A value in config.yaml
// pins its setting; otherwise the database's value applies, else the
// default. See docs/ai/settings.md.

// SettingClass is how much a change to a setting needs (docs/ai/reeve.md
// § Classes): read is free, change needs an approval, risky can need a
// signed one.
type SettingClass string

const (
	ClassRead   SettingClass = "read"
	ClassChange SettingClass = "change"
	ClassRisky  SettingClass = "risky"
)

// SettingMode is when a change takes effect: live (read on each use), new
// agents (agents started after it), or restart (wired into services at
// start).
type SettingMode string

const (
	ModeLive      SettingMode = "live"
	ModeNewAgents SettingMode = "new_agents"
	ModeRestart   SettingMode = "restart"
)

// SettingKind is a setting's type.
type SettingKind string

const (
	KindBool     SettingKind = "bool"
	KindString   SettingKind = "string"
	KindInt      SettingKind = "int"
	KindEnum     SettingKind = "enum"
	KindDuration SettingKind = "duration"
	KindStrings  SettingKind = "strings"
)

// Setting declares one setting that can live in the database.
type Setting struct {
	Key         string       `json:"key"`
	Title       string       `json:"title"`
	Description string       `json:"description,omitempty"`
	Kind        SettingKind  `json:"kind"`
	Enum        []string     `json:"enum,omitempty"`
	Class       SettingClass `json:"class"`
	Mode        SettingMode  `json:"mode"`
	// Secret: the value is the name of a secret in Legato's secret store
	// or 1Password, never the secret itself.
	Secret bool `json:"secret,omitempty"`
	// Check is the setting's own rule, past its kind and enum.
	Check func(v any) error `json:"-"`
}

var burnliteProjectPattern = regexp.MustCompile(`^[A-Z]{2,6}[0-9]{0,4}$`)

var registry = []Setting{
	// Agents.
	{Key: "agents.escape_key", Title: "Detach key", Description: "The key that leaves an agent's terminal in the TUI, as ctrl+].", Kind: KindString, Class: ClassChange, Mode: ModeRestart},
	{Key: "agents.sparkline_window", Title: "Activity window", Description: "How far back the activity bar of an agent reaches.", Kind: KindDuration, Class: ClassChange, Mode: ModeRestart},
	{Key: "agents.peer_awareness", Title: "Peer awareness", Description: "Tell agents in the same Git worktree about each other.", Kind: KindBool, Class: ClassChange, Mode: ModeRestart},
	{Key: "agents.reviews", Title: "Reviews", Description: "Ask agents for a review tour and a handoff packet at the end of their work.", Kind: KindBool, Class: ClassChange, Mode: ModeRestart},
	{Key: "agents.mcp.enabled", Title: "Legato tools", Description: "Give agents Legato's tools (cards, artifacts, notes) through `legato mcp serve`.", Kind: KindBool, Class: ClassChange, Mode: ModeRestart},
	{Key: "agents.sandbox.enabled", Title: "Sandbox", Description: "Run every agent in Legato's sandbox unless its spawn opts out.", Kind: KindBool, Class: ClassRisky, Mode: ModeRestart},
	{Key: "agents.sandbox.backend", Title: "Sandbox backend", Description: "Only bwrap for now.", Kind: KindString, Class: ClassRisky, Mode: ModeRestart},
	{Key: "agents.sandbox.default_mounts", Title: "Toolchains in the sandbox", Description: "Show the person's toolchains (read-only) and package caches inside the sandbox.", Kind: KindBool, Class: ClassRisky, Mode: ModeRestart},
	{Key: "agents.sandbox.scratch_dir", Title: "Sandbox scratch folder", Kind: KindString, Class: ClassRisky, Mode: ModeRestart},

	// Notifications.
	{Key: "notifications.ntfy.url", Title: "ntfy server", Kind: KindString, Class: ClassChange, Mode: ModeRestart},
	{Key: "notifications.ntfy.topic", Title: "ntfy topic", Kind: KindString, Class: ClassChange, Mode: ModeRestart},
	{Key: "notifications.os.enabled", Title: "Desktop notifications", Kind: KindBool, Class: ClassChange, Mode: ModeRestart},
	{Key: "notifications.approvals", Title: "Notify of approvals", Kind: KindBool, Class: ClassChange, Mode: ModeRestart},
	{Key: "notifications.handoff", Title: "Notify of handoffs", Kind: KindBool, Class: ClassChange, Mode: ModeRestart},

	// Look and feel.
	{Key: "theme", Title: "Theme", Kind: KindString, Class: ClassChange, Mode: ModeRestart},
	{Key: "icons", Title: "Icons", Description: "Unicode glyphs, or Nerd Font ones.", Kind: KindEnum, Enum: []string{"unicode", "nerdfonts"}, Class: ClassChange, Mode: ModeRestart},
	{Key: "editor", Title: "Editor", Description: "The editor the TUI opens; $VISUAL, then $EDITOR, when empty.", Kind: KindString, Class: ClassChange, Mode: ModeRestart},
	{Key: "web.tint", Title: "Tint", Description: "A colour that tells this Legato from others in the web UI.", Kind: KindEnum, Enum: []string{"", "red", "orange", "yellow", "green", "teal", "blue", "purple", "pink"}, Class: ClassChange, Mode: ModeLive},

	// Updates.
	{Key: "updates.mode", Title: "Updates", Description: "auto installs new releases; ask offers them; off never looks.", Kind: KindEnum, Enum: []string{"auto", "ask", "off"}, Class: ClassRisky, Mode: ModeLive},

	// Burnlite.
	{Key: "burnlite.projects", Title: "Burnlite projects", Description: "The projects searched for import; none is every project.", Kind: KindStrings, Class: ClassChange, Mode: ModeLive, Check: func(v any) error {
		for _, p := range v.([]string) {
			if !burnliteProjectPattern.MatchString(p) {
				return fmt.Errorf("%q is not a Burnlite project key: two to six capital letters, then up to four digits, like EST", p)
			}
		}
		return nil
	}},
}

// fileOnly are the settings that live only in config.yaml: what is needed
// before the database opens, what unseals secrets, how the instance is
// reached, and the signing trust root.
var fileOnly = []string{
	"db", "data_dir",
	"secrets.master_key", "secrets.onepassword",
	"web.enabled", "web.port", "web.tls", "web.public_url",
	"approval_devices.android_app_digests",
}

// Settings lists the registry, in its order.
func Settings() []Setting { return slices.Clone(registry) }

// FileOnlySettings lists the keys (or key prefixes) of the file-only
// settings.
func FileOnlySettings() []string { return slices.Clone(fileOnly) }

// IsFileOnly reports whether key is, or is under, a file-only setting.
func IsFileOnly(key string) bool {
	for _, f := range fileOnly {
		if key == f || strings.HasPrefix(key, f+".") {
			return true
		}
	}
	return false
}

// LookupSetting finds a registered setting by key.
func LookupSetting(key string) (Setting, bool) {
	for _, s := range registry {
		if s.Key == key {
			return s, true
		}
	}
	return Setting{}, false
}

// ErrUnknownSetting is a key the registry does not declare.
var ErrUnknownSetting = errors.New("not a setting that can be changed here")

// DecodeSetting reads a JSON value for s and checks it against its kind,
// its enum and its own rule.
func DecodeSetting(s Setting, raw json.RawMessage) (any, error) {
	fail := func(want string) error { return fmt.Errorf("%s: want %s, got %s", s.Key, want, strings.TrimSpace(string(raw))) }
	var v any
	switch s.Kind {
	case KindBool:
		var b bool
		if json.Unmarshal(raw, &b) != nil {
			return nil, fail("true or false")
		}
		v = b
	case KindInt:
		var n int
		if json.Unmarshal(raw, &n) != nil {
			return nil, fail("a whole number")
		}
		v = n
	case KindString, KindEnum, KindDuration:
		var str string
		if json.Unmarshal(raw, &str) != nil {
			return nil, fail("a string")
		}
		if s.Kind == KindEnum && !slices.Contains(s.Enum, str) {
			return nil, fmt.Errorf("%s: %q is not one of %s", s.Key, str, strings.Join(nonEmpty(s.Enum), ", "))
		}
		if s.Kind == KindDuration && str != "" {
			if _, err := time.ParseDuration(str); err != nil {
				return nil, fmt.Errorf("%s: %q is not a duration such as 10m", s.Key, str)
			}
		}
		v = str
	case KindStrings:
		var list []string
		if json.Unmarshal(raw, &list) != nil {
			return nil, fail("a list of strings")
		}
		if list == nil {
			list = []string{}
		}
		v = list
	default:
		return nil, fmt.Errorf("%s: unknown kind %q", s.Key, s.Kind)
	}
	if s.Check != nil {
		if err := s.Check(v); err != nil {
			return nil, err
		}
	}
	return v, nil
}

func nonEmpty(values []string) []string {
	out := []string{}
	for _, v := range values {
		if v != "" {
			out = append(out, v)
		}
	}
	return out
}

// Pinned reports whether config.yaml gives key (or a part of it) a value:
// such a setting is changed only by editing the file.
func (c *Config) Pinned(key string) bool {
	if c.pins[key] {
		return true
	}
	// A value given to a key above it (a list, a scalar) pins it too.
	for parent := key; strings.Contains(parent, "."); {
		parent = parent[:strings.LastIndex(parent, ".")]
		if c.pins[parent] {
			return true
		}
	}
	return false
}

// PinnedKeys lists the registered settings config.yaml pins.
func (c *Config) PinnedKeys() []string {
	var keys []string
	for _, s := range registry {
		if c.Pinned(s.Key) {
			keys = append(keys, s.Key)
		}
	}
	return keys
}

// pinnedKeys lists every dotted path the YAML document gives a value,
// mappings walked, other values (lists, scalars) as leaves.
func pinnedKeys(data []byte) map[string]bool {
	pins := map[string]bool{}
	var doc yaml.Node
	if yaml.Unmarshal(data, &doc) != nil || len(doc.Content) == 0 {
		return pins
	}
	var walk func(prefix string, n *yaml.Node)
	walk = func(prefix string, n *yaml.Node) {
		if n.Kind != yaml.MappingNode {
			if prefix != "" && !(n.Kind == yaml.ScalarNode && n.Tag == "!!null") {
				pins[prefix] = true
			}
			return
		}
		for i := 0; i+1 < len(n.Content); i += 2 {
			key := n.Content[i].Value
			if prefix != "" {
				key = prefix + "." + key
			}
			walk(key, n.Content[i+1])
		}
	}
	walk("", doc.Content[0])
	return pins
}

// Overlay applies database values to a configuration loaded from the file:
// each to its setting unless the file pins it, then the whole configuration
// is checked with the rules Load uses. On an error cfg may be partly
// changed; load it again.
func Overlay(cfg *Config, values map[string]json.RawMessage) error {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		s, ok := LookupSetting(key)
		if !ok || cfg.Pinned(key) {
			continue
		}
		v, err := DecodeSetting(s, values[key])
		if err != nil {
			return err
		}
		if err := setSettingValue(cfg, key, v); err != nil {
			return err
		}
	}
	return validate(cfg)
}

// Defaults is the configuration of an empty config.yaml.
func Defaults() *Config {
	cfg := defaults()
	applyDefaults(cfg)
	_ = validate(cfg)
	return cfg
}

// SettingValue reads key's value from cfg, pointers followed (nil is the
// zero value of the pointed type).
func SettingValue(cfg *Config, key string) (any, error) {
	field, err := settingField(cfg, key)
	if err != nil {
		return nil, err
	}
	if field.Kind() == reflect.Pointer {
		if field.IsNil() {
			return pointerDefault(key, field), nil
		}
		return field.Elem().Interface(), nil
	}
	return field.Interface(), nil
}

// pointerDefault is the value an unset pointer setting means.
func pointerDefault(key string, field reflect.Value) any {
	switch key {
	case "notifications.approvals", "notifications.handoff", "agents.sandbox.default_mounts":
		return true
	}
	return reflect.Zero(field.Type().Elem()).Interface()
}

func setSettingValue(cfg *Config, key string, v any) error {
	field, err := settingField(cfg, key)
	if err != nil {
		return err
	}
	value := reflect.ValueOf(v)
	if field.Kind() == reflect.Pointer {
		ptr := reflect.New(field.Type().Elem())
		ptr.Elem().Set(value.Convert(field.Type().Elem()))
		field.Set(ptr)
		return nil
	}
	if !value.Type().ConvertibleTo(field.Type()) {
		return fmt.Errorf("%s: cannot hold a %s", key, value.Type())
	}
	field.Set(value.Convert(field.Type()))
	return nil
}

// settingField finds the field of cfg a dotted key names, by yaml tags.
func settingField(cfg *Config, key string) (reflect.Value, error) {
	v := reflect.ValueOf(cfg).Elem()
	for _, part := range strings.Split(key, ".") {
		if v.Kind() != reflect.Struct {
			return reflect.Value{}, fmt.Errorf("%s: %w", key, ErrUnknownSetting)
		}
		found := false
		t := v.Type()
		for i := 0; i < t.NumField(); i++ {
			tag := strings.Split(t.Field(i).Tag.Get("yaml"), ",")[0]
			if tag == part {
				v = v.Field(i)
				found = true
				break
			}
		}
		if !found {
			return reflect.Value{}, fmt.Errorf("%s: %w", key, ErrUnknownSetting)
		}
	}
	return v, nil
}
