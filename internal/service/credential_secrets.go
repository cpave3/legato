package service

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/cpave3/legato/internal/engine/credential"
	"github.com/cpave3/legato/internal/engine/store"
)

// ErrSecretInvalid: a secret's name, type or value cannot be stored.
var ErrSecretInvalid = errors.New("invalid secret")

// SecretInfo describes a stored secret, never its value.
type SecretInfo struct {
	Name      string `json:"name"`
	Type      string `json:"type"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

// SecretStoreService is Legato's own secret store, legato://name. It reads
// the master key the first time a value is sealed or opened, so a Legato
// that stores nothing never asks the keyring for one.
type SecretStoreService struct {
	db   *store.Store
	opts credential.MasterKeyOptions

	mu     sync.Mutex
	key    []byte
	source string
}

func NewSecretStoreService(db *store.Store, opts credential.MasterKeyOptions) *SecretStoreService {
	return &SecretStoreService{db: db, opts: opts}
}

func (s *SecretStoreService) legato(ctx context.Context) (credential.LegatoStore, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.key == nil {
		key, source, err := credential.LoadMasterKey(ctx, s.opts)
		if err != nil {
			return credential.LegatoStore{}, err
		}
		s.key, s.source = key, source
	}
	return credential.LegatoStore{Records: sealedRecords{s.db}, MasterKey: s.key}, nil
}

// Source names where the master key lives, making it on first use.
func (s *SecretStoreService) Source(ctx context.Context) (string, error) {
	if _, err := s.legato(ctx); err != nil {
		return "", err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.source, nil
}

// Add stores a value, replacing one of the same name.
func (s *SecretStoreService) Add(ctx context.Context, name, secretType string, value []byte) error {
	if !credential.ValidSecretName(name) {
		return fmt.Errorf("%w: %q: use letters, digits, dots, dashes and underscores", ErrSecretInvalid, name)
	}
	if !credential.KnownType(secretType) {
		return fmt.Errorf("%w: unknown type %q (value, userpass, keypair, oauth-client, oauth-refresh)", ErrSecretInvalid, secretType)
	}
	value = bytes.TrimRight(value, "\r\n")
	if len(value) == 0 {
		return fmt.Errorf("%w: the value is empty", ErrSecretInvalid)
	}
	legato, err := s.legato(ctx)
	if err != nil {
		return err
	}
	return legato.Put(ctx, credential.Ref{Scheme: "legato", Path: name}, credential.Secret{Type: secretType, Value: value})
}

// Remove deletes a stored value.
func (s *SecretStoreService) Remove(ctx context.Context, name string) error {
	err := s.db.DeleteSecret(ctx, name)
	if errors.Is(err, store.ErrNotFound) {
		return fmt.Errorf("%w: no secret %q", credential.ErrNotFound, name)
	}
	return err
}

// List names the stored secrets.
func (s *SecretStoreService) List(ctx context.Context) ([]SecretInfo, error) {
	rows, err := s.db.ListSecrets(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]SecretInfo, len(rows))
	for i, row := range rows {
		out[i] = SecretInfo{Name: row.Name, Type: row.Type, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt}
	}
	return out, nil
}

// Get reads legato://name, as a store of the broker.
func (s *SecretStoreService) Get(ctx context.Context, ref credential.Ref) (credential.Secret, error) {
	legato, err := s.legato(ctx)
	if err != nil {
		return credential.Secret{}, err
	}
	return legato.Get(ctx, ref)
}

// sealedRecords keeps the store's sealed values in the database.
type sealedRecords struct{ db *store.Store }

func (r sealedRecords) GetSealed(ctx context.Context, name string) (credential.SealedRecord, error) {
	row, err := r.db.GetSecret(ctx, name)
	if errors.Is(err, store.ErrNotFound) {
		return credential.SealedRecord{}, fmt.Errorf("%w: no secret legato://%s (legato secret add %s)", credential.ErrNotFound, name, name)
	}
	if err != nil {
		return credential.SealedRecord{}, err
	}
	return credential.SealedRecord{Name: row.Name, Type: row.Type, Nonce: row.Nonce, Ciphertext: row.Ciphertext}, nil
}

func (r sealedRecords) PutSealed(ctx context.Context, rec credential.SealedRecord) error {
	return r.db.PutSecret(ctx, store.SecretRow{Name: rec.Name, Type: rec.Type, Nonce: rec.Nonce, Ciphertext: rec.Ciphertext})
}

func (r sealedRecords) DeleteSealed(ctx context.Context, name string) error {
	err := r.db.DeleteSecret(ctx, name)
	if errors.Is(err, store.ErrNotFound) {
		return credential.ErrNotFound
	}
	return err
}
