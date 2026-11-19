package setup

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/cpave3/legato/internal/service"
)

// gitRepo makes dir a repository whose origin is remote ("" for none),
// last used at used.
func gitRepo(t *testing.T, dir, remote string, used time.Time) {
	t.Helper()
	git := filepath.Join(dir, ".git")
	if err := os.MkdirAll(git, 0o755); err != nil {
		t.Fatal(err)
	}
	config := "[core]\n\tbare = false\n"
	if remote != "" {
		config += "[remote \"origin\"]\n\turl = " + remote + "\n\tfetch = +refs/heads/*:refs/remotes/origin/*\n"
	}
	for name, body := range map[string]string{"config": config, "HEAD": "ref: refs/heads/main\n"} {
		path := filepath.Join(git, name)
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(path, used, used); err != nil {
			t.Fatal(err)
		}
	}
}

var setupNow = time.Date(2026, 11, 19, 12, 0, 0, 0, time.UTC)

func repoTree(t *testing.T) (home string) {
	t.Helper()
	home = t.TempDir()
	gitRepo(t, filepath.Join(home, "Projects", "legato"), "git@github.com:cpave3/legato.git", setupNow.Add(-time.Hour))
	gitRepo(t, filepath.Join(home, "Projects", "acme", "api"), "https://github.com/acme/api.git", setupNow.AddDate(0, -3, 0))
	gitRepo(t, filepath.Join(home, "code", "notes"), "", setupNow.Add(-48*time.Hour))
	// A repository inside one is not offered on its own.
	gitRepo(t, filepath.Join(home, "Projects", "legato", "vendor", "inner"), "", setupNow)
	if err := os.MkdirAll(filepath.Join(home, "Projects", "not-a-repo", "docs"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(home, "Projects", "node_modules", "x", ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	return home
}

func TestFindRepositoriesUnderTheUsualFolders(t *testing.T) {
	home := repoTree(t)
	found := FindRepositories([]string{filepath.Join(home, "Projects"), filepath.Join(home, "src"), filepath.Join(home, "code")}, setupNow)
	var dirs []string
	for _, f := range found {
		dirs = append(dirs, strings.TrimPrefix(f.Dir, home+"/"))
	}
	want := []string{"Projects/acme/api", "Projects/legato", "code/notes"}
	if !slices.Equal(dirs, want) {
		t.Fatalf("found %v, want %v", dirs, want)
	}
	byDir := map[string]FoundRepository{}
	for _, f := range found {
		byDir[strings.TrimPrefix(f.Dir, home+"/")] = f
	}
	if r := byDir["Projects/legato"]; r.Repo != "cpave3/legato" || !r.Recent {
		t.Errorf("legato = %+v", r)
	}
	if r := byDir["Projects/acme/api"]; r.Repo != "acme/api" || r.Recent {
		t.Errorf("api = %+v", r)
	}
	if r := byDir["code/notes"]; r.Repo != "" || !r.Recent {
		t.Errorf("notes = %+v", r)
	}
}

type fakePresets struct {
	presets []service.RepositoryPreset
	saved   []service.RepositoryPreset
}

func (f *fakePresets) List(context.Context) ([]service.RepositoryPreset, error) {
	return f.presets, nil
}

func (f *fakePresets) Save(_ context.Context, p service.RepositoryPreset) (service.RepositoryPreset, error) {
	f.saved = append(f.saved, p)
	f.presets = append(f.presets, p)
	return p, nil
}

func runRepositoryStep(t *testing.T, home, input string, presets *fakePresets) *setupRun {
	t.Helper()
	return runSetup(t, "s\n"+input+"s\n", nil, []service.DoctorReport{machine(nil)}, func(r *setupRun) {
		r.repositories = presets
		r.repositoryRoots = []string{filepath.Join(home, "Projects"), filepath.Join(home, "code"), filepath.Join(home, "missing")}
	})
}

func TestSetupSavesTheTickedRepositoriesAsPresets(t *testing.T) {
	home := repoTree(t)
	presets := &fakePresets{}
	// Recent ones come ticked: legato and notes. Untick notes (3), tick
	// api (1), then save.
	r := runRepositoryStep(t, home, "3 1\n\n", presets)
	if r.err != nil {
		t.Fatalf("setup = %v\n%s", r.err, r.out.String())
	}
	var names []string
	for _, p := range presets.saved {
		names = append(names, p.Name+"="+p.Repo+"@"+strings.TrimPrefix(p.PrimaryDir, home+"/"))
	}
	want := []string{"api=acme/api@Projects/acme/api", "legato=cpave3/legato@Projects/legato"}
	if !slices.Equal(names, want) {
		t.Fatalf("saved %v, want %v\n%s", names, want, r.out.String())
	}
}

func TestSetupKeepsTheRepositoriesAlreadySaved(t *testing.T) {
	home := repoTree(t)
	presets := &fakePresets{presets: []service.RepositoryPreset{{Name: "legato", Repo: "cpave3/legato", PrimaryDir: filepath.Join(home, "Projects", "legato")}}}
	r := runRepositoryStep(t, home, "\n", presets)
	if r.err != nil {
		t.Fatal(r.err)
	}
	if !strings.Contains(r.out.String(), "legato (saved)") {
		t.Errorf("the saved repository is not shown:\n%s", r.out.String())
	}
	for _, p := range presets.saved {
		if p.Name == "legato" {
			t.Fatalf("saved legato again: %+v", presets.saved)
		}
	}
}

func TestTheRepositoryStepCanBeSkipped(t *testing.T) {
	home := repoTree(t)
	presets := &fakePresets{}
	r := runRepositoryStep(t, home, "s\n", presets)
	if r.err != nil || len(presets.saved) != 0 {
		t.Fatalf("err %v, saved %+v", r.err, presets.saved)
	}
}
