package service

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cpave3/legato/internal/engine/engram"
)

func TestTheReevesEngramIsNamedAfterTheInstanceAndKeptInItsFolder(t *testing.T) {
	got := ReeveEngramFor("srv-1a2b3c4d5e6f7a8b", "/state/legato/reeve")
	if got.Name != "reeve-1a2b3c4d" {
		t.Fatalf("name = %q", got.Name)
	}
	if got.Home != filepath.Join("/state/legato/reeve", "engram") {
		t.Fatalf("home = %q", got.Home)
	}
	if got.Seed != ReeveSeedTemplate || !strings.Contains(got.Seed, "Reeve") || !strings.Contains(got.Seed, "secret") {
		t.Fatalf("seed = %q", got.Seed)
	}
}

type reeveEngramFixture struct {
	devicesFixture
	cli  *fakeEngramCLI
	svc  *ReeveEngramService
	home string
}

func newReeveEngramFixture(t *testing.T) *reeveEngramFixture {
	t.Helper()
	f := newDevicesFixture(t)
	home := t.TempDir()
	cli := &fakeEngramCLI{where: testWhere}
	engrams, _ := bootService(cli)
	svc := NewReeveEngramService(ReeveEngramOptions{
		Engram: func(context.Context) (ReeveEngram, error) {
			return ReeveEngram{Name: "reeve-1a2b3c4d", Home: home, Seed: ReeveSeedTemplate}, nil
		},
		Engrams: engrams, Approvals: f.approvals, Store: f.db,
	})
	svc.Register()
	return &reeveEngramFixture{devicesFixture: f, cli: cli, svc: svc, home: home}
}

// issue makes the engram with a seed, as engram identity new does.
func (f *reeveEngramFixture) issue(t *testing.T, seed string) string {
	t.Helper()
	dir := filepath.Join(f.home, "identities", "reeve-1a2b3c4d")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "identity.md")
	if err := os.WriteFile(path, []byte("---\nname: reeve-1a2b3c4d\ncreated: 2026-11-10T23:08:45Z\n---\n\n"+seed), 0o600); err != nil {
		t.Fatal(err)
	}
	f.cli.ids = append(f.cli.ids, engram.Identity{Name: "reeve-1a2b3c4d"})
	return path
}

func TestBeforeItsFirstSessionTheReevesEngramShowsTheBuiltInSeed(t *testing.T) {
	f := newReeveEngramFixture(t)
	view, err := f.svc.View(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if view.Name != "reeve-1a2b3c4d" || view.Exists || !view.Enabled || view.Seed.Text != ReeveSeedTemplate || view.Seed.Edited || view.Seed.Newer {
		t.Fatalf("view = %+v", view)
	}
}

func TestTheReevesEngramShowsItsSeedMemoriesAndThreads(t *testing.T) {
	f := newReeveEngramFixture(t)
	f.issue(t, ReeveSeedTemplate)
	f.cli.memories = []engram.Memory{{ID: "m1", Kind: "preference", Body: "Short answers"}}
	f.cli.threads = []engram.Thread{{ID: "t1", Title: "Check worker 2"}}
	view, err := f.svc.View(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !view.Exists || view.Seed.Text != ReeveSeedTemplate || view.Seed.Edited || view.Store != f.home {
		t.Fatalf("view = %+v", view)
	}
	if len(view.Memories) != 1 || view.Memories[0].ID != "m1" || len(view.Threads) != 1 || view.Threads[0].ID != "t1" {
		t.Fatalf("memories %+v threads %+v", view.Memories, view.Threads)
	}
	if f.cli.home != f.home {
		t.Fatalf("engram ran against %q", f.cli.home)
	}
}

// An earlier version's built-in seed is not an edit: Settings offers the
// newer one, and Legato does not write it by itself.
func TestAnOlderBuiltInSeedIsOfferedTheNewOne(t *testing.T) {
	f := newReeveEngramFixture(t)
	old := "You are the Reeve. An older seed.\n"
	f.svc.templates = []string{old, ReeveSeedTemplate}
	path := f.issue(t, old)
	view, _ := f.svc.View(context.Background())
	if view.Seed.Edited || !view.Seed.Newer || view.Seed.Template != ReeveSeedTemplate {
		t.Fatalf("seed = %+v", view.Seed)
	}
	if data, _ := os.ReadFile(path); !strings.HasSuffix(string(data), old) {
		t.Fatalf("Legato rewrote the seed: %s", data)
	}
}

func TestAnEditedSeedIsKeptAndNotOfferedAnUnchangedTemplate(t *testing.T) {
	f := newReeveEngramFixture(t)
	f.issue(t, "Mine: be brief.\n")
	view, _ := f.svc.View(context.Background())
	if !view.Seed.Edited || view.Seed.Newer || view.Seed.Text != "Mine: be brief.\n" {
		t.Fatalf("seed = %+v", view.Seed)
	}
}

func TestEditingTheSeedNeedsASignature(t *testing.T) {
	f := newReeveEngramFixture(t)
	if _, err := f.svc.ProposeSeed(context.Background(), "Mine.", "web"); !errors.Is(err, ErrNoDeviceEnrolled) {
		t.Fatalf("with no device: %v", err)
	}
}

func TestASignedSeedIsWrittenIntoTheEngramKeepingItsHeader(t *testing.T) {
	f := newReeveEngramFixture(t)
	path := f.issue(t, ReeveSeedTemplate)
	p := newPhone(t)
	f.enroll(t, p, "Pixel")
	ctx := context.Background()
	approval, err := f.svc.ProposeSeed(ctx, "Mine: be brief.", "web")
	if err != nil {
		t.Fatal(err)
	}
	if !approval.Signed || approval.Source != ApprovalSourceReeveSeed {
		t.Fatalf("approval = %+v", approval)
	}
	if data, _ := os.ReadFile(path); !strings.HasSuffix(string(data), ReeveSeedTemplate) {
		t.Fatal("the seed changed before the approval")
	}
	if _, err := f.approvals.Decide(ctx, approval.ID, ApprovalDecision{Decision: ApprovalAllow, Option: "allow", By: "phone", Signature: f.sign(t, p, approval.ID, "allow")}); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	if string(data) != "---\nname: reeve-1a2b3c4d\ncreated: 2026-11-10T23:08:45Z\n---\n\nMine: be brief.\n" {
		t.Fatalf("identity.md = %q", data)
	}
	if view, _ := f.svc.View(ctx); !view.Seed.Edited || view.Seed.Newer {
		t.Fatalf("seed = %+v", view.Seed)
	}
	// The built-in seed changes in a later version: Settings offers it.
	f.svc.templates = []string{ReeveSeedTemplate, "A newer built-in seed.\n"}
	if view, _ := f.svc.View(ctx); !view.Seed.Edited || !view.Seed.Newer || view.Seed.Template != "A newer built-in seed.\n" {
		t.Fatalf("after the template changed: %+v", view.Seed)
	}
}

func TestASignedSeedBeforeTheFirstSessionIssuesTheEngramWithIt(t *testing.T) {
	f := newReeveEngramFixture(t)
	ctx := context.Background()
	p := newPhone(t)
	f.enroll(t, p, "Pixel")
	approval, err := f.svc.ProposeSeed(ctx, "Mine.", "web")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.approvals.Decide(ctx, approval.ID, ApprovalDecision{Decision: ApprovalAllow, Option: "allow", By: "phone", Signature: f.sign(t, p, approval.ID, "allow")}); err != nil {
		t.Fatal(err)
	}
	if f.cli.issued["reeve-1a2b3c4d"] != "Mine.\n" {
		t.Fatalf("issued = %v", f.cli.issued)
	}
}

func TestRemovingAMemoryAndAThread(t *testing.T) {
	f := newReeveEngramFixture(t)
	f.issue(t, ReeveSeedTemplate)
	ctx := context.Background()
	if err := f.svc.RemoveMemory(ctx, "m1"); err != nil {
		t.Fatal(err)
	}
	if err := f.svc.CloseThread(ctx, "t1"); err != nil {
		t.Fatal(err)
	}
	if len(f.cli.removed) != 1 || f.cli.removed[0] != "m1" || !strings.Contains(f.cli.closed["t1"], "Settings") {
		t.Fatalf("removed %v closed %v", f.cli.removed, f.cli.closed)
	}
}

func TestWithEngramsOffTheReevesEngramSaysSo(t *testing.T) {
	f := newReeveEngramFixture(t)
	f.svc.opts.Engram = func(context.Context) (ReeveEngram, error) { return ReeveEngram{}, ErrEngramsOff }
	view, err := f.svc.View(context.Background())
	if err != nil || view.Enabled || !strings.Contains(view.Error, "engram.enabled") {
		t.Fatalf("view = %+v, %v", view, err)
	}
}
