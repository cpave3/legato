package service

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/cpave3/legato/internal/engine/events"
	"github.com/cpave3/legato/internal/engine/store"
)

// The service is the shared caller for manual settings and MQTT; listeners
// can reread the saved snapshot, including through another opener.
func TestServerThemeChangeNotifiesAndSurvivesReopen(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "themes.db")
	db, err := store.New(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	bus := events.New()
	changed, unsubscribe := bus.SubscribeInvalidations(events.EventServerThemeChanged)
	defer unsubscribe()
	themes := NewThemePreferenceService(db, bus)
	before, err := themes.Current(ctx)
	if err != nil || before.ID != "" || before.Revision != 0 {
		t.Fatalf("initial: %+v %v", before, err)
	}
	saved, err := themes.Change(ctx, before.Revision, "ocular-dark-vivid", "web")
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-changed:
	default:
		t.Fatal("missing committed invalidation")
	}
	reopened, err := store.New(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	actual, err := NewThemePreferenceService(reopened, nil).Current(ctx)
	if err != nil || actual != saved || actual.ID != "ocular-dark-vivid" || actual.Revision != 1 {
		t.Fatalf("reopened: %+v %v", actual, err)
	}
}

func TestServerThemeRejectsStaleAndUnknownChoicesWithoutNotification(t *testing.T) {
	db, err := store.New(filepath.Join(t.TempDir(), "themes.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	bus := events.New()
	changed, unsubscribe := bus.SubscribeInvalidations(events.EventServerThemeChanged)
	defer unsubscribe()
	service := NewThemePreferenceService(db, bus)
	ctx := context.Background()
	saved, err := service.Change(ctx, 0, "dracula", "web")
	if err != nil {
		t.Fatal(err)
	}
	<-changed
	for _, input := range []struct {
		revision uint64
		id       string
		want     error
	}{{0, "ocular-light", ErrThemeConflict}, {1, "shell-command", ErrUnknownTheme}} {
		if _, err := service.Change(ctx, input.revision, input.id, "mqtt:broker-grant"); !errors.Is(err, input.want) {
			t.Fatalf("error %v want %v", err, input.want)
		}
		actual, err := service.Current(ctx)
		if err != nil || actual != saved {
			t.Fatalf("refused mutation changed authority: %+v %v", actual, err)
		}
		select {
		case <-changed:
			t.Fatal("refused mutation notified clients")
		default:
		}
	}
	cleared, err := service.Change(ctx, 1, "", "web")
	if err != nil || cleared.ID != "" || cleared.Revision != 2 {
		t.Fatalf("explicit clear: %+v %v", cleared, err)
	}
}

func TestServerThemeConcurrentProcessesCannotOverwriteTheSameRevision(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "shared.db")
	first, err := store.New(path)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	second, err := store.New(path)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	start := make(chan struct{})
	results := make(chan error, 2)
	for i, db := range []*store.Store{first, second} {
		go func(index int, db *store.Store) {
			<-start
			_, err := NewThemePreferenceService(db, nil).Change(ctx, 0, []string{"dracula", "ocular-dark"}[index], "web")
			results <- err
		}(i, db)
	}
	close(start)
	success, conflicts := 0, 0
	for range 2 {
		err := <-results
		switch {
		case err == nil:
			success++
		case errors.Is(err, ErrThemeConflict):
			conflicts++
		default:
			t.Fatal(err)
		}
	}
	if success != 1 || conflicts != 1 {
		t.Fatalf("success/conflicts: %d/%d", success, conflicts)
	}
	value, err := NewThemePreferenceService(first, nil).Current(ctx)
	if err != nil || value.Revision != 1 {
		t.Fatalf("authority: %+v %v", value, err)
	}
}

func TestServerThemeSaveFailureDoesNotNotify(t *testing.T) {
	db, err := store.New(filepath.Join(t.TempDir(), "closed.db"))
	if err != nil {
		t.Fatal(err)
	}
	bus := events.New()
	changes, unsubscribe := bus.SubscribeInvalidations(events.EventServerThemeChanged)
	defer unsubscribe()
	service := NewThemePreferenceService(db, bus)
	db.Close()
	if _, err := service.Change(context.Background(), 0, "dracula", "web"); err == nil {
		t.Fatal("closed database was reported saved")
	}
	select {
	case <-changes:
		t.Fatal("failed save emitted an event")
	default:
	}
}
