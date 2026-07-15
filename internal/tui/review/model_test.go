package review

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	gitpkg "github.com/cpave3/legato/internal/engine/git"
	"github.com/cpave3/legato/internal/engine/store"
	"github.com/cpave3/legato/internal/service"
)

func newFixture(t *testing.T) (*service.ReviewService, *store.Store) {
	t.Helper()
	s, err := store.New(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })

	repo := t.TempDir()
	git := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = repo
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	git("init", "-b", "main")
	git("config", "user.email", "t@example.com")
	git("config", "user.name", "T")
	if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git("add", ".")
	git("commit", "-m", "initial")
	git("checkout", "-b", "feature")

	ctx := context.Background()
	if err := s.CreateTask(ctx, store.Task{ID: "task-1", Title: "Review me", Status: "Doing",
		CreatedAt: "2026-01-01T00:00:00Z", UpdatedAt: "2026-01-01T00:00:00Z"}); err != nil {
		t.Fatal(err)
	}
	if err := s.SetTaskWorktree(ctx, "task-1", &store.TaskWorktree{
		PrimaryDir: repo, Path: repo, Branch: "feature", BaseBranch: "main"}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "a.go"), []byte("package a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git("add", "-A")
	git("commit", "-m", "add a\n\nbecause reasons")

	svc := service.NewReviewService(s, nil, nil)
	if err := svc.Ready(ctx, "task-1", "done"); err != nil {
		t.Fatal(err)
	}
	return svc, s
}

func key(s string) tea.KeyMsg {
	if len(s) == 1 {
		return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
	}
	switch s {
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "esc":
		return tea.KeyMsg{Type: tea.KeyEsc}
	case "space":
		return tea.KeyMsg{Type: tea.KeySpace}
	}
	panic("unknown key " + s)
}

// drive runs a cmd (possibly a batch) and feeds every produced message back
// into the model, returning the settled model.
func drive(t *testing.T, m Model, cmd tea.Cmd) Model {
	t.Helper()
	if cmd == nil {
		return m
	}
	msg := cmd()
	if msg == nil {
		return m
	}
	if batch, ok := msg.(tea.BatchMsg); ok {
		for _, c := range batch {
			m = drive(t, m, c)
		}
		return m
	}
	var next tea.Cmd
	m, next = m.Update(msg)
	return drive(t, m, next)
}

func TestQueueLoadsAndOpensTour(t *testing.T) {
	svc, _ := newFixture(t)
	m := New(svc)
	m.SetSize(120, 40)

	m = drive(t, m, m.Init())
	if len(m.queue) != 1 || m.queue[0].TaskID != "task-1" {
		t.Fatalf("queue = %+v", m.queue)
	}
	if m.mode != modeQueue {
		t.Fatalf("mode = %v, want queue", m.mode)
	}

	var cmd tea.Cmd
	m, cmd = m.Update(key("enter"))
	m = drive(t, m, cmd)
	if m.mode != modeTour {
		t.Fatalf("mode = %v, want tour", m.mode)
	}
	if m.view == nil || len(m.view.Steps) != 1 {
		t.Fatalf("view = %+v", m.view)
	}
	// The focused step's diff loads automatically.
	if len(m.diff) != 1 || m.diff[0].NewPath != "a.go" {
		t.Fatalf("diff = %+v", m.diff)
	}
}

func TestTourToggleReviewedAndComplete(t *testing.T) {
	svc, s := newFixture(t)
	m := New(svc)
	m.SetSize(120, 40)
	m = drive(t, m, m.Init())
	var cmd tea.Cmd
	m, cmd = m.Update(key("enter"))
	m = drive(t, m, cmd)

	// Space toggles reviewed on the focused step.
	m, cmd = m.Update(key("space"))
	m = drive(t, m, cmd)
	steps, _ := s.ListReviewSteps(context.Background(), "task-1")
	if steps[0].ReviewedAt == nil {
		t.Fatal("space should mark the step reviewed")
	}

	// c completes the review.
	m, cmd = m.Update(key("c"))
	m = drive(t, m, cmd)
	tour, _ := s.GetReviewTour(context.Background(), "task-1")
	if tour.Status != "reviewed" {
		t.Fatalf("tour status = %q, want reviewed", tour.Status)
	}
}

func TestTourQuestionInput(t *testing.T) {
	svc, s := newFixture(t)
	m := New(svc)
	m.SetSize(120, 40)
	m = drive(t, m, m.Init())
	var cmd tea.Cmd
	m, cmd = m.Update(key("enter"))
	m = drive(t, m, cmd)

	// 'a' opens the question input; typed text accumulates; enter submits.
	m, _ = m.Update(key("a"))
	if !m.asking {
		t.Fatal("a should open the question input")
	}
	for _, r := range "why?" {
		m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	// While typing, j/k must edit text, not navigate steps.
	if m.input != "why?" {
		t.Fatalf("input = %q", m.input)
	}
	m, cmd = m.Update(key("enter"))
	m = drive(t, m, cmd)
	if m.asking {
		t.Fatal("enter should close the question input")
	}
	msgs, _ := s.ListReviewMessages(context.Background(), "task-1")
	if len(msgs) != 1 || msgs[0].Body != "why?" {
		t.Fatalf("transcript = %+v", msgs)
	}
}

func TestTourEscReturnsToQueueThenBoard(t *testing.T) {
	svc, _ := newFixture(t)
	m := New(svc)
	m.SetSize(120, 40)
	m = drive(t, m, m.Init())
	var cmd tea.Cmd
	m, cmd = m.Update(key("enter"))
	m = drive(t, m, cmd)

	m, cmd = m.Update(key("esc"))
	m = drive(t, m, cmd)
	if m.mode != modeQueue {
		t.Fatalf("esc from tour should return to queue, mode = %v", m.mode)
	}

	m, cmd = m.Update(key("esc"))
	if cmd == nil {
		t.Fatal("esc from queue should emit ReturnToBoardMsg")
	}
	if _, ok := cmd().(ReturnToBoardMsg); !ok {
		t.Fatal("expected ReturnToBoardMsg")
	}
	_ = m
}

func TestRenderDiffPlacesMatchedHunkNoteImmediatelyBeforeHunk(t *testing.T) {
	files := []gitpkg.FileDiff{{
		NewPath: "a.go",
		Status:  gitpkg.FileModified,
		Hunks: []gitpkg.Hunk{{
			Header: "@@ -1 +1 @@",
			Anchor: "anchor-1",
			Lines:  []gitpkg.Line{{Kind: gitpkg.LineAdded, Text: "new line"}},
		}},
	}}
	notes := []store.ReviewHunkNote{{
		StepID: "step-1", FilePath: "a.go", HunkAnchor: "anchor-1", Body: "Check this edge case.",
	}}

	out, unmatched := renderDiff(files, notes, 80)
	fileAt := strings.Index(out, "── a.go")
	noteAt := strings.Index(out, "Check this edge case.")
	hunkAt := strings.Index(out, "@@ -1 +1 @@")
	if fileAt < 0 || noteAt < fileAt || hunkAt < noteAt {
		t.Fatalf("matched note should render immediately before its hunk:\n%s", out)
	}
	if len(unmatched) != 0 {
		t.Fatalf("unmatched = %+v, want none", unmatched)
	}
}

func TestViewportRendersUnmatchedNotesForSelectedStepWhenDiffIsEmpty(t *testing.T) {
	m := New(nil)
	m.SetSize(120, 40)
	m.mode = modeTour
	m.view = &service.ReviewTourView{
		Steps: []store.ReviewStep{{ID: "selected", Title: "Selected step"}},
		HunkNotes: []store.ReviewHunkNote{
			{StepID: "selected", FilePath: "gone.go", HunkAnchor: "old", Body: "Still needs attention."},
			{StepID: "other", FilePath: "other.go", HunkAnchor: "other", Body: "Wrong step note."},
		},
	}

	m.refreshViewport()
	out := m.viewport.View()
	if !strings.Contains(out, "UNMATCHED HUNK NOTES") || !strings.Contains(out, "Still needs attention.") {
		t.Fatalf("selected step's unmatched note should be visible:\n%s", out)
	}
	if strings.Contains(out, "Wrong step note.") {
		t.Fatalf("note from another step should not render:\n%s", out)
	}
}

func TestViewRendersWithoutPanic(t *testing.T) {
	svc, _ := newFixture(t)
	m := New(svc)
	m.SetSize(120, 40)
	m = drive(t, m, m.Init())
	if !strings.Contains(m.View(), "task-1") && !strings.Contains(m.View(), "Review me") {
		t.Fatalf("queue view missing task:\n%s", m.View())
	}
	var cmd tea.Cmd
	m, cmd = m.Update(key("enter"))
	m = drive(t, m, cmd)
	out := m.View()
	if !strings.Contains(out, "add a") {
		t.Fatalf("tour view missing step title:\n%s", out)
	}
}
