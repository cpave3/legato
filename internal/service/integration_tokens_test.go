package service

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cpave3/legato/internal/engine/store"
)

func newIntegrationStore(t *testing.T) *store.Store {
	t.Helper()
	s, err := store.New(filepath.Join(t.TempDir(), "integrations.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestAnIntegrationTokenAuthenticatesWithItsScopes(t *testing.T) {
	svc := NewIntegrationTokenService(newIntegrationStore(t))
	ctx := context.Background()
	token, err := svc.Create(ctx, "argus", []string{ScopeOwnerRead, ScopeDelegate})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(token, "lgi_") || len(token) < 40 {
		t.Fatalf("token = %q", token)
	}
	id, err := svc.Authenticate(ctx, token)
	if err != nil || id.Name != "argus" || !id.Has(ScopeDelegate) || !id.Has(ScopeOwnerRead) {
		t.Fatalf("authenticate = %+v, %v", id, err)
	}
	list, _ := svc.List(ctx)
	if len(list) != 1 || list[0].Name != "argus" {
		t.Fatalf("list = %+v", list)
	}
	if err := svc.Revoke(ctx, "argus"); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Authenticate(ctx, token); !errors.Is(err, ErrIntegrationUnauthorized) {
		t.Fatalf("revoked err = %v", err)
	}
}

func TestIntegrationTokensRefuseBadScopesAndNames(t *testing.T) {
	svc := NewIntegrationTokenService(newIntegrationStore(t))
	ctx := context.Background()
	for _, tc := range []struct {
		name   string
		scopes []string
	}{
		{"argus", []string{"tasks:write"}},
		{"argus", nil},
		{"Argus!", []string{ScopeDelegate}},
		{"", []string{ScopeDelegate}},
	} {
		if _, err := svc.Create(ctx, tc.name, tc.scopes); err == nil {
			t.Errorf("create(%q, %v) succeeded", tc.name, tc.scopes)
		}
	}
	if _, err := svc.Create(ctx, "argus", []string{ScopeDelegate}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Create(ctx, "argus", []string{ScopeDelegate}); err == nil {
		t.Fatal("second live token with the same name was created")
	}
	if _, err := svc.Authenticate(ctx, "lgi_nonsense"); !errors.Is(err, ErrIntegrationUnauthorized) {
		t.Fatalf("unknown token err = %v", err)
	}
}
