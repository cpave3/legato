package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strings"
	"sync"

	"github.com/cpave3/legato/config"
	"github.com/cpave3/legato/internal/engine/store"
)

// Where a setting's value comes from.
const (
	SettingFromDefault  = "default"
	SettingFromFile     = "config.yaml"
	SettingFromDatabase = "database"
)

// ErrSettingPinned is a change to a setting config.yaml gives a value: only
// an edit to the file changes it.
var ErrSettingPinned = errors.New("this setting is pinned by config.yaml; change it there")

// SettingValue is a setting with its effective value and where it comes
// from.
type SettingValue struct {
	config.Setting
	Value  any    `json:"value"`
	Source string `json:"source"`
	Pinned bool   `json:"pinned"`
	// PendingRestart: a restart-mode setting whose value changed since this
	// process started; it applies at the next restart.
	PendingRestart bool   `json:"pending_restart,omitempty"`
	UpdatedAt      string `json:"updated_at,omitempty"`
	UpdatedBy      string `json:"updated_by,omitempty"`
}

// SettingsService keeps the settings of the registry that live in the
// database, and gives the effective configuration: config.yaml, then the
// database's values for the settings the file does not pin.
type SettingsService struct {
	db   *store.Store
	load func() (*config.Config, error)

	mu       sync.Mutex
	started  map[string]string // canonical JSON of each value at MarkStarted
	onChange []func()
}

// NewSettingsService keeps settings in db; load reads config.yaml
// (config.Load when nil).
func NewSettingsService(db *store.Store, load func() (*config.Config, error)) *SettingsService {
	if load == nil {
		load = config.Load
	}
	return &SettingsService{db: db, load: load}
}

// ApplySettings overlays the database's settings on cfg, a configuration
// loaded from config.yaml, in place. When the overlay would leave the
// configuration invalid (the file changed since the values were saved),
// cfg is left as the file has it and the error is returned: a database
// value never stops Legato from starting.
//
// A secret setting keeps a secret's name in the database; resolve reads the
// secret (nil leaves such settings empty).
func ApplySettings(ctx context.Context, db *store.Store, cfg *config.Config, resolve SecretResolver) error {
	values, err := db.SettingValues(ctx)
	if err != nil {
		return err
	}
	entries, err := db.SettingEntries(ctx)
	if err != nil {
		return err
	}
	if len(values) == 0 && len(entries) == 0 {
		return nil
	}
	probe, err := config.Load()
	if err == nil {
		err = overlayAll(probe, values, entries)
	}
	if err != nil {
		return fmt.Errorf("the settings saved in the database are not applied: %w", err)
	}
	if err := overlayAll(cfg, values, entries); err != nil {
		return err
	}
	var failed []string
	for _, setting := range config.Settings() {
		if !setting.Secret || cfg.Pinned(setting.Key) {
			continue
		}
		raw, ok := values[setting.Key]
		if !ok {
			continue
		}
		var ref string
		_ = json.Unmarshal(raw, &ref)
		plain := ""
		if ref != "" && resolve != nil {
			if plain, err = resolve(ctx, ref); err != nil {
				failed = append(failed, fmt.Sprintf("%s (%s): %v", setting.Key, ref, err))
				plain = ""
			}
		}
		_ = config.SetSetting(cfg, setting.Key, plain)
	}
	if len(failed) > 0 {
		return fmt.Errorf("secret settings not read: %s", strings.Join(failed, "; "))
	}
	return nil
}

// overlayAll applies the database's single values, then its list entries.
func overlayAll(cfg *config.Config, values map[string]json.RawMessage, entries map[string]map[string]json.RawMessage) error {
	if err := config.Overlay(cfg, values); err != nil {
		return err
	}
	if len(entries) == 0 {
		return nil
	}
	return config.OverlayEntries(cfg, entries)
}

// SecretResolver reads the secret a reference names (legato://name,
// op://vault/item/field).
type SecretResolver func(ctx context.Context, ref string) (string, error)

// Effective is the effective configuration.
func (s *SettingsService) Effective(ctx context.Context) (*config.Config, error) {
	cfg, err := s.load()
	if err != nil {
		return nil, err
	}
	values, err := s.db.SettingValues(ctx)
	if err != nil {
		return nil, err
	}
	entries, err := s.db.SettingEntries(ctx)
	if err != nil {
		return nil, err
	}
	if err := overlayAll(cfg, values, entries); err != nil {
		log.Printf("settings: %v", err)
		return s.load()
	}
	return cfg, nil
}

// List is every registered setting with its value and source.
func (s *SettingsService) List(ctx context.Context) ([]SettingValue, error) {
	cfg, err := s.Effective(ctx)
	if err != nil {
		return nil, err
	}
	rows, err := s.db.ListSettings(ctx)
	if err != nil {
		return nil, err
	}
	byKey := map[string]store.SettingRow{}
	for _, r := range rows {
		byKey[r.Key] = r
	}
	var out []SettingValue
	for _, setting := range config.Settings() {
		v, err := describeSetting(cfg, setting, byKey)
		if err != nil {
			return nil, err
		}
		v.PendingRestart = s.pending(v)
		out = append(out, v)
	}
	return out, nil
}

// MarkStarted keeps the values this process started with: a restart-mode
// setting that changes after it waits for a restart.
func (s *SettingsService) MarkStarted(ctx context.Context) {
	cfg, err := s.Effective(ctx)
	if err != nil {
		return
	}
	started := map[string]string{}
	for _, setting := range config.Settings() {
		if v, err := config.SettingValue(cfg, setting.Key); err == nil {
			raw, _ := json.Marshal(v)
			started[setting.Key] = string(raw)
		}
	}
	s.mu.Lock()
	s.started = started
	s.mu.Unlock()
}

func (s *SettingsService) pending(v SettingValue) bool {
	if v.Mode != config.ModeRestart {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	was, ok := s.started[v.Key]
	if !ok {
		return false
	}
	now, _ := json.Marshal(v.Value)
	return string(now) != was
}

// PendingRestart lists the settings waiting for a restart.
func (s *SettingsService) PendingRestart(ctx context.Context) ([]string, error) {
	all, err := s.List(ctx)
	if err != nil {
		return nil, err
	}
	var keys []string
	for _, v := range all {
		if v.PendingRestart {
			keys = append(keys, v.Key)
		}
	}
	return keys, nil
}

// OnChange runs f after every change this service keeps.
func (s *SettingsService) OnChange(f func()) {
	s.mu.Lock()
	s.onChange = append(s.onChange, f)
	s.mu.Unlock()
}

func (s *SettingsService) changed() {
	s.mu.Lock()
	fs := append([]func(){}, s.onChange...)
	s.mu.Unlock()
	for _, f := range fs {
		f()
	}
}

// Get is one setting with its value and source.
func (s *SettingsService) Get(ctx context.Context, key string) (SettingValue, error) {
	setting, ok := config.LookupSetting(key)
	if !ok {
		return SettingValue{}, fmt.Errorf("%s: %w", key, config.ErrUnknownSetting)
	}
	cfg, err := s.Effective(ctx)
	if err != nil {
		return SettingValue{}, err
	}
	rows, err := s.db.ListSettings(ctx)
	if err != nil {
		return SettingValue{}, err
	}
	byKey := map[string]store.SettingRow{}
	for _, r := range rows {
		byKey[r.Key] = r
	}
	v, err := describeSetting(cfg, setting, byKey)
	if err != nil {
		return SettingValue{}, err
	}
	v.PendingRestart = s.pending(v)
	return v, nil
}

func describeSetting(cfg *config.Config, setting config.Setting, rows map[string]store.SettingRow) (SettingValue, error) {
	value, err := config.SettingValue(cfg, setting.Key)
	if err != nil {
		return SettingValue{}, err
	}
	v := SettingValue{Setting: setting, Value: value, Source: SettingFromDefault}
	switch row, inDB := rows[setting.Key]; {
	case cfg.Pinned(setting.Key):
		v.Source, v.Pinned = SettingFromFile, true
		if setting.Secret && value != "" {
			// A secret given in the file is never shown.
			v.Value = "(set in config.yaml)"
		}
	case inDB:
		v.Source, v.UpdatedAt, v.UpdatedBy = SettingFromDatabase, row.UpdatedAt, row.UpdatedBy
	}
	return v, nil
}

// Check says whether setting key to raw would be taken, without keeping
// it: the value by its own rule, then the whole effective configuration
// with the rules config.Load uses.
func (s *SettingsService) Check(ctx context.Context, key string, raw json.RawMessage) (json.RawMessage, error) {
	setting, ok := config.LookupSetting(key)
	if !ok {
		return nil, fmt.Errorf("%s: %w", key, config.ErrUnknownSetting)
	}
	cfg, err := s.load()
	if err != nil {
		return nil, err
	}
	if cfg.Pinned(key) {
		return nil, fmt.Errorf("%s: %w", key, ErrSettingPinned)
	}
	value, err := config.DecodeSetting(setting, raw)
	if err != nil {
		return nil, err
	}
	canonical, _ := json.Marshal(value)
	values, err := s.db.SettingValues(ctx)
	if err != nil {
		return nil, err
	}
	values[key] = canonical
	if err := config.Overlay(cfg, values); err != nil {
		return nil, err
	}
	return canonical, nil
}

// Set keeps a setting's value in the database, once Check takes it. by
// says who changed it.
func (s *SettingsService) Set(ctx context.Context, key string, raw json.RawMessage, by string) error {
	if err := ownRuleAllowed(ctx, key); err != nil {
		return err
	}
	canonical, err := s.Check(ctx, key, raw)
	if err != nil {
		return err
	}
	if err := s.db.SetSetting(ctx, key, canonical, by); err != nil {
		return err
	}
	s.changed()
	return nil
}

// Reset deletes a setting's database value, so the default applies.
func (s *SettingsService) Reset(ctx context.Context, key string) error {
	if err := ownRuleAllowed(ctx, key); err != nil {
		return err
	}
	if _, ok := config.LookupSetting(key); !ok {
		return fmt.Errorf("%s: %w", key, config.ErrUnknownSetting)
	}
	cfg, err := s.load()
	if err != nil {
		return err
	}
	if cfg.Pinned(key) {
		return fmt.Errorf("%s: %w", key, ErrSettingPinned)
	}
	values, err := s.db.SettingValues(ctx)
	if err != nil {
		return err
	}
	delete(values, key)
	if err := config.Overlay(cfg, values); err != nil {
		return err
	}
	if err := s.db.DeleteSetting(ctx, key); err != nil {
		return err
	}
	s.changed()
	return nil
}

// RestoreSettings puts settings back to the values a restart record kept
// (service.SettingsRestorer): absent ones are deleted.
func (s *SettingsService) RestoreSettings(ctx context.Context, settings []RestartSetting) error {
	for _, setting := range settings {
		var err error
		if setting.List != "" {
			if len(setting.Old) == 0 {
				if err = s.db.DeleteSettingEntry(ctx, setting.List, setting.Key); errors.Is(err, store.ErrNotFound) {
					err = nil
				}
			} else {
				err = s.db.SetSettingEntry(ctx, setting.List, setting.Key, setting.Old, "rollback")
			}
			if err != nil {
				return err
			}
			continue
		}
		if len(setting.Old) == 0 {
			err = s.db.DeleteSetting(ctx, setting.Key)
		} else {
			err = s.db.SetSetting(ctx, setting.Key, setting.Old, "rollback")
		}
		if err != nil {
			return err
		}
	}
	return nil
}

// StringSetting reads and changes one string setting, as the tint and the
// update mode are kept (TintStore).
type StringSetting struct{ Settings *SettingsService }

// GetInstanceValue is the setting's effective value, "" when unset.
func (s StringSetting) GetInstanceValue(ctx context.Context, key string) (string, error) {
	v, err := s.Settings.Get(ctx, key)
	if err != nil {
		return "", err
	}
	str, _ := v.Value.(string)
	return str, nil
}

// SetInstanceValue changes it.
func (s StringSetting) SetInstanceValue(ctx context.Context, key, value string) error {
	raw, _ := json.Marshal(value)
	return s.Settings.Set(ctx, key, raw, "web")
}

// SettingEntryValue is one entry of a list, with where it comes from.
type SettingEntryValue struct {
	Name      string `json:"name"`
	Value     any    `json:"value"`
	Source    string `json:"source"`
	Pinned    bool   `json:"pinned"`
	UpdatedAt string `json:"updated_at,omitempty"`
	UpdatedBy string `json:"updated_by,omitempty"`
}

// SettingList is a list of named entries with its entries.
type SettingList struct {
	config.EntryList
	Entries []SettingEntryValue `json:"entries"`
}

// Lists is every list of named entries the database keeps, with the
// file's entries (pinned) and the database's.
func (s *SettingsService) Lists(ctx context.Context) ([]SettingList, error) {
	cfg, err := s.Effective(ctx)
	if err != nil {
		return nil, err
	}
	rows, err := s.db.ListSettingEntries(ctx)
	if err != nil {
		return nil, err
	}
	var out []SettingList
	for _, l := range config.EntryListDefs() {
		list := SettingList{EntryList: l, Entries: []SettingEntryValue{}}
		for _, name := range cfg.FileEntries(l.Key) {
			v, _, _ := config.EntryValue(cfg, l.Key, name)
			if l.Key == "credentials" || l.Key == "credential_sources" {
				v = redactEntry(v)
			}
			list.Entries = append(list.Entries, SettingEntryValue{Name: name, Value: v, Source: SettingFromFile, Pinned: true})
		}
		for _, r := range rows {
			if r.List != l.Key || cfg.PinnedEntry(l.Key, r.Name) {
				continue
			}
			var v any
			_ = json.Unmarshal([]byte(r.Value), &v)
			list.Entries = append(list.Entries, SettingEntryValue{Name: r.Name, Value: v, Source: SettingFromDatabase, UpdatedAt: r.UpdatedAt, UpdatedBy: r.UpdatedBy})
		}
		out = append(out, list)
	}
	return out, nil
}

// redactEntry hides a secret given in the file: a credential's secret is a
// reference, but a file may hold anything.
func redactEntry(v any) any {
	m, ok := v.(map[string]any)
	if !ok {
		return v
	}
	if secret, ok := m["secret"].(string); ok && !strings.Contains(secret, "://") {
		m["secret"] = "(set in config.yaml)"
	}
	return m
}

// SetEntry keeps one entry of a list in the database: refused when the
// file has an entry of that name, or when the whole configuration would be
// invalid with it.
func (s *SettingsService) SetEntry(ctx context.Context, list, name string, raw json.RawMessage, by string) error {
	if err := ownRuleAllowed(ctx, list); err != nil {
		return err
	}
	if _, err := s.CheckEntry(ctx, list, name, raw); err != nil {
		return err
	}
	if err := s.db.SetSettingEntry(ctx, list, strings.TrimSpace(name), raw, by); err != nil {
		return err
	}
	s.changed()
	return nil
}

// CheckEntry says whether SetEntry would take an entry.
func (s *SettingsService) CheckEntry(ctx context.Context, list, name string, raw json.RawMessage) (json.RawMessage, error) {
	if _, ok := config.LookupEntryList(list); !ok {
		return nil, fmt.Errorf("%s: %w", list, config.ErrUnknownSetting)
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, errors.New("an entry needs a name")
	}
	var probe map[string]any
	if json.Unmarshal(raw, &probe) != nil {
		return nil, fmt.Errorf("%s %q: want an object", list, name)
	}
	cfg, err := s.load()
	if err != nil {
		return nil, err
	}
	if cfg.PinnedEntry(list, name) {
		return nil, fmt.Errorf("%s %q: %w", list, name, config.ErrEntryPinned)
	}
	values, err := s.db.SettingValues(ctx)
	if err != nil {
		return nil, err
	}
	entries, err := s.db.SettingEntries(ctx)
	if err != nil {
		return nil, err
	}
	if entries[list] == nil {
		entries[list] = map[string]json.RawMessage{}
	}
	entries[list][name] = raw
	if err := overlayAll(cfg, values, entries); err != nil {
		return nil, err
	}
	return raw, nil
}

// DeleteEntry removes one database entry of a list.
func (s *SettingsService) DeleteEntry(ctx context.Context, list, name string) error {
	if err := ownRuleAllowed(ctx, list); err != nil {
		return err
	}
	if _, ok := config.LookupEntryList(list); !ok {
		return fmt.Errorf("%s: %w", list, config.ErrUnknownSetting)
	}
	cfg, err := s.load()
	if err != nil {
		return err
	}
	if cfg.PinnedEntry(list, name) {
		return fmt.Errorf("%s %q: %w", list, name, config.ErrEntryPinned)
	}
	if err := s.db.DeleteSettingEntry(ctx, list, name); err != nil {
		return err
	}
	s.changed()
	return nil
}
