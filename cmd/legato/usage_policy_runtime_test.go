package main

import (
	"context"
	"encoding/json"
	"github.com/cpave3/legato/internal/engine/events"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cpave3/legato/config"
	"github.com/cpave3/legato/internal/engine/hooks"
	"github.com/cpave3/legato/internal/engine/ipc"
	"github.com/cpave3/legato/internal/engine/store"
	"github.com/cpave3/legato/internal/service"
)

func TestUsagePinIsAcknowledgedByTheDirectoryOwner(t *testing.T) {
	runtimeDir, err := os.MkdirTemp("", "up-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(runtimeDir) })
	t.Setenv("XDG_RUNTIME_DIR", runtimeDir)
	ctx := context.Background()
	dir := t.TempDir()
	db, err := store.New(filepath.Join(dir, "usage.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	cfg := config.UsageConfig{Profiles: []config.UsageProfile{{Name: "save", Agent: "chimera"}}}
	opts := service.UsagePolicyOptions{Store: db, Config: func(context.Context) (config.UsageConfig, error) { return cfg, nil }, Readings: func(context.Context) (service.SubscriptionUsage, error) { return service.SubscriptionUsage{}, nil }}
	owner := &usagePolicyHost{policy: service.NewUsagePolicyService(opts)}
	owner.owner.Store(true)
	usagePolicyHosts.Store(dir, owner)
	defer usagePolicyHosts.Delete(dir)
	release, err := claimUsagePoller(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	server, err := ipc.NewServer(ipc.SocketPath(), handleUsagePinIPC)
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	requester := service.NewUsagePolicyService(opts)
	if err := requestUsagePin(ctx, dir, requester, &service.UsagePinRequest{Profile: "save"}); err != nil {
		t.Fatal(err)
	}
	snap, err := requester.Snapshot(ctx)
	if err != nil || snap.Active.Pin == nil || snap.Active.Profile.Name != "save" {
		t.Fatalf("acknowledged state=%+v err=%v", snap, err)
	}
	if err := requestUsagePin(ctx, dir, requester, &service.UsagePinRequest{Profile: "missing"}); err == nil || !strings.Contains(err.Error(), "unknown usage profile") {
		t.Fatalf("owner error=%v", err)
	}
	if err := requestUsagePin(ctx, dir, requester, nil); err != nil {
		t.Fatal(err)
	}
	other := t.TempDir()
	unlock, err := claimUsagePoller(other)
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()
	timeout, cancel := context.WithTimeout(ctx, 40*time.Millisecond)
	defer cancel()
	if err := requestUsagePin(timeout, other, requester, &service.UsagePinRequest{Profile: "save"}); err == nil || !strings.Contains(err.Error(), "did not acknowledge") {
		t.Fatalf("wrong directory succeeded: %v", err)
	}
	snap, _ = requester.Snapshot(ctx)
	if snap.Active.Pin != nil {
		t.Fatal("another data directory's request changed the pin")
	}
}

func TestUsagePinCLIControlsPolicyWithReadersOff(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	text := "db:\n  path: " + filepath.Join(dir, "legato.db") + "\ndata_dir: " + dir + "\nusage:\n  claude:\n    enabled: false\n  codex:\n    enabled: false\n  profiles:\n    - name: save\n      agent: chimera\n"
	if err := os.WriteFile(path, []byte(text), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("LEGATO_CONFIG", path)
	for i, args := range [][]string{{"usage", "pin", "save", "--json"}, {"usage", "--json"}, {"usage", "unpin", "--json"}} {
		output, code := runCLICaptured(t, args...)
		if code != 0 || !strings.Contains(output, `"policy"`) {
			t.Fatalf("%v => %d %s", args, code, output)
		}
		var envelope struct {
			Data service.SubscriptionUsage `json:"data"`
		}
		if err := json.Unmarshal([]byte(output), &envelope); err != nil {
			t.Fatal(err)
		}
		policy := envelope.Data.Policy
		if policy == nil {
			t.Fatal("CLI did not expose policy")
		}
		if i < 2 && (policy.Active.Pin == nil || policy.Active.Pin.Profile != "save" || policy.Active.Profile.Name != "save") {
			t.Fatalf("pin did not survive CLI reopen: %+v", policy.Active)
		}
		if i == 2 && policy.Active.Pin != nil {
			t.Fatalf("unpin did not clear durable pin: %+v", policy.Active.Pin)
		}
	}
	output, code := runCLICaptured(t, "usage", "pin", "save", "--until", "tomorrow", "--json")
	if code == 0 || !strings.Contains(output, "invalid_time") {
		t.Fatalf("invalid expiry => %d %s", code, output)
	}
	for _, path := range []string{"usage pin", "usage unpin"} {
		if entry := legatoCommandClasses[path]; entry.Class != "change" {
			t.Fatalf("%s permissions=%+v", path, entry)
		}
	}
}

func TestUsagePolicyRefusesAnUninstalledTierInsteadOfLaunchingBaseArgs(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	file := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(file, []byte("data_dir: "+dir+"\nusage:\n  claude:\n    enabled: false\n  codex:\n    enabled: false\n"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("LEGATO_CONFIG", file)
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	db, err := store.New(filepath.Join(dir, "usage.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	usage := subscriptionUsageFor(cfg, db, events.New())
	settings := settingsFor(db)
	for _, entry := range []struct{ list, name, raw string }{
		{"adapters", "codex", `{"tiers":{"fresh":{"launch_args":["--model","new-model"]}}}`},
		{"usage.profiles", "save", `{"agent":"codex","tier":"fresh"}`},
	} {
		if err := settings.SetEntry(ctx, entry.list, entry.name, json.RawMessage(entry.raw), "test"); err != nil {
			t.Fatal(err)
		}
	}
	if err := settings.Set(ctx, "usage.fallback_profile", json.RawMessage(`"save"`), "test"); err != nil {
		t.Fatal(err)
	}
	release, err := claimUsagePoller(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	usage.Poll(ctx)
	snap, err := usage.Snapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if snap.Policy.Active.Profile.Agent != "" || !strings.Contains(snap.Policy.Active.Error, "restart Legato") {
		t.Fatalf("uninstalled tier applied: %+v", snap.Policy.Active)
	}
}

func TestUsageCommittedDefaultsReachTheSpawnLaunchCommand(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	file := filepath.Join(dir, "config.yaml")
	t.Setenv("HOME", dir)
	text := "data_dir: " + dir + "\nusage:\n  claude:\n    enabled: false\n  codex:\n    enabled: false\n  profiles:\n    - name: save\n      agent: codex\n      tier: careful\n  fallback_profile: save\nadapters:\n  codex:\n    tiers:\n      careful:\n        launch_args: [--model, profile-model]\n"
	if err := os.WriteFile(file, []byte(text), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("LEGATO_CONFIG", file)
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	db, err := store.New(filepath.Join(dir, "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.CreateTask(ctx, store.Task{ID: "profile-task", Title: "New work"}); err != nil {
		t.Fatal(err)
	}
	usage := subscriptionUsageFor(cfg, db, events.New())
	release, err := claimUsagePoller(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	usage.Poll(ctx)
	codex := hooks.NewCodexAdapter("legato")
	codex.SetTiers(map[string][]string{"careful": {"--model", "profile-model"}})
	tmux := &usageLaunchTmux{contractTmux: contractTmux{alive: map[string]bool{}}}
	agents := service.NewAgentService(db, tmux, dir, service.AgentServiceOptions{Adapter: hooks.NewClaudeCodeAdapter("legato"), Adapters: map[string]service.AIToolAdapter{"codex": codex}, NewWorkDefaults: usageNewWorkDefaults(db), DisablePeerAwareness: true})
	if err := agents.SpawnAgent(ctx, "profile-task", 0, 0); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(tmux.command, "codex ") || !strings.Contains(tmux.command, "profile-model") {
		t.Fatalf("runtime launch = %q", tmux.command)
	}
	execution, err := db.GetAgentSessionByTaskID(ctx, "profile-task")
	if err != nil || execution.AgentKind != "codex" {
		t.Fatalf("stored execution=%+v err=%v", execution, err)
	}
}

type usageLaunchTmux struct {
	contractTmux
	command string
}

func (t *usageLaunchTmux) Spawn(name, _ string, _, _ int, _ ...string) error {
	t.alive[name] = true
	return nil
}
func (t *usageLaunchTmux) SendKeysLine(_, line string) error         { t.command = line; return nil }
func (t *usageLaunchTmux) SendKeysShellCommand(_, line string) error { t.command = line; return nil }

func TestUsageRootAndPinCommandsKeepSeparatePermissionClasses(t *testing.T) {
	for _, tc := range []struct {
		args  []string
		class string
	}{{[]string{"usage", "--json"}, "read"}, {[]string{"usage", "pin", "save"}, "change"}, {[]string{"usage", "unpin"}, "change"}, {[]string{"usage", "unknown"}, "never"}} {
		if class, _ := classifyLegatoCommand(tc.args); class != tc.class {
			t.Fatalf("%v => %s want %s", tc.args, class, tc.class)
		}
	}
}

func TestUsageRepositoryDefaultsReachTheSpawnOfATaskInThatRepository(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	file := filepath.Join(dir, "config.yaml")
	t.Setenv("HOME", dir)
	text := "data_dir: " + dir + "\nusage:\n  claude:\n    enabled: false\n  codex:\n    enabled: false\n  profiles:\n    - name: save\n      agent: codex\n      tier: careful\n    - name: claude\n      agent: claude-code\n  fallback_profile: save\n  repositories:\n    - {repository: acme/*, profile: claude}\nadapters:\n  codex:\n    tiers:\n      careful:\n        launch_args: [--model, profile-model]\n"
	if err := os.WriteFile(file, []byte(text), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("LEGATO_CONFIG", file)
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	db, err := store.New(filepath.Join(dir, "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for _, id := range []string{"acme-task", "other-task"} {
		if err := db.CreateTask(ctx, store.Task{ID: id, Title: "New work"}); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.SetCodeContext(ctx, "acme-task", &store.CodeContext{Repo: "acme/widget"}); err != nil {
		t.Fatal(err)
	}
	usage := subscriptionUsageFor(cfg, db, events.New())
	release, err := claimUsagePoller(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	usage.Poll(ctx)
	codex := hooks.NewCodexAdapter("legato")
	codex.SetTiers(map[string][]string{"careful": {"--model", "profile-model"}})
	claude := hooks.NewClaudeCodeAdapter("legato")
	tmux := &usageLaunchTmux{contractTmux: contractTmux{alive: map[string]bool{}}}
	// The options serve, commands and the TUI build their agent service with.
	agents := service.NewAgentService(db, tmux, dir, withUsageNewWork(service.AgentServiceOptions{Adapter: claude, Adapters: map[string]service.AIToolAdapter{"codex": codex, "claude-code": claude}, DisablePeerAwareness: true}, db))
	// A checkout the operator chose, of acme/widget, for a task that records
	// no repository.
	checkout := filepath.Join(dir, "widget")
	for _, args := range [][]string{{"init", "-q", checkout}, {"-C", checkout, "-c", "user.email=t@example.com", "-c", "user.name=t", "commit", "-q", "--allow-empty", "-m", "init"}, {"-C", checkout, "remote", "add", "origin", "git@github.com:acme/widget.git"}} {
		if out, err := exec.Command("git", args...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
	}
	if err := db.CreateTask(ctx, store.Task{ID: "dir-task", Title: "New work"}); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		task, kind string
		opts       service.AgentSpawnOptions
	}{{"acme-task", "claude-code", service.AgentSpawnOptions{}}, {"other-task", "codex", service.AgentSpawnOptions{}}, {"dir-task", "claude-code", service.AgentSpawnOptions{WorkingDir: checkout}}} {
		if err := agents.SpawnAgent(ctx, tc.task, 0, 0, tc.opts); err != nil {
			t.Fatal(err)
		}
		session, err := db.GetAgentSessionByTaskID(ctx, tc.task)
		if err != nil || session.AgentKind != tc.kind {
			t.Fatalf("%s: session=%+v err=%v", tc.task, session, err)
		}
	}
}
