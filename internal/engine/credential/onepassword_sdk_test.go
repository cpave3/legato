package credential

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fakeOnePassword stands in for 1Password behind the SDK.
type fakeOnePassword struct {
	secrets map[string]string
	refs    []string
	err     error
	vaults  []OnePasswordVault
	items   map[string][]OnePasswordItem  // by vault ID
	fields  map[string][]OnePasswordField // by item ID
}

func (f *fakeOnePassword) Vaults(context.Context) ([]OnePasswordVault, error) { return f.vaults, nil }

func (f *fakeOnePassword) Items(_ context.Context, vaultID string) ([]OnePasswordItem, error) {
	return f.items[vaultID], nil
}

func (f *fakeOnePassword) Fields(_ context.Context, _, itemID string) ([]OnePasswordField, error) {
	return f.fields[itemID], nil
}

func (f *fakeOnePassword) Resolve(_ context.Context, ref string) (string, error) {
	f.refs = append(f.refs, ref)
	if f.err != nil {
		return "", f.err
	}
	return f.secrets[ref], nil
}

// connectFake returns a Connect that records the tokens it was given.
func connectFake(api *fakeOnePassword, tokens *[]string) func(context.Context, string) (OnePasswordAPI, error) {
	return func(_ context.Context, token string) (OnePasswordAPI, error) {
		*tokens = append(*tokens, token)
		return api, nil
	}
}

func TestOnePasswordSDKStoreReadsAReferenceAsTheServiceAccount(t *testing.T) {
	api := &fakeOnePassword{secrets: map[string]string{"op://Legato/k/credential": "sk-op"}}
	var tokens []string
	store := &OnePasswordSDKStore{Token: "ops_token", Connect: connectFake(api, &tokens)}

	got, err := store.Get(context.Background(), mustRef(t, "op://Legato/k/credential"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got.Value) != "sk-op" || got.Type != TypeValue {
		t.Fatalf("secret = %+v", got)
	}
	if len(tokens) != 1 || tokens[0] != "ops_token" {
		t.Fatalf("connected with %v", tokens)
	}
}

func TestOnePasswordSDKStoreReusesItsClientUntilTheTokenFileChanges(t *testing.T) {
	path := filepath.Join(t.TempDir(), "op-token")
	if err := os.WriteFile(path, []byte("ops_from_file\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	api := &fakeOnePassword{secrets: map[string]string{"op://Legato/k/credential": "sk-op"}}
	var tokens []string
	store := &OnePasswordSDKStore{TokenFile: path, Connect: connectFake(api, &tokens)}
	ref := mustRef(t, "op://Legato/k/credential")

	_, _ = store.Get(context.Background(), ref)
	_, _ = store.Get(context.Background(), ref)
	if len(tokens) != 1 || tokens[0] != "ops_from_file" {
		t.Fatalf("connected with %v; want one client for an unchanged token", tokens)
	}

	_ = os.WriteFile(path, []byte("ops_rotated"), 0o600)
	if _, err := store.Get(context.Background(), ref); err != nil {
		t.Fatal(err)
	}
	if len(tokens) != 2 || tokens[1] != "ops_rotated" {
		t.Fatalf("connected with %v; want a new client for the new token", tokens)
	}
}

func TestOnePasswordSDKStoreRefusesATokenFileOthersCanRead(t *testing.T) {
	path := filepath.Join(t.TempDir(), "op-token")
	_ = os.WriteFile(path, []byte("ops_secret_token"), 0o644)
	var tokens []string
	store := &OnePasswordSDKStore{TokenFile: path, Connect: connectFake(&fakeOnePassword{}, &tokens)}

	_, err := store.Get(context.Background(), mustRef(t, "op://Legato/k/credential"))
	if err == nil || !strings.Contains(err.Error(), "chmod 600") || strings.Contains(err.Error(), "ops_secret_token") {
		t.Fatalf("err = %v", err)
	}
	if len(tokens) != 0 {
		t.Fatal("connected with a token others can read")
	}
}

func TestOnePasswordSDKStoreSaysWhyAReadFailedWithoutTheToken(t *testing.T) {
	api := &fakeOnePassword{err: errors.New("no item matched the secret reference (token ops_token)")}
	var tokens []string
	store := &OnePasswordSDKStore{Token: "ops_token", Connect: connectFake(api, &tokens)}

	_, err := store.Get(context.Background(), mustRef(t, "op://Legato/k/credential"))
	if err == nil || !strings.Contains(err.Error(), "op://Legato/k/credential") || !strings.Contains(err.Error(), "no item matched") {
		t.Fatalf("err = %v; want the reference and 1Password's reason", err)
	}
	if strings.Contains(err.Error(), "ops_token") {
		t.Fatalf("err = %v; it names the token", err)
	}
}

func TestOnePasswordSDKStoreSaysWhyItCouldNotConnectWithoutTheToken(t *testing.T) {
	store := &OnePasswordSDKStore{Token: "ops_bad", Connect: func(context.Context, string) (OnePasswordAPI, error) {
		return nil, errors.New("invalid service account token ops_bad")
	}}

	_, err := store.Get(context.Background(), mustRef(t, "op://Legato/k/credential"))
	if err == nil || !strings.Contains(err.Error(), "secrets.onepassword") || !strings.Contains(err.Error(), "invalid service account token") {
		t.Fatalf("err = %v; want the setting and 1Password's reason", err)
	}
	if strings.Contains(err.Error(), "ops_bad") {
		t.Fatalf("err = %v; it names the token", err)
	}
}

func TestOnePasswordSDKStoreListsAVaultsItemsOfACategoryByVaultName(t *testing.T) {
	api := &fakeOnePassword{
		vaults: []OnePasswordVault{{ID: "pers", Title: "Personal"}, {ID: "agif", Title: "Legato"}},
		items: map[string][]OnePasswordItem{"agif": {
			{ID: "en2x", Title: "proxmox_vms", VaultID: "agif", Category: "SshKey", UpdatedAt: "2026-10-29T07:13:48Z"},
			{ID: "lg01", Title: "a login", VaultID: "agif", Category: "Login"},
			{ID: "5xv7", Title: "legato_default", VaultID: "agif", Category: "SshKey"},
		}},
	}
	var tokens []string
	store := &OnePasswordSDKStore{Token: "ops_x", Connect: connectFake(api, &tokens)}

	// The category is named as op names it, the vault by its name.
	items, err := store.ListItems(context.Background(), "Legato", "SSH Key")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 || items[0].ID != "en2x" || items[0].VaultID != "agif" || items[0].UpdatedAt != "2026-10-29T07:13:48Z" || items[1].ID != "5xv7" {
		t.Fatalf("items = %+v", items)
	}
}

func TestOnePasswordSDKStoreSaysWhenItCannotSeeAVault(t *testing.T) {
	api := &fakeOnePassword{vaults: []OnePasswordVault{{ID: "agif", Title: "Legato"}}}
	var tokens []string
	store := &OnePasswordSDKStore{Token: "ops_x", Connect: connectFake(api, &tokens)}

	_, err := store.ListItems(context.Background(), "Private", "SSH Key")
	if err == nil || !strings.Contains(err.Error(), "Private") || !strings.Contains(err.Error(), "service account") {
		t.Fatalf("err = %v", err)
	}
}

func TestOnePasswordSDKStoreReadsAnSSHKeysPublicKey(t *testing.T) {
	api := &fakeOnePassword{fields: map[string][]OnePasswordField{"5xv7": {
		{Title: "notes", Value: "not this"},
		{Title: "private key", PublicKey: "ssh-ed25519 AAAA public"},
	}}}
	var tokens []string
	store := &OnePasswordSDKStore{Token: "ops_x", Connect: connectFake(api, &tokens)}

	got, err := store.Field(context.Background(), "agif", "5xv7", "public key")
	if err != nil || got != "ssh-ed25519 AAAA public" {
		t.Fatalf("field = %q, %v", got, err)
	}
}

func TestOnePasswordSDKStoreNeedsAServiceAccountToken(t *testing.T) {
	var tokens []string
	store := &OnePasswordSDKStore{Connect: connectFake(&fakeOnePassword{}, &tokens)}

	_, err := store.Get(context.Background(), mustRef(t, "op://Legato/k/credential"))
	if err == nil || !strings.Contains(err.Error(), "secrets.onepassword.token_file") {
		t.Fatalf("err = %v; want the setting to give", err)
	}
	if len(tokens) != 0 {
		t.Fatal("connected without a token")
	}
}

func TestOnePasswordSDKStoreTrimsTheNewlineAfterAValue(t *testing.T) {
	// 1Password ends an SSH key in OpenSSH format with a newline; op read
	// --no-newline does not, and both stores give the same bytes.
	api := &fakeOnePassword{secrets: map[string]string{"op://Legato/k/private key?ssh-format=openssh": "-----BEGIN OPENSSH PRIVATE KEY-----\nAAAA\n-----END OPENSSH PRIVATE KEY-----\n"}}
	var tokens []string
	store := &OnePasswordSDKStore{Token: "ops_x", Connect: connectFake(api, &tokens)}

	got, err := store.Get(context.Background(), mustRef(t, "op://Legato/k/private key?ssh-format=openssh"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.HasSuffix(string(got.Value), "\n") || !strings.HasSuffix(string(got.Value), "-----END OPENSSH PRIVATE KEY-----") {
		t.Fatalf("value = %q", got.Value)
	}
}
