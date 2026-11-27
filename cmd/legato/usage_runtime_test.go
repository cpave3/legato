package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cpave3/legato/config"
	"github.com/cpave3/legato/internal/engine/credential"
	"github.com/cpave3/legato/internal/engine/events"
	"github.com/cpave3/legato/internal/engine/store"
	"github.com/cpave3/legato/internal/service"
)

// Only one process of a data folder polls the subscriptions' usage.
func TestOneProcessHoldsTheUsagePoller(t *testing.T) {
	dir := t.TempDir()
	release, err := claimUsagePoller(dir)
	if err != nil {
		t.Fatal(err)
	}
	if again, err := claimUsagePoller(dir); err == nil {
		again()
		t.Fatal("a second claim succeeded while the first holds it")
	}
	release()
	again, err := claimUsagePoller(dir)
	if err != nil {
		t.Fatalf("after release: %v", err)
	}
	again()
}

func TestUsageReadersFollowTheSettings(t *testing.T) {
	off := false
	cfg := config.Defaults()
	if got := usageReaders(cfg, nil); len(got) != 2 || got[0].Provider() != "claude" || got[1].Provider() != "codex" {
		t.Fatalf("readers = %v", got)
	}
	cfg.Usage.Codex.Enabled = &off
	if got := usageReaders(cfg, nil); len(got) != 1 || got[0].Provider() != "claude" {
		t.Fatalf("codex off: readers = %v", got)
	}
	cfg.Usage.Claude.Enabled = &off
	if subscriptionUsageFor(cfg, nil, nil) == nil {
		t.Fatal("policy must remain visible with both providers off")
	}
	// OpenCode Go is off until it is turned on, and never a reader before.
	cfg.Usage.OpenCode.APIKey = "opencode-key-SENTINEL"
	if got := usageReaders(cfg, nil); len(got) != 0 {
		t.Fatalf("opencode off with a key: readers = %v", got)
	}
}

// OpenCode Go's reader is wired with the key the settings resolved at start;
// a config.yaml pin that still holds the secret's name resolves through the
// sealed store, and a key that cannot be read leaves the reader to say so.
func TestOpenCodeUsageReaderIsWiredWithTheResolvedKey(t *testing.T) {
	dir := t.TempDir()
	db, err := store.New(filepath.Join(dir, "legato.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	cfg := config.Defaults()
	cfg.DataDir = dir
	off := false
	cfg.Usage.Claude.Enabled, cfg.Usage.Codex.Enabled = &off, &off
	if err := configuredSecretStore(cfg, db).Add(context.Background(), "opencode-go", credential.TypeValue, []byte("opencode-key-SENTINEL")); err != nil {
		t.Fatal(err)
	}

	on := true
	cfg.Usage.OpenCode.Enabled = &on
	cfg.Usage.OpenCode.APIKey = "legato://opencode-go" // as a config.yaml pin holds it
	readers := usageReaders(cfg, db)
	if len(readers) != 1 {
		t.Fatalf("readers = %v", readers)
	}
	reader, ok := readers[0].(*service.OpenCodeUsageReader)
	if !ok {
		t.Fatalf("reader = %T", readers[0])
	}
	// The base URL is the engine's documented default; the wiring leaves it
	// to the client to apply.
	if reader.Provider() != service.UsageProviderOpenCode || reader.Client.APIKey != "opencode-key-SENTINEL" || reader.Client.BaseURL != "" {
		t.Fatalf("reader = %+v", reader.Client)
	}

	// A key the settings already resolved (a database setting) is used as is.
	cfg.Usage.OpenCode.APIKey = "opencode-key-PLAIN"
	if readers = usageReaders(cfg, db); len(readers) != 1 {
		t.Fatalf("plain key: readers = %v", readers)
	}
	if reader, ok = readers[0].(*service.OpenCodeUsageReader); !ok || reader.Client.APIKey != "opencode-key-PLAIN" {
		t.Fatalf("plain key: reader = %+v", readers[0])
	}

	// No key named: the reader is there and says so when it reads, so the
	// operator sees setup is unfinished.
	cfg.Usage.OpenCode.APIKey = ""
	if readers = usageReaders(cfg, db); len(readers) != 1 {
		t.Fatalf("no key: readers = %v", readers)
	}
	if reader, ok = readers[0].(*service.OpenCodeUsageReader); !ok || reader.Client.APIKey != "" {
		t.Fatalf("no key: reader = %+v", readers[0])
	}
	_, err = reader.Read(context.Background())
	var unavailable *service.UsageUnavailableError
	if !errors.As(err, &unavailable) || unavailable.Reason != service.UsageReasonMissingKey {
		t.Fatalf("no key: err = %v, want %s", err, service.UsageReasonMissingKey)
	}

	// A secret that cannot be read is the same as none: the reason says to
	// name a key, and the value never reaches an error.
	cfg.Usage.OpenCode.APIKey = "legato://never-stored"
	if readers = usageReaders(cfg, db); len(readers) != 1 {
		t.Fatalf("unreadable secret: readers = %v", readers)
	}
	if reader, ok = readers[0].(*service.OpenCodeUsageReader); !ok || reader.Client.APIKey != "" {
		t.Fatalf("unreadable secret: reader = %+v", readers[0])
	}
}

type countingUsageReader struct{ reads atomic.Int32 }

func (r *countingUsageReader) Provider() string { return service.UsageProviderClaude }
func (r *countingUsageReader) Read(context.Context) (service.ProviderUsage, error) {
	r.reads.Add(1)
	return service.ProviderUsage{Windows: []service.UsageWindow{{Meter: service.UsageMeterWeekly, UsedPercent: 1}}}, nil
}

// A process that started while another polled takes the poller over once
// that one lets go of usage.lock.
func TestASurvivingProcessTakesOverThePoller(t *testing.T) {
	old := usageClaimRetry
	usageClaimRetry = 20 * time.Millisecond
	t.Cleanup(func() { usageClaimRetry = old })

	dir := t.TempDir()
	db, err := store.New(filepath.Join(dir, "legato.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	cfg := config.Defaults()
	cfg.DataDir = dir
	reader := &countingUsageReader{}
	svc := service.NewSubscriptionUsageService(service.SubscriptionUsageOptions{Store: db, Readers: []service.SubscriptionUsageReader{reader}})

	release, err := claimUsagePoller(dir)
	if err != nil {
		t.Fatal(err)
	}
	stop := startSubscriptionUsage(cfg, svc, events.New())
	time.Sleep(150 * time.Millisecond)
	if n := reader.reads.Load(); n != 0 {
		t.Fatalf("reads = %d while another process polls", n)
	}
	release()
	deadline := time.Now().Add(3 * time.Second)
	for reader.reads.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if reader.reads.Load() == 0 {
		t.Fatal("the poller was not taken over after the lock was released")
	}
	stopped := make(chan struct{})
	go func() { stop(); close(stopped) }()
	select {
	case <-stopped:
	case <-time.After(3 * time.Second):
		t.Fatal("stopping hung")
	}
	if again, err := claimUsagePoller(dir); err != nil {
		t.Fatalf("the lock was kept after stopping: %v", err)
	} else {
		again()
	}
}

func TestUsageRuntimeReadsEachNamedClaudeDirectory(t *testing.T) {
	if runtime.GOOS == "darwin" {
		t.Skip("default HostCredentials accesses the real Keychain; covered with Linux file fixtures only")
	}
	cfg := config.Defaults()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	named := filepath.Join(t.TempDir(), "work")
	cfg.ClaudeAccounts = []config.ClaudeAccount{{Name: "work", Label: "Work", ConfigDir: named}}
	for dir, token := range map[string]string{filepath.Join(home, ".claude"): "fixture-default", named: "fixture-work"} {
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, ".credentials.json"), []byte(fmt.Sprintf(`{"claudeAiOauth":{"accessToken":%q,"expiresAt":%d}}`, token, time.Now().Add(time.Hour).UnixMilli())), 0600); err != nil {
			t.Fatal(err)
		}
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		percent := 10
		if r.Header.Get("Authorization") == "Bearer fixture-work" {
			percent = 80
		}
		fmt.Fprintf(w, `{"seven_day":{"utilization":%d,"resets_at":null}}`, percent)
	}))
	defer srv.Close()
	readers := usageReaders(cfg, nil)
	if len(readers) != 3 {
		t.Fatalf("got %d readers, want two Claude accounts and Codex", len(readers))
	}
	for i, want := range []float64{10, 80} {
		reader := readers[i].(*service.ClaudeUsageReader)
		reader.Client.BaseURL = srv.URL
		got, err := reader.Read(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if len(got.Windows) != 1 || got.Windows[0].UsedPercent != want {
			t.Fatalf("account %d used another login", i)
		}
	}
}
