package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"

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
	Value     any    `json:"value"`
	Source    string `json:"source"`
	Pinned    bool   `json:"pinned"`
	UpdatedAt string `json:"updated_at,omitempty"`
	UpdatedBy string `json:"updated_by,omitempty"`
}

// SettingsService keeps the settings of the registry that live in the
// database, and gives the effective configuration: config.yaml, then the
// database's values for the settings the file does not pin.
type SettingsService struct {
	db   *store.Store
	load func() (*config.Config, error)
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
func ApplySettings(ctx context.Context, db *store.Store, cfg *config.Config) error {
	values, err := db.SettingValues(ctx)
	if err != nil || len(values) == 0 {
		return err
	}
	probe, err := config.Load()
	if err == nil {
		err = config.Overlay(probe, values)
	}
	if err != nil {
		return fmt.Errorf("the settings saved in the database are not applied: %w", err)
	}
	return config.Overlay(cfg, values)
}

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
	if err := config.Overlay(cfg, values); err != nil {
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
		out = append(out, v)
	}
	return out, nil
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
	return describeSetting(cfg, setting, byKey)
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
	value, err := config.DecodeSetting(setting, raw)
	if err != nil {
		return nil, err
	}
	canonical, _ := json.Marshal(value)
	cfg, err := s.load()
	if err != nil {
		return nil, err
	}
	if cfg.Pinned(key) {
		return nil, fmt.Errorf("%s: %w", key, ErrSettingPinned)
	}
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
	canonical, err := s.Check(ctx, key, raw)
	if err != nil {
		return err
	}
	return s.db.SetSetting(ctx, key, canonical, by)
}

// Reset deletes a setting's database value, so the default applies.
func (s *SettingsService) Reset(ctx context.Context, key string) error {
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
	return s.db.DeleteSetting(ctx, key)
}

// RestoreSettings puts settings back to the values a restart record kept
// (service.SettingsRestorer): absent ones are deleted.
func (s *SettingsService) RestoreSettings(ctx context.Context, settings []RestartSetting) error {
	for _, setting := range settings {
		var err error
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
