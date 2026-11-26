package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cpave3/legato/config"
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
	for _, args := range [][]string{{"usage", "pin", "save", "--json"}, {"usage", "--json"}, {"usage", "unpin", "--json"}} {
		output, code := runCLICaptured(t, args...)
		if code != 0 || !strings.Contains(output, `"policy"`) {
			t.Fatalf("%v => %d %s", args, code, output)
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
