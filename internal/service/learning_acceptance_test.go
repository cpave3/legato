package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cpave3/legato/internal/engine/events"
	"github.com/cpave3/legato/internal/engine/store"
)

// acceptanceAgentTransport is the boundary fake for agent/tmux work. The
// workflow executors, workflow service, learning service, and store are real.
type acceptanceAgentTransport struct {
	store       *store.Store
	deliveries  []string
	assignments []AgentSpawnOptions
}

func (f *acceptanceAgentTransport) PromptAgent(_ context.Context, _ string, prompt string) error {
	f.deliveries = append(f.deliveries, prompt)
	return nil
}

func (f *acceptanceAgentTransport) SpawnWorkflowAgent(ctx context.Context, taskID, _ string, opts AgentSpawnOptions) (*store.AgentSession, error) {
	f.assignments = append(f.assignments, opts)
	if _, err := f.store.InsertAgentSessionReturningID(ctx, store.AgentSession{
		TaskID: taskID, TmuxSession: fmt.Sprintf("fake-%s-%d", taskID, len(f.assignments)), AgentKind: opts.AgentKind, Status: "running",
	}); err != nil {
		return nil, err
	}
	return f.store.GetAgentSessionByTaskID(ctx, taskID)
}

func (f *acceptanceAgentTransport) KillAgent(context.Context, string) error { return nil }
func (f *acceptanceAgentTransport) FinishWorkflowAgent(context.Context, store.WorkflowStepAttempt) error {
	return nil
}

func TestLegatoLearnConnectedAcceptance(t *testing.T) {
	ctx := context.Background()
	db := newServiceTestStore(t)
	learning := NewLearningService(db, events.New())
	board := NewBoardService(db, events.New())

	// Start with an ordinary persisted task and a real active task-agent session,
	// rather than constructing workflow contexts or reflection requests directly.
	task, err := board.CreateTask(ctx, "Fix callback ordering", "Keep callbacks tied to their active request.", "Backlog", "High", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.InsertAgentSessionReturningID(ctx, store.AgentSession{
		TaskID: task.ID, TmuxSession: "fake-task-agent", AgentKind: "fake", Status: "running",
	}); err != nil {
		t.Fatal(err)
	}

	agents := &acceptanceAgentTransport{store: db}
	definition, err := BuiltInLearningWorkflow(LearningWorkflowOptions{
		Adapter: "fake", Tier: "acceptance", WorkingDir: t.TempDir(), PacketMaxItems: 100, PacketMaxBytes: 128 * 1024,
	})
	if err != nil {
		t.Fatal(err)
	}
	workflow := NewWorkflowService(db, []WorkflowExecutor{
		NewAgentInterviewExecutor(learning, agents, db),
		NewLearningPacketPrepareExecutor(learning),
		NewAgentRunExecutor(agents),
		NewLearningProposalsSubmitExecutor(learning),
	})

	run, err := workflow.Start(ctx, definition, map[string]string{
		"task": task.ID, "project_key": "repo:acme/legato", "project_label": "acme/legato",
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	interviewStep := definition.Steps[0].ID
	interviewAttempt := activeAcceptanceAttempt(t, run, interviewStep)
	request, err := db.GetLearningReflectionRequestByAttempt(ctx, interviewAttempt.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(agents.deliveries) != 1 || !strings.Contains(agents.deliveries[0], request.ID) {
		t.Fatalf("reflection delivery = %q, request = %q", agents.deliveries, request.ID)
	}

	reflection, err := learning.SubmitReflection(ctx, request.ID, ReflectionDocument{
		SchemaVersion: request.SchemaVersion,
		Observations: []ReflectionObservation{{
			Kind: "technical_fact", Content: "A completion callback must carry the active workflow attempt ID.",
			Rationale: "This prevents stale agents from advancing a newer attempt.",
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(reflection.Evidence) != 1 {
		t.Fatalf("reflection evidence = %+v", reflection.Evidence)
	}
	run, err = workflow.Complete(ctx, run.Run.ID, interviewStep, interviewAttempt.ID,
		map[string]string{"available": "true", "request_id": request.ID}, nil)
	if err != nil {
		t.Fatal(err)
	}

	// Packet preparation ran synchronously through its real executor. The fake
	// curator is now waiting and returns its declared output through Complete.
	curatorStep := definition.Steps[2].ID
	curatorAttempt := activeAcceptanceAttempt(t, run, curatorStep)
	if len(agents.assignments) != 1 || !strings.Contains(agents.assignments[0].Assignment, "Bounded packet") {
		t.Fatalf("curator assignment was not delivered: %+v", agents.assignments)
	}
	var packetOutputs struct {
		PacketDigest string `json:"packet_digest"`
	}
	if err := json.Unmarshal([]byte(run.Steps[1].OutputsJSON), &packetOutputs); err != nil {
		t.Fatal(err)
	}
	curatorOperations := CuratorOperationsDocument{
		SchemaVersion: CuratorOperationsSchemaVersion,
		PacketDigest:  packetOutputs.PacketDigest,
		Operations: []CuratorOperation{{
			Operation: "create", NewLearningID: "learn-curator-session-specific", EvidenceIDs: []string{reflection.Evidence[0].ID},
			Claim: "During Fix callback ordering, change this callback.", Kind: "technical_fact",
			Scope: "project", ProjectKey: "repo:acme/legato", Confidence: 0.9,
		}},
	}
	encodedCuratorOperations, err := json.Marshal(curatorOperations)
	if err != nil {
		t.Fatal(err)
	}
	run, err = workflow.Complete(ctx, run.Run.ID, curatorStep, curatorAttempt.ID,
		map[string]string{"operations": string(encodedCuratorOperations)}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := learning.Show(ctx, "learn-acceptance-callback"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("curator draft was ingested before critic approval: %v", err)
	}
	criticStep := definition.Steps[3].ID
	criticAttempt := activeAcceptanceAttempt(t, run, criticStep)
	if len(agents.assignments) != 2 || !strings.Contains(agents.assignments[1].Assignment, "Curator draft operations") {
		t.Fatalf("critic assignment was not delivered: %+v", agents.assignments)
	}
	criticOperations := CuratorOperationsDocument{
		SchemaVersion: CuratorOperationsSchemaVersion,
		PacketDigest:  packetOutputs.PacketDigest,
		Operations: []CuratorOperation{{
			Operation: "create", NewLearningID: "learn-acceptance-callback", EvidenceIDs: []string{reflection.Evidence[0].ID},
			Claim: "Always bind completion callbacks to the active workflow attempt.", Kind: "technical_fact",
			Scope: "project", ProjectKey: "repo:acme/legato", Confidence: 0.9,
		}},
	}
	encodedCriticOperations, err := json.Marshal(criticOperations)
	if err != nil {
		t.Fatal(err)
	}
	run, err = workflow.Complete(ctx, run.Run.ID, criticStep, criticAttempt.ID,
		map[string]string{"operations": string(encodedCriticOperations)}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if run.Run.Status != "succeeded" || run.Steps[4].Status != "succeeded" {
		t.Fatalf("workflow did not deterministically submit critic proposals: %+v", run)
	}

	if _, err := learning.Show(ctx, "learn-curator-session-specific"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("critic rewrite did not replace curator draft: %v", err)
	}
	candidate, err := learning.Show(ctx, "learn-acceptance-callback")
	if err != nil {
		t.Fatal(err)
	}
	accepted, err := learning.Accept(ctx, candidate.Learning.ID, candidate.Learning.Revision, "", "", "")
	if err != nil {
		t.Fatal(err)
	}

	// A documentation incorporation is verifiable, but intentionally never
	// becomes standing agent context.
	root := t.TempDir()
	docTarget := "docs/callbacks.md"
	docContent := "Callbacks must include the active workflow attempt ID.\n"
	if err := os.MkdirAll(filepath.Join(root, "docs"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, filepath.FromSlash(docTarget)), []byte(docContent), 0o600); err != nil {
		t.Fatal(err)
	}
	docInc, err := learning.ProposeIncorporation(ctx, accepted.Learning.ID, accepted.Learning.Revision, "documentation", docTarget, "document")
	if err != nil {
		t.Fatal(err)
	}
	docInc, err = learning.SubmitIncorporation(ctx, docInc.ID, docInc.Revision, []string{docTarget + ":1"}, "exact_content", docContent)
	if err != nil {
		t.Fatal(err)
	}
	docInc, err = learning.VerifyIncorporationTarget(ctx, docInc.ID, docInc.Revision, root, false)
	if err != nil || docInc.Status != "verified" {
		t.Fatalf("documentation verification = %+v, %v", docInc, err)
	}
	compiled, err := learning.CompileLearningPrompt(ctx, LearningPromptCompileInput{
		TaskID: task.ID, Adapter: "fake", ProjectKey: "repo:acme/legato", MaxItems: 20, MaxBytes: 4096,
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(compiled.Rendered, accepted.Learning.Claim) {
		t.Fatalf("documentation-only learning leaked into prompt: %q", compiled.Rendered)
	}

	// Accepted learnings default disabled and require an explicit activation.
	const preference = "Prefer focused diffs with no unrelated formatting changes."
	preferenceView, err := learning.Record(ctx, LearningRecordInput{
		Scope: "user", Kind: "preference", Claim: preference, SourceKind: "user_feedback",
		SourceRef: "acceptance:user-preference", Author: "user",
	})
	if err != nil {
		t.Fatal(err)
	}
	preferenceView, err = learning.Accept(ctx, preferenceView.Learning.ID, preferenceView.Learning.Revision, "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if preferenceView.Learning.PromptEnabled {
		t.Fatal("accepted learning enabled before user decision")
	}
	preferenceView, err = learning.SetPromptEnabled(ctx, preferenceView.Learning.ID, preferenceView.Learning.Revision, true)
	if err != nil {
		t.Fatal(err)
	}

	// Exercise project standing-context isolation separately from the workflow's
	// documentation destination.
	projectPrompt, err := learning.Record(ctx, LearningRecordInput{
		Scope: "project", ProjectKey: "repo:acme/legato", Kind: "technical_fact",
		Claim: "Run the Legato callback contract tests.", SourceKind: "acceptance",
		SourceRef: "acceptance:project-context", Author: "user",
	})
	if err != nil {
		t.Fatal(err)
	}
	projectPrompt, err = learning.Accept(ctx, projectPrompt.Learning.ID, projectPrompt.Learning.Revision, "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	projectPrompt, err = learning.SetPromptEnabled(ctx, projectPrompt.Learning.ID, projectPrompt.Learning.Revision, true)
	if err != nil {
		t.Fatal(err)
	}

	for _, project := range []string{"repo:acme/legato", "repo:unrelated/example"} {
		prompt, compileErr := learning.CompileLearningPrompt(ctx, LearningPromptCompileInput{
			TaskID: task.ID, Adapter: "fake", ProjectKey: project, MaxItems: 20, MaxBytes: 4096,
		})
		if compileErr != nil {
			t.Fatal(compileErr)
		}
		if !strings.Contains(prompt.Rendered, "- "+preference) {
			t.Fatalf("user preference missing for %q: %q", project, prompt.Rendered)
		}
		containsProjectLearning := strings.Contains(prompt.Rendered, "- "+projectPrompt.Learning.Claim)
		if project == "repo:acme/legato" && !containsProjectLearning {
			t.Fatalf("project learning missing from matching project: %q", prompt.Rendered)
		}
		if project != "repo:acme/legato" && containsProjectLearning {
			t.Fatalf("project learning leaked into %q: %q", project, prompt.Rendered)
		}
	}

	if err := os.Remove(filepath.Join(root, filepath.FromSlash(docTarget))); err != nil {
		t.Fatal(err)
	}
	drift, err := learning.CheckIncorporationDrift(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	foundStale := false
	for _, result := range drift {
		if result.IncorporationID == docInc.ID && result.Outcome == "stale" && result.Reason == "target missing" {
			foundStale = true
		}
	}
	if !foundStale {
		t.Fatalf("missing documentation was not marked stale: %+v", drift)
	}
	stale, err := learning.Show(ctx, accepted.Learning.ID)
	if err != nil || stale.Learning.Status != "stale" {
		t.Fatalf("learning after drift = %+v, %v", stale, err)
	}
}

func TestLegatoLearnEmptyReflectionCompletesWithoutLearning(t *testing.T) {
	ctx := context.Background()
	db := newServiceTestStore(t)
	bus := events.New()
	learning := NewLearningService(db, bus)
	board := NewBoardService(db, bus)
	task, err := board.CreateTask(ctx, "No durable lesson", "Routine work", "Backlog", "Low", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.InsertAgentSessionReturningID(ctx, store.AgentSession{TaskID: task.ID, TmuxSession: "empty-task-agent", AgentKind: "fake", Status: "running"}); err != nil {
		t.Fatal(err)
	}
	agents := &acceptanceAgentTransport{store: db}
	definition, err := BuiltInLearningWorkflow(LearningWorkflowOptions{Adapter: "fake", WorkingDir: t.TempDir(), PacketMaxItems: 20, PacketMaxBytes: 64 * 1024})
	if err != nil {
		t.Fatal(err)
	}
	workflow := NewWorkflowService(db, []WorkflowExecutor{
		NewAgentInterviewExecutor(learning, agents, db), NewLearningPacketPrepareExecutor(learning),
		NewAgentRunExecutor(agents), NewLearningProposalsSubmitExecutor(learning),
	})
	run, err := workflow.Start(ctx, definition, map[string]string{"task": task.ID, "project_key": "repo:acme/empty", "project_label": "acme/empty"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	interviewAttempt := activeAcceptanceAttempt(t, run, "interview")
	request, err := db.GetLearningReflectionRequestByAttempt(ctx, interviewAttempt.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := learning.SubmitReflection(ctx, request.ID, ReflectionDocument{SchemaVersion: ReflectionSchemaVersion, Observations: []ReflectionObservation{}}); err != nil {
		t.Fatal(err)
	}
	run, err = workflow.Complete(ctx, run.Run.ID, "interview", interviewAttempt.ID, map[string]string{"available": "true", "request_id": request.ID}, nil)
	if err != nil {
		t.Fatal(err)
	}
	var packetOutputs struct {
		PacketDigest string `json:"packet_digest"`
	}
	if err := json.Unmarshal([]byte(run.Steps[1].OutputsJSON), &packetOutputs); err != nil {
		t.Fatal(err)
	}
	empty, err := json.Marshal(CuratorOperationsDocument{SchemaVersion: CuratorOperationsSchemaVersion, PacketDigest: packetOutputs.PacketDigest, Operations: []CuratorOperation{}})
	if err != nil {
		t.Fatal(err)
	}
	curatorAttempt := activeAcceptanceAttempt(t, run, "curator")
	run, err = workflow.Complete(ctx, run.Run.ID, "curator", curatorAttempt.ID, map[string]string{"operations": string(empty)}, nil)
	if err != nil {
		t.Fatal(err)
	}
	criticAttempt := activeAcceptanceAttempt(t, run, "critic")
	run, err = workflow.Complete(ctx, run.Run.ID, "critic", criticAttempt.ID, map[string]string{"operations": string(empty)}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if run.Run.Status != "succeeded" {
		t.Fatalf("workflow status = %s", run.Run.Status)
	}
	if evidence, err := db.ListLearningEvidenceByReflection(ctx, request.ID); err != nil || len(evidence) != 0 {
		t.Fatalf("evidence = %+v, err = %v", evidence, err)
	}
	if proposals, err := learning.ListProposals(ctx, "pending"); err != nil || len(proposals) != 0 {
		t.Fatalf("proposals = %+v, err = %v", proposals, err)
	}
}

func activeAcceptanceAttempt(t *testing.T, view *store.WorkflowRunView, stepID string) store.WorkflowStepAttempt {
	t.Helper()
	for _, attempt := range view.Attempts {
		if attempt.StepID == stepID && attempt.Status == "waiting_agent" {
			return attempt
		}
	}
	t.Fatalf("no waiting attempt for step %q in %+v", stepID, view.Attempts)
	return store.WorkflowStepAttempt{}
}
