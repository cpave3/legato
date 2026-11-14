package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cpave3/legato/internal/engine/store"
)

// readsInstance is an instance with task LEG-160, outside any agent
// session. It returns the database's path.
func readsInstance(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "legato.db")
	cfgPath := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(cfgPath, []byte("db:\n  path: "+dbPath+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("LEGATO_CONFIG", cfgPath)
	t.Setenv("LEGATO_TASK_ID", "")
	t.Setenv("LEGATO_PLAN_NAME", "")
	db, err := store.New(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.CreateTask(context.Background(), store.Task{ID: "LEG-160", Title: "Seatbelt", Status: "Doing"}); err != nil {
		t.Fatal(err)
	}
	return dbPath
}

// taskDescription is the task's description, which task note appends to.
func taskDescription(t *testing.T, dbPath, taskID string) string {
	t.Helper()
	db, err := store.New(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	task, err := db.GetTask(context.Background(), taskID)
	if err != nil {
		t.Fatal(err)
	}
	return task.Description
}

// `legato task note` adds a note; a word that asks to read, in place of
// the task ID or as the whole body, is refused and points to `task notes`.
func TestTaskNoteRefusesAWordThatAsksToRead(t *testing.T) {
	dbPath := readsInstance(t)
	for _, args := range [][]string{
		{"task", "note", "LEG-160", "list"},
		{"task", "note", "list", "LEG-160"},
		{"task", "note", "LEG-160", " Show "},
		{"task", "note", "ls", "LEG-160"},
		{"task", "note", "LEG-160", "get", "--json"},
	} {
		out, code := captureStderrWithCode(t, func() int { return runCLI(args) })
		if code != exitUsage || !strings.Contains(out, "legato task notes") {
			t.Errorf("legato %s = %d %q", strings.Join(args, " "), code, out)
		}
	}
	t.Setenv("LEGATO_TASK_ID", "LEG-160")
	if out, code := captureStderrWithCode(t, func() int { return runCLI([]string{"task", "note", "list"}) }); code != exitUsage || !strings.Contains(out, "legato task notes") {
		t.Errorf("legato task note list (in a session) = %d %q", code, out)
	}
	if got := taskDescription(t, dbPath, "LEG-160"); got != "" {
		t.Fatalf("a refused note was written: %q", got)
	}

	// A note that only mentions such a word is a note.
	t.Setenv("LEGATO_TASK_ID", "")
	if code := runCLI([]string{"task", "note", "LEG-160", "list the notes before you start", "--json"}); code != 0 {
		t.Fatalf("an ordinary note = %d", code)
	}
	if got := taskDescription(t, dbPath, "LEG-160"); !strings.Contains(got, "list the notes before you start") {
		t.Fatalf("description = %q", got)
	}
}

// The Reeve's legato tool refuses such a call before proposing it, and
// says what reads notes.
func TestTheReeveIsNeverOfferedANoteThatAsksToRead(t *testing.T) {
	class, description := classifyLegatoCommand([]string{"task", "note", "LEG-160", "list"})
	if class != "never" || !strings.Contains(description, "legato task notes") {
		t.Fatalf("task note LEG-160 list = %s %q", class, description)
	}
	if class, _ := classifyLegatoCommand([]string{"task", "note", "LEG-160", "PR opened"}); class != "change" {
		t.Fatalf("an ordinary note = %s", class)
	}
	if class, _ := classifyLegatoCommand([]string{"task", "notes", "LEG-160"}); class != "read" {
		t.Fatalf("task notes = %s", class)
	}
}

// `legato plan show --task <id>` finds the task's latest plan when no
// name is given, as the Reeve asked for LEG-160's.
func TestPlanShowFindsTheTasksLatestPlan(t *testing.T) {
	dbPath := readsInstance(t)
	db, err := store.New(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	plan := store.Plan{ID: "pl-LEG-160-macos-seatbelt-sandbox", TaskID: "LEG-160", Name: "macos-seatbelt-sandbox", Title: "Seatbelt", Status: "proposed", LatestRevision: 1}
	if err := db.InsertPlanRevision(context.Background(), plan, store.PlanRevision{ID: "rev-1", PlanID: plan.ID, Revision: 1, Markdown: "# Use sandbox-exec", ManifestJSON: "{}"}, nil); err != nil {
		t.Fatal(err)
	}
	db.Close()

	var code int
	out := captureStdout(t, func() int { code = runCLI([]string{"plan", "show", "--task", "LEG-160"}); return code })
	if code != 0 {
		t.Fatalf("plan show = %d %s", code, out)
	}
	var view struct {
		Plan     store.Plan         `json:"plan"`
		Revision store.PlanRevision `json:"revision"`
	}
	if err := json.Unmarshal([]byte(out), &view); err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	if view.Plan.ID != plan.ID || view.Revision.Markdown != "# Use sandbox-exec" || view.Plan.Status != "proposed" {
		t.Fatalf("view = %+v", view)
	}
	out = captureStdout(t, func() int { code = runCLI([]string{"plan", "status", "--task", "LEG-160"}); return code })
	if code != 0 || !strings.Contains(out, plan.ID) {
		t.Fatalf("plan status = %d %s", code, out)
	}
	// A change names its plan: no falling back to the latest.
	if _, code := captureStderrWithCode(t, func() int { return runCLI([]string{"plan", "withdraw", "--task", "LEG-160"}) }); code == 0 {
		t.Fatal("plan withdraw acted on a plan it did not name")
	}
}
