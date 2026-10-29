package service

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cpave3/legato/config"
	"github.com/cpave3/legato/internal/engine/credential"
	"github.com/cpave3/legato/internal/engine/store"
	"golang.org/x/crypto/ssh"
)

// fakeVault answers op as a 1Password vault of SSH keys would.
type fakeVault struct {
	mu      sync.Mutex
	items   []fakeItem
	listErr error
	reads   []string // op read references: private keys
}

type fakeItem struct {
	id, title, private, public string
	version                    int
}

func (v *fakeVault) add(t *testing.T, id, title string) fakeItem {
	t.Helper()
	private, public := sshKeyPEM(t)
	item := fakeItem{id: id, title: title, private: private, public: strings.TrimSpace(string(ssh.MarshalAuthorizedKey(public))), version: 1}
	v.mu.Lock()
	v.items = append(v.items, item)
	v.mu.Unlock()
	return item
}

func (v *fakeVault) remove(id string) {
	v.mu.Lock()
	defer v.mu.Unlock()
	for i, item := range v.items {
		if item.id == id {
			v.items = append(v.items[:i], v.items[i+1:]...)
			return
		}
	}
}

func (v *fakeVault) Run(_ context.Context, name string, args, env []string, _ []byte) ([]byte, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	switch {
	case args[0] == "item" && args[1] == "list":
		if v.listErr != nil {
			return nil, v.listErr
		}
		var rows []map[string]any
		for _, item := range v.items {
			rows = append(rows, map[string]any{"id": item.id, "title": item.title, "version": item.version, "category": "SSH_KEY", "vault": map[string]string{"id": "vault-id", "name": "Legato"}})
		}
		return json.Marshal(rows)
	case args[0] == "item" && args[1] == "get":
		for _, item := range v.items {
			if item.id == args[2] {
				return []byte(item.public + "\n"), nil
			}
		}
	case args[0] == "read":
		ref := args[len(args)-1]
		v.reads = append(v.reads, ref)
		for _, item := range v.items {
			if strings.Contains(ref, "/"+item.id+"/") {
				return []byte(item.private), nil
			}
		}
	}
	return nil, errors.New("not found")
}

func (v *fakeVault) privateReads() int {
	v.mu.Lock()
	defer v.mu.Unlock()
	return len(v.reads)
}

func sourcesFixture(t *testing.T, vault *fakeVault, credentials ...config.CredentialConfig) (brokerFixture, *CredentialChannels, *time.Time) {
	t.Helper()
	approvals, db, _ := newApprovalFixture(t)
	approvals.opts.PollInterval = 5 * time.Millisecond
	ctx := context.Background()
	createTask(t, db, "task-1")
	if err := db.CreateExecution(ctx, store.Execution{ID: "ex-1", Kind: store.ExecutionInteractive, Status: store.ExecutionRunning, TaskID: "task-1", ExecutionTaskID: "task-1", StartedAt: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	window := config.CredentialRule{Operation: config.StringList{"ssh.login"}, Approve: "window", Window: "30m", WindowDuration: 30 * time.Minute}
	broker, err := NewCredentialBroker(db, approvals, CredentialBrokerOptions{
		Credentials: credentials,
		Sources:     []config.CredentialSourceConfig{{Vault: "op://Legato", Profile: "ssh", Policy: []config.CredentialRule{window}}},
		Stores:      map[string]credential.Store{"op": credential.OnePasswordStore{Runner: vault}},
		Messenger:   &recordingMessenger{},
		WaitTimeout: 100 * time.Millisecond,
		Now:         func() time.Time { return now },
	})
	if err != nil {
		t.Fatal(err)
	}
	broker.RegisterResolver()
	channels := NewCredentialChannels(broker, CredentialChannelsOptions{SSHSocketDir: t.TempDir()})
	return brokerFixture{broker: broker, approvals: approvals, store: db}, channels, &now
}

func keyNames(t *testing.T, channels *CredentialChannels) []string {
	t.Helper()
	keys, err := channels.Keys(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, k := range keys {
		names = append(names, k.Credential)
	}
	return names
}

func TestEverySSHKeyOfAVaultIsACredential(t *testing.T) {
	vault := &fakeVault{}
	vault.add(t, "id-1", "legato_default")
	vault.add(t, "id-2", "proxmox_vms")
	f, channels, _ := sourcesFixture(t, vault)
	ctx := context.Background()

	if !channels.HasSSH() {
		t.Fatal("a vault of SSH keys gives sessions no socket")
	}
	if got := strings.Join(keyNames(t, channels), ","); got != "legato_default,proxmox_vms" {
		t.Fatalf("keys = %s", got)
	}
	if vault.privateReads() != 0 {
		t.Fatal("listing the keys read a private key")
	}
	summaries := f.broker.Credentials()
	if len(summaries) != 2 || summaries[0].Source != "op://Legato" || summaries[0].Profile != "ssh" || summaries[0].Rules[0].Approve != "window" {
		t.Fatalf("credentials = %+v", summaries)
	}

	// A login with one of them asks with the source's rules, and signs.
	d, _ := f.broker.Authorize(ctx, CredentialUse{TaskID: "task-1", ExecutionID: "ex-1", Credential: "proxmox_vms", Operation: "ssh.login", Resource: "ssh://cameron@pve.lan"})
	approval, _ := f.approvals.Get(ctx, d.ApprovalID)
	if approval == nil || approval.Options[0].Label != "For 30 minutes" {
		t.Fatalf("approval = %+v", approval)
	}
	signer, err := channels.Signer(ctx, "proxmox_vms")
	if err != nil || signer.PublicKey().Type() != "ssh-ed25519" || vault.privateReads() != 1 {
		t.Fatalf("signer = %v, %v, reads %d", signer, err, vault.privateReads())
	}
}

func TestAKeyAddedToTheVaultIsUsedWithoutARestart(t *testing.T) {
	vault := &fakeVault{}
	vault.add(t, "id-1", "legato_default")
	_, channels, now := sourcesFixture(t, vault)
	if got := strings.Join(keyNames(t, channels), ","); got != "legato_default" {
		t.Fatalf("keys = %s", got)
	}
	vault.add(t, "id-2", "new_key")
	vault.remove("id-1")
	if got := strings.Join(keyNames(t, channels), ","); got != "legato_default" {
		t.Fatalf("keys within the cache = %s", got)
	}
	*now = now.Add(sourceStaleAfter + time.Second)
	if got := strings.Join(keyNames(t, channels), ","); got != "new_key" {
		t.Fatalf("keys after the vault changed = %s", got)
	}
}

func TestAVaultThatCannotBeListedKeepsItsLastKeys(t *testing.T) {
	vault := &fakeVault{}
	vault.add(t, "id-1", "legato_default")
	_, channels, now := sourcesFixture(t, vault)
	keyNames(t, channels)
	vault.listErr = errors.New("network down")
	*now = now.Add(sourceStaleAfter + time.Second)
	if got := strings.Join(keyNames(t, channels), ","); got != "legato_default" {
		t.Fatalf("keys = %s", got)
	}
}

func TestAConfiguredCredentialWinsOverAVaultItemOfItsName(t *testing.T) {
	vault := &fakeVault{}
	item := vault.add(t, "id-1", "legato_default")
	t.Setenv("CONFIGURED_KEY", item.private)
	own := config.CredentialConfig{Name: "legato_default", Secret: "env://CONFIGURED_KEY", Profile: "ssh", Type: "keypair",
		Policy: []config.CredentialRule{{Operation: config.StringList{"ssh.login"}, Approve: "auto"}}}
	f, channels, _ := sourcesFixture(t, vault, own)
	keyNames(t, channels)
	summaries := f.broker.Credentials()
	if len(summaries) != 1 || summaries[0].Source != "" || summaries[0].Rules[0].Approve != "auto" {
		t.Fatalf("credentials = %+v", summaries)
	}
}

func TestAnItemsTitleIsMadeACredentialName(t *testing.T) {
	vault := &fakeVault{}
	vault.add(t, "id-1", "My Home Server!")
	vault.add(t, "id-2", "  ")
	_, channels, _ := sourcesFixture(t, vault)
	if got := strings.Join(keyNames(t, channels), ","); got != "My-Home-Server,id-2" {
		t.Fatalf("keys = %s", got)
	}
}

func TestTheGuidanceNamesTheVault(t *testing.T) {
	vault := &fakeVault{}
	f, _, _ := sourcesFixture(t, vault)
	sources := f.broker.Sources()
	if len(sources) != 1 || sources[0].Vault != "op://Legato" || sources[0].Profile != "ssh" {
		t.Fatalf("sources = %+v", sources)
	}
}
