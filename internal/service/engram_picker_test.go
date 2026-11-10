package service

import (
	"context"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/cpave3/legato/config"
	"github.com/cpave3/legato/internal/engine/engram"
	"github.com/cpave3/legato/internal/engine/store"
)

type pickerFixture struct {
	store *store.Store
	cli   *fakeEngramCLI
	svc   *EngramService
}

func newPickerFixture(t *testing.T) *pickerFixture {
	t.Helper()
	s, err := store.New(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	cli := &fakeEngramCLI{where: testWhere, ids: []engram.Identity{
		{Name: "zeta", Summary: "z"}, {Name: "argus", Summary: "a"}, {Name: "mneme", Summary: "m"}, {Name: "hestia", Summary: "h"}, {Name: "clio"},
	}, places: map[string]engram.Place{
		"/work/legato": {Primary: "repo:github.com/cpave3/legato", Scopes: []string{"repo:github.com/cpave3/legato", "dir:/work/legato"}},
		"/notes":       {Primary: "dir:/notes", Scopes: []string{"dir:/notes"}},
	}}
	svc := NewEngramService(EngramOptions{
		Config: func(context.Context) (config.EngramConfig, error) {
			return config.EngramConfig{Enabled: config.EngramAuto}, nil
		},
		Runner: func(string) EngramCLI { return cli },
		Store:  s,
	})
	return &pickerFixture{store: s, cli: cli, svc: svc}
}

func names(p EngramPicker) []string {
	var out []string
	for _, e := range p.Engrams {
		out = append(out, e.Name)
	}
	return out
}

func TestThePickerOrdersEngramsForAPlace(t *testing.T) {
	f := newPickerFixture(t)
	ctx := context.Background()
	at := time.Date(2026, 11, 10, 9, 0, 0, 0, time.UTC)
	place := "repo:github.com/cpave3/legato"
	_ = f.store.BindEngram(ctx, place, "hestia", false, at)
	_ = f.store.BindEngram(ctx, place, "zeta", false, at)
	_ = f.store.BindEngram(ctx, place, "argus", true, at)
	_ = f.store.TouchEngramBinding(ctx, place, "zeta", at.Add(2*time.Hour))
	_ = f.store.TouchEngramBinding(ctx, place, "hestia", at.Add(time.Hour))
	// The task last ran as mneme, which is not bound here.
	if err := f.store.CreateExecution(ctx, store.Execution{ID: "ex-1", Kind: store.ExecutionInteractive, Status: store.ExecutionSucceeded, TaskID: "task-1", ExecutionTaskID: "task-1", StartedAt: at, EndedAt: &at}); err != nil {
		t.Fatal(err)
	}
	if err := f.store.SetExecutionEngram(ctx, "ex-1", "mneme", "ex-1"); err != nil {
		t.Fatal(err)
	}

	p, err := f.svc.Picker(ctx, EngramPickerRequest{Dir: "/work/legato", TaskID: "task-1"})
	if err != nil {
		t.Fatal(err)
	}
	// last, default, bound by last use, then the rest A-Z.
	if want := []string{"mneme", "argus", "zeta", "hestia", "clio"}; !slices.Equal(names(p), want) {
		t.Fatalf("order = %v, want %v", names(p), want)
	}
	if p.Engrams[0].Mark != "last" || p.Engrams[1].Mark != "default" || p.Engrams[2].Mark != "" {
		t.Fatalf("marks = %+v", p.Engrams)
	}
	if p.Engrams[0].Bound || !p.Engrams[1].Bound || !p.Engrams[2].Bound || p.Engrams[4].Bound {
		t.Fatalf("bound = %+v", p.Engrams)
	}
	if p.Preselect != "mneme" {
		t.Fatalf("preselect = %q, want the task's last engram", p.Preselect)
	}
	if p.Place == nil || p.Place.Key != place || p.Place.Kind != "repo" || p.Place.Label != "github.com/cpave3/legato" || p.Place.Default != "argus" {
		t.Fatalf("place = %+v", p.Place)
	}
}

func TestThePickerPreselectsTheDefaultThenNone(t *testing.T) {
	f := newPickerFixture(t)
	ctx := context.Background()
	if p, _ := f.svc.Picker(ctx, EngramPickerRequest{Dir: "/work/legato"}); p.Preselect != "" {
		t.Fatalf("preselect with nothing bound = %q", p.Preselect)
	}
	_ = f.store.BindEngram(ctx, "repo:github.com/cpave3/legato", "argus", true, time.Now())
	if p, _ := f.svc.Picker(ctx, EngramPickerRequest{Dir: "/work/legato"}); p.Preselect != "argus" {
		t.Fatalf("preselect = %q, want the default", p.Preselect)
	}
}

func TestARepoBindingBeatsAFolderBinding(t *testing.T) {
	f := newPickerFixture(t)
	ctx := context.Background()
	_ = f.store.BindEngram(ctx, "dir:/work/legato", "hestia", true, time.Now())
	_ = f.store.BindEngram(ctx, "repo:github.com/cpave3/legato", "argus", true, time.Now())
	p, _ := f.svc.Picker(ctx, EngramPickerRequest{Dir: "/work/legato"})
	if p.Preselect != "argus" || p.Place.Key != "repo:github.com/cpave3/legato" {
		t.Fatalf("preselect = %q, place = %+v", p.Preselect, p.Place)
	}
	// Only the folder is bound: its binding matches, under its own key.
	f2 := newPickerFixture(t)
	_ = f2.store.BindEngram(ctx, "dir:/work/legato", "hestia", true, time.Now())
	p, _ = f2.svc.Picker(ctx, EngramPickerRequest{Dir: "/work/legato"})
	if p.Preselect != "hestia" || p.Place.Key != "dir:/work/legato" {
		t.Fatalf("folder-only: preselect = %q, place = %+v", p.Preselect, p.Place)
	}
}

func TestAFolderOutsideGitIsAFolder(t *testing.T) {
	f := newPickerFixture(t)
	p, _ := f.svc.Picker(context.Background(), EngramPickerRequest{Dir: "/notes"})
	if p.Place == nil || p.Place.Kind != "folder" || p.Place.Label != "/notes" {
		t.Fatalf("place = %+v", p.Place)
	}
}

func TestAJobClonesPlaceIsItsRepository(t *testing.T) {
	f := newPickerFixture(t)
	_ = f.store.BindEngram(context.Background(), "repo:github.com/cpave3/legato", "argus", true, time.Now())
	p, _ := f.svc.Picker(context.Background(), EngramPickerRequest{Repo: "CPave3/Legato"})
	if p.Place == nil || p.Place.Key != "repo:github.com/cpave3/legato" || p.Preselect != "argus" {
		t.Fatalf("picker = %+v", p)
	}
}

func TestAMissingEngramIsLeftOutAndAMissingDefaultFallsBackToNone(t *testing.T) {
	f := newPickerFixture(t)
	ctx := context.Background()
	_ = f.store.BindEngram(ctx, "repo:github.com/cpave3/legato", "ghost", true, time.Now())
	p, _ := f.svc.Picker(ctx, EngramPickerRequest{Dir: "/work/legato"})
	if slices.Contains(names(p), "ghost") || p.Preselect != "" {
		t.Fatalf("picker = %+v", p)
	}
	// It is still the place's default, so it can be cleared.
	if p.Place.Default != "ghost" {
		t.Fatalf("place = %+v", p.Place)
	}
}

func TestThePickerIsOffWhenEngramIs(t *testing.T) {
	f := newPickerFixture(t)
	f.cli.whereErr = engram.ErrNotInstalled
	p, err := f.svc.Picker(context.Background(), EngramPickerRequest{Dir: "/work/legato"})
	if err != nil || p.Enabled || len(p.Engrams) != 0 {
		t.Fatalf("picker = %+v %v", p, err)
	}
}

func TestRemember(t *testing.T) {
	f := newPickerFixture(t)
	ctx := context.Background()
	place := "repo:github.com/cpave3/legato"
	if err := f.svc.Remember(ctx, EngramPickerRequest{Dir: "/work/legato"}, "mneme"); err != nil {
		t.Fatal(err)
	}
	if err := f.svc.Remember(ctx, EngramPickerRequest{Dir: "/work/legato"}, "argus"); err != nil {
		t.Fatal(err)
	}
	bindings, _ := f.store.EngramBindings(ctx, []string{place})
	if len(bindings) != 2 {
		t.Fatalf("bindings = %+v", bindings)
	}
	for _, b := range bindings {
		if b.IsDefault != (b.Engram == "argus") {
			t.Fatalf("bindings = %+v", bindings)
		}
	}
	// No engram clears the default; both stay bound.
	if err := f.svc.Remember(ctx, EngramPickerRequest{Dir: "/work/legato"}, ""); err != nil {
		t.Fatal(err)
	}
	bindings, _ = f.store.EngramBindings(ctx, []string{place})
	if len(bindings) != 2 || bindings[0].IsDefault || bindings[1].IsDefault {
		t.Fatalf("after clearing = %+v", bindings)
	}
}

func TestSpawningABoundEngramRecordsItsUse(t *testing.T) {
	f := newPickerFixture(t)
	ctx := context.Background()
	place := "repo:github.com/cpave3/legato"
	_ = f.store.BindEngram(ctx, place, "mneme", false, time.Now())
	if err := f.svc.Touch(ctx, "/work/legato", "mneme"); err != nil {
		t.Fatal(err)
	}
	bindings, _ := f.store.EngramBindings(ctx, []string{place})
	if bindings[0].LastUsedAt == nil {
		t.Fatal("last_used_at was not set")
	}
}

func TestTheBindingsListEachPlaceWithItsEngrams(t *testing.T) {
	f := newPickerFixture(t)
	ctx := context.Background()
	_ = f.store.BindEngram(ctx, "repo:github.com/cpave3/legato", "argus", true, time.Now())
	_ = f.store.BindEngram(ctx, "repo:github.com/cpave3/legato", "ghost", false, time.Now())
	_ = f.store.BindEngram(ctx, "dir:/notes", "mneme", false, time.Now())
	view, err := f.svc.Bindings(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !view.Status.Enabled || len(view.Places) != 2 {
		t.Fatalf("view = %+v", view)
	}
	folder, repo := view.Places[0], view.Places[1]
	if folder.Label != "/notes" || folder.Kind != "folder" || repo.Label != "github.com/cpave3/legato" || repo.Kind != "repo" {
		t.Fatalf("places = %+v", view.Places)
	}
	if len(repo.Engrams) != 2 || repo.Engrams[0].Name != "argus" || !repo.Engrams[0].Default || repo.Engrams[1].Name != "ghost" || !repo.Engrams[1].Missing {
		t.Fatalf("repo engrams = %+v", repo.Engrams)
	}
	// Without engram, nothing can be called missing.
	f.cli.whereErr = engram.ErrNotInstalled
	view, _ = f.svc.Bindings(ctx)
	if view.Status.Enabled || view.Places[1].Engrams[1].Missing {
		t.Fatalf("without engram: %+v", view)
	}
}

func TestBindingsCanBeUnboundAndTheirDefaultChanged(t *testing.T) {
	f := newPickerFixture(t)
	ctx := context.Background()
	place := "repo:github.com/cpave3/legato"
	_ = f.store.BindEngram(ctx, place, "argus", true, time.Now())
	_ = f.store.BindEngram(ctx, place, "mneme", false, time.Now())
	if err := f.svc.SetDefault(ctx, place, "mneme"); err != nil {
		t.Fatal(err)
	}
	if err := f.svc.SetDefault(ctx, place, ""); err != nil {
		t.Fatal(err)
	}
	if err := f.svc.Unbind(ctx, place, "argus"); err != nil {
		t.Fatal(err)
	}
	view, _ := f.svc.Bindings(ctx)
	if len(view.Places) != 1 || len(view.Places[0].Engrams) != 1 || view.Places[0].Engrams[0].Default {
		t.Fatalf("view = %+v", view)
	}
}
