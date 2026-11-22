package service

import (
	"context"
	"encoding/json"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/cpave3/legato/internal/engine/chimera"
	"github.com/cpave3/legato/internal/engine/store"
)

// nativeEvent is one event of a native stream, as a bridge writes it.
func nativeEvent(t *testing.T, at time.Time, fields map[string]any) chimera.EventEnvelope {
	t.Helper()
	if at.IsZero() {
		at = time.Now()
	}
	fields["ts"] = at.UnixMilli()
	data, err := json.Marshal(fields)
	if err != nil {
		t.Fatal(err)
	}
	return chimera.EventEnvelope{EventID: "e", Type: fields["type"].(string), Timestamp: at.UnixMilli(), Data: data}
}

func toolStart(t *testing.T, name string, args map[string]any) chimera.EventEnvelope {
	return nativeEvent(t, time.Time{}, map[string]any{"type": "tool_call_start", "callId": "c1", "name": name, "args": args})
}

// The classifier finds the files a tool call changes, whichever agent tool
// made it: the main loop's mapped names, a subagent's raw ones, a Codex
// fileChange and a Chimera patch.
func TestEditedPathsNameTheFilesAToolCallChanges(t *testing.T) {
	cases := []struct {
		name  string
		event chimera.EventEnvelope
		want  string
	}{
		{"claude edit", toolStart(t, "edit", map[string]any{"path": "/w/main.go"}), "/w/main.go"},
		{"claude write", toolStart(t, "write", map[string]any{"path": "/w/new.go", "content": "x"}), "/w/new.go"},
		{"multi edit", toolStart(t, "MultiEdit", map[string]any{"file_path": "/w/a.go"}), "/w/a.go"},
		{"notebook", toolStart(t, "NotebookEdit", map[string]any{"notebook_path": "/w/n.ipynb"}), "/w/n.ipynb"},
		{"codex patch", toolStart(t, "patch", map[string]any{"changes": []any{
			map[string]any{"path": "/w/a.go", "kind": "update"}, map[string]any{"path": "/w/b.go", "kind": "add"},
		}}), "/w/a.go,/w/b.go"},
		{"chimera patch text", toolStart(t, "patch", map[string]any{"patch": "*** Begin Patch\n*** Update File: src/a.ts\n@@\n-x\n+y\n*** Add File: src/b.ts\n+z\n*** End Patch"}), "src/a.ts,src/b.ts"},
		{"subagent Edit", nativeEvent(t, time.Time{}, map[string]any{"type": "subagent_event", "subagentId": "s1", "event": map[string]any{
			"type": "tool_call_start", "callId": "c2", "name": "Edit", "args": map[string]any{"path": "/w/sub.go"},
		}}), "/w/sub.go"},
		{"read", toolStart(t, "read", map[string]any{"path": "/w/main.go"}), ""},
		{"bash", toolStart(t, "bash", map[string]any{"command": "rm -rf x"}), ""},
		{"subagent Read", nativeEvent(t, time.Time{}, map[string]any{"type": "subagent_event", "event": map[string]any{
			"type": "tool_call_start", "name": "Read", "args": map[string]any{"path": "/w/sub.go"},
		}}), ""},
		{"text", nativeEvent(t, time.Time{}, map[string]any{"type": "assistant_text_done", "text": "edit /w/x"}), ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := EditedPaths(c.event)
			sort.Strings(got)
			if strings.Join(got, ",") != c.want {
				t.Fatalf("EditedPaths = %v, want %s", got, c.want)
			}
		})
	}
}

func TestInsideWorkingDir(t *testing.T) {
	cases := []struct {
		path, dir string
		want      bool
	}{
		{"/w/main.go", "/w", true},
		{"/w/sub/x.go", "/w/", true},
		{"src/a.ts", "/w", true},
		{"/w", "/w", false},
		{"/wx/a.go", "/w", false},
		{"/w/../etc/passwd", "/w", false},
		{"../outside.go", "/w", false},
		{"/home/me/.claude/plans/p.md", "/w", false},
		{"/w/a.go", "", false},
	}
	for _, c := range cases {
		if got := InsideWorkingDir(c.path, c.dir); got != c.want {
			t.Errorf("InsideWorkingDir(%q, %q) = %v, want %v", c.path, c.dir, got, c.want)
		}
	}
}

func evidenceAgent(slot string) WatchedAgent {
	return WatchedAgent{TaskID: "LEG-1", Slot: slot, ExecutionID: "ex-" + slot, WorkingDir: "/w"}
}

// S1 and S4: a session that only reads stays Exploring; an edit inside its
// working folder, its own or a subagent's, makes it Implementing.
func TestNativeEditsAreEvidence(t *testing.T) {
	f := newStageFixture(t)
	ctx := context.Background()
	createTask(t, f.store, "LEG-1")
	place := TaskPlace("LEG-1", "")
	if err := f.stages.Start(ctx, place, "ex-main"); err != nil {
		t.Fatal(err)
	}
	agent := evidenceAgent(store.SlotMain)

	f.stages.ObserveNativeEvent(agent, toolStart(t, "read", map[string]any{"path": "/w/main.go"}))
	f.stages.ObserveNativeEvent(agent, toolStart(t, "bash", map[string]any{"command": "go test ./..."}))
	if got := f.stage(t, place); got.Stage != store.WorkStageExploring {
		t.Fatalf("after reads stage = %s, want exploring", got.Stage)
	}

	f.stages.ObserveNativeEvent(agent, toolStart(t, "edit", map[string]any{"path": "/home/me/.claude/plans/plan.md"}))
	if got := f.stage(t, place); got.Stage != store.WorkStageExploring {
		t.Fatalf("after an edit outside the working folder stage = %s, want exploring", got.Stage)
	}

	f.stages.ObserveNativeEvent(agent, nativeEvent(t, time.Time{}, map[string]any{"type": "subagent_event", "event": map[string]any{
		"type": "tool_call_start", "name": "Edit", "args": map[string]any{"path": "/w/internal/x.go"},
	}}))
	got := f.stage(t, place)
	if got.Stage != store.WorkStageImplementing || got.Source != store.WorkStageSourceEvidence {
		t.Fatalf("after a subagent's edit stage = %s (%s), want implementing by evidence", got.Stage, got.Source)
	}
	if got.Reason != "edited internal/x.go" {
		t.Fatalf("reason = %q", got.Reason)
	}
}

// S5: a reviewer slot's edits are no evidence, and the main slot's stage
// is its own.
func TestAReviewerSlotsEditsAreNoEvidence(t *testing.T) {
	f := newStageFixture(t)
	ctx := context.Background()
	createTask(t, f.store, "LEG-1")
	_ = f.stages.Start(ctx, TaskPlace("LEG-1", ""), "ex-main")
	_ = f.stages.Start(ctx, TaskPlace("LEG-1", SlotReviewer), "ex-reviewer")

	f.stages.ObserveNativeEvent(evidenceAgent(SlotReviewer), toolStart(t, "write", map[string]any{"path": "/w/notes.md"}))

	if got := f.stage(t, TaskPlace("LEG-1", SlotReviewer)); got.Stage != store.WorkStageReviewing {
		t.Fatalf("reviewer stage = %s, want reviewing", got.Stage)
	}
	if got := f.stage(t, TaskPlace("LEG-1", "")); got.Stage != store.WorkStageExploring {
		t.Fatalf("main stage = %s, want exploring", got.Stage)
	}
}

// A replayed stream does not count old events again: an edit from before
// the agent reported Finished does not take it back to Implementing. S9: a
// new edit after Finished does.
func TestReplayedEditsDoNotUndoALaterStage(t *testing.T) {
	f := newStageFixture(t)
	ctx := context.Background()
	createTask(t, f.store, "LEG-1")
	place := TaskPlace("LEG-1", "")
	_ = f.stages.Start(ctx, place, "ex-main")
	agent := evidenceAgent(store.SlotMain)
	edited := time.Now().Add(-time.Minute)
	f.stages.ObserveNativeEvent(agent, nativeEvent(t, edited, map[string]any{"type": "tool_call_start", "name": "edit", "args": map[string]any{"path": "/w/a.go"}}))
	if _, err := f.stages.Report(ctx, place, "ex-main", store.WorkStageFinished, "done"); err != nil {
		t.Fatal(err)
	}

	f.stages.ObserveNativeEvent(agent, nativeEvent(t, edited, map[string]any{"type": "tool_call_start", "name": "edit", "args": map[string]any{"path": "/w/a.go"}}))
	if got := f.stage(t, place); got.Stage != store.WorkStageFinished {
		t.Fatalf("after a replayed edit stage = %s, want finished", got.Stage)
	}

	f.stages.ObserveNativeEvent(agent, nativeEvent(t, time.Now().Add(time.Second), map[string]any{"type": "tool_call_start", "name": "edit", "args": map[string]any{"path": "/w/a.go"}}))
	if got := f.stage(t, place); got.Stage != store.WorkStageImplementing {
		t.Fatalf("after a new edit stage = %s, want implementing", got.Stage)
	}
}

// A workflow agent's evidence lands on its execution's place.
func TestAWorkflowAgentsEditsLandOnItsExecution(t *testing.T) {
	f := newStageFixture(t)
	ctx := context.Background()
	createTask(t, f.store, "LEG-1")
	agent := WatchedAgent{TaskID: "LEG-1", Slot: store.SlotMain, ExecutionID: "ex-wf", ExecutionKind: store.ExecutionWorkflowAgent, WorkingDir: "/w"}
	place := agent.Place()
	if place.PlaceExecutionID != "ex-wf" {
		t.Fatalf("place = %+v, want the execution's", place)
	}
	_ = f.stages.Start(ctx, place, "ex-wf")
	f.stages.ObserveNativeEvent(agent, toolStart(t, "edit", map[string]any{"path": "/w/a.go"}))
	if got := f.stage(t, place); got.Stage != store.WorkStageImplementing {
		t.Fatalf("stage = %s, want implementing", got.Stage)
	}
	if got := f.stage(t, TaskPlace("LEG-1", "")); got.Stage != "" {
		t.Fatalf("the task's own place got %s", got.Stage)
	}
}
