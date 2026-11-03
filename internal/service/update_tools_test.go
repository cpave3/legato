package service

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// fakeSTTool is an st on a PATH of its own. Its version, whether it is a
// release, and the latest release are files in state; st update copies
// latest to version, or fails when state/fail exists. Every call is
// appended to state/calls.
type fakeSTTool struct {
	t     *testing.T
	state string
}

const fakeSTScript = `#!/bin/sh
s="$STATE"
echo "$*" >> "$s/calls"
[ -f "$s/hang" ] && exec sleep 30
case "$*" in
"version --json") printf '{"version":"%s","release":%s}\n' "$(cat "$s/version")" "$(cat "$s/release")" ;;
"update --check --json")
  if [ "$(cat "$s/version")" = "$(cat "$s/latest")" ]; then a=false; else a=true; fi
  printf '{"current":"%s","latest":"%s","available":%s,"url":"https://example.com/st"}\n' "$(cat "$s/version")" "$(cat "$s/latest")" "$a" ;;
"update")
  if [ -f "$s/fail" ]; then echo "downloading st-linux-amd64"; echo "st update: checksum mismatch for st-linux-amd64" >&2; exit 1; fi
  cp "$s/latest" "$s/version"; echo "st updated" ;;
*) exit 2 ;;
esac
`

func newFakeSTTool(t *testing.T, version string, release bool, latest string) *fakeSTTool {
	t.Helper()
	dir, state := t.TempDir(), t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "st"), []byte(fakeSTScript), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+":/usr/bin:/bin")
	t.Setenv("STATE", state)
	f := &fakeSTTool{t: t, state: state}
	f.set("version", version)
	f.set("release", map[bool]string{true: "true", false: "false"}[release])
	f.set("latest", latest)
	return f
}

func (f *fakeSTTool) set(name, value string) {
	if err := os.WriteFile(filepath.Join(f.state, name), []byte(value), 0o644); err != nil {
		f.t.Fatal(err)
	}
}

func (f *fakeSTTool) calls() string {
	data, _ := os.ReadFile(filepath.Join(f.state, "calls"))
	return string(data)
}

func TestTheUpdaterLooksAfterAReleaseOfST(t *testing.T) {
	newFakeSTTool(t, "v0.1.0", true, "v0.2.0")
	st := NewSTUpdater(ToolUpdaterOptions{})
	ctx := context.Background()

	got := st.Check(ctx)
	if !got.Managed || got.Version != "v0.1.0" || got.Latest != "v0.2.0" || !got.Available || got.State != UpdateAvailable || got.Path == "" {
		t.Fatalf("checked = %+v", got)
	}
	if err := st.Apply(ctx); err != nil {
		t.Fatal(err)
	}
	if got := st.Status(); got.Version != "v0.2.0" || got.Available || got.State != UpdateUpToDate {
		t.Fatalf("after the update = %+v", got)
	}
	if err := st.Apply(ctx); !errors.Is(err, ErrNoUpdate) {
		t.Fatalf("a second update = %v, want ErrNoUpdate", err)
	}
}

func TestAnSTBuiltFromSourceIsLeftAlone(t *testing.T) {
	fake := newFakeSTTool(t, "dev", false, "v0.2.0")
	st := NewSTUpdater(ToolUpdaterOptions{})
	ctx := context.Background()

	if got := st.Check(ctx); got.Managed || got.State != ToolUnmanaged || got.Version != "dev" {
		t.Fatalf("checked = %+v", got)
	}
	if err := st.Apply(ctx); !errors.Is(err, ErrToolNotManaged) {
		t.Fatalf("Apply = %v, want ErrToolNotManaged", err)
	}
	if strings.Contains(fake.calls(), "update") {
		t.Fatalf("st was asked to update: %q", fake.calls())
	}
}

func TestWithoutSTItIsMissing(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	st := NewSTUpdater(ToolUpdaterOptions{})
	if got := st.Check(context.Background()); got.State != ToolMissing || got.Managed {
		t.Fatalf("checked = %+v", got)
	}
}

func TestAFailedSTUpdateSaysWhy(t *testing.T) {
	fake := newFakeSTTool(t, "v0.1.0", true, "v0.2.0")
	fake.set("fail", "")
	st := NewSTUpdater(ToolUpdaterOptions{})
	ctx := context.Background()
	st.Check(ctx)

	if err := st.Apply(ctx); err == nil {
		t.Fatal("a failed st update succeeded")
	}
	if got := st.Status(); got.State != UpdateFailed || got.Error != "st update: checksum mismatch for st-linux-amd64" || got.Version != "v0.1.0" {
		t.Fatalf("after the failure = %+v", got)
	}
}

func TestAnSTThatHangsIsGivenUp(t *testing.T) {
	fake := newFakeSTTool(t, "v0.1.0", true, "v0.2.0")
	fake.set("hang", "")
	st := NewSTUpdater(ToolUpdaterOptions{Timeout: 100 * time.Millisecond})
	start := time.Now()
	got := st.Check(context.Background())
	if got.State != UpdateFailed || time.Since(start) > 5*time.Second {
		t.Fatalf("checked = %+v after %s", got, time.Since(start))
	}
}

func TestInAutoModeSTIsUpdatedWithoutWaitingForTheAgents(t *testing.T) {
	newFakeSTTool(t, "v0.1.0", true, "v0.2.0")
	st := NewSTUpdater(ToolUpdaterOptions{})
	f := newUpdater(t, "v1.3.0", func(o *UpdateOptions) { o.Tools = []*ToolUpdater{st} })
	if err := f.svc.SetMode(context.Background(), UpdateModeAuto); err != nil {
		t.Fatal(err)
	}
	f.setBusy(true)
	running(t, f)

	eventually(t, "st to be updated", func() bool { return st.Status().Version == "v0.2.0" })
	if got := f.svc.Status(context.Background()); len(got.Tools) != 1 || got.Tools[0].Name != "st" || got.State != UpdateUpToDate {
		t.Fatalf("status = %+v", got)
	}
}

func TestInAskModeSTWaitsForThePerson(t *testing.T) {
	fake := newFakeSTTool(t, "v0.1.0", true, "v0.2.0")
	st := NewSTUpdater(ToolUpdaterOptions{})
	f := newUpdater(t, "v1.3.0", func(o *UpdateOptions) { o.Tools = []*ToolUpdater{st} })
	running(t, f)

	eventually(t, "st's release to be found", func() bool { return st.Status().Available })
	time.Sleep(20 * time.Millisecond)
	if strings.Contains(fake.calls(), "update\n") {
		t.Fatal("st was updated without being asked")
	}
	if err := f.svc.ApplyTool(context.Background(), "st"); err != nil {
		t.Fatal(err)
	}
	if st.Status().Version != "v0.2.0" {
		t.Fatalf("st after ApplyTool = %+v", st.Status())
	}
	if err := f.svc.ApplyTool(context.Background(), "nope"); !errors.Is(err, ErrToolNotManaged) {
		t.Fatalf("ApplyTool of an unknown tool = %v", err)
	}
}

func TestWithUpdatesOffSTIsNotAsked(t *testing.T) {
	fake := newFakeSTTool(t, "v0.1.0", true, "v0.2.0")
	st := NewSTUpdater(ToolUpdaterOptions{})
	f := newUpdater(t, "v1.3.0", func(o *UpdateOptions) { o.Tools = []*ToolUpdater{st} })
	if err := f.svc.SetMode(context.Background(), UpdateModeOff); err != nil {
		t.Fatal(err)
	}
	running(t, f)
	time.Sleep(30 * time.Millisecond)
	if calls := fake.calls(); calls != "" {
		t.Fatalf("st was asked %q", calls)
	}
}

func TestALegatoBuiltFromSourceStillKeepsSTCurrent(t *testing.T) {
	newFakeSTTool(t, "v0.1.0", true, "v0.2.0")
	st := NewSTUpdater(ToolUpdaterOptions{})
	f := newUpdater(t, "dev", func(o *UpdateOptions) { o.Tools = []*ToolUpdater{st} })
	if err := f.svc.SetMode(context.Background(), UpdateModeAuto); err != nil {
		t.Fatal(err)
	}
	running(t, f)

	eventually(t, "st to be updated", func() bool { return st.Status().Version == "v0.2.0" })
	if n := f.releases.checkCount(); n != 0 {
		t.Fatalf("a Legato built from source asked GitHub %d times", n)
	}
}
