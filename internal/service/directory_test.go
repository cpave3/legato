package service_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cpave3/legato/internal/service"
)

// directoryTree makes each named directory under a new home and returns the
// home. A name that ends in "/.git" makes its parent a repository.
func directoryTree(t *testing.T, dirs ...string) string {
	t.Helper()
	home := t.TempDir()
	for _, dir := range dirs {
		if err := os.MkdirAll(filepath.Join(home, dir), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return home
}

func suggestionNames(result service.DirectorySuggestions) string {
	names := make([]string, len(result.Items))
	for i, item := range result.Items {
		names[i] = item.Name
	}
	return strings.Join(names, " ")
}

func TestSuggestListsTheDirectoriesThatStartWithWhatWasTyped(t *testing.T) {
	home := directoryTree(t, "Development/automation", "Development/autopilot", "Development/other")
	if err := os.WriteFile(filepath.Join(home, "Development", "auto.txt"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	svc := service.NewDirectoryService(nil, nil, home)

	result, err := svc.Suggest(context.Background(), "~/Development/auto", 8)
	if err != nil {
		t.Fatal(err)
	}

	if got := suggestionNames(result); got != "automation autopilot" {
		t.Fatalf("suggestions = %q", got)
	}
	if result.Base != "~/Development/" || result.Prefix != "auto" {
		t.Fatalf("base = %q, prefix = %q", result.Base, result.Prefix)
	}
	first := result.Items[0]
	if first.Path != filepath.Join(home, "Development", "automation") || first.Display != "~/Development/automation/" {
		t.Fatalf("first = %+v", first)
	}
}

func TestSuggestPutsRepositoriesFirstAndPresetsBeforeThem(t *testing.T) {
	home := directoryTree(t,
		"dev/alpha",
		"dev/beta/.git",
		"dev/delta/.git",
		"dev/gamma/.git",
	)
	presets := []service.DirectoryPreset{
		{Name: "elsewhere", Path: filepath.Join(home, "elsewhere")},
		{Name: "gamma", Path: filepath.Join(home, "dev", "gamma")},
	}
	svc := service.NewDirectoryService(nil, presets, home)

	result, err := svc.Suggest(context.Background(), "~/dev/", 8)
	if err != nil {
		t.Fatal(err)
	}

	if got := suggestionNames(result); got != "gamma beta delta alpha" {
		t.Fatalf("suggestions = %q", got)
	}
	gamma, alpha := result.Items[0], result.Items[3]
	if !gamma.Git || gamma.Preset != 2 {
		t.Fatalf("gamma = %+v", gamma)
	}
	if alpha.Git || alpha.Preset != 0 {
		t.Fatalf("alpha = %+v", alpha)
	}
}

func TestSuggestPutsTheDirectoryThatWasTypedInFullFirst(t *testing.T) {
	home := directoryTree(t, "p/legato", "p/legato-old/.git")
	svc := service.NewDirectoryService(nil, nil, home)

	result, err := svc.Suggest(context.Background(), "~/p/legato", 8)
	if err != nil {
		t.Fatal(err)
	}

	if got := suggestionNames(result); got != "legato legato-old" {
		t.Fatalf("suggestions = %q", got)
	}
}

func TestSuggestShowsHiddenDirectoriesOnlyWhenADotWasTyped(t *testing.T) {
	home := directoryTree(t, ".config", "code")
	svc := service.NewDirectoryService(nil, nil, home)
	ctx := context.Background()

	all, err := svc.Suggest(ctx, "~/", 8)
	if err != nil {
		t.Fatal(err)
	}
	if got := suggestionNames(all); got != "code" {
		t.Fatalf("suggestions = %q", got)
	}

	hidden, err := svc.Suggest(ctx, "~/.c", 8)
	if err != nil {
		t.Fatal(err)
	}
	if got := suggestionNames(hidden); got != ".config" {
		t.Fatalf("suggestions = %q", got)
	}
}

func TestSuggestKeepsToTheLimitAndSaysHowManyMatched(t *testing.T) {
	home := directoryTree(t, "a1", "a2", "a3", "b1")
	svc := service.NewDirectoryService(nil, nil, home)

	result, err := svc.Suggest(context.Background(), "~/a", 2)
	if err != nil {
		t.Fatal(err)
	}

	if got := suggestionNames(result); got != "a1 a2" || result.Total != 3 {
		t.Fatalf("suggestions = %q, total = %d", got, result.Total)
	}
}

func TestSuggestSaysWhetherWhatWasTypedIsADirectory(t *testing.T) {
	home := directoryTree(t, "work/app")
	svc := service.NewDirectoryService(nil, nil, home)
	ctx := context.Background()

	there, err := svc.Suggest(ctx, "~/work/app/", 8)
	if err != nil {
		t.Fatal(err)
	}
	if !there.Exists || there.Resolved != filepath.Join(home, "work", "app") {
		t.Fatalf("there = %+v", there)
	}

	missing, err := svc.Suggest(ctx, "~/work/nothing/here", 8)
	if err != nil {
		t.Fatal(err)
	}
	if missing.Exists || len(missing.Items) != 0 {
		t.Fatalf("missing = %+v", missing)
	}
}

func TestSuggestMatchesWithoutRegardToCase(t *testing.T) {
	home := directoryTree(t, "Projects", "private")
	svc := service.NewDirectoryService(nil, nil, home)

	result, err := svc.Suggest(context.Background(), "~/pr", 8)
	if err != nil {
		t.Fatal(err)
	}

	if got := suggestionNames(result); got != "private Projects" {
		t.Fatalf("suggestions = %q", got)
	}
}

func TestExpandAndDisplayTurnTheHomeDirectoryIntoATildeAndBack(t *testing.T) {
	svc := service.NewDirectoryService(nil, nil, "/home/me")

	for typed, want := range map[string]string{
		"~":                 "/home/me",
		"~/Projects/legato": "/home/me/Projects/legato",
		"~/Projects/x/":     "/home/me/Projects/x",
		"/srv/app":          "/srv/app",
		"  /srv/app  ":      "/srv/app",
		"":                  "",
	} {
		if got := svc.Expand(typed); got != want {
			t.Errorf("Expand(%q) = %q, want %q", typed, got, want)
		}
	}
	for path, want := range map[string]string{
		"/home/me":           "~",
		"/home/me/Projects":  "~/Projects",
		"/home/merlin/notes": "/home/merlin/notes",
		"/srv/app":           "/srv/app",
	} {
		if got := svc.Display(path); got != want {
			t.Errorf("Display(%q) = %q, want %q", path, got, want)
		}
	}
}

func TestPresetsListTheConfiguredDirectoriesThenTheSavedOnes(t *testing.T) {
	home := directoryTree(t, "Projects/legato", "Development/autopilot")
	configured := []service.DirectoryPreset{{Name: "Legato", Path: "~/Projects/legato"}}
	svc := service.NewDirectoryService(newTestStoreForReport(t), configured, home)
	ctx := context.Background()

	saved, err := svc.SavePreset(ctx, "~/Development/autopilot/", "")
	if err != nil {
		t.Fatal(err)
	}
	if saved.Name != "autopilot" || saved.Display != "~/Development/autopilot" || !saved.Saved {
		t.Fatalf("saved = %+v", saved)
	}

	presets, err := svc.Presets(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(presets) != 2 {
		t.Fatalf("presets = %+v", presets)
	}
	if presets[0].Name != "Legato" || presets[0].Path != filepath.Join(home, "Projects", "legato") || presets[0].Saved {
		t.Fatalf("configured = %+v", presets[0])
	}
	if presets[1] != saved {
		t.Fatalf("saved preset = %+v, want %+v", presets[1], saved)
	}
}

func TestSavingADirectoryThatIsAlreadyAPresetAddsNothing(t *testing.T) {
	home := directoryTree(t, "Projects/legato", "work")
	configured := []service.DirectoryPreset{{Name: "Legato", Path: "~/Projects/legato"}}
	svc := service.NewDirectoryService(newTestStoreForReport(t), configured, home)
	ctx := context.Background()

	again, err := svc.SavePreset(ctx, "~/Projects/legato", "another name")
	if err != nil {
		t.Fatal(err)
	}
	if again.Name != "Legato" || again.Saved {
		t.Fatalf("again = %+v", again)
	}
	for range 2 {
		if _, err := svc.SavePreset(ctx, "~/work", ""); err != nil {
			t.Fatal(err)
		}
	}

	presets, err := svc.Presets(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(presets) != 2 {
		t.Fatalf("presets = %+v", presets)
	}
}

func TestSavePresetRefusesWhatIsNotADirectory(t *testing.T) {
	home := directoryTree(t)
	svc := service.NewDirectoryService(newTestStoreForReport(t), nil, home)

	_, err := svc.SavePreset(context.Background(), "~/nowhere", "")

	if err == nil || !strings.Contains(err.Error(), "~/nowhere is not a directory") {
		t.Fatalf("err = %v", err)
	}
}
