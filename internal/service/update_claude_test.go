package service

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/cpave3/legato/internal/engine/claudecode"
	"github.com/cpave3/legato/internal/engine/events"
)

// fakeClaudeReleases is Claude Code's release server: channels, and a
// Fetch that writes a script printing its version, or fails.
type fakeClaudeReleases struct {
	mu       sync.Mutex
	channels map[string]string
	fail     error
	fetched  []string
}

func (f *fakeClaudeReleases) Latest(_ context.Context, channel string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	v, ok := f.channels[channel]
	if !ok {
		return "", errors.New("no such channel")
	}
	return v, nil
}

func (f *fakeClaudeReleases) Fetch(_ context.Context, version, dir string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.fetched = append(f.fetched, version)
	if f.fail != nil {
		return "", f.fail
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	path := filepath.Join(dir, version)
	return path, os.WriteFile(path, []byte("#!/bin/sh\necho '"+version+" (Claude Code)'\n"), 0o755)
}

func (f *fakeClaudeReleases) set(channel, version string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.channels[channel] = version
}

func (f *fakeClaudeReleases) fetches() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.fetched...)
}

type claudeToolFixture struct {
	t        *testing.T
	folder   claudecode.Folder
	releases *fakeClaudeReleases
	native   string // the operator's native install, "" for none
	tool     *ClaudeCodeTool
}

// newClaudeToolFixture is a ClaudeCodeTool on a folder of its own, with the
// operator's native install of nativeVersion ("" for none) and the stable
// channel at stable.
func newClaudeToolFixture(t *testing.T, nativeVersion, stable string, opts ...func(*ClaudeCodeToolOptions)) *claudeToolFixture {
	t.Helper()
	f := &claudeToolFixture{t: t, folder: claudecode.Folder{Dir: filepath.Join(t.TempDir(), "tools", "claude")},
		releases: &fakeClaudeReleases{channels: map[string]string{"stable": stable, "latest": stable}}}
	if nativeVersion != "" {
		f.native = filepath.Join(t.TempDir(), ".local", "share", "claude", "versions", nativeVersion)
		if err := os.MkdirAll(filepath.Dir(f.native), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(f.native, []byte("#!/bin/sh\necho '"+nativeVersion+" (Claude Code)'\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	o := ClaudeCodeToolOptions{
		Folder: f.folder, Releases: f.releases,
		Installed: func() (string, string, bool) {
			if f.native == "" {
				return "", "", false
			}
			return claudecode.InstalledVersion(f.native)
		},
	}
	for _, opt := range opts {
		opt(&o)
	}
	f.tool = NewClaudeCodeTool(o)
	return f
}

func (f *claudeToolFixture) check() ToolStatus {
	f.t.Helper()
	return f.tool.Check(context.Background())
}

func TestClaudeCodeAdoptsTheOperatorsNativeInstallWithoutDownloading(t *testing.T) {
	f := newClaudeToolFixture(t, "2.1.332", "2.1.326")
	got := f.check()
	if got.Name != "claude" || !got.Managed || got.Version != "2.1.332" || got.Available || got.State != UpdateUpToDate || got.Channel != "stable" {
		t.Fatalf("checked = %+v", got)
	}
	if fetched := f.releases.fetches(); len(fetched) != 0 {
		t.Fatalf("fetched %v", fetched)
	}
	if cur, _ := f.folder.Current(); cur != "2.1.332" || !f.folder.Has("2.1.332") {
		t.Fatalf("the folder's current = %q", cur)
	}
	if cmd, managed := f.tool.Command(); cmd != f.folder.Command() || !managed {
		t.Fatalf("Command = %q, %v", cmd, managed)
	}
	// The operator's install is left where it was.
	if _, err := os.Stat(f.native); err != nil {
		t.Fatal(err)
	}
}

func TestClaudeCodeWithNoNativeInstallIsMissing(t *testing.T) {
	f := newClaudeToolFixture(t, "", "2.1.326")
	if got := f.check(); got.State != ToolMissing || got.Managed {
		t.Fatalf("checked = %+v", got)
	}
	if cmd, managed := f.tool.Command(); cmd != "claude" || managed {
		t.Fatalf("Command = %q, %v; want claude from PATH", cmd, managed)
	}
	if err := f.tool.Apply(context.Background()); !errors.Is(err, ErrToolNotManaged) {
		t.Fatalf("Apply = %v", err)
	}
}

func TestClaudeCodeUpdateKeepsTheReplacedVersionAsLastKnownGood(t *testing.T) {
	f := newClaudeToolFixture(t, "2.1.326", "2.1.330")
	if got := f.check(); !got.Available || got.Latest != "2.1.330" || got.State != UpdateAvailable {
		t.Fatalf("checked = %+v", got)
	}
	if err := f.tool.Apply(context.Background()); err != nil {
		t.Fatal(err)
	}
	got := f.tool.Status()
	if got.Version != "2.1.330" || got.Available || got.State != UpdateUpToDate || got.LastGood != "2.1.326" || !got.CanRollback {
		t.Fatalf("after the update = %+v", got)
	}
	if fetched := f.releases.fetches(); len(fetched) != 1 || fetched[0] != "2.1.330" {
		t.Fatalf("fetched %v", fetched)
	}
	if err := f.tool.Apply(context.Background()); !errors.Is(err, ErrNoUpdate) {
		t.Fatalf("a second update = %v", err)
	}
}

func TestClaudeCodeFollowsTheChannelSet(t *testing.T) {
	f := newClaudeToolFixture(t, "2.1.326", "2.1.326", func(o *ClaudeCodeToolOptions) {
		o.Channel = func(context.Context) string { return "latest" }
	})
	f.releases.set("latest", "2.1.332")
	if got := f.check(); got.Channel != "latest" || got.Latest != "2.1.332" || !got.Available {
		t.Fatalf("checked = %+v", got)
	}
}

func TestAFailedClaudeCodeUpdateLeavesTheVersionInUse(t *testing.T) {
	f := newClaudeToolFixture(t, "2.1.326", "2.1.330")
	f.check()
	f.releases.fail = errors.New("Claude Code 2.1.330 failed its checksum: the download is not the release")
	if err := f.tool.Apply(context.Background()); err == nil {
		t.Fatal("want an error")
	}
	got := f.tool.Status()
	if got.State != UpdateFailed || !strings.Contains(got.Error, "checksum") || got.Version != "2.1.326" {
		t.Fatalf("after a failed update = %+v", got)
	}
	if cur, _ := f.folder.Current(); cur != "2.1.326" {
		t.Fatalf("current = %s", cur)
	}
	if lg, _ := f.folder.LastGood(); lg != "" {
		t.Fatalf("last good = %s after a failed update", lg)
	}
}

func TestClaudeCodeRollsBackToTheLastKnownGoodAndHoldsTheBadVersion(t *testing.T) {
	f := newClaudeToolFixture(t, "2.1.326", "2.1.330")
	ctx := context.Background()
	f.check()
	if err := f.tool.Apply(ctx); err != nil {
		t.Fatal(err)
	}
	if err := f.tool.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	got := f.tool.Status()
	if got.Version != "2.1.326" || got.Held != "2.1.330" || got.Available || got.CanRollback || got.State != UpdateUpToDate {
		t.Fatalf("after the rollback = %+v", got)
	}
	if fetched := f.releases.fetches(); len(fetched) != 1 {
		t.Fatalf("a rollback to a version on disk fetched: %v", fetched)
	}
	// The held version is not offered again.
	if got := f.check(); got.Available {
		t.Fatalf("the held version is offered again: %+v", got)
	}
	// A rollback with nothing to go back to.
	if err := f.tool.Rollback(ctx); !errors.Is(err, ErrNoLastGood) {
		t.Fatalf("a second rollback = %v", err)
	}
	// A release newer than the held one is.
	f.releases.set("stable", "2.1.331")
	if got := f.check(); !got.Available || got.Latest != "2.1.331" {
		t.Fatalf("a newer release is not offered: %+v", got)
	}
	if err := f.tool.Apply(ctx); err != nil {
		t.Fatal(err)
	}
	if got := f.tool.Status(); got.Held != "" || got.LastGood != "2.1.326" || got.Version != "2.1.331" {
		t.Fatalf("after updating past the held version = %+v", got)
	}
}

func TestClaudeCodeRollbackFetchesALastKnownGoodNoLongerOnDisk(t *testing.T) {
	f := newClaudeToolFixture(t, "2.1.326", "2.1.330")
	ctx := context.Background()
	f.check()
	if err := f.tool.Apply(ctx); err != nil {
		t.Fatal(err)
	}
	os.Remove(filepath.Join(f.folder.VersionsDir(), "2.1.326"))
	if err := f.tool.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	if fetched := f.releases.fetches(); len(fetched) != 2 || fetched[1] != "2.1.326" {
		t.Fatalf("fetched %v", fetched)
	}
	if cur, _ := f.folder.Current(); cur != "2.1.326" || !f.folder.Has("2.1.326") {
		t.Fatalf("current = %s", cur)
	}
}

func TestClaudeCodeWithoutALastKnownGoodCannotRollBack(t *testing.T) {
	f := newClaudeToolFixture(t, "2.1.326", "2.1.326")
	f.check()
	if got := f.tool.Status(); got.CanRollback || got.LastGood != "" {
		t.Fatalf("status = %+v", got)
	}
	if err := f.tool.Rollback(context.Background()); !errors.Is(err, ErrNoLastGood) {
		t.Fatalf("Rollback = %v", err)
	}
}

func TestClaudeCodeWhoseVersionWentMissingIsRepairedByUpdating(t *testing.T) {
	f := newClaudeToolFixture(t, "2.1.326", "2.1.326")
	f.check()
	os.Remove(filepath.Join(f.folder.VersionsDir(), "2.1.326"))
	got := f.check()
	if got.State != UpdateFailed || !strings.Contains(got.Error, "2.1.326") || !got.Managed {
		t.Fatalf("checked = %+v", got)
	}
	if err := f.tool.Apply(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := f.tool.Status(); got.State != UpdateUpToDate || !f.folder.Has("2.1.326") {
		t.Fatalf("after the repair = %+v", got)
	}
}

func TestYourOwnClaudeIsUnmanaged(t *testing.T) {
	own := filepath.Join(t.TempDir(), "claude")
	if err := os.WriteFile(own, []byte("#!/bin/sh\necho '9.9.9 (Claude Code)'\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	f := newClaudeToolFixture(t, "2.1.326", "2.1.330", func(o *ClaudeCodeToolOptions) { o.Override = own })
	got := f.check()
	if got.Managed || got.State != ToolUnmanaged || got.Path != own || got.Version != "9.9.9" || got.Available || !strings.HasPrefix(got.Detail, "Your own claude") {
		t.Fatalf("checked = %+v", got)
	}
	if cmd, managed := f.tool.Command(); cmd != own || managed {
		t.Fatalf("Command = %q, %v", cmd, managed)
	}
	ctx := context.Background()
	if err := f.tool.Apply(ctx); !errors.Is(err, ErrToolNotManaged) {
		t.Fatalf("Apply = %v", err)
	}
	if err := f.tool.Rollback(ctx); !errors.Is(err, ErrToolNotManaged) {
		t.Fatalf("Rollback = %v", err)
	}
	if fetched := f.releases.fetches(); len(fetched) != 0 {
		t.Fatalf("fetched %v", fetched)
	}
	if _, err := f.folder.Current(); !errors.Is(err, claudecode.ErrNoVersion) {
		t.Fatalf("the native install was adopted under an override: %v", err)
	}
}

func TestInAskModeClaudeCodeWaitsAndRollsBackThroughTheUpdater(t *testing.T) {
	c := newClaudeToolFixture(t, "2.1.326", "2.1.330")
	f := newUpdater(t, "v1.3.0", func(o *UpdateOptions) { o.Tools = []ManagedTool{c.tool} })
	running(t, f)
	ctx := context.Background()

	eventually(t, "Claude Code's release to be found", func() bool { return c.tool.Status().Available })
	if fetched := c.releases.fetches(); len(fetched) != 0 {
		t.Fatalf("installed without being asked: %v", fetched)
	}
	if err := f.svc.ApplyTool(ctx, "claude"); err != nil {
		t.Fatal(err)
	}
	if err := f.svc.RollbackTool(ctx, "claude"); err != nil {
		t.Fatal(err)
	}
	if got := c.tool.Status(); got.Version != "2.1.326" || got.Held != "2.1.330" {
		t.Fatalf("after the rollback = %+v", got)
	}
	if err := f.svc.RollbackTool(ctx, "nope"); !errors.Is(err, ErrToolNotManaged) {
		t.Fatalf("RollbackTool of an unknown tool = %v", err)
	}
}

func TestAToolWithoutRollbackCannotBeRolledBack(t *testing.T) {
	newFakeSTTool(t, "v0.1.0", true, "v0.2.0")
	st := NewSTUpdater(ToolUpdaterOptions{})
	f := newUpdater(t, "v1.3.0", func(o *UpdateOptions) { o.Tools = []ManagedTool{st} })
	if err := f.svc.RollbackTool(context.Background(), "st"); !errors.Is(err, ErrToolNotManaged) {
		t.Fatalf("RollbackTool(st) = %v", err)
	}
}

func TestInAutoModeClaudeCodeIsUpdatedAtOnce(t *testing.T) {
	c := newClaudeToolFixture(t, "2.1.326", "2.1.330")
	f := newUpdater(t, "v1.3.0", func(o *UpdateOptions) { o.Tools = []ManagedTool{c.tool} })
	if err := f.svc.SetMode(context.Background(), UpdateModeAuto); err != nil {
		t.Fatal(err)
	}
	f.setBusy(true)
	running(t, f)
	eventually(t, "Claude Code to be updated", func() bool { return c.tool.Status().Version == "2.1.330" })
}

// An agent is found by the name of the process in its pane, which is the
// name of what was run: Legato's link must be named claude.
func TestAnAgentLaunchedThroughLegatosClaudeIsRecognised(t *testing.T) {
	cmd := claudecode.Folder{Dir: "/data/legato/tools/claude"}.Command()
	if !agentForeground("claude-code", cmd) {
		t.Fatalf("an agent run as %s is not recognised", cmd)
	}
}

// Another process (legato tools) changed a tool: a running updater checks
// its tools again when told, so the web UI shows the change.
func TestARunningUpdaterChecksItsToolsAgainWhenTold(t *testing.T) {
	c := newClaudeToolFixture(t, "2.1.326", "2.1.326")
	bus := events.New()
	f := newUpdater(t, "v1.3.0", func(o *UpdateOptions) {
		o.Tools = []ManagedTool{c.tool}
		o.Bus = bus
	})
	running(t, f)
	eventually(t, "the first check", func() bool { return c.tool.Status().State == UpdateUpToDate })

	c.releases.set("stable", "2.1.330")
	bus.Publish(events.Event{Type: events.EventToolsChanged})
	eventually(t, "the tools to be checked again", func() bool { return c.tool.Status().Available })
}
