package service

import (
	"context"
	"errors"
	"strings"
	"testing"
)

const mainInstance = "srv-0123456789abcdef"

func TestAPairedMainGetsAHandoffOnlyTokenOfItsOwn(t *testing.T) {
	svc := NewIntegrationTokenService(newIntegrationStore(t))
	ctx := context.Background()
	token, err := svc.CreateMain(ctx, "workstation", mainInstance)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(token, IntegrationTokenPrefix) {
		t.Fatalf("token %q", token)
	}
	id, err := svc.Authenticate(ctx, token)
	if err != nil {
		t.Fatal(err)
	}
	if id.Kind != IntegrationKindMain || id.InstanceID != mainInstance || id.Name != "workstation" {
		t.Fatalf("identity %+v", id)
	}
	if len(id.Scopes) != 1 || !id.Has(ScopeHandoff) {
		t.Fatalf("scopes %v", id.Scopes)
	}
	mains, err := svc.ListMains(ctx)
	if err != nil || len(mains) != 1 || mains[0].InstanceID != mainInstance || mains[0].Name != "workstation" || mains[0].PairedAt == "" {
		t.Fatalf("mains %+v, %v", mains, err)
	}
}

func TestRepairingTheSameMainReplacesItsToken(t *testing.T) {
	svc := NewIntegrationTokenService(newIntegrationStore(t))
	ctx := context.Background()
	first, _ := svc.CreateMain(ctx, "workstation", mainInstance)
	second, err := svc.CreateMain(ctx, "desk", mainInstance)
	if err != nil {
		t.Fatalf("re-pairing: %v", err)
	}
	if _, err := svc.Authenticate(ctx, first); !errors.Is(err, ErrIntegrationUnauthorized) {
		t.Fatalf("the replaced token: %v", err)
	}
	if id, err := svc.Authenticate(ctx, second); err != nil || id.Name != "desk" {
		t.Fatalf("the new token: %+v, %v", id, err)
	}
	if mains, _ := svc.ListMains(ctx); len(mains) != 1 || mains[0].Name != "desk" {
		t.Fatalf("mains %+v", mains)
	}
}

func TestAnotherMainCannotTakeAPairedMainsName(t *testing.T) {
	svc := NewIntegrationTokenService(newIntegrationStore(t))
	ctx := context.Background()
	if _, err := svc.CreateMain(ctx, "workstation", mainInstance); err != nil {
		t.Fatal(err)
	}
	_, err := svc.CreateMain(ctx, "workstation", "srv-fedcba9876543210")
	if !errors.Is(err, ErrMainNameTaken) {
		t.Fatalf("err %v", err)
	}
}

func TestAMainsNameDoesNotCollideWithAnArgusToken(t *testing.T) {
	svc := NewIntegrationTokenService(newIntegrationStore(t))
	ctx := context.Background()
	if _, err := svc.Create(ctx, "argus", []string{ScopeDelegate}); err != nil {
		t.Fatal(err)
	}
	token, err := svc.CreateMain(ctx, "argus", mainInstance)
	if err != nil {
		t.Fatalf("a main named argus: %v", err)
	}
	if id, _ := svc.Authenticate(ctx, token); id.Has(ScopeDelegate) {
		t.Fatalf("the main got Argus's scopes: %+v", id)
	}
	if mains, _ := svc.ListMains(ctx); len(mains) != 1 {
		t.Fatalf("mains %+v", mains)
	}
	// Revoking Argus's token by name leaves the main paired.
	if err := svc.Revoke(ctx, "argus"); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Authenticate(ctx, token); err != nil {
		t.Fatalf("the main after revoking argus: %v", err)
	}
}

func TestTheHandoffScopeComesOnlyFromPairing(t *testing.T) {
	svc := NewIntegrationTokenService(newIntegrationStore(t))
	ctx := context.Background()
	for _, scopes := range [][]string{{ScopeHandoff}, {ScopeDelegate, ScopeHandoff}} {
		if _, err := svc.Create(ctx, "sneaky", scopes); err == nil {
			t.Fatalf("Create made a token with %v", scopes)
		}
	}
	id, _ := svc.Authenticate(ctx, mustCreate(t, svc, "argus", ScopeDelegate))
	if id.Kind != IntegrationKindArgus || id.InstanceID != "" {
		t.Fatalf("an Argus token: %+v", id)
	}
}

func TestUnpairingAMainRevokesItsToken(t *testing.T) {
	svc := NewIntegrationTokenService(newIntegrationStore(t))
	ctx := context.Background()
	token, _ := svc.CreateMain(ctx, "workstation", mainInstance)
	if err := svc.RevokeMain(ctx, mainInstance); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Authenticate(ctx, token); !errors.Is(err, ErrIntegrationUnauthorized) {
		t.Fatalf("after unpairing: %v", err)
	}
	if mains, _ := svc.ListMains(ctx); len(mains) != 0 {
		t.Fatalf("mains %+v", mains)
	}
	if err := svc.RevokeMain(ctx, mainInstance); !errors.Is(err, ErrMainNotPaired) {
		t.Fatalf("unpairing again: %v", err)
	}
}

func TestAMainNeedsAnInstanceIDAndAName(t *testing.T) {
	svc := NewIntegrationTokenService(newIntegrationStore(t))
	ctx := context.Background()
	if _, err := svc.CreateMain(ctx, "workstation", "workstation"); err == nil {
		t.Fatal("made a main token without an instance ID")
	}
	if _, err := svc.CreateMain(ctx, "Work Station", mainInstance); err == nil {
		t.Fatal("made a main token with a name that is not normalised")
	}
}

func mustCreate(t *testing.T, svc *IntegrationTokenService, name string, scopes ...string) string {
	t.Helper()
	token, err := svc.Create(context.Background(), name, scopes)
	if err != nil {
		t.Fatal(err)
	}
	return token
}
