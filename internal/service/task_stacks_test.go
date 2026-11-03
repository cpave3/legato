package service

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cpave3/legato/internal/engine/events"
	"github.com/cpave3/legato/internal/engine/store"
)

// stackIn makes branches in a job clone, each on the one before it from
// main, and writes the graph st would. pushed branches get a
// remote-tracking ref at their commit, as a push would leave.
func stackIn(t *testing.T, clone store.JobClone, pushed bool, names ...string) {
	t.Helper()
	parent := "main"
	base := gitIn(t, clone.Path, "rev-parse", "HEAD")
	var entries []string
	for _, name := range names {
		gitIn(t, clone.Path, "checkout", "-q", "-B", name)
		gitIn(t, clone.Path, "commit", "-q", "--allow-empty", "-m", name)
		head := gitIn(t, clone.Path, "rev-parse", "HEAD")
		if pushed {
			gitIn(t, clone.Path, "update-ref", "refs/remotes/origin/"+name, head)
		}
		entries = append(entries, fmt.Sprintf(`%q: {"name": %q, "parent": %q, "base_sha": %q, "head_sha": %q}`, name, name, parent, base, head))
		parent, base = name, head
	}
	graph := `{"version": 1, "root": "main", "branches": {` + strings.Join(entries, ", ") + `}}`
	if err := os.MkdirAll(filepath.Join(clone.Path, ".git", "stack"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(clone.Path, ".git", "stack", "graph.json"), []byte(graph), 0o644); err != nil {
		t.Fatal(err)
	}
}

func stackNames(s TaskStack) string {
	var out []string
	for _, b := range s.Branches {
		out = append(out, b.Name+":"+b.State)
	}
	return strings.Join(out, " ")
}

func TestAStackIsRecordedBeforeItsCloneIsRemoved(t *testing.T) {
	f, clone := jobFixture(t)
	stacks := NewTaskStackService(f.store, func() time.Time { return *f.now })
	f.svc.SetBeforeRemove(stacks.Record)
	stackIn(t, clone, true, "legato/task1-a", "legato/task1-b")
	f.end(t, "ex-abc")
	ctx := context.Background()
	if err := f.svc.RemoveJobClone(ctx, clone.ID); err != nil {
		t.Fatal(err)
	}
	if exists(clone.Path) {
		t.Fatal("the clone is still there")
	}

	got, err := stacks.List(ctx, "task1")
	if err != nil || len(got) != 1 {
		t.Fatalf("stacks = %+v, %v", got, err)
	}
	s := got[0]
	if s.Live || s.Repo != "acme/widget" || s.Root != "main" || !s.Importable || s.DocumentName != "task1-widget-stack.json" ||
		stackNames(s) != "legato/task1-a:pushed legato/task1-b:pushed" {
		t.Fatalf("stack = %+v", s)
	}
	doc, err := stacks.Document(ctx, "task1", clone.ID)
	if err != nil {
		t.Fatal(err)
	}
	if doc.Name != "task1-widget-stack.json" || !strings.Contains(string(doc.Data), `"kind": "staccato.graph"`) {
		t.Fatalf("document = %s %s", doc.Name, doc.Data)
	}
}

func TestAStackIsReadAgainWhileItsCloneExists(t *testing.T) {
	f, clone := jobFixture(t)
	stacks := NewTaskStackService(f.store, func() time.Time { return *f.now })
	ctx := context.Background()
	stackIn(t, clone, true, "legato/task1-a")
	if err := stacks.Record(ctx, clone); err != nil {
		t.Fatal(err)
	}
	stackIn(t, clone, false, "legato/task1-a", "legato/task1-b")

	got, err := stacks.List(ctx, "task1")
	if err != nil || len(got) != 1 {
		t.Fatalf("stacks = %+v, %v", got, err)
	}
	if s := got[0]; !s.Live || s.Importable || stackNames(s) != "legato/task1-a:unpushed legato/task1-b:unpushed" ||
		!strings.Contains(s.Reason, "legato/task1-b") {
		t.Fatalf("stack = %+v", s)
	}
	if _, err := stacks.Document(ctx, "task1", clone.ID); !errors.Is(err, ErrStackNotImportable) || !strings.Contains(err.Error(), "legato/task1-a") {
		t.Fatalf("document of an unpushed stack: %v", err)
	}
}

func TestACloneWithoutAStackLeavesNoRecord(t *testing.T) {
	f, clone := jobFixture(t)
	stacks := NewTaskStackService(f.store, func() time.Time { return *f.now })
	ctx := context.Background()
	stackIn(t, clone, true, "legato/task1-a")
	if err := stacks.Record(ctx, clone); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(clone.Path, ".git", "stack", "graph.json")); err != nil {
		t.Fatal(err)
	}
	if err := stacks.Record(ctx, clone); err != nil {
		t.Fatal(err)
	}
	if got, err := stacks.List(ctx, "task1"); err != nil || len(got) != 0 {
		t.Fatalf("stacks = %+v, %v", got, err)
	}
}

func TestAStackIsRecordedWhenItsAgentEnds(t *testing.T) {
	f, clone := jobFixture(t)
	stacks := NewTaskStackService(f.store, func() time.Time { return *f.now })
	stackIn(t, clone, true, "legato/task1-a")
	bus := events.New()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	stacks.StartEventLoop(ctx, bus)
	bus.Publish(events.Event{Type: events.EventAgentDied, Payload: events.AgentDiedPayload{TaskID: "task1"}})

	deadline := time.Now().Add(5 * time.Second)
	for {
		if got, _ := f.store.ListTaskStacks(context.Background(), "task1"); len(got) == 1 {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("the stack was not recorded when the agent ended")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestAStackIsOnlyForTheTasksOfItsClone(t *testing.T) {
	f, clone := jobFixture(t)
	stacks := NewTaskStackService(f.store, func() time.Time { return *f.now })
	ctx := context.Background()
	stackIn(t, clone, true, "legato/task1-a")
	if err := stacks.Record(ctx, clone); err != nil {
		t.Fatal(err)
	}
	if _, err := stacks.Document(ctx, "other", clone.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("another task's document: %v", err)
	}
	if _, err := stacks.Document(ctx, "task1", clone.ID+1); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("an unknown clone's document: %v", err)
	}
}
