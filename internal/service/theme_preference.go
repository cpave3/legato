package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/cpave3/legato/internal/engine/events"
	"github.com/cpave3/legato/internal/engine/store"
	"github.com/cpave3/legato/themes"
)

var ErrUnknownTheme = errors.New("unknown server theme")
var ErrThemeConflict = errors.New("server theme changed; refresh and try again")

type ThemePreference = store.ThemePreference

type themePreferenceStore interface {
	ThemePreference(context.Context) (store.ThemePreference, error)
	ChangeThemePreference(context.Context, uint64, string, string) (store.ThemePreference, error)
}

// ThemePreferenceService is the sole mutation boundary for server appearance.
// Client follow mode and local/terminal themes remain client-owned.
type ThemePreferenceService struct {
	store themePreferenceStore
	bus   *events.Bus
}

func NewThemePreferenceService(db themePreferenceStore, bus *events.Bus) *ThemePreferenceService {
	return &ThemePreferenceService{store: db, bus: bus}
}
func (s *ThemePreferenceService) Current(ctx context.Context) (ThemePreference, error) {
	return s.store.ThemePreference(ctx)
}
func (s *ThemePreferenceService) Change(ctx context.Context, expected uint64, id, actor string) (ThemePreference, error) {
	if id != "" && !themes.ValidID(id) {
		return ThemePreference{}, fmt.Errorf("%w: %q", ErrUnknownTheme, id)
	}
	value, err := s.store.ChangeThemePreference(ctx, expected, id, actor)
	if errors.Is(err, store.ErrConflict) {
		return ThemePreference{}, ErrThemeConflict
	}
	if err != nil {
		return ThemePreference{}, err
	}
	if s.bus != nil {
		s.bus.Publish(events.Event{Type: events.EventServerThemeChanged, At: time.Now(), Payload: value})
	}
	return value, nil
}
