package credential

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Runner runs a command with extra environment and standard input, and
// returns its standard output. Stores run op and security through one, so
// tests can stand in for them.
type Runner interface {
	Run(ctx context.Context, name string, args, env []string, stdin []byte) ([]byte, error)
}

// ExecRunner runs real commands.
type ExecRunner struct{}

func (ExecRunner) Run(ctx context.Context, name string, args, env []string, stdin []byte) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Env = append(os.Environ(), env...)
	if stdin != nil {
		cmd.Stdin = bytes.NewReader(stdin)
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		// What the command printed can hold the secret or part of it; say
		// only that it failed.
		return nil, fmt.Errorf("%s failed: %w", name, err)
	}
	return out, nil
}

// EnvStore reads env://NAME from the process environment.
type EnvStore struct{}

func (EnvStore) Get(_ context.Context, ref Ref) (Secret, error) {
	value, ok := os.LookupEnv(ref.Path)
	if !ok || value == "" {
		return Secret{}, fmt.Errorf("%w: environment variable %s is not set", ErrNotFound, ref.Path)
	}
	return Secret{Type: TypeValue, Value: []byte(value)}, nil
}

// FileStore reads file:///path. A file that other users can read is refused.
type FileStore struct{}

func (FileStore) Get(_ context.Context, ref Ref) (Secret, error) {
	info, err := os.Stat(ref.Path)
	if errors.Is(err, os.ErrNotExist) {
		return Secret{}, fmt.Errorf("%w: no file %s", ErrNotFound, ref.Path)
	}
	if err != nil {
		return Secret{}, fmt.Errorf("reading %s: %w", ref.Path, err)
	}
	if !info.Mode().IsRegular() {
		return Secret{}, fmt.Errorf("%s is not a regular file", ref.Path)
	}
	if info.Mode().Perm()&0o077 != 0 {
		return Secret{}, fmt.Errorf("%s can be read by other users (mode %o); run chmod 600 on it", ref.Path, info.Mode().Perm())
	}
	data, err := os.ReadFile(ref.Path)
	if err != nil {
		return Secret{}, fmt.Errorf("reading %s: %w", ref.Path, err)
	}
	return Secret{Type: TypeValue, Value: trimNewline(data)}, nil
}

// SystemdCredsStore reads systemd-creds://name from $CREDENTIALS_DIRECTORY,
// where systemd puts a unit's LoadCredential= and LoadCredentialEncrypted=
// credentials, decrypted.
type SystemdCredsStore struct{}

func (SystemdCredsStore) Get(_ context.Context, ref Ref) (Secret, error) {
	dir := os.Getenv("CREDENTIALS_DIRECTORY")
	if dir == "" {
		return Secret{}, fmt.Errorf("systemd-creds://%s: Legato is not running under a systemd unit with credentials; give it LoadCredentialEncrypted=%s:<file>", ref.Path, ref.Path)
	}
	data, err := os.ReadFile(filepath.Join(dir, ref.Path))
	if errors.Is(err, os.ErrNotExist) {
		return Secret{}, fmt.Errorf("%w: the unit has no credential %s", ErrNotFound, ref.Path)
	}
	if err != nil {
		return Secret{}, fmt.Errorf("reading credential %s: %w", ref.Path, err)
	}
	return Secret{Type: TypeValue, Value: trimNewline(data)}, nil
}

// OnePasswordStore reads op://vault/item/field with the op command, as a
// 1Password service account, which never asks a person to approve. The
// token is Token, or the content of TokenFile read for each read, so it
// never has to be in an environment that agents inherit.
type OnePasswordStore struct {
	Token     string
	TokenFile string
	Runner    Runner
}

func (s OnePasswordStore) token() (string, error) {
	return serviceAccountToken(s.Token, s.TokenFile)
}

// serviceAccountToken is token, or the content of file when one is given,
// read now so that a new token takes effect without a restart.
func serviceAccountToken(token, file string) (string, error) {
	if file == "" {
		return token, nil
	}
	secret, err := FileStore{}.Get(context.Background(), Ref{Scheme: "file", Path: file})
	if err != nil {
		return "", fmt.Errorf("secrets.onepassword.token_file: %w", err)
	}
	return strings.TrimSpace(string(secret.Value)), nil
}

func (s OnePasswordStore) Get(ctx context.Context, ref Ref) (Secret, error) {
	runner := s.Runner
	if runner == nil {
		runner = ExecRunner{}
	}
	token, err := s.token()
	if err != nil {
		return Secret{}, err
	}
	var env []string
	if token != "" {
		env = append(env, "OP_SERVICE_ACCOUNT_TOKEN="+token)
	}
	out, err := runner.Run(ctx, "op", []string{"read", "--no-newline", ref.String()}, env, nil)
	if err != nil {
		return Secret{}, fmt.Errorf("1Password could not read %s (is op installed, and secrets.onepassword.token or token_file a service account token that can see that vault?)", ref)
	}
	return Secret{Type: TypeValue, Value: trimNewline(out)}, nil
}

// KeychainStore reads keychain://service/account from the macOS Keychain.
type KeychainStore struct {
	Runner Runner
}

func (s KeychainStore) Get(ctx context.Context, ref Ref) (Secret, error) {
	runner := s.Runner
	if runner == nil {
		runner = ExecRunner{}
	}
	service, account, _ := strings.Cut(ref.Path, "/")
	out, err := runner.Run(ctx, "security", []string{"find-generic-password", "-s", service, "-a", account, "-w"}, nil, nil)
	if err != nil {
		return Secret{}, fmt.Errorf("%w: the Keychain has no password for service %q, account %q", ErrNotFound, service, account)
	}
	return Secret{Type: TypeValue, Value: trimNewline(out)}, nil
}

// SealedRecord is one value of Legato's own store as it is kept: sealed.
type SealedRecord struct {
	Name       string
	Type       string
	Nonce      []byte
	Ciphertext []byte
}

// SealedRecords keeps sealed records; the database implements it.
type SealedRecords interface {
	GetSealed(ctx context.Context, name string) (SealedRecord, error)
	PutSealed(ctx context.Context, record SealedRecord) error
	DeleteSealed(ctx context.Context, name string) error
}

// LegatoStore is Legato's own store, legato://name: values sealed under a
// master key, kept by SealedRecords.
type LegatoStore struct {
	Records   SealedRecords
	MasterKey []byte
}

func (s LegatoStore) Get(ctx context.Context, ref Ref) (Secret, error) {
	record, err := s.Records.GetSealed(ctx, ref.Path)
	if err != nil {
		return Secret{}, err
	}
	plain, err := Open(s.MasterKey, record.Name, record.Type, record.Nonce, record.Ciphertext)
	if err != nil {
		return Secret{}, fmt.Errorf("legato://%s cannot be opened with this master key", ref.Path)
	}
	return Secret{Type: record.Type, Value: plain}, nil
}

func (s LegatoStore) Put(ctx context.Context, ref Ref, secret Secret) error {
	if !KnownType(secret.Type) {
		return fmt.Errorf("unknown secret type %q", secret.Type)
	}
	nonce, sealed, err := Seal(s.MasterKey, ref.Path, secret.Type, secret.Value)
	if err != nil {
		return err
	}
	return s.Records.PutSealed(ctx, SealedRecord{Name: ref.Path, Type: secret.Type, Nonce: nonce, Ciphertext: sealed})
}

func (s LegatoStore) Delete(ctx context.Context, ref Ref) error {
	return s.Records.DeleteSealed(ctx, ref.Path)
}

// Seal encrypts a value with AES-256-GCM under a key derived from master for
// one name and type, which are also its additional data: a sealed value
// moved to another name, or read as another type, does not open.
func Seal(master []byte, name, secretType string, plain []byte) (nonce, sealed []byte, err error) {
	aead, err := sealer(master, name, secretType)
	if err != nil {
		return nil, nil, err
	}
	nonce = make([]byte, aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, nil, err
	}
	return nonce, aead.Seal(nil, nonce, plain, additionalData(name, secretType)), nil
}

// Open reverses Seal.
func Open(master []byte, name, secretType string, nonce, sealed []byte) ([]byte, error) {
	aead, err := sealer(master, name, secretType)
	if err != nil {
		return nil, err
	}
	if len(nonce) != aead.NonceSize() {
		return nil, errors.New("bad nonce")
	}
	return aead.Open(nil, nonce, sealed, additionalData(name, secretType))
}

func sealer(master []byte, name, secretType string) (cipher.AEAD, error) {
	if len(master) != 32 {
		return nil, errors.New("the master key must be 32 bytes")
	}
	key, err := hkdf.Key(sha256.New, master, nil, "legato-secret-v1:"+secretType+":"+name, 32)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

func additionalData(name, secretType string) []byte {
	return []byte("legato-secret-v1\x00" + secretType + "\x00" + name)
}

func trimNewline(data []byte) []byte {
	return bytes.TrimRight(data, "\r\n")
}

// OnePasswordItem is an item of a vault, as op item list gives it.
type OnePasswordItem struct {
	ID        string
	Title     string
	VaultID   string
	Version   int
	UpdatedAt string
	// Category is set by the SDK's list, as it names categories ("SshKey").
	Category string
}

// ListItems lists a vault's items of one category ("SSH Key"), names and
// identifiers only.
func (s OnePasswordStore) ListItems(ctx context.Context, vault, category string) ([]OnePasswordItem, error) {
	out, err := s.op(ctx, "item", "list", "--vault", vault, "--categories", category, "--format", "json")
	if err != nil {
		return nil, fmt.Errorf("1Password could not list vault %s (does the service account have read access to it?)", vault)
	}
	var rows []struct {
		ID        string `json:"id"`
		Title     string `json:"title"`
		Version   int    `json:"version"`
		UpdatedAt string `json:"updated_at"`
		Vault     struct {
			ID string `json:"id"`
		} `json:"vault"`
	}
	if err := json.Unmarshal(out, &rows); err != nil {
		return nil, fmt.Errorf("1Password's list of vault %s does not read: %w", vault, err)
	}
	items := make([]OnePasswordItem, 0, len(rows))
	for _, r := range rows {
		items = append(items, OnePasswordItem{ID: r.ID, Title: r.Title, VaultID: r.Vault.ID, Version: r.Version, UpdatedAt: r.UpdatedAt})
	}
	return items, nil
}

// Field reads one field of an item. Use it only for fields that are not
// secret (an SSH key's public key); secrets are read with Get.
func (s OnePasswordStore) Field(ctx context.Context, vault, item, label string) (string, error) {
	out, err := s.op(ctx, "item", "get", item, "--vault", vault, "--fields", "label="+label)
	if err != nil {
		return "", fmt.Errorf("1Password could not read the %s of item %s", label, item)
	}
	return strings.TrimSpace(string(out)), nil
}

func (s OnePasswordStore) op(ctx context.Context, args ...string) ([]byte, error) {
	runner := s.Runner
	if runner == nil {
		runner = ExecRunner{}
	}
	token, err := s.token()
	if err != nil {
		return nil, err
	}
	var env []string
	if token != "" {
		env = append(env, "OP_SERVICE_ACCOUNT_TOKEN="+token)
	}
	return runner.Run(ctx, "op", args, env, nil)
}

// ParseVaultRef reads op://<vault>, the vault a source of credentials lists.
func ParseVaultRef(s string) (string, error) {
	scheme, path, ok := strings.Cut(strings.TrimSpace(s), "://")
	vault := strings.TrimSuffix(path, "/")
	if !ok || scheme != "op" || vault == "" || strings.Contains(vault, "/") {
		return "", fmt.Errorf("vault %q: want op://<vault>, such as op://Legato", s)
	}
	return vault, nil
}
