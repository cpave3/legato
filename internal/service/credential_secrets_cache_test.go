package service

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/cpave3/legato/internal/engine/credential"
	"github.com/cpave3/legato/internal/engine/store"
)

// countingCreds stands in for systemd-creds and counts the unseals.
type countingCreds struct {
	mu       sync.Mutex
	stored   []byte
	decrypts int
}

func (c *countingCreds) Run(_ context.Context, name string, args, _ []string, stdin []byte) ([]byte, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if name != "systemd-creds" {
		return nil, errors.New("unexpected command " + name)
	}
	switch args[0] {
	case "encrypt":
		c.stored = append([]byte{}, stdin...)
		return nil, os.WriteFile(args[len(args)-1], []byte("sealed"), 0o600)
	case "decrypt":
		c.decrypts++
		return c.stored, nil
	}
	return nil, errors.New("unexpected systemd-creds " + args[0])
}

func (c *countingCreds) unseals() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.decrypts
}

// systemdCredsSecrets is a store with five secrets whose key systemd-creds
// keeps, and options for opening it again as a new process would.
func systemdCredsSecrets(t *testing.T) (*store.Store, credential.MasterKeyOptions, *countingCreds, []string) {
	t.Helper()
	db, err := store.New(filepath.Join(t.TempDir(), "secrets.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	creds := &countingCreds{}
	opts := credential.MasterKeyOptions{
		DataDir:   t.TempDir(),
		Source:    "systemd-creds",
		GOOS:      "linux",
		Available: func(string) bool { return true },
		Runner:    creds,
	}
	names := []string{"jira", "github", "slack-app", "slack-bot", "discord"}
	setup := NewSecretStoreService(db, opts)
	for _, name := range names {
		if err := setup.Add(context.Background(), name, credential.TypeValue, []byte("value of "+name)); err != nil {
			t.Fatal(err)
		}
	}
	credential.ForgetMasterKey(opts.DataDir)
	creds.mu.Lock()
	creds.decrypts = 0
	creds.mu.Unlock()
	return db, opts, creds, names
}

// Each secret setting is read through a store of its own (settingSecrets
// builds one per read), so the key must be shared across them.
func TestSecretReadsThroughSeparateStoresUnsealTheKeyOnce(t *testing.T) {
	db, opts, creds, names := systemdCredsSecrets(t)

	for _, name := range names {
		got, err := NewSecretStoreService(db, opts).Get(context.Background(), credential.Ref{Scheme: "legato", Path: name})
		if err != nil || string(got.Value) != "value of "+name {
			t.Fatalf("%s = %q, %v", name, got.Value, err)
		}
	}
	if n := creds.unseals(); n != 1 {
		t.Fatalf("%d secret reads unsealed the key %d times, want 1", len(names), n)
	}
}

func TestASecretTheCachedKeyCannotOpenIsTriedOnceMoreWithTheKeyUnsealedAgain(t *testing.T) {
	db, opts, creds, names := systemdCredsSecrets(t)
	secrets := NewSecretStoreService(db, opts)
	if _, err := secrets.Get(context.Background(), credential.Ref{Scheme: "legato", Path: names[0]}); err != nil {
		t.Fatal(err)
	}

	// A value sealed under some other key.
	other := make([]byte, 32)
	other[0] = 7
	if err := (credential.LegatoStore{Records: sealedRecords{db}, MasterKey: other}).Put(context.Background(), credential.Ref{Scheme: "legato", Path: "stray"}, credential.Secret{Type: credential.TypeValue, Value: []byte("x")}); err != nil {
		t.Fatal(err)
	}
	before := creds.unseals()
	_, err := secrets.Get(context.Background(), credential.Ref{Scheme: "legato", Path: "stray"})
	if !errors.Is(err, credential.ErrWrongMasterKey) {
		t.Fatalf("err = %v, want ErrWrongMasterKey", err)
	}
	if n := creds.unseals() - before; n != 1 {
		t.Fatalf("the failed read unsealed the key %d more times, want 1", n)
	}
}
