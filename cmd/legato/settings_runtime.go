package main

import (
	"context"
	"sync"
	"time"

	"github.com/cpave3/legato/config"
	"github.com/cpave3/legato/internal/engine/credential"
	"github.com/cpave3/legato/internal/engine/events"
	"github.com/cpave3/legato/internal/engine/store"
	"github.com/cpave3/legato/internal/service"
)

// processSettings holds this process's settings service for each store:
// one per process, made at start, so it knows the values the process
// started with (what waits for a restart).
var processSettings sync.Map

// settingsFor is this process's settings service over db.
func settingsFor(db *store.Store) *service.SettingsService {
	if s, ok := processSettings.Load(db); ok {
		return s.(*service.SettingsService)
	}
	s := service.NewSettingsService(db, nil)
	s.MarkStarted(context.Background())
	actual, _ := processSettings.LoadOrStore(db, s)
	return actual.(*service.SettingsService)
}

// wireSettings serves the settings, tells the bus of each change, and puts
// changes waiting for a restart in Attention.
func wireSettings(db *store.Store, bus *events.Bus, attention *service.AttentionService) *service.SettingsService {
	settings := settingsFor(db)
	if attention != nil {
		mover := settingsMover(db)
		_ = attention.RegisterProducerWithEvents(service.NewSettingsMoveAttentionAdapter(mover), events.EventSettingsChanged)
	}
	settings.OnChange(func() {
		bus.Publish(events.Event{Type: events.EventSettingsChanged, At: time.Now()})
	})
	if attention != nil {
		registerSettingsAttention(attention, settings)
	}
	return settings
}

// registerSettingsAttention puts changes waiting for a restart in an
// Attention service, with an action that restarts Legato.
func registerSettingsAttention(attention *service.AttentionService, settings *service.SettingsService) {
	adapter := service.NewSettingsAttentionAdapter(settings, restartAll)
	_ = attention.RegisterProducerWithEvents(adapter, events.EventSettingsChanged)
	_ = attention.RegisterHandler(adapter.Producer(), adapter)
}

// settingsMover moves pinned settings into the database for this process.
func settingsMover(db *store.Store) *service.SettingsMover {
	cfg, _ := config.Load()
	rb := &service.RestartRollback{Store: db, Settings: settingsFor(db)}
	if cfg != nil {
		resolved, _ := config.ResolveDatabase(cfg)
		rb.DBDriver, rb.DBPath = resolved.Driver, resolved.Path
	}
	return &service.SettingsMover{Settings: settingsFor(db), Rollback: rb}
}

// wizardSettings keeps what the first-run wizard is told in the database:
// Jira's settings, and its token in Legato's secret store.
type wizardSettings struct {
	cfg *config.Config
	db  *store.Store
}

func (w wizardSettings) SetSetting(ctx context.Context, key string, raw []byte) error {
	return settingsFor(w.db).Set(ctx, key, raw, "setup")
}

func (w wizardSettings) StoreSecret(ctx context.Context, name, value string) (string, error) {
	if err := configuredSecretStore(w.cfg, w.db).Add(ctx, name, credential.TypeValue, []byte(value)); err != nil {
		return "", err
	}
	return "legato://" + name, nil
}
