package service

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/cpave3/legato/internal/engine/store"
)

// RestartSetting is a database setting a restart altered, with the value it
// had before (nil: it was not set).
type RestartSetting struct {
	// List is set for an entry of a list of named entries; Key is then
	// the entry's name.
	List string          `json:"list,omitempty"`
	Key  string          `json:"key"`
	Old  json.RawMessage `json:"old,omitempty"`
}

// RestartPlan is what a restart changed and can put back.
type RestartPlan struct {
	Reason      string
	RequestedBy string
	Version     string
	// ConfigPath is the instance's config.yaml; ConfigBackup a copy of it
	// from before the change ("" when the file was not changed).
	ConfigPath   string
	ConfigBackup string
	Settings     []RestartSetting
}

// SettingsRestorer puts database settings back to their old values.
type SettingsRestorer interface {
	RestoreSettings(ctx context.Context, settings []RestartSetting) error
}

// RestartPointer is the file beside config.yaml that says a restart is
// pending. It is read before the configuration loads, so it holds what is
// needed to roll back without it.
type RestartPointer struct {
	RecordID     string `json:"record_id"`
	ConfigPath   string `json:"config_path"`
	ConfigBackup string `json:"config_backup,omitempty"`
	DBDriver     string `json:"db_driver"`
	DBPath       string `json:"db_path"`
}

// RestartPointerPath is the pointer file of a config.yaml.
func RestartPointerPath(configPath string) string { return configPath + ".restart-pending" }

// ErrRestartPending is BeginRestart's answer while another restart is pending.
var ErrRestartPending = errors.New("a restart is already pending")

// RestartRollback keeps restart records and rolls a failed restart back.
type RestartRollback struct {
	Store    *store.Store
	Settings SettingsRestorer
	DBDriver string
	DBPath   string
	Now      func() time.Time
}

// Begin records a restart and writes its pointer. Only one restart is
// pending at a time: the pointer is created exclusively.
func (r *RestartRollback) Begin(ctx context.Context, plan RestartPlan) (store.RestartRecord, error) {
	id := newRestartID()
	settings, _ := json.Marshal(plan.Settings)
	if plan.Settings == nil {
		settings = []byte("[]")
	}
	rec := store.RestartRecord{ID: id, Reason: plan.Reason, RequestedBy: plan.RequestedBy, Version: plan.Version,
		ConfigPath: plan.ConfigPath, ConfigBackup: plan.ConfigBackup, Settings: string(settings), State: store.RestartPending}
	pointer := RestartPointer{RecordID: id, ConfigPath: plan.ConfigPath, ConfigBackup: plan.ConfigBackup, DBDriver: r.DBDriver, DBPath: r.DBPath}
	raw, _ := json.Marshal(pointer)
	f, err := os.OpenFile(RestartPointerPath(plan.ConfigPath), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if errors.Is(err, os.ErrExist) {
		return rec, ErrRestartPending
	}
	if err != nil {
		return rec, err
	}
	if _, err := f.Write(raw); err != nil {
		f.Close()
		return rec, err
	}
	if err := f.Close(); err != nil {
		return rec, err
	}
	if err := r.Store.InsertRestartRecord(ctx, rec); err != nil {
		_ = os.Remove(RestartPointerPath(plan.ConfigPath))
		return rec, err
	}
	return rec, nil
}

func newRestartID() string {
	b := make([]byte, 6)
	_, _ = rand.Read(b)
	return "rs-" + hex.EncodeToString(b)
}

// ReadRestartPointer reads the pending restart of a config.yaml, if any.
func ReadRestartPointer(configPath string) (RestartPointer, bool) {
	raw, err := os.ReadFile(RestartPointerPath(configPath))
	if err != nil {
		return RestartPointer{}, false
	}
	var p RestartPointer
	if json.Unmarshal(raw, &p) != nil || p.RecordID == "" {
		return RestartPointer{}, false
	}
	return p, true
}

// claimPointer takes the pointer away so only one process acts on it.
func claimPointer(configPath string) bool {
	claimed := RestartPointerPath(configPath) + fmt.Sprintf(".%d", os.Getpid())
	if err := os.Rename(RestartPointerPath(configPath), claimed); err != nil {
		return false
	}
	_ = os.Remove(claimed)
	return true
}

// RestoreConfig puts the pointer's backup back over config.yaml, atomically.
func RestoreConfig(p RestartPointer) error {
	if p.ConfigBackup == "" {
		return nil
	}
	raw, err := os.ReadFile(p.ConfigBackup)
	if err != nil {
		return fmt.Errorf("reading the backup %s: %w", p.ConfigBackup, err)
	}
	tmp := filepath.Join(filepath.Dir(p.ConfigPath), "."+filepath.Base(p.ConfigPath)+".restore")
	if err := os.WriteFile(tmp, raw, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, p.ConfigPath)
}

// RollBackUnloadable is the first thing a start does when its configuration
// does not load: with a pending restart that changed config.yaml, it puts
// the backup back and records why, and reports true; the caller then starts
// again on the old file. open opens the database the pointer names
// (store.Open when nil).
func RollBackUnloadable(ctx context.Context, configPath string, loadErr error, open func(store.Options) (*store.Store, error)) (bool, error) {
	p, ok := ReadRestartPointer(configPath)
	if !ok || p.ConfigBackup == "" || !claimPointer(configPath) {
		return false, nil
	}
	if err := RestoreConfig(p); err != nil {
		return false, err
	}
	if open == nil {
		open = store.Open
	}
	if db, err := open(store.Options{Driver: p.DBDriver, Path: p.DBPath}); err == nil {
		defer db.Close()
		_, _ = db.FinishRestartRecord(ctx, p.RecordID, store.RestartRolledBack, "The configuration did not load: "+loadErr.Error())
	}
	return true, nil
}

// Settle decides a pending restart once this process has started: healthy
// within timeout, it is applied; otherwise config.yaml and the database
// settings are put back, and the caller restarts on them. The outcome is
// store.RestartApplied, store.RestartRolledBack, or "" when no restart of
// this configuration was pending (or another process settled it).
func (r *RestartRollback) Settle(ctx context.Context, configPath string, healthy func(context.Context) error, timeout, every time.Duration) (string, error) {
	p, ok := ReadRestartPointer(configPath)
	if !ok {
		return "", nil
	}
	deadline := time.Now().Add(timeout)
	lastErr := errors.New("not healthy")
	for {
		if lastErr = healthy(ctx); lastErr == nil {
			db, _, done, err := r.pointerStore(p)
			if err != nil {
				return "", err
			}
			defer done()
			if _, err := db.GetRestartRecord(ctx, p.RecordID); err != nil {
				return "", err
			}
			if !claimPointer(configPath) {
				return "", nil
			}
			_, err = db.FinishRestartRecord(ctx, p.RecordID, store.RestartApplied, "")
			return store.RestartApplied, err
		}
		if !time.Now().Before(deadline) {
			break
		}
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(every):
		}
	}
	why := fmt.Sprintf("Legato was not healthy within %s of starting: %v", timeout.Round(time.Second), lastErr)
	rolled, err := r.rollBack(ctx, configPath, p, why)
	if err != nil || !rolled {
		return "", err
	}
	return store.RestartRolledBack, nil
}

// rollBack puts back config.yaml and the database settings of the restart
// p, in the database p names, and records why. The record is read before
// the pointer is claimed, so a pointer whose record is missing is kept.
// false when another process claimed it first.
func (r *RestartRollback) rollBack(ctx context.Context, configPath string, p RestartPointer, why string) (bool, error) {
	db, settingsRestorer, done, err := r.pointerStore(p)
	if err != nil {
		return false, err
	}
	defer done()
	rec, err := db.GetRestartRecord(ctx, p.RecordID)
	if err != nil {
		return false, err
	}
	if !claimPointer(configPath) {
		return false, nil
	}
	if err := RestoreConfig(p); err != nil {
		return false, err
	}
	var settings []RestartSetting
	_ = json.Unmarshal([]byte(rec.Settings), &settings)
	if len(settings) > 0 && settingsRestorer != nil {
		if err := settingsRestorer.RestoreSettings(ctx, settings); err != nil {
			return false, err
		}
	}
	_, err = db.FinishRestartRecord(ctx, p.RecordID, store.RestartRolledBack, why)
	return true, err
}

// pointerStore is the database the pointer p names, with what puts its
// settings back: this rollback's own when it is the same database, else
// that database opened for the rollback. done closes what was opened.
func (r *RestartRollback) pointerStore(p RestartPointer) (*store.Store, SettingsRestorer, func(), error) {
	if p.DBPath == "" || (p.DBPath == r.DBPath && p.DBDriver == r.DBDriver) {
		return r.Store, r.Settings, func() {}, nil
	}
	db, err := store.Open(store.Options{Driver: p.DBDriver, Path: p.DBPath})
	if err != nil {
		return nil, nil, nil, fmt.Errorf("opening the restart's database %s: %w", p.DBPath, err)
	}
	return db, NewSettingsService(db, nil), func() { db.Close() }, nil
}
