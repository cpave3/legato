package service_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cpave3/legato/internal/engine/libretto"
	"github.com/cpave3/legato/internal/service"
)

// writeStoryboard puts a storyboard file in a new folder and returns its path.
func writeStoryboard(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "storyboard.json")
	if err := os.WriteFile(path, []byte(`{"scenes":[{"id":"intro","card":{"title":"Hello"},"notes":"Say hello."}]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func showStudio(ui *service.AgentUIService, storyboard, key string) (*service.AgentUIItem, error) {
	return ui.Show(context.Background(), service.AgentUIShowInput{
		TaskID: "task-1", Placement: service.AgentUIPanel, Format: service.AgentUIFormatStudio,
		Title: "Talk", Key: key, Content: service.AgentUIStudioContent(storyboard, ""),
	})
}

func TestAgentUIShowKeepsWhereAStudiosStoryboardIs(t *testing.T) {
	ui := service.NewAgentUIService(newTestStoreForReport(t), nil, service.AgentUIOptions{})
	path := writeStoryboard(t)
	item, err := showStudio(ui, path, "talk")
	if err != nil {
		t.Fatal(err)
	}
	if item.Format != service.AgentUIFormatStudio || item.Placement != service.AgentUIPanel {
		t.Errorf("item = %+v", item)
	}
	// The storyboard is not copied: it is the file the agent and the studio
	// both go on editing, so the item only says where it is.
	got, err := ui.Get(context.Background(), item.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	if where, ok := service.AgentUIStoryboard(got); !ok || where != path {
		t.Errorf("storyboard = %q, %v; want %q", where, ok, path)
	}
	// Showing it again under the same key is the same studio, not another.
	again, err := showStudio(ui, path, "talk")
	if err != nil || again.ID != item.ID {
		t.Errorf("shown again: %+v, err %v; want the same item", again, err)
	}
	// The agent can put one scene in front of the person: the item says
	// which, and stays the same studio on the same storyboard.
	focused, err := ui.Show(context.Background(), service.AgentUIShowInput{
		TaskID: "task-1", Placement: service.AgentUIPanel, Format: service.AgentUIFormatStudio,
		Title: "Talk", Key: "talk", Content: service.AgentUIStudioContent(path, "intro"),
	})
	if err != nil || focused.ID != item.ID {
		t.Fatalf("shown on a scene: %+v, err %v", focused, err)
	}
	got, err = ui.Get(context.Background(), item.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	if where, ok := service.AgentUIStoryboard(got); !ok || where != path || !strings.Contains(got.Content, `"scene":"intro"`) {
		t.Errorf("after naming a scene the item holds %s, storyboard %q, %v", got.Content, where, ok)
	}
}

func TestAgentUIShowRejectsAStudioWithNoStoryboardToOpen(t *testing.T) {
	ui := service.NewAgentUIService(newTestStoreForReport(t), nil, service.AgentUIOptions{})
	path := writeStoryboard(t)
	tests := []struct {
		name, content, want string
	}{
		{"not JSON", "storyboard.json", "does not say where the storyboard is"},
		{"no path", `{"storyboard": ""}`, "does not say where the storyboard is"},
		{"a relative path", `{"storyboard": "storyboard.json"}`, "must be an absolute path"},
		{"a file that is not there", service.AgentUIStudioContent(filepath.Join(filepath.Dir(path), "gone.json"), ""), "there is no storyboard file at"},
		{"a folder", service.AgentUIStudioContent(filepath.Dir(path), ""), "there is no storyboard file at"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ui.Show(context.Background(), service.AgentUIShowInput{
				TaskID: "task-1", Placement: service.AgentUIPanel, Format: service.AgentUIFormatStudio, Title: "Talk", Content: tt.content,
			})
			if !errors.Is(err, service.ErrAgentUIInvalid) || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("got %v; want an invalid-item error containing %q", err, tt.want)
			}
		})
	}
	if items, _ := ui.List(context.Background(), "task-1"); len(items) != 0 {
		t.Errorf("a rejected studio left %d items", len(items))
	}
	// A studio can only be a panel, and takes no files of its own.
	if _, err := ui.Show(context.Background(), service.AgentUIShowInput{
		TaskID: "task-1", Placement: service.AgentUIInline, Format: service.AgentUIFormatStudio, Title: "Talk",
		Content: service.AgentUIStudioContent(path, ""), FallbackText: "a studio",
	}); !errors.Is(err, service.ErrAgentUIInvalid) {
		t.Errorf("a studio in the conversation: got %v, want it turned down", err)
	}
}

// fakeLibretto stands in for the libretto command: it counts the studios it
// starts and lets a test end one.
type fakeLibretto struct {
	probe   error
	started []string
	exits   []chan struct{}
	stopped int
}

func (f *fakeLibretto) Probe(context.Context) (libretto.Info, error) {
	return libretto.Info{Version: "0.4.0", Manifest: 1, Studio: 5}, f.probe
}

func (f *fakeLibretto) StartStudio(_ context.Context, storyboard string) (*libretto.Studio, error) {
	f.started = append(f.started, storyboard)
	exited := make(chan struct{})
	f.exits = append(f.exits, exited)
	n := len(f.started)
	return &libretto.Studio{URL: "http://127.0.0.1:" + strings.Repeat("9", n), Exited: exited, Stop: func() {
		f.stopped++
		select {
		case <-exited:
		default:
			close(exited)
		}
	}}, nil
}

func TestAStudioIsStartedWhenItIsFirstAskedForAndKeptRunning(t *testing.T) {
	ui := service.NewAgentUIService(newTestStoreForReport(t), nil, service.AgentUIOptions{})
	path := writeStoryboard(t)
	item, err := showStudio(ui, path, "talk")
	if err != nil {
		t.Fatal(err)
	}
	run := &fakeLibretto{}
	studios := service.NewAgentStudioService(ui, run)

	first, err := studios.Address(context.Background(), item.ID)
	if err != nil {
		t.Fatal(err)
	}
	second, err := studios.Address(context.Background(), item.ID)
	if err != nil || second != first || len(run.started) != 1 || run.started[0] != path {
		t.Errorf("asked twice: %q then %q, err %v, started %v; want one studio on %s", first, second, err, run.started, path)
	}
	// Another item on the same storyboard is the same studio.
	other, err := showStudio(ui, path, "talk-again")
	if err != nil {
		t.Fatal(err)
	}
	if got, err := studios.Address(context.Background(), other.ID); err != nil || got != first || len(run.started) != 1 {
		t.Errorf("a second item on the same storyboard: %q, err %v, started %d", got, err, len(run.started))
	}

	// A studio that died is started again the next time it is wanted.
	close(run.exits[0])
	third, err := studios.Address(context.Background(), item.ID)
	if err != nil || third == first || len(run.started) != 2 {
		t.Errorf("after the studio died: %q, err %v, started %d; want a new one", third, err, len(run.started))
	}

	// Closing ends what is running.
	studios.Close()
	if run.stopped != 1 {
		t.Errorf("close stopped %d studios, want the one still running", run.stopped)
	}
}

func TestAStudioIsNotStartedWhenLibrettoIsMissingOrNotAStudio(t *testing.T) {
	ui := service.NewAgentUIService(newTestStoreForReport(t), nil, service.AgentUIOptions{})
	item, err := showStudio(ui, writeStoryboard(t), "talk")
	if err != nil {
		t.Fatal(err)
	}
	// With no libretto, or one too old, the reason comes back as it is, for
	// the server to explain, and nothing is started.
	for _, reason := range []error{libretto.ErrNotInstalled, &libretto.TooOldError{Version: "0.3.0"}} {
		run := &fakeLibretto{probe: reason}
		_, err := service.NewAgentStudioService(ui, run).Address(context.Background(), item.ID)
		if !errors.Is(err, reason) || len(run.started) != 0 {
			t.Errorf("with %v: got %v, started %d", reason, err, len(run.started))
		}
	}

	run := &fakeLibretto{}
	studios := service.NewAgentStudioService(ui, run)
	note, err := ui.Show(context.Background(), service.AgentUIShowInput{
		TaskID: "task-1", Placement: service.AgentUIPanel, Format: service.AgentUIFormatMarkdown, Title: "Notes", Content: "# Notes",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := studios.Address(context.Background(), note.ID); !errors.Is(err, service.ErrAgentUINotAStudio) {
		t.Errorf("an item that is not a studio: got %v", err)
	}
	if _, err := studios.Address(context.Background(), "ui-0000000000"); err == nil {
		t.Error("an item that is not there: want an error")
	}
	if len(run.started) != 0 {
		t.Errorf("%d studios were started for items that are not studios", len(run.started))
	}
}

func TestStudioNoteTellsTheAgentOfATakeToReview(t *testing.T) {
	answer := []byte(`{"api":4,"scenes":[{"id":"intro","status":"recorded","take":{"src":"takes/intro-01.webm"}},{"id":"steps","status":"recorded","take":{"src":"takes/steps-02.webm"}}]}`)
	note, ok := service.StudioNote("/talks/restack/storyboard.json", "POST", "api/scenes/steps/takes", answer)
	if !ok {
		t.Fatal("a take that was kept should be told to the agent")
	}
	for _, want := range []string{
		"[legato studio]", `scene "steps"`, "/talks/restack/takes/steps-02.webm", "/talks/restack/storyboard.json",
		"libretto transcribe", "libretto review", "skip", "Do not add, remove or split scenes",
	} {
		if !strings.Contains(note, want) {
			t.Errorf("the note does not say %q:\n%s", want, note)
		}
	}

	// A take in which nothing was heard is not the scene's take, and there
	// is nothing to review.
	silent := []byte(`{"api":4,"notice":"Nothing was heard","scenes":[{"id":"steps","status":"waiting"}]}`)
	if note, ok := service.StudioNote("/talks/restack/storyboard.json", "POST", "api/scenes/steps/takes", silent); ok {
		t.Errorf("a take that was not kept was told to the agent:\n%s", note)
	}
}

func TestStudioNoteTellsTheAgentOfWhatThePersonAskedFor(t *testing.T) {
	for _, tt := range []struct{ method, rest, want string }{
		{"POST", "api/scenes/steps/comments", `a comment on the scene "steps"`},
		{"POST", "api/scenes/steps/offplan/2", `a comment on the scene "steps"`},
		{"DELETE", "api/scenes/steps", `removed the scene "steps"`},
		{"POST", "api/scenes", "added a scene to the plan"},
		{"PATCH", "api/scenes/steps", `changed what the scene "steps" is to show`},
		{"POST", "api/agree", "agreed the plan"},
	} {
		note, ok := service.StudioNote("/talks/restack/storyboard.json", tt.method, tt.rest, []byte(`{"scenes":[]}`))
		if !ok || !strings.Contains(note, tt.want) || !strings.Contains(note, "/talks/restack/storyboard.json") || !strings.Contains(note, "libretto validate") || !strings.Contains(note, "delete") {
			t.Errorf("%s %s: ok=%v\n%s", tt.method, tt.rest, ok, note)
		}
	}
	// What needs nobody is told to nobody.
	for _, tt := range []struct{ method, rest string }{
		{"GET", "api/project"}, {"POST", "api/scenes/steps/take"}, {"POST", "api/face"}, {"POST", "api/render"}, {"DELETE", "api/scenes/steps/comments/1"},
		{"POST", "api/scenes/steps/move"}, {"POST", "api/chapters"},
	} {
		if note, ok := service.StudioNote("/talks/restack/storyboard.json", tt.method, tt.rest, []byte(`{"scenes":[]}`)); ok {
			t.Errorf("%s %s was told to the agent:\n%s", tt.method, tt.rest, note)
		}
	}
}

func TestStudioNoteAsksForAPlanWhenThePersonWritesWhatTheyWantToCover(t *testing.T) {
	note, ok := service.StudioNote("/talks/restack/storyboard.json", "POST", "api/brief", []byte(`{"brief":"How restack works","scenes":[]}`))
	if !ok {
		t.Fatal("a brief should be told to the agent")
	}
	for _, want := range []string{"[legato studio]", "/talks/restack/storyboard.json", "brief", "talking points", "shows", "Draw no pictures yet", "agree"} {
		if !strings.Contains(note, want) {
			t.Errorf("the note does not say %q:\n%s", want, note)
		}
	}
}
