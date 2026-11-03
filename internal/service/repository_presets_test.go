package service

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cpave3/legato/internal/engine/store"
)

// ownerChecker clones the repositories of one owner.
type ownerChecker string

func (o ownerChecker) CloneProblem(repo string) string {
	if strings.HasPrefix(repo, string(o)+"/") {
		return ""
	}
	return repo + " is not one of the repositories of github (settings.repositories in config.yaml)"
}

func newPresetFixture(t *testing.T) (*RepositoryPresetService, string) {
	t.Helper()
	db, err := store.New(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	home := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, "src", "notes"), 0o755); err != nil {
		t.Fatal(err)
	}
	svc := NewRepositoryPresetService(db, []RepositoryPreset{{Name: "Legato", Repo: "cpave3/legato", BaseBranch: "main"}}, home)
	svc.SetCloneChecker(ownerChecker("cpave3"))
	return svc, home
}

func TestRepositoriesAddedInTheUIComeAfterTheConfiguredOnes(t *testing.T) {
	svc, home := newPresetFixture(t)
	ctx := context.Background()
	if _, err := svc.Save(ctx, RepositoryPreset{Name: "Chimera", Repo: "cpave3/chimera"}); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if _, err := svc.Save(ctx, RepositoryPreset{Name: "Widget", Repo: "acme/widget", BaseBranch: "develop"}); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if _, err := svc.Save(ctx, RepositoryPreset{Name: "Notes", PrimaryDir: "~/src/notes"}); err != nil {
		t.Fatalf("Save: %v", err)
	}
	listed, err := svc.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, p := range listed {
		names = append(names, p.Name)
	}
	if strings.Join(names, ",") != "Legato,Chimera,Widget,Notes" {
		t.Fatalf("listed %v", names)
	}
	if !listed[0].Configured || listed[1].Configured || listed[1].ID == 0 {
		t.Fatalf("configured flags: %+v", listed)
	}
	if listed[1].CloneProblem != "" || !strings.Contains(listed[2].CloneProblem, "settings.repositories") || listed[3].CloneProblem != "" {
		t.Fatalf("clone problems: %+v", listed)
	}
	if listed[3].PrimaryDir != filepath.Join(home, "src", "notes") {
		t.Fatalf("folder = %q, want it expanded", listed[3].PrimaryDir)
	}

	presets, _ := svc.WorkflowPresets(ctx)
	if len(presets) != 4 || presets[2].Repo != "acme/widget" || presets[2].BaseBranch != "develop" {
		t.Fatalf("workflow presets %+v", presets)
	}
	dirs, _ := svc.DirectoryPresets(ctx)
	if len(dirs) != 1 || dirs[0].Name != "Notes" {
		t.Fatalf("directory presets %+v", dirs)
	}
}

func TestARepositoryIsCheckedWhenAdded(t *testing.T) {
	svc, _ := newPresetFixture(t)
	ctx := context.Background()
	for _, bad := range []RepositoryPreset{
		{Name: "", Repo: "cpave3/x"},
		{Name: "Nothing"},
		{Name: "Legato", Repo: "cpave3/other"},
		{Name: "Bad", Repo: "not a repo"},
		{Name: "Many", Repo: "cpave3/*"},
		{Name: "Relative", PrimaryDir: "src/notes"},
		{Name: "Missing", PrimaryDir: "/no/such/folder"},
	} {
		if _, err := svc.Save(ctx, bad); !errors.Is(err, ErrRepositoryInvalid) {
			t.Errorf("%+v: error %v, want ErrRepositoryInvalid", bad, err)
		}
	}
	if _, err := svc.Save(ctx, RepositoryPreset{Name: "Chimera", Repo: "https://github.com/cpave3/chimera.git"}); err != nil {
		t.Fatalf("a GitHub address: %v", err)
	}
	listed, _ := svc.List(ctx)
	if listed[len(listed)-1].Repo != "cpave3/chimera" {
		t.Fatalf("a GitHub address is kept as owner/name: %+v", listed[len(listed)-1])
	}
}

func TestOnlyARepositoryAddedInTheUICanBeRemoved(t *testing.T) {
	svc, _ := newPresetFixture(t)
	ctx := context.Background()
	saved, err := svc.Save(ctx, RepositoryPreset{Name: "Chimera", Repo: "cpave3/chimera"})
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.Delete(ctx, saved.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if err := svc.Delete(ctx, saved.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("deleting twice: %v", err)
	}
	if listed, _ := svc.List(ctx); len(listed) != 1 {
		t.Fatalf("after delete %+v", listed)
	}
}

// statusChecker clones every repository and says each copy is ready.
type statusChecker struct{ ownerChecker }

func (statusChecker) CacheStatus(repo string) RepositoryCacheStatus {
	return RepositoryCacheStatus{Repo: repo, State: CacheCopying, Percent: 40}
}

func TestRepositoriesSayHowTheirCopyStands(t *testing.T) {
	svc, _ := newPresetFixture(t)
	svc.SetCloneChecker(statusChecker{ownerChecker("cpave3")})
	ctx := context.Background()
	if _, err := svc.Save(ctx, RepositoryPreset{Name: "Notes", PrimaryDir: "~/src/notes"}); err != nil {
		t.Fatal(err)
	}
	listed, err := svc.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if listed[0].Cache == nil || listed[0].Cache.State != CacheCopying || listed[0].Cache.Percent != 40 {
		t.Fatalf("Legato's copy = %+v", listed[0].Cache)
	}
	if listed[1].Cache != nil {
		t.Fatalf("a folder with no GitHub repository has a copy: %+v", listed[1].Cache)
	}
}

func TestARepositoryAddedIsCopiedAtOnce(t *testing.T) {
	svc, _ := newPresetFixture(t)
	var added []string
	svc.SetOnAdded(func(repo string) { added = append(added, repo) })
	ctx := context.Background()
	if _, err := svc.Save(ctx, RepositoryPreset{Name: "Chimera", Repo: "https://github.com/cpave3/chimera"}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Save(ctx, RepositoryPreset{Name: "Notes", PrimaryDir: "~/src/notes"}); err != nil {
		t.Fatal(err)
	}
	if len(added) != 1 || added[0] != "cpave3/chimera" {
		t.Fatalf("added = %v, want cpave3/chimera only", added)
	}
}

// A fresh clone's setup copies files from the repository's main checkout:
// the primary_dir of the first repository that names it and has one.
func TestARepositorysMainCheckoutIsItsPrimaryDir(t *testing.T) {
	svc, home := newPresetFixture(t)
	ctx := context.Background()
	if _, err := svc.Save(ctx, RepositoryPreset{Name: "Widget", Repo: "acme/widget", PrimaryDir: "~/src/notes"}); err != nil {
		t.Fatalf("Save: %v", err)
	}

	if got := svc.PrimaryDir("Acme/Widget"); got != filepath.Join(home, "src", "notes") {
		t.Errorf("PrimaryDir(acme/widget) = %q", got)
	}
	if got := svc.PrimaryDir("cpave3/legato"); got != "" {
		t.Errorf("a repository with no primary_dir has %q", got)
	}
	if got := svc.PrimaryDir("someone/else"); got != "" {
		t.Errorf("an unknown repository has %q", got)
	}
}
