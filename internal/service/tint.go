package service

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
)

// Tints are the colours a Legato can be tinted, so the web UI shows at a
// glance which server it is on. The web UI draws each.
var Tints = []string{"red", "orange", "yellow", "green", "teal", "blue", "purple", "pink"}

// ErrUnknownTint is a colour that is not one of Tints.
var ErrUnknownTint = errors.New("not a tint")

const tintKey = "web.tint"

// TintStore keeps settings of this Legato.
type TintStore interface {
	GetInstanceValue(ctx context.Context, key string) (string, error)
	SetInstanceValue(ctx context.Context, key, value string) error
}

// TintService is this Legato's tint, kept in its database: every browser
// and phone that uses this server shows it.
type TintService struct{ store TintStore }

// NewTintService reads and keeps the tint in store.
func NewTintService(store TintStore) *TintService { return &TintService{store: store} }

// Tint is this Legato's tint, "" for none.
func (t *TintService) Tint(ctx context.Context) (string, error) {
	return t.store.GetInstanceValue(ctx, tintKey)
}

// SetTint sets this Legato's tint, one of Tints, or "" for none.
func (t *TintService) SetTint(ctx context.Context, tint string) error {
	tint = strings.ToLower(strings.TrimSpace(tint))
	if tint != "" && !slices.Contains(Tints, tint) {
		return fmt.Errorf("%w: %q (use one of %s, or none)", ErrUnknownTint, tint, strings.Join(Tints, ", "))
	}
	return t.store.SetInstanceValue(ctx, tintKey, tint)
}
