package service

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cpave3/legato/internal/engine/store"
)

// briefingCheckout is a repository on branch feat/login whose briefing is
// one page with a stylesheet.
func briefingCheckout(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	write := func(name, content string) {
		t.Helper()
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	run("init", "-b", "main")
	run("config", "user.email", "test@example.com")
	run("config", "user.name", "Test")
	write("README.md", "hello\n")
	run("add", "-A")
	run("commit", "-m", "initial")
	run("switch", "-c", "feat/login")
	write(".review/feat-login/index.html", `<link rel="stylesheet" href="style.css"><h1>Login</h1>`)
	write(".review/feat-login/style.css", "h1 {}")
	return dir
}

// runningAgentIn records a running agent for taskID whose execution started
// in dir.
func runningAgentIn(t *testing.T, s *store.Store, taskID, dir string) {
	t.Helper()
	ctx := context.Background()
	id, err := s.InsertAgentSessionReturningID(ctx, store.AgentSession{TaskID: taskID, AgentKind: "claude-code", TmuxSession: "legato-" + taskID, Status: "running"})
	if err != nil {
		t.Fatal(err)
	}
	executionID := "ex-" + taskID
	if err := s.CreateExecution(ctx, store.Execution{ID: executionID, Kind: store.ExecutionInteractive, Status: store.ExecutionRunning, TaskID: taskID, ExecutionTaskID: taskID, AgentKind: "claude-code", AgentSessionID: &id, WorkingDir: dir, StartedAt: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}
}

func newBriefingStore(t *testing.T) *store.Store {
	t.Helper()
	s, err := store.New(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestReviewBriefingsReadsTheBriefingInTheAgentsCheckout(t *testing.T) {
	s := newBriefingStore(t)
	dir := briefingCheckout(t)
	runningAgentIn(t, s, "task1", dir)
	briefings := NewReviewBriefingService(s)

	index, err := briefings.Briefing(context.Background(), "task1")
	if err != nil {
		t.Fatal(err)
	}
	if index.Branch != "feat/login" || len(index.Pages) != 1 || index.Pages[0].Path != ".review/feat-login/index.html" {
		t.Fatalf("index = %+v", index)
	}
	html, err := briefings.Page(context.Background(), "task1", ".review/feat-login/index.html")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(html, `href="data:text/css;base64,`) || !strings.Contains(html, "<h1>Login</h1>") {
		t.Fatalf("page = %s", html)
	}
}

func TestReviewBriefingsSaysWhenTheAgentHasNoCheckout(t *testing.T) {
	s := newBriefingStore(t)
	runningAgentIn(t, s, "task1", t.TempDir())
	briefings := NewReviewBriefingService(s)

	for _, taskID := range []string{"task1", "not-running"} {
		if _, err := briefings.Briefing(context.Background(), taskID); !errors.Is(err, ErrNoReviewBriefing) {
			t.Errorf("Briefing(%s) error = %v, want ErrNoReviewBriefing", taskID, err)
		}
	}
}
