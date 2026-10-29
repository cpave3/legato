package credential

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fakeRunner stands in for op, security and the other commands stores run.
type fakeRunner struct {
	name  string
	args  []string
	env   []string
	stdin []byte
	out   []byte
	err   error
}

func (f *fakeRunner) Run(_ context.Context, name string, args, env []string, stdin []byte) ([]byte, error) {
	f.name, f.args, f.env, f.stdin = name, args, env, stdin
	return f.out, f.err
}

func mustRef(t *testing.T, s string) Ref {
	t.Helper()
	ref, err := ParseRef(s)
	if err != nil {
		t.Fatal(err)
	}
	return ref
}

func TestEnvStoreReadsTheVariable(t *testing.T) {
	t.Setenv("ELEVEN_KEY", "sk-123")
	got, err := EnvStore{}.Get(context.Background(), mustRef(t, "env://ELEVEN_KEY"))
	if err != nil || string(got.Value) != "sk-123" {
		t.Fatalf("got %q, %v", got.Value, err)
	}
	if _, err := (EnvStore{}).Get(context.Background(), mustRef(t, "env://UNSET_KEY_FOR_TEST")); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unset variable err = %v", err)
	}
}

func TestFileStoreRefusesAFileOthersCanRead(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "key")
	if err := os.WriteFile(path, []byte("sk-file\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := FileStore{}.Get(context.Background(), mustRef(t, "file://"+path))
	if err == nil || !strings.Contains(err.Error(), path) || strings.Contains(err.Error(), "sk-file") {
		t.Fatalf("err = %v, want one that names the file and not its content", err)
	}
	if err := os.Chmod(path, 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := FileStore{}.Get(context.Background(), mustRef(t, "file://"+path))
	if err != nil || string(got.Value) != "sk-file" {
		t.Fatalf("got %q, %v (want the content without its trailing newline)", got.Value, err)
	}
}

func TestSystemdCredsStoreReadsTheUnitsCredentialsDirectory(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "github-app"), []byte("PEM"), 0o400); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CREDENTIALS_DIRECTORY", dir)
	got, err := SystemdCredsStore{}.Get(context.Background(), mustRef(t, "systemd-creds://github-app"))
	if err != nil || string(got.Value) != "PEM" {
		t.Fatalf("got %q, %v", got.Value, err)
	}
	t.Setenv("CREDENTIALS_DIRECTORY", "")
	if _, err := (SystemdCredsStore{}).Get(context.Background(), mustRef(t, "systemd-creds://github-app")); err == nil || !strings.Contains(err.Error(), "LoadCredential") {
		t.Fatalf("outside a unit err = %v, want one that says how to give Legato the credential", err)
	}
}

func TestOnePasswordStoreRunsOpReadWithTheServiceAccountToken(t *testing.T) {
	runner := &fakeRunner{out: []byte("sk-op\n")}
	store := OnePasswordStore{Token: "ops_token", Runner: runner}
	got, err := store.Get(context.Background(), mustRef(t, "op://Legato/eleven labs/credential"))
	if err != nil || string(got.Value) != "sk-op" {
		t.Fatalf("got %q, %v", got.Value, err)
	}
	if runner.name != "op" || strings.Join(runner.args, " ") != "read --no-newline op://Legato/eleven labs/credential" {
		t.Fatalf("ran %s %v", runner.name, runner.args)
	}
	if !contains(runner.env, "OP_SERVICE_ACCOUNT_TOKEN=ops_token") {
		t.Fatalf("env = %v, want the service account token", runner.env)
	}

	runner.err, runner.out = errors.New("exit status 1"), []byte("[ERROR] sk-op isn't an item")
	if _, err := store.Get(context.Background(), mustRef(t, "op://Legato/x/y")); err == nil || strings.Contains(err.Error(), "sk-op") {
		t.Fatalf("err = %v, want one that does not repeat what op printed", err)
	}
}

func TestKeychainStoreRunsSecurity(t *testing.T) {
	runner := &fakeRunner{out: []byte("sk-mac\n")}
	got, err := KeychainStore{Runner: runner}.Get(context.Background(), mustRef(t, "keychain://legato/eleven"))
	if err != nil || string(got.Value) != "sk-mac" {
		t.Fatalf("got %q, %v", got.Value, err)
	}
	if runner.name != "security" || strings.Join(runner.args, " ") != "find-generic-password -s legato -a eleven -w" {
		t.Fatalf("ran %s %v", runner.name, runner.args)
	}
}

func TestSealOpensOnlyUnderTheSameNameAndType(t *testing.T) {
	master := make([]byte, 32)
	nonce, sealed, err := Seal(master, "eleven", TypeValue, []byte("sk-123"))
	if err != nil {
		t.Fatal(err)
	}
	plain, err := Open(master, "eleven", TypeValue, nonce, sealed)
	if err != nil || string(plain) != "sk-123" {
		t.Fatalf("open = %q, %v", plain, err)
	}
	if _, err := Open(master, "other", TypeValue, nonce, sealed); err == nil {
		t.Fatal("a sealed value opened under another name")
	}
	if _, err := Open(master, "eleven", TypeKeyPair, nonce, sealed); err == nil {
		t.Fatal("a sealed value opened as another type")
	}
	wrong := make([]byte, 32)
	wrong[0] = 1
	if _, err := Open(wrong, "eleven", TypeValue, nonce, sealed); err == nil {
		t.Fatal("a sealed value opened under another key")
	}
}

type memoryRecords map[string]SealedRecord

func (m memoryRecords) GetSealed(_ context.Context, name string) (SealedRecord, error) {
	r, ok := m[name]
	if !ok {
		return SealedRecord{}, ErrNotFound
	}
	return r, nil
}
func (m memoryRecords) PutSealed(_ context.Context, r SealedRecord) error { m[r.Name] = r; return nil }
func (m memoryRecords) DeleteSealed(_ context.Context, name string) error {
	if _, ok := m[name]; !ok {
		return ErrNotFound
	}
	delete(m, name)
	return nil
}

func TestLegatoStoreKeepsValuesSealed(t *testing.T) {
	records := memoryRecords{}
	store := LegatoStore{Records: records, MasterKey: make([]byte, 32)}
	ctx := context.Background()
	ref := mustRef(t, "legato://eleven")

	if err := store.Put(ctx, ref, Secret{Type: TypeValue, Value: []byte("sk-123")}); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(records["eleven"].Ciphertext), "sk-123") {
		t.Fatal("the value is kept in the clear")
	}
	got, err := store.Get(ctx, ref)
	if err != nil || string(got.Value) != "sk-123" || got.Type != TypeValue {
		t.Fatalf("got %+v, %v", got, err)
	}
	if err := store.Delete(ctx, ref); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Get(ctx, ref); !errors.Is(err, ErrNotFound) {
		t.Fatalf("after delete err = %v", err)
	}
}

func contains(list []string, want string) bool {
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
}

func TestOnePasswordStoreReadsTheTokenFromAFileForEachRead(t *testing.T) {
	path := filepath.Join(t.TempDir(), "op-token")
	if err := os.WriteFile(path, []byte("ops_from_file\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runner := &fakeRunner{out: []byte("sk-op")}
	store := OnePasswordStore{TokenFile: path, Runner: runner}
	if _, err := store.Get(context.Background(), mustRef(t, "op://Legato/k/credential")); err != nil {
		t.Fatal(err)
	}
	if len(runner.env) != 1 || runner.env[0] != "OP_SERVICE_ACCOUNT_TOKEN=ops_from_file" {
		t.Fatalf("env = %v", runner.env)
	}
	// A new token in the file is used by the next read.
	_ = os.WriteFile(path, []byte("ops_rotated"), 0o600)
	_, _ = store.Get(context.Background(), mustRef(t, "op://Legato/k/credential"))
	if runner.env[0] != "OP_SERVICE_ACCOUNT_TOKEN=ops_rotated" {
		t.Fatalf("env = %v", runner.env)
	}
}

func TestOnePasswordStoreRefusesATokenFileOthersCanRead(t *testing.T) {
	path := filepath.Join(t.TempDir(), "op-token")
	_ = os.WriteFile(path, []byte("ops_secret_token"), 0o644)
	runner := &fakeRunner{out: []byte("sk-op")}
	_, err := OnePasswordStore{TokenFile: path, Runner: runner}.Get(context.Background(), mustRef(t, "op://Legato/k/credential"))
	if err == nil || !strings.Contains(err.Error(), "chmod 600") || strings.Contains(err.Error(), "ops_secret_token") {
		t.Fatalf("err = %v", err)
	}
	if runner.name != "" {
		t.Fatal("op ran with a token others can read")
	}
}

func TestOnePasswordStoreNeverPassesItsOwnEnvironmentsToken(t *testing.T) {
	t.Setenv("OP_SERVICE_ACCOUNT_TOKEN", "ops_inherited")
	runner := &fakeRunner{out: []byte("sk-op")}
	_, _ = OnePasswordStore{Token: "ops_configured", Runner: runner}.Get(context.Background(), mustRef(t, "op://Legato/k/credential"))
	if len(runner.env) != 1 || runner.env[0] != "OP_SERVICE_ACCOUNT_TOKEN=ops_configured" {
		t.Fatalf("env = %v", runner.env)
	}
}

// opRunner answers op commands by what they ask.
type opRunner struct {
	calls [][]string
	list  string
	field string
}

func (o *opRunner) Run(_ context.Context, name string, args, env []string, _ []byte) ([]byte, error) {
	o.calls = append(o.calls, args)
	switch {
	case len(args) > 1 && args[0] == "item" && args[1] == "list":
		return []byte(o.list), nil
	case len(args) > 1 && args[0] == "item" && args[1] == "get":
		return []byte(o.field), nil
	}
	return nil, errors.New("unexpected op command")
}

func TestOnePasswordStoreListsAVaultsItemsOfACategory(t *testing.T) {
	runner := &opRunner{list: `[
		{"id":"en2x","title":"proxmox_vms","version":2,"category":"SSH_KEY","updated_at":"2026-10-29T07:13:48Z","vault":{"id":"agif","name":"Legato"}},
		{"id":"5xv7","title":"legato_default","version":3,"category":"SSH_KEY","updated_at":"2026-10-29T06:56:31Z","vault":{"id":"agif","name":"Legato"}}
	]`}
	items, err := OnePasswordStore{Token: "ops_x", Runner: runner}.ListItems(context.Background(), "Legato", "SSH Key")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 || items[0].ID != "en2x" || items[0].Title != "proxmox_vms" || items[0].VaultID != "agif" || items[1].Version != 3 {
		t.Fatalf("items = %+v", items)
	}
	if got := strings.Join(runner.calls[0], " "); got != "item list --vault Legato --categories SSH Key --format json" {
		t.Fatalf("op %s", got)
	}
}

func TestOnePasswordStoreReadsAnItemsPublicField(t *testing.T) {
	runner := &opRunner{field: "ssh-ed25519 AAAA public\n"}
	got, err := OnePasswordStore{Runner: runner}.Field(context.Background(), "agif", "5xv7", "public key")
	if err != nil || got != "ssh-ed25519 AAAA public" {
		t.Fatalf("field = %q, %v", got, err)
	}
	if strings.Join(runner.calls[0], " ") != "item get 5xv7 --vault agif --fields label=public key" {
		t.Fatalf("op %v", runner.calls[0])
	}
}

func TestAVaultReferenceNamesOneVault(t *testing.T) {
	for ref, ok := range map[string]bool{"op://Legato": true, "op://Legato/": true, "op://Legato/item": false, "op://": false, "legato://x": false} {
		vault, err := ParseVaultRef(ref)
		if (err == nil) != ok || (ok && vault != "Legato") {
			t.Errorf("%s: %q, %v", ref, vault, err)
		}
	}
}
