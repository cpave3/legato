package service

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cpave3/legato/internal/engine/events"
	"github.com/cpave3/legato/internal/engine/store"
	workflowengine "github.com/cpave3/legato/internal/engine/workflow"
)

func learnTemplate(t *testing.T) *workflowengine.Definition {
	t.Helper()
	example, ok := workflowengine.ExampleNamed("learn")
	if !ok {
		t.Fatal("learn is not bundled")
	}
	definition, err := workflowengine.Parse("learn.workflow.yaml", example.YAML)
	if err != nil {
		t.Fatal(err)
	}
	return definition
}

// The template is a copy of the manual Learn workflow's prompts; this keeps
// the two from drifting apart.
func TestTheLearnTemplateAsksWhatTheManualLearnWorkflowAsks(t *testing.T) {
	template := learnTemplate(t)
	manual, err := BuiltInLearningWorkflow(LearningWorkflowOptions{Adapter: "fake", PacketMaxItems: 100, PacketMaxBytes: 256 * 1024})
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"curator", "critic"} {
		var got, want string
		for _, step := range template.Steps {
			if step.ID == id {
				got, _ = step.With["prompt"].(string)
			}
		}
		for _, step := range manual.Steps {
			if step.ID == id {
				want, _ = step.With["prompt"].(string)
			}
		}
		if want == "" || got != want {
			t.Errorf("%s prompt differs from the manual workflow's:\n got: %q\nwant: %q", id, got, want)
		}
	}
	packet := template.Steps[1].With["limits"].(map[string]any)
	if packet["max_items"] != 100 || packet["max_bytes"] != 256*1024 {
		t.Errorf("packet limits = %v, want the configuration's defaults", packet)
	}
}

type learnTemplateFixture struct {
	db          *store.Store
	board       BoardService
	learning    *LearningService
	agents      *acceptanceAgentTransport
	workflows   *WorkflowService
	coordinator *WorkflowTriggerCoordinator
	task        *Card
}

func newLearnTemplateFixture(t *testing.T, definitions ...*workflowengine.Definition) learnTemplateFixture {
	t.Helper()
	ctx := context.Background()
	db, err := store.New(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	for i, name := range []string{"Backlog", "Doing", "Review", "Done"} {
		if err := db.CreateColumnMapping(ctx, store.ColumnMapping{ColumnName: name, RemoteStatuses: `[]`, SortOrder: i}); err != nil {
			t.Fatal(err)
		}
	}
	board := NewBoardService(db, events.New())
	task, err := board.CreateTask(ctx, "Fix callback ordering", "Keep callbacks tied to their active request.", "Doing", "High", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.InsertAgentSessionReturningID(ctx, store.AgentSession{TaskID: task.ID, TmuxSession: "fake-task-agent", AgentKind: "fake", Status: "running"}); err != nil {
		t.Fatal(err)
	}
	learning := NewLearningService(db, events.New())
	agents := &acceptanceAgentTransport{store: db}
	workflows := NewWorkflowService(db, []WorkflowExecutor{
		NewAgentInterviewExecutor(learning, agents, db),
		NewLearningPacketPrepareExecutor(learning),
		NewAgentRunExecutor(agents),
		NewLearningProposalsSubmitExecutor(learning),
	})
	coordinator := NewWorkflowTriggerCoordinator(db, workflows, func() workflowengine.DiscoveryResult {
		return workflowengine.DiscoveryResult{Definitions: definitions}
	})
	return learnTemplateFixture{db: db, board: board, learning: learning, agents: agents, workflows: workflows, coordinator: coordinator, task: task}
}

func (f learnTemplateFixture) moveToReview(t *testing.T, taskID string) {
	t.Helper()
	ctx := context.Background()
	if err := f.board.MoveCard(ctx, taskID, "Review"); err != nil {
		t.Fatal(err)
	}
	if err := f.coordinator.CaptureLatestForTask(ctx, taskID); err != nil {
		t.Fatal(err)
	}
}

func TestTheLearnTemplateSwitchedOnLearnsFromACardEnteringReview(t *testing.T) {
	ctx := context.Background()
	definition := learnTemplate(t)
	f := newLearnTemplateFixture(t, definition)
	if err := f.coordinator.SetTriggerEnabled(ctx, "learn", workflowengine.TriggerTaskEnteredColumn, true); err != nil {
		t.Fatal(err)
	}

	f.moveToReview(t, f.task.ID)

	runs, err := f.workflows.ListRuns(ctx, store.WorkflowRunFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(runs) != 1 || runs[0].WorkflowName != "learn" {
		t.Fatalf("runs = %+v", runs)
	}
	run, err := f.workflows.Show(ctx, runs[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	interview := activeAcceptanceAttempt(t, run, "interview")
	request, err := f.db.GetLearningReflectionRequestByAttempt(ctx, interview.ID)
	if err != nil {
		t.Fatal(err)
	}
	if request.TaskID != f.task.ID || len(f.agents.deliveries) != 1 || !strings.Contains(f.agents.deliveries[0], request.ID) {
		t.Fatalf("the card's agent was not asked to reflect: request %+v, deliveries %q", request, f.agents.deliveries)
	}
	reflection, err := f.learning.SubmitReflection(ctx, request.ID, ReflectionDocument{
		SchemaVersion: request.SchemaVersion,
		Observations: []ReflectionObservation{{
			Kind: "technical_fact", Content: "A completion callback must carry the active workflow attempt ID.",
			Rationale: "This prevents stale agents from advancing a newer attempt.",
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	run, err = f.workflows.Complete(ctx, run.Run.ID, "interview", interview.ID, map[string]string{"available": "true", "request_id": request.ID}, nil)
	if err != nil {
		t.Fatal(err)
	}

	var packet struct {
		PacketDigest string `json:"packet_digest"`
	}
	if err := json.Unmarshal([]byte(run.Steps[1].OutputsJSON), &packet); err != nil {
		t.Fatal(err)
	}
	operations := func(id, claim string) string {
		encoded, _ := json.Marshal(CuratorOperationsDocument{SchemaVersion: CuratorOperationsSchemaVersion, PacketDigest: packet.PacketDigest, Operations: []CuratorOperation{{
			Operation: "create", NewLearningID: id, EvidenceIDs: []string{reflection.Evidence[0].ID},
			Claim: claim, Kind: "technical_fact", Scope: "user", Confidence: 0.9,
		}}})
		return string(encoded)
	}
	curator := activeAcceptanceAttempt(t, run, "curator")
	if len(f.agents.assignments) != 1 || !strings.Contains(f.agents.assignments[0].Assignment, "Bounded packet") {
		t.Fatalf("curator assignment = %+v", f.agents.assignments)
	}
	run, err = f.workflows.Complete(ctx, run.Run.ID, "curator", curator.ID, map[string]string{"operations": operations("learn-curator-draft", "During this task, change this callback.")}, nil)
	if err != nil {
		t.Fatal(err)
	}
	critic := activeAcceptanceAttempt(t, run, "critic")
	if len(f.agents.assignments) != 2 || !strings.Contains(f.agents.assignments[1].Assignment, "Curator draft operations") {
		t.Fatalf("critic assignment = %+v", f.agents.assignments)
	}
	run, err = f.workflows.Complete(ctx, run.Run.ID, "critic", critic.ID, map[string]string{"operations": operations("learn-template-callback", "Always bind completion callbacks to the active workflow attempt.")}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if run.Run.Status != "succeeded" {
		t.Fatalf("run = %+v", run.Run)
	}
	candidate, err := f.learning.Show(ctx, "learn-template-callback")
	if err != nil {
		t.Fatal(err)
	}
	if candidate.Learning.Status != "candidate" {
		t.Fatalf("learning = %+v, want a candidate", candidate.Learning)
	}
	if _, err := f.learning.Show(ctx, "learn-curator-draft"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("the curator's draft was ingested: %v", err)
	}

	// The run's own task is a Learn run's: moving it to Review starts nothing.
	f.moveToReview(t, run.Run.TaskID)
	if again, err := f.workflows.ListRuns(ctx, store.WorkflowRunFilter{}); err != nil || len(again) != 1 {
		t.Fatalf("runs after the run's own task entered Review = %d, %v", len(again), err)
	}
}

func TestTheLearnTemplatePausedOrAbsentStartsNothing(t *testing.T) {
	ctx := context.Background()
	for name, definitions := range map[string][]*workflowengine.Definition{
		"paused": {learnTemplate(t)},
		"absent": nil,
	} {
		t.Run(name, func(t *testing.T) {
			f := newLearnTemplateFixture(t, definitions...)
			f.moveToReview(t, f.task.ID)
			requests, err := f.db.ListWorkflowTriggerRequests(ctx, "")
			if err != nil {
				t.Fatal(err)
			}
			runs, err := f.workflows.ListRuns(ctx, store.WorkflowRunFilter{})
			if err != nil {
				t.Fatal(err)
			}
			if len(requests) != 0 || len(runs) != 0 || len(f.agents.deliveries) != 0 {
				t.Fatalf("requests %+v, runs %+v, deliveries %q", requests, runs, f.agents.deliveries)
			}
		})
	}
}

func TestOnlyARunOfAWorkflowWithLearnStepsIsMarkedALearnRun(t *testing.T) {
	ctx := context.Background()
	for name, tc := range map[string]struct {
		yaml   string
		marked bool
	}{
		"a reflection alone": {marked: true, yaml: `schema_version: 1
name: reflect
title: Reflect
inputs:
  task: {type: task, required: true}
steps:
  - {id: interview, title: Reflect, type: agent.interview, with: {task: "${{ workflow.inputs.task }}"}}
`},
		"no Learn step": {marked: false, yaml: `schema_version: 1
name: note
title: Note
inputs:
  task: {type: task, required: true}
steps:
  - {id: note, title: Note, type: task.note, with: {task: "${{ workflow.inputs.task }}", body: hi}}
`},
	} {
		t.Run(name, func(t *testing.T) {
			definition, err := workflowengine.Parse("/workflows/"+name+".workflow.yaml", []byte(tc.yaml))
			if err != nil {
				t.Fatal(err)
			}
			f := newLearnTemplateFixture(t)
			workflows := NewWorkflowService(f.db, []WorkflowExecutor{
				&fakeWorkflowExecutor{typeName: "agent.interview", result: WorkflowExecutionResult{Status: "waiting_agent"}},
				&fakeWorkflowExecutor{typeName: "task.note", result: WorkflowExecutionResult{Status: "succeeded"}},
			})
			run, err := workflows.Start(ctx, definition, map[string]string{"task": f.task.ID})
			if err != nil {
				t.Fatal(err)
			}
			task, err := f.db.GetTask(ctx, run.Run.TaskID)
			if err != nil {
				t.Fatal(err)
			}
			metadata, _ := store.ParseTaskMetadata(task.Metadata)
			legato, _ := metadata["legato"].(map[string]any)
			if marked := legato["learning_workflow"] == true; marked != tc.marked {
				t.Fatalf("marked = %v, want %v (metadata %v)", marked, tc.marked, metadata)
			}
		})
	}
}
