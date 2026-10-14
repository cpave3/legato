package overlay

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

func spawnKey(t *testing.T, m AgentSpawnOverlay, keys ...tea.KeyMsg) (AgentSpawnOverlay, tea.Msg) {
	t.Helper()
	var last tea.Msg
	for _, key := range keys {
		next, cmd := m.Update(key)
		m = next.(AgentSpawnOverlay)
		last = nil
		if cmd != nil {
			last = cmd()
		}
	}
	return m, last
}

func runes(text string) tea.KeyMsg { return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(text)} }

var (
	keyEnter    = tea.KeyMsg{Type: tea.KeyEnter}
	keyEsc      = tea.KeyMsg{Type: tea.KeyEsc}
	keyTab      = tea.KeyMsg{Type: tea.KeyTab}
	keyShiftTab = tea.KeyMsg{Type: tea.KeyShiftTab}
	keyUp       = tea.KeyMsg{Type: tea.KeyUp}
	keyDown     = tea.KeyMsg{Type: tea.KeyDown}
	keyRight    = tea.KeyMsg{Type: tea.KeyRight}
	keyBack     = tea.KeyMsg{Type: tea.KeyBackspace}
	keyCtrlW    = tea.KeyMsg{Type: tea.KeyCtrlW}
	keyCtrlS    = tea.KeyMsg{Type: tea.KeyCtrlS}
)

func spawnPresets() []SpawnDir {
	return []SpawnDir{
		{Name: "autopilot", Path: "/home/me/Development/autopilot", Display: "~/Development/autopilot"},
		{Name: "legato", Path: "/home/me/Projects/legato", Display: "~/Projects/legato"},
	}
}

// spawnInPathMode returns an ephemeral overlay whose "where" field is asking
// for a path that starts in the first preset.
func spawnInPathMode(t *testing.T) AgentSpawnOverlay {
	t.Helper()
	m := NewAgentSpawn(nil, "chimera", "/home/me/Development/autopilot", "", "").
		WithLocations(SpawnDir{Path: "/home/me/Development/autopilot"}, spawnPresets()).
		WithSaving()
	m, _ = spawnKey(t, m, keyTab, keyTab, runes("/"))
	return m
}

func suggestions(query string, names ...string) AgentSpawnSuggestionsMsg {
	base := query[:strings.LastIndex(query, "/")+1]
	msg := AgentSpawnSuggestionsMsg{Query: query, Base: base, Prefix: query[len(base):], Total: len(names)}
	for _, name := range names {
		msg.Items = append(msg.Items, SpawnSuggestion{
			Name:    name,
			Path:    "/home/me" + strings.TrimPrefix(base, "~") + name,
			Display: base + name + "/",
		})
	}
	return msg
}

func TestAgentSpawnEphemeralStartsOnTheTitleAndSpawnsInTheDefaultDirectory(t *testing.T) {
	m := NewAgentSpawn([]string{"chimera"}, "openai", "/tmp", "", "")

	view := m.View()
	for _, want := range []string{"Spawn agent", "ephemeral", "title", "agent", "where", "/tmp"} {
		if !strings.Contains(view, want) {
			t.Errorf("view lacks %q:\n%s", want, view)
		}
	}

	_, msg := spawnKey(t, m, keyEnter)
	submit, ok := msg.(AgentSpawnSubmitMsg)
	if !ok {
		t.Fatalf("expected AgentSpawnSubmitMsg, got %T", msg)
	}
	want := AgentSpawnSubmitMsg{Title: "Ephemeral session", WorkingDir: "/tmp"}
	if submit != want {
		t.Fatalf("submit = %+v, want %+v", submit, want)
	}
}

func TestAgentSpawnSubmitsTheTitleThatWasTyped(t *testing.T) {
	m := NewAgentSpawn(nil, "", "/tmp", "", "")

	_, msg := spawnKey(t, m, runes("debug"), tea.KeyMsg{Type: tea.KeySpace}, runes("runs"), keyBack, keyEnter)

	if submit := msg.(AgentSpawnSubmitMsg); submit.Title != "debug run" {
		t.Fatalf("Title = %q", submit.Title)
	}
}

func TestAgentSpawnCancelsOnEscape(t *testing.T) {
	m := NewAgentSpawn(nil, "", "/tmp", "", "")

	_, msg := spawnKey(t, m, keyEsc)

	if _, ok := msg.(AgentSpawnCancelledMsg); !ok {
		t.Fatalf("expected AgentSpawnCancelledMsg, got %T", msg)
	}
}

func TestAgentSpawnForATaskKeepsItsTitleAndStartsOnTheAgent(t *testing.T) {
	m := NewAgentSpawn([]string{"chimera"}, "openai", "/workspace", "REX-1", "Fix login")

	view := m.View()
	if !strings.Contains(view, "REX-1") || strings.Contains(view, "ephemeral") {
		t.Errorf("view should name the task, not an ephemeral session:\n%s", view)
	}

	// The title is context here: typing changes the agent field, not it.
	m, _ = spawnKey(t, m, runes("l"))
	_, msg := spawnKey(t, m, keyEnter)
	want := AgentSpawnSubmitMsg{TaskID: "REX-1", Title: "Fix login", AgentKind: "chimera", WorkingDir: "/workspace"}
	if submit := msg.(AgentSpawnSubmitMsg); submit != want {
		t.Fatalf("submit = %+v, want %+v", submit, want)
	}
}

func TestAgentSpawnForATaskTabsBetweenAgentAndWhereOnly(t *testing.T) {
	m := NewAgentSpawn(nil, "openai", "/workspace", "REX-1", "Fix login")

	m, _ = spawnKey(t, m, keyTab)
	if m.focus != spawnFocusWhere {
		t.Fatalf("after tab: focus = %d, want where", m.focus)
	}
	m, _ = spawnKey(t, m, keyTab)
	if m.focus != spawnFocusAgent {
		t.Fatalf("after second tab: focus = %d, want agent", m.focus)
	}
	m, _ = spawnKey(t, m, keyShiftTab)
	if m.focus != spawnFocusWhere {
		t.Fatalf("after shift+tab: focus = %d, want where", m.focus)
	}
}

func TestAgentSpawnOffersTheDefaultAdapterFirstThenTheOthersThenAShell(t *testing.T) {
	m := NewAgentSpawn([]string{"claude-code", "codex"}, "chimera", "/tmp", "", "")

	view := m.View()
	for _, want := range []string{"chimera", "claude", "codex", "shell"} {
		if !strings.Contains(view, want) {
			t.Errorf("view lacks agent %q:\n%s", want, view)
		}
	}

	for presses, want := range []string{"", "claude-code", "codex", "shell", "shell"} {
		keys := []tea.KeyMsg{keyTab}
		for range presses {
			keys = append(keys, keyRight)
		}
		_, msg := spawnKey(t, m, append(keys, keyEnter)...)
		if got := msg.(AgentSpawnSubmitMsg).AgentKind; got != want {
			t.Errorf("after %d presses: AgentKind = %q, want %q", presses, got, want)
		}
	}
}

func TestAgentSpawnChangesTheAgentWithHAndL(t *testing.T) {
	m := NewAgentSpawn([]string{"codex"}, "chimera", "/tmp", "", "")

	_, msg := spawnKey(t, m, keyTab, runes("l"), runes("l"), runes("h"), keyEnter)

	if got := msg.(AgentSpawnSubmitMsg).AgentKind; got != "codex" {
		t.Fatalf("AgentKind = %q, want codex", got)
	}
}

func TestAgentSpawnListsTheDefaultDirectoryThenThePresets(t *testing.T) {
	m := NewAgentSpawn(nil, "chimera", "/srv/app", "", "").
		WithLocations(SpawnDir{Name: "app", Path: "/srv/app", Display: "/srv/app"}, spawnPresets())

	view := m.View()
	for _, want := range []string{"1 ▸ app", "2   autopilot", "~/Development/autopilot", "3   legato", "/   custom path…"} {
		if !strings.Contains(view, want) {
			t.Errorf("view lacks %q:\n%s", want, view)
		}
	}

	// Focus "where", then pick the third row by its number.
	_, msg := spawnKey(t, m, keyTab, keyTab, runes("3"), keyEnter)
	if got := msg.(AgentSpawnSubmitMsg).WorkingDir; got != "/home/me/Projects/legato" {
		t.Fatalf("WorkingDir = %q", got)
	}
}

func TestAgentSpawnSelectsTheDefaultDirectoryWhenItIsAPreset(t *testing.T) {
	m := NewAgentSpawn(nil, "chimera", "/home/me/Projects/legato", "", "").
		WithLocations(SpawnDir{Path: "/home/me/Projects/legato"}, spawnPresets())

	if len(m.rows) != 2 || m.row != 1 {
		t.Fatalf("rows = %+v, row = %d", m.rows, m.row)
	}

	// Up moves to the first preset; h and l still change the agent here.
	_, msg := spawnKey(t, m, keyTab, keyTab, keyUp, keyEnter)
	if got := msg.(AgentSpawnSubmitMsg).WorkingDir; got != "/home/me/Development/autopilot" {
		t.Fatalf("WorkingDir = %q", got)
	}
}

func TestAgentSpawnDigitsInTheTitleAreTyped(t *testing.T) {
	m := NewAgentSpawn(nil, "chimera", "/srv/app", "", "").
		WithLocations(SpawnDir{Path: "/srv/app"}, spawnPresets())

	_, msg := spawnKey(t, m, runes("2"), keyEnter)

	submit := msg.(AgentSpawnSubmitMsg)
	if submit.Title != "2" || submit.WorkingDir != "/srv/app" {
		t.Fatalf("submit = %+v", submit)
	}
}

func TestAgentSpawnSlashAsksForAPathInsideTheSelectedDirectory(t *testing.T) {
	m := NewAgentSpawn(nil, "chimera", "/home/me/Development/autopilot", "", "").
		WithLocations(SpawnDir{Path: "/home/me/Development/autopilot"}, spawnPresets())

	m, msg := spawnKey(t, m, keyTab, keyTab, runes("/"))

	if !m.pathMode || m.path != "~/Development/autopilot/" {
		t.Fatalf("pathMode = %v, path = %q", m.pathMode, m.path)
	}
	if changed, ok := msg.(AgentSpawnPathChangedMsg); !ok || changed.Query != "~/Development/autopilot/" {
		t.Fatalf("msg = %#v", msg)
	}
}

func TestAgentSpawnEnterOnTheCustomRowAsksForAPath(t *testing.T) {
	m := NewAgentSpawn(nil, "chimera", "/srv/app", "", "").
		WithLocations(SpawnDir{Path: "/srv/app", Display: "/srv/app"}, nil)

	m, msg := spawnKey(t, m, keyTab, keyTab, keyDown, keyEnter)

	if !m.pathMode || m.path != "/srv/app/" {
		t.Fatalf("pathMode = %v, path = %q", m.pathMode, m.path)
	}
	if _, ok := msg.(AgentSpawnPathChangedMsg); !ok {
		t.Fatalf("msg = %#v", msg)
	}
}

func TestAgentSpawnTypingAPathAsksForSuggestionsAndSpawnsInTheFirst(t *testing.T) {
	m := spawnInPathMode(t)

	m, msg := spawnKey(t, m, keyCtrlW, runes("auto"))
	if changed, ok := msg.(AgentSpawnPathChangedMsg); !ok || changed.Query != "~/Development/auto" {
		t.Fatalf("msg = %#v", msg)
	}
	next, _ := m.Update(suggestions("~/Development/auto", "autopilot", "automation"))
	m = next.(AgentSpawnOverlay)

	_, msg = spawnKey(t, m, keyEnter)
	if got := msg.(AgentSpawnSubmitMsg).WorkingDir; got != "/home/me/Development/autopilot" {
		t.Fatalf("WorkingDir = %q", got)
	}

	_, msg = spawnKey(t, m, keyDown, keyEnter)
	if got := msg.(AgentSpawnSubmitMsg).WorkingDir; got != "/home/me/Development/automation" {
		t.Fatalf("after down: WorkingDir = %q", got)
	}
}

func TestAgentSpawnAPathThatEndsInASlashIsTheDirectoryItself(t *testing.T) {
	m := spawnInPathMode(t)
	next, _ := m.Update(suggestions("~/Development/autopilot/", "docs", "src"))
	m = next.(AgentSpawnOverlay)

	_, msg := spawnKey(t, m, keyEnter)
	if got := msg.(AgentSpawnSubmitMsg).WorkingDir; got != "~/Development/autopilot/" {
		t.Fatalf("WorkingDir = %q", got)
	}

	// Down picks a directory inside it, and up returns to the one typed.
	_, msg = spawnKey(t, m, keyDown, keyDown, keyEnter)
	if got := msg.(AgentSpawnSubmitMsg).WorkingDir; got != "/home/me/Development/autopilot/src" {
		t.Fatalf("after down, down: WorkingDir = %q", got)
	}
	_, msg = spawnKey(t, m, keyDown, keyUp, keyEnter)
	if got := msg.(AgentSpawnSubmitMsg).WorkingDir; got != "~/Development/autopilot/" {
		t.Fatalf("after down, up: WorkingDir = %q", got)
	}
}

func TestAgentSpawnTabCompletesThePathToThePickedSuggestion(t *testing.T) {
	m := spawnInPathMode(t)
	m, _ = spawnKey(t, m, keyCtrlW, runes("auto"))
	next, _ := m.Update(suggestions("~/Development/auto", "autopilot", "automation"))
	m = next.(AgentSpawnOverlay)

	m, msg := spawnKey(t, m, keyDown, keyTab)

	if m.path != "~/Development/automation/" || m.focus != spawnFocusWhere {
		t.Fatalf("path = %q, focus = %d", m.path, m.focus)
	}
	if changed, ok := msg.(AgentSpawnPathChangedMsg); !ok || changed.Query != "~/Development/automation/" {
		t.Fatalf("msg = %#v", msg)
	}
}

func TestAgentSpawnCtrlWGoesUpADirectory(t *testing.T) {
	m := spawnInPathMode(t)

	for _, want := range []string{"~/Development/", "~/", "~/"} {
		m, _ = spawnKey(t, m, keyCtrlW)
		if m.path != want {
			t.Fatalf("path = %q, want %q", m.path, want)
		}
	}
}

func TestAgentSpawnIgnoresSuggestionsForAPathThatHasChanged(t *testing.T) {
	m := spawnInPathMode(t)
	m, _ = spawnKey(t, m, keyCtrlW, runes("auto"))

	next, _ := m.Update(suggestions("~/Development/aut", "authz"))
	m = next.(AgentSpawnOverlay)

	_, msg := spawnKey(t, m, keyEnter)
	if got := msg.(AgentSpawnSubmitMsg).WorkingDir; got != "~/Development/auto" {
		t.Fatalf("WorkingDir = %q", got)
	}
}

func TestAgentSpawnEscapeInAPathGoesBackToThePresets(t *testing.T) {
	m := spawnInPathMode(t)

	m, msg := spawnKey(t, m, keyEsc)
	if m.pathMode || msg != nil {
		t.Fatalf("pathMode = %v, msg = %#v", m.pathMode, msg)
	}

	_, msg = spawnKey(t, m, keyEsc)
	if _, ok := msg.(AgentSpawnCancelledMsg); !ok {
		t.Fatalf("expected AgentSpawnCancelledMsg, got %T", msg)
	}
}

func TestAgentSpawnShiftTabLeavesAPathForTheAgentField(t *testing.T) {
	m := spawnInPathMode(t)

	m, _ = spawnKey(t, m, keyShiftTab)

	if m.focus != spawnFocusAgent {
		t.Fatalf("focus = %d, want agent", m.focus)
	}
}

func TestAgentSpawnCtrlSSavesThePathAsAPreset(t *testing.T) {
	m := spawnInPathMode(t)
	m, _ = spawnKey(t, m, keyCtrlW, runes("auto"))
	next, _ := m.Update(suggestions("~/Development/auto", "automation"))
	m = next.(AgentSpawnOverlay)
	if view := m.View(); !strings.Contains(view, "ctrl+s") {
		t.Fatalf("view should offer to save the path:\n%s", view)
	}

	m, msg := spawnKey(t, m, keyCtrlS)
	save, ok := msg.(AgentSpawnSavePresetMsg)
	if !ok || save.Path != "/home/me/Development/automation" {
		t.Fatalf("msg = %#v", msg)
	}

	saved := SpawnDir{Name: "automation", Path: "/home/me/Development/automation", Display: "~/Development/automation"}
	next, _ = m.Update(AgentSpawnPresetsMsg{Presets: append(spawnPresets(), saved), Saved: saved.Path})
	m = next.(AgentSpawnOverlay)
	view := m.View()
	if !strings.Contains(view, "saved as preset 3") || strings.Contains(view, "ctrl+s") {
		t.Fatalf("view should say the path was saved, and stop offering it:\n%s", view)
	}
	if len(m.rows) != 3 {
		t.Fatalf("rows = %+v", m.rows)
	}
}

func TestAgentSpawnSaysWhyAPresetWasNotSaved(t *testing.T) {
	m := spawnInPathMode(t)

	next, _ := m.Update(AgentSpawnPresetsMsg{Err: "~/nowhere is not a directory"})

	if view := next.(AgentSpawnOverlay).View(); !strings.Contains(view, "~/nowhere is not a directory") {
		t.Fatalf("view lacks the reason:\n%s", view)
	}
}

func TestAgentSpawnDoesNotOfferToSaveWithoutSaving(t *testing.T) {
	m := NewAgentSpawn(nil, "chimera", "/srv/app", "", "")
	m, _ = spawnKey(t, m, keyTab, keyTab, runes("/"))

	_, msg := spawnKey(t, m, keyCtrlS)

	if msg != nil || strings.Contains(m.View(), "ctrl+s") {
		t.Fatalf("msg = %#v, view:\n%s", msg, m.View())
	}
}

func TestAgentSpawnShowsWhatASuggestionIs(t *testing.T) {
	m := spawnInPathMode(t)
	m, _ = spawnKey(t, m, keyCtrlW, keyCtrlW, runes("P"))
	msg := suggestions("~/P", "Projects", "Pictures")
	msg.Items[0].Git = true
	msg.Total = 7
	next, _ := m.Update(msg)
	m = next.(AgentSpawnOverlay)
	next, _ = m.Update(tea.WindowSizeMsg{Width: 110, Height: 34})
	m = next.(AgentSpawnOverlay)

	view := m.View()
	for _, want := range []string{"~/Projects/", "git", "dir", "2 of 7 matches", "→ chimera in ~/Projects/", "tab complete", "esc back to presets"} {
		if !strings.Contains(view, want) {
			t.Errorf("view lacks %q:\n%s", want, view)
		}
	}

	// A suggestion that is one of the listed directories says which.
	next, _ = m.Update(suggestions("~/P", "Projects/legato"))
	m = next.(AgentSpawnOverlay)
	if view := m.View(); !strings.Contains(view, "preset 2") {
		t.Errorf("view lacks the preset's number:\n%s", view)
	}
}

func TestAgentSpawnViewIsExactlyTheSizeItWasGiven(t *testing.T) {
	for _, size := range []tea.WindowSizeMsg{{Width: 120, Height: 40}, {Width: 72, Height: 20}, {Width: 44, Height: 14}} {
		for name, m := range map[string]AgentSpawnOverlay{
			"presets": NewAgentSpawn([]string{"claude-code", "codex"}, "chimera", "/srv/app", "", "a title").
				WithLocations(SpawnDir{Path: "/srv/app"}, spawnPresets()),
			"path": spawnInPathMode(t),
		} {
			next, _ := m.Update(size)
			lines := strings.Split(next.(AgentSpawnOverlay).View(), "\n")
			if len(lines) != size.Height {
				t.Errorf("%s at %dx%d: %d lines", name, size.Width, size.Height, len(lines))
			}
			for i, line := range lines {
				if width := lipgloss.Width(line); width != size.Width {
					t.Errorf("%s at %dx%d: line %d is %d wide", name, size.Width, size.Height, i, width)
					break
				}
			}
		}
	}
}
