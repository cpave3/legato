package service

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cpave3/legato/internal/engine/credential"
	"github.com/cpave3/legato/internal/engine/store"
)

func newSecretStoreService(t *testing.T) (*SecretStoreService, *store.Store) {
	t.Helper()
	db, err := store.New(filepath.Join(t.TempDir(), "secrets.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return NewSecretStoreService(db, credential.MasterKeyOptions{DataDir: t.TempDir(), Source: "file"}), db
}

func TestSecretsAddedToLegatoAreSealedAndReadBack(t *testing.T) {
	secrets, db := newSecretStoreService(t)
	ctx := context.Background()

	if err := secrets.Add(ctx, "eleven", credential.TypeValue, []byte("sk-123\n")); err != nil {
		t.Fatal(err)
	}
	row, _ := db.GetSecret(ctx, "eleven")
	if strings.Contains(string(row.Ciphertext), "sk-123") {
		t.Fatal("the value is stored in the clear")
	}
	got, err := secrets.Get(ctx, credential.Ref{Scheme: "legato", Path: "eleven"})
	if err != nil || string(got.Value) != "sk-123" {
		t.Fatalf("got %q, %v (want the value without its trailing newline)", got.Value, err)
	}
	list, err := secrets.List(ctx)
	if err != nil || len(list) != 1 || list[0].Name != "eleven" || list[0].Type != credential.TypeValue {
		t.Fatalf("list = %+v, %v", list, err)
	}
	if source, err := secrets.Source(ctx); err != nil || source != "file" {
		t.Fatalf("source = %q, %v", source, err)
	}
	if err := secrets.Remove(ctx, "eleven"); err != nil {
		t.Fatal(err)
	}
	if _, err := secrets.Get(ctx, credential.Ref{Scheme: "legato", Path: "eleven"}); !errors.Is(err, credential.ErrNotFound) {
		t.Fatalf("after remove err = %v", err)
	}
}

func TestASecretNeedsAGoodNameATypeAndAValue(t *testing.T) {
	secrets, _ := newSecretStoreService(t)
	ctx := context.Background()
	for name, call := range map[string]func() error{
		"bad name": func() error { return secrets.Add(ctx, "../x", credential.TypeValue, []byte("v")) },
		"bad type": func() error { return secrets.Add(ctx, "x", "password", []byte("v")) },
		"empty":    func() error { return secrets.Add(ctx, "x", credential.TypeValue, []byte("\n")) },
	} {
		if err := call(); !errors.Is(err, ErrSecretInvalid) {
			t.Errorf("%s: err = %v", name, err)
		}
	}
	if err := secrets.Remove(ctx, "missing"); !errors.Is(err, credential.ErrNotFound) {
		t.Fatalf("removing a missing secret: %v", err)
	}
}
