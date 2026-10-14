package tui

import (
	"context"
	"errors"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/cpave3/legato/internal/service"
	"github.com/cpave3/legato/internal/tui/agents"
	"github.com/cpave3/legato/internal/tui/theme"
)

// fakeDirectoryService stands for a home directory at /home/me.
type fakeDirectoryService struct {
	presets []service.DirectoryPreset
	found   []service.DirectorySuggestion
	queries []string
	saveErr error
}

func (f *fakeDirectoryService) Presets(context.Context) ([]service.DirectoryPreset, error) {
	return f.presets, nil
}

func (f *fakeDirectoryService) SavePreset(_ context.Context, path, _ string) (service.DirectoryPreset, error) {
	if f.saveErr != nil {
		return service.DirectoryPreset{}, f.saveErr
	}
	path = f.Expand(path)
	preset := service.DirectoryPreset{Name: path[strings.LastIndex(path, "/")+1:], Path: path, Display: f.Display(path), Saved: true}
	f.presets = append(f.presets, preset)
	return preset, nil
}

func (f *fakeDirectoryService) Suggest(_ context.Context, query string, _ int) (service.DirectorySuggestions, error) {
	f.queries = append(f.queries, query)
	return service.DirectorySuggestions{Query: query, Base: "~/", Prefix: strings.TrimPrefix(query, "~/"), Items: f.found, Total: len(f.found)}, nil
}

func (f *fakeDirectoryService) Expand(path string) string {
	path = strings.TrimSuffix(strings.TrimSpace(path), "/")
	if rest, ok := strings.CutPrefix(path, "~"); ok {
		return "/home/me" + rest
	}
	return path
}

func (f *fakeDirectoryService) Display(path string) string {
	if rest, ok := strings.CutPrefix(path, "/home/me"); ok {
		return "~" + rest
	}
	return path
}

func newSpawnApp(t *testing.T, dirs *fakeDirectoryService) (App, *fakeAgentService) {
	t.Helper()
	agentSvc := &fakeAgentService{captureErr: true, defaultAdapter: "chimera"}
	app := NewApp(&fakeBoardService{}, nil, agentSvc, nil, &fakeReportService{}, theme.NewIcons("unicode"), nil, "", nil, nil, "/home/me/Projects/legato", nil, nil)
	if dirs != nil {
		app.SetDirectoryService(dirs)
	}
	app, _ = updateApp(app, tea.WindowSizeMsg{Width: 110, Height: 32})
	app, _ = updateApp(app, agents.OpenAgentSpawnMsg{})
	if app.overlayType != overlayAgentSpawn {
		t.Fatalf("expected the spawn overlay, got overlay %d", app.overlayType)
	}
	return app, agentSvc
}

// spawnOverlayKeys sends keys to the open overlay and delivers what each one
// asks the app for, and what the app answers.
func spawnOverlayKeys(t *testing.T, app App, keys ...tea.KeyMsg) App {
	t.Helper()
	for _, key := range keys {
		var cmd tea.Cmd
		app, cmd = updateApp(app, key)
		for depth := 0; cmd != nil && depth < 4; depth++ {
			app, cmd = updateApp(app, cmd())
		}
	}
	return app
}

// spawnWithEnter confirms the overlay and runs the spawn it starts.
func spawnWithEnter(t *testing.T, app App) App {
	t.Helper()
	app, cmd := updateApp(app, tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("enter should submit the overlay")
	}
	app, cmd = updateApp(app, cmd())
	if app.overlayType != overlayNone {
		t.Fatal("overlay should close after the spawn")
	}
	if cmd == nil {
		t.Fatal("expected a spawn command")
	}
	for _, each := range cmd().(tea.BatchMsg) {
		each()
	}
	return app
}

func typed(text string) tea.KeyMsg { return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(text)} }

func TestSpawnOverlayListsTheDirectoryPresets(t *testing.T) {
	dirs := &fakeDirectoryService{presets: []service.DirectoryPreset{
		{Name: "autopilot", Path: "/home/me/Development/autopilot", Display: "~/Development/autopilot"},
		{Name: "Legato", Path: "/home/me/Projects/legato", Display: "~/Projects/legato"},
	}}
	app, agentSvc := newSpawnApp(t, dirs)

	view := app.View()
	for _, want := range []string{"1   autopilot", "2 ▸ Legato", "~/Projects/legato"} {
		if !strings.Contains(view, want) {
			t.Errorf("view lacks %q:\n%s", want, view)
		}
	}

	// Focus "where", pick the first preset and spawn.
	tab := tea.KeyMsg{Type: tea.KeyTab}
	app = spawnOverlayKeys(t, app, tab, tab, typed("1"))
	spawnWithEnter(t, app)
	if agentSvc.lastSpawnOpts == nil || agentSvc.lastSpawnOpts.WorkingDir != "/home/me/Development/autopilot" {
		t.Fatalf("spawn options = %+v", agentSvc.lastSpawnOpts)
	}
}

func TestSpawnOverlayWithoutPresetsStartsInTheWorkingDirectory(t *testing.T) {
	app, agentSvc := newSpawnApp(t, nil)

	if view := app.View(); !strings.Contains(view, "1 ▸ legato") {
		t.Errorf("view should list the working directory:\n%s", view)
	}

	spawnWithEnter(t, app)
	if agentSvc.lastSpawnOpts == nil || agentSvc.lastSpawnOpts.WorkingDir != "/home/me/Projects/legato" {
		t.Fatalf("spawn options = %+v", agentSvc.lastSpawnOpts)
	}
}

func TestSpawnOverlaySuggestsDirectoriesForATypedPath(t *testing.T) {
	dirs := &fakeDirectoryService{found: []service.DirectorySuggestion{
		{Name: "autopilot", Path: "/home/me/autopilot", Display: "~/autopilot/", Git: true},
	}}
	app, agentSvc := newSpawnApp(t, dirs)
	tab := tea.KeyMsg{Type: tea.KeyTab}
	ctrlW := tea.KeyMsg{Type: tea.KeyCtrlW}

	app = spawnOverlayKeys(t, app, tab, tab, typed("/"), ctrlW, ctrlW, typed("au"))

	if last := dirs.queries[len(dirs.queries)-1]; last != "~/au" {
		t.Fatalf("queries = %q", dirs.queries)
	}
	if view := app.View(); !strings.Contains(view, "~/autopilot/") || !strings.Contains(view, "git") {
		t.Fatalf("view lacks the suggestion:\n%s", view)
	}

	spawnWithEnter(t, app)
	if agentSvc.lastSpawnOpts == nil || agentSvc.lastSpawnOpts.WorkingDir != "/home/me/autopilot" {
		t.Fatalf("spawn options = %+v", agentSvc.lastSpawnOpts)
	}
}

func TestSpawnOverlayExpandsATypedPathBeforeSpawning(t *testing.T) {
	dirs := &fakeDirectoryService{}
	app, agentSvc := newSpawnApp(t, dirs)
	tab := tea.KeyMsg{Type: tea.KeyTab}

	// "/" starts the path inside the working directory; nothing completes it.
	app = spawnOverlayKeys(t, app, tab, tab, typed("/"), typed("web"))
	spawnWithEnter(t, app)

	if agentSvc.lastSpawnOpts == nil || agentSvc.lastSpawnOpts.WorkingDir != "/home/me/Projects/legato/web" {
		t.Fatalf("spawn options = %+v", agentSvc.lastSpawnOpts)
	}
}

func TestSpawnOverlaySavesATypedPathAsAPreset(t *testing.T) {
	dirs := &fakeDirectoryService{}
	app, _ := newSpawnApp(t, dirs)
	tab := tea.KeyMsg{Type: tea.KeyTab}

	app = spawnOverlayKeys(t, app, tab, tab, typed("/"), typed("web/"), tea.KeyMsg{Type: tea.KeyCtrlS})

	if len(dirs.presets) != 1 || dirs.presets[0].Path != "/home/me/Projects/legato/web" {
		t.Fatalf("presets = %+v", dirs.presets)
	}
	if view := app.View(); !strings.Contains(view, "saved as preset 2") {
		t.Fatalf("view should say the path was saved:\n%s", view)
	}

	dirs.saveErr = errors.New("~/x is not a directory")
	app = spawnOverlayKeys(t, app, tea.KeyMsg{Type: tea.KeyCtrlW}, typed("docs"), tea.KeyMsg{Type: tea.KeyCtrlS})
	if view := app.View(); !strings.Contains(view, "~/x is not a directory") {
		t.Fatalf("view should say why the path was not saved:\n%s", view)
	}
}
