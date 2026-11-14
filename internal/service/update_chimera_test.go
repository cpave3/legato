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

// The fake speaks Chimera's version report (including modApi) and UpdateReport.
func newFakeChimeraTool(t *testing.T, version string, release bool) *fakeSTTool {
	t.Helper()
	dir, state := t.TempDir(), t.TempDir()
	const script = `#!/bin/sh
s="$STATE"
echo "$*" >> "$s/calls"
case "$*" in
"version --json") printf '{"version":"%s","release":%s,"modApi":[1]}\n' "$(cat "$s/version")" "$(cat "$s/release")" ;;
"update --check --json")
  if [ "$(cat "$s/version")" = "v1.2.0" ]; then a=false; else a=true; fi
  printf '{"current":"%s","latest":"v1.2.0","available":%s,"url":"https://example.com/chimera"}\n' "$(cat "$s/version")" "$a" ;;
"update")
  if [ "$(cat "$s/release")" = false ]; then
    echo "this chimera ($(cat "$s/version")) was built from a checkout, not downloaded from a release, so chimera update leaves it alone: rebuild it from the checkout (pnpm -r build)" >&2; exit 1
  fi
  echo v1.2.0 > "$s/version"
  echo '{"current":"v1.1.0","latest":"v1.2.0","available":true,"url":"https://example.com/chimera","updated":true}' ;;
*) exit 2 ;;
esac
`
	if err := os.WriteFile(filepath.Join(dir, "chimera"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+":/usr/bin:/bin")
	t.Setenv("STATE", state)
	f := &fakeSTTool{t: t, state: state}
	f.set("version", version)
	f.set("release", map[bool]string{true: "true", false: "false"}[release])
	return f
}

func TestTheUpdaterLooksAfterAReleaseOfChimera(t *testing.T) {
	fake := newFakeChimeraTool(t, "v1.1.0", true)
	tool := NewChimeraUpdater(ToolUpdaterOptions{})
	ctx := context.Background()
	got := tool.Check(ctx)
	if got.Name != "chimera" || !got.Managed || got.Version != "v1.1.0" || got.Latest != "v1.2.0" || !got.Available || got.State != UpdateAvailable || got.Path == "" {
		t.Fatalf("checked = %+v", got)
	}
	if strings.Contains(fake.calls(), "update\n") {
		t.Fatal("check installed a release")
	}
	if err := tool.Apply(ctx); err != nil {
		t.Fatal(err)
	}
	if got := tool.Status(); got.Version != "v1.2.0" || got.Available || got.State != UpdateUpToDate {
		t.Fatalf("after update = %+v", got)
	}
	if err := tool.Apply(ctx); !errors.Is(err, ErrNoUpdate) {
		t.Fatalf("second update = %v", err)
	}
}

func TestAChimeraCheckoutIsLeftAlone(t *testing.T) {
	fake := newFakeChimeraTool(t, "dev", false)
	tool := NewChimeraUpdater(ToolUpdaterOptions{})
	ctx := context.Background()
	if got := tool.Check(ctx); got.Managed || got.State != ToolUnmanaged || got.Version != "dev" {
		t.Fatalf("checked = %+v", got)
	}
	if err := tool.Apply(ctx); !errors.Is(err, ErrToolNotManaged) {
		t.Fatalf("Apply = %v", err)
	}
	if strings.Contains(fake.calls(), "update") {
		t.Fatalf("checkout was asked to update: %q", fake.calls())
	}
}

func TestWithoutChimeraItIsMissing(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	tool := NewChimeraUpdater(ToolUpdaterOptions{})
	if got := tool.Check(context.Background()); got.State != ToolMissing || got.Managed {
		t.Fatalf("checked = %+v", got)
	}
	if err := tool.Apply(context.Background()); !errors.Is(err, ErrToolNotManaged) {
		t.Fatalf("Apply = %v", err)
	}
}

func TestChimeraFollowsLegatosUpdateMode(t *testing.T) {
	for _, mode := range []string{UpdateModeAsk, UpdateModeAuto, UpdateModeOff} {
		t.Run(mode, func(t *testing.T) {
			fake := newFakeChimeraTool(t, "v1.1.0", true)
			tool := NewChimeraUpdater(ToolUpdaterOptions{})
			f := newUpdater(t, "dev", func(o *UpdateOptions) { o.Tools = []*ToolUpdater{tool} })
			ctx := context.Background()
			if err := f.svc.SetMode(ctx, mode); err != nil {
				t.Fatal(err)
			}
			f.setBusy(true)
			running(t, f)
			switch mode {
			case UpdateModeAuto:
				eventually(t, "Chimera updated while agents are busy", func() bool { return tool.Status().Version == "v1.2.0" })
			case UpdateModeAsk:
				eventually(t, "Chimera release found", func() bool { return tool.Status().Available })
				if strings.Contains(fake.calls(), "update\n") {
					t.Fatal("installed without being asked")
				}
				if err := f.svc.ApplyTool(ctx, "chimera"); err != nil {
					t.Fatal(err)
				}
				if tool.Status().Version != "v1.2.0" {
					t.Fatalf("after apply = %+v", tool.Status())
				}
			case UpdateModeOff:
				time.Sleep(30 * time.Millisecond)
				if fake.calls() != "" {
					t.Fatalf("Chimera called with updates off: %q", fake.calls())
				}
			}
		})
	}
}
