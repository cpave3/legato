package main

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"errors"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/cpave3/legato/config"
	"github.com/cpave3/legato/internal/engine/store"
	"github.com/cpave3/legato/internal/service"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
)

func channelsTestBroker(t *testing.T, db *store.Store, credentials ...config.CredentialConfig) *service.CredentialBroker {
	t.Helper()
	broker, err := service.NewCredentialBroker(db, service.NewApprovalService(db, service.ApprovalServiceOptions{}), service.CredentialBrokerOptions{Credentials: credentials})
	if err != nil {
		t.Fatal(err)
	}
	return broker
}

func channelsTestDB(t *testing.T) *store.Store {
	t.Helper()
	db, err := store.New(filepath.Join(t.TempDir(), "legato.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func sshTestKey(t *testing.T) string {
	t.Helper()
	_, priv, _ := ed25519.GenerateKey(rand.Reader)
	block, err := ssh.MarshalPrivateKey(priv, "")
	if err != nil {
		t.Fatal(err)
	}
	return string(pem.EncodeToMemory(block))
}

func shortDir(t *testing.T) string {
	t.Helper()
	// Unix socket paths are short; t.TempDir() can be long.
	dir, err := os.MkdirTemp("/tmp", "lgch")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	return dir
}

func TestOnlyOneProcessHostsTheChannels(t *testing.T) {
	dir := shortDir(t)
	db := channelsTestDB(t)
	t.Setenv("CH_TEST_KEY", "sk-test")
	creds := []config.CredentialConfig{{Name: "eleven", Secret: "env://CH_TEST_KEY", Profile: "elevenlabs"}}
	cfg := &config.Config{Credentials: creds, CredentialChannels: config.CredentialChannelsConfig{HTTPProxy: config.HTTPProxyChannelConfig{Listen: "127.0.0.1:0"}}}

	first := startChannelHost(context.Background(), cfg, db, channelsTestBroker(t, db, creds...), nil, dir)
	defer first.Stop()
	second := startChannelHost(context.Background(), cfg, db, channelsTestBroker(t, db, creds...), nil, dir)
	defer second.Stop()

	if !first.Owner() || second.Owner() {
		t.Fatalf("owners: first %v second %v", first.Owner(), second.Owner())
	}
	url := readProxyURL(dir)
	if url == "" || url != first.ProxyURL() {
		t.Fatalf("proxy url %q, first serves %q", url, first.ProxyURL())
	}
	// Any process gives its sessions the owner's address.
	env, err := second.Channels().SessionEnv(context.Background(), "t1", "ex-1")
	if err != nil || env["ELEVENLABS_BASE_URL"] != url+"/c/eleven" {
		t.Fatalf("env = %v, %v", env, err)
	}
	resp, err := http.Get(url + "/c/eleven/v1/voices")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 401 {
		t.Fatalf("status %d; want 401 for a request with no key", resp.StatusCode)
	}

	first.Stop()
	if readProxyURL(dir) != "" {
		t.Fatal("the address file outlived its host")
	}
}

func TestTheHostOpensASocketForEachRunningExecution(t *testing.T) {
	dir := shortDir(t)
	db := channelsTestDB(t)
	t.Setenv("CH_SSH_KEY", sshTestKey(t))
	creds := []config.CredentialConfig{{Name: "homelab", Secret: "env://CH_SSH_KEY", Profile: "ssh", Type: "keypair"}}
	cfg := &config.Config{Credentials: creds}
	ctx := context.Background()
	if err := db.CreateExecution(ctx, store.Execution{ID: "ex-run", Kind: store.ExecutionInteractive, Status: store.ExecutionRunning, TaskID: "t1", ExecutionTaskID: "t1", StartedAt: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}

	host := startChannelHost(ctx, cfg, db, channelsTestBroker(t, db, creds...), nil, dir)
	defer host.Stop()
	host.Reconcile(ctx)
	path := host.Channels().SSHSocketPath("ex-run")
	conn, err := net.Dial("unix", path)
	if err != nil {
		t.Fatalf("no socket for a running execution: %v", err)
	}
	keys, err := agent.NewClient(conn).List()
	conn.Close()
	if err != nil || len(keys) != 1 || keys[0].Comment != "homelab" {
		t.Fatalf("keys = %v, %v", keys, err)
	}

	_ = db.TerminalizeExecution(ctx, "ex-run", store.ExecutionSucceeded)
	host.Reconcile(ctx)
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("socket of an ended execution still there: %v", err)
	}
}

func TestASessionSpawnedByTheHostHasItsSocketAtOnce(t *testing.T) {
	dir := shortDir(t)
	db := channelsTestDB(t)
	t.Setenv("CH_SSH_KEY", sshTestKey(t))
	creds := []config.CredentialConfig{{Name: "homelab", Secret: "env://CH_SSH_KEY", Profile: "ssh", Type: "keypair"}}
	ctx := context.Background()
	_ = db.CreateExecution(ctx, store.Execution{ID: "ex-new", Kind: store.ExecutionInteractive, Status: store.ExecutionRunning, TaskID: "t1", ExecutionTaskID: "t1", StartedAt: time.Now().UTC()})
	host := startChannelHost(ctx, &config.Config{Credentials: creds}, db, channelsTestBroker(t, db, creds...), nil, dir)
	defer host.Stop()
	env, err := host.Channels().SessionEnv(ctx, "t1", "ex-new")
	if err != nil {
		t.Fatal(err)
	}
	conn, err := net.Dial("unix", env["SSH_AUTH_SOCK"])
	if err != nil {
		t.Fatalf("socket not open when the session starts: %v", err)
	}
	conn.Close()
}

func TestNoChannelCredentialsMeansNoHost(t *testing.T) {
	dir := shortDir(t)
	db := channelsTestDB(t)
	host := startChannelHost(context.Background(), &config.Config{}, db, channelsTestBroker(t, db), nil, dir)
	defer host.Stop()
	if host.Owner() || readProxyURL(dir) != "" {
		t.Fatal("a host started with nothing to serve")
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Fatalf("files made: %v", entries)
	}
}

func TestTheHostAsksForEndedSessionsWhileItServes(t *testing.T) {
	dir := shortDir(t)
	db := channelsTestDB(t)
	t.Setenv("CH_SSH_KEY", sshTestKey(t))
	creds := []config.CredentialConfig{{Name: "homelab", Secret: "env://CH_SSH_KEY", Profile: "ssh", Type: "keypair"}}
	ctx := context.Background()
	_ = db.CreateExecution(ctx, store.Execution{ID: "ex-gone", Kind: store.ExecutionInteractive, Status: store.ExecutionRunning, TaskID: "t1", ExecutionTaskID: "t1", StartedAt: time.Now().UTC()})
	host := startChannelHost(ctx, &config.Config{Credentials: creds}, db, channelsTestBroker(t, db, creds...), nil, dir)
	defer host.Stop()
	host.Reconcile(ctx)
	if _, err := os.Stat(host.Channels().SSHSocketPath("ex-gone")); err != nil {
		t.Fatalf("no socket while the session runs: %v", err)
	}
	// The agent service finds the session gone and ends its execution.
	host.mu.Lock()
	host.lastSessionCheck = time.Time{}
	host.reconcileSessions = func(ctx context.Context) error {
		return db.TerminalizeExecution(ctx, "ex-gone", store.ExecutionLost)
	}
	host.mu.Unlock()
	host.Reconcile(ctx)
	if _, err := os.Stat(host.Channels().SSHSocketPath("ex-gone")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("the socket of a lost session is still there: %v", err)
	}
}

type envSetter struct {
	service.AgentService
	provider service.SessionEnvProvider
}

func (e *envSetter) SetSessionEnv(p service.SessionEnvProvider) { e.provider = p }

func TestSessionsAreGivenTheChannelsEvenWithNoChannelCredentials(t *testing.T) {
	db := channelsTestDB(t)
	agents := &envSetter{}
	giveSessionsChannels(agents, service.NewCredentialChannels(channelsTestBroker(t, db), service.CredentialChannelsOptions{}))
	if agents.provider == nil {
		t.Fatal("no session environment: OP_SERVICE_ACCOUNT_TOKEN would reach agents")
	}
}

func TestAHostProcessDropsTheTokenFromItsOwnEnvironment(t *testing.T) {
	t.Setenv("OP_SERVICE_ACCOUNT_TOKEN", "ops_inherited")
	t.Setenv("LEGATO_SECRETS_PASSPHRASE", "words")
	dropSecretEnv()
	for _, name := range []string{"OP_SERVICE_ACCOUNT_TOKEN", "LEGATO_SECRETS_PASSPHRASE"} {
		if _, ok := os.LookupEnv(name); ok {
			t.Errorf("%s still set: a tmux server Legato starts would inherit it", name)
		}
	}
}
