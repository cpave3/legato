package service

import (
	"context"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/cpave3/legato/internal/engine/store"
	workflowengine "github.com/cpave3/legato/internal/engine/workflow"
)

// slotAgentWorkflow is a run whose agent.run step works on the selected
// task's card in its named slot triage (LEG-618).
const slotAgentWorkflow = `
schema_version: 1
name: triage-on-the-card
title: Triage on the card
inputs:
  task: {type: task, required: true}
steps:
 - id: triage
   title: Triage
   type: agent.run
   with: {task: "${{ workflow.inputs.task }}", slot: triage, prompt: Triage it, agent: claude-code}
   outputs:
     verdict: {type: string}
`

// startSlotRunBesideTheCardsAgent starts a person's agent on LEG-7, then a
// run whose step starts LEG-7's triage agent.
func startSlotRunBesideTheCardsAgent(t *testing.T, yaml string) (AgentService, *store.Store, *mockTmux, *WorkflowService, *store.WorkflowRunView, *store.AgentSession) {
	t.Helper()
	ctx := context.Background()
	agents, s, mt := newTestAgentService(t)
	createTask(t, s, "LEG-7")
	if err := agents.SpawnAgent(ctx, "LEG-7", 0, 0); err != nil {
		t.Fatal(err)
	}
	main, err := s.GetAgentSessionByTaskID(ctx, "LEG-7")
	if err != nil {
		t.Fatal(err)
	}
	workflows := NewWorkflowService(s, []WorkflowExecutor{NewAgentRunExecutor(NewWorkflowAgentRunner(agents, s))})
	def, err := workflowengine.Parse("slot.yaml", []byte(yaml))
	if err != nil {
		t.Fatal(err)
	}
	view, err := workflows.Start(ctx, def, map[string]string{"task": "LEG-7"})
	if err != nil {
		t.Fatal(err)
	}
	return agents, s, mt, workflows, view, main
}

func TestAnAgentRunStepWithASlotStartsBesideTheCardsOwnAgent(t *testing.T) {
	ctx := context.Background()
	_, s, mt, workflows, view, main := startSlotRunBesideTheCardsAgent(t, slotAgentWorkflow)
	attempt := view.Attempts[0]
	if attempt.Status != "waiting_agent" || attempt.TaskID != "LEG-7" {
		t.Fatalf("attempt = %+v; want it waiting on LEG-7's triage agent", attempt)
	}
	triage, err := s.GetAgentSessionInSlot(ctx, "LEG-7", "triage")
	if err != nil {
		t.Fatalf("LEG-7 has no triage agent: %v", err)
	}
	if triage.TmuxSession != "legato-LEG-7_triage" || triage.WorkflowRunID != view.Run.ID || triage.ExecutionKind != store.ExecutionWorkflowAgent {
		t.Fatalf("triage session = %+v", triage)
	}
	// The attempt is the triage agent's, not the card's own agent's.
	if attempt.AgentSessionID == nil || *attempt.AgentSessionID != triage.ID || attempt.TmuxSession != "legato-LEG-7_triage" {
		t.Fatalf("attempt's agent = %v %q; want session %d", attempt.AgentSessionID, attempt.TmuxSession, triage.ID)
	}
	if again, err := s.GetAgentSessionByTaskID(ctx, "LEG-7"); err != nil || again.ID != main.ID || !mt.sessionAlive("legato-LEG-7") {
		t.Fatalf("the card's own agent = %+v, %v", again, err)
	}
	// The run page links the attempt to the agent under the card.
	got, err := workflows.Show(ctx, view.Run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Attempts[0].AgentID != "LEG-7_triage" {
		t.Fatalf("attempt's agent ID = %q", got.Attempts[0].AgentID)
	}
}

func TestASlottedStepIsRefusedWhenItsSlotIsTaken(t *testing.T) {
	ctx := context.Background()
	_, s, _, workflows, first, _ := startSlotRunBesideTheCardsAgent(t, slotAgentWorkflow)
	triage, err := s.GetAgentSessionInSlot(ctx, "LEG-7", "triage")
	if err != nil {
		t.Fatal(err)
	}
	def, err := workflowengine.Parse("slot.yaml", []byte(slotAgentWorkflow))
	if err != nil {
		t.Fatal(err)
	}
	second, err := workflows.Start(ctx, def, map[string]string{"task": "LEG-7"})
	if err != nil {
		t.Fatal(err)
	}
	if second.Run.Status != "failed" || !strings.Contains(second.Attempts[0].Error, "task LEG-7 already has an agent in its triage slot") {
		t.Fatalf("run = %s, attempt = %+v", second.Run.Status, second.Attempts[0])
	}
	if still, err := s.GetAgentSessionInSlot(ctx, "LEG-7", "triage"); err != nil || still.ID != triage.ID || still.WorkflowRunID != first.Run.ID {
		t.Fatalf("the first run's triage agent = %+v, %v", still, err)
	}
}

func TestCompletingASlottedStepRemovesOnlyItsAgentUnlessItIsKept(t *testing.T) {
	ctx := context.Background()
	for _, keep := range []bool{false, true} {
		yaml := slotAgentWorkflow
		if keep {
			yaml = strings.Replace(yaml, "agent: claude-code}", "agent: claude-code, keep_agent: true}", 1)
		}
		_, s, mt, workflows, view, main := startSlotRunBesideTheCardsAgent(t, yaml)
		done, err := workflows.Complete(ctx, view.Run.ID, "triage", view.Attempts[0].ID, map[string]string{"verdict": "ready"}, nil)
		if err != nil {
			t.Fatal(err)
		}
		if done.Run.Status != "succeeded" {
			t.Fatalf("keep=%v: run = %s", keep, done.Run.Status)
		}
		_, err = s.GetAgentSessionInSlot(ctx, "LEG-7", "triage")
		if running := err == nil; running != keep {
			t.Fatalf("keep=%v: the triage agent running = %v", keep, running)
		}
		if again, err := s.GetAgentSessionByTaskID(ctx, "LEG-7"); err != nil || again.ID != main.ID || !mt.sessionAlive("legato-LEG-7") {
			t.Fatalf("keep=%v: the card's own agent = %+v, %v", keep, again, err)
		}
	}
}

func TestCancellingASlottedRunStopsOnlyItsAgent(t *testing.T) {
	ctx := context.Background()
	_, s, mt, workflows, view, main := startSlotRunBesideTheCardsAgent(t, slotAgentWorkflow)
	if _, err := workflows.Cancel(ctx, view.Run.ID, "test"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetAgentSessionInSlot(ctx, "LEG-7", "triage"); err == nil {
		t.Fatal("the triage agent still runs")
	}
	if again, err := s.GetAgentSessionByTaskID(ctx, "LEG-7"); err != nil || again.ID != main.ID || !mt.sessionAlive("legato-LEG-7") {
		t.Fatalf("the card's own agent = %+v, %v", again, err)
	}
}

func TestASlottedStepIsPromptedInItsSlot(t *testing.T) {
	ctx := context.Background()
	_, s, mt, workflows, view, main := startSlotRunBesideTheCardsAgent(t, slotAgentWorkflow)
	triage, err := s.GetAgentSessionInSlot(ctx, "LEG-7", "triage")
	if err != nil {
		t.Fatal(err)
	}
	// The card's agent works; the triage agent stopped short of the step.
	if err := s.UpdateAgentActivityForSession(ctx, main.ID, store.ActivityWorking); err != nil {
		t.Fatal(err)
	}
	if err := s.UpdateAgentActivityForSession(ctx, triage.ID, ""); err != nil {
		t.Fatal(err)
	}
	sentTo := func(name string) []string { return append(mt.sentMultilineFor(name), mt.sentLinesFor(name)...) }
	before := len(sentTo("legato-LEG-7"))
	if _, err := workflows.PromptStep(ctx, view.Run.ID, "triage"); err != nil {
		t.Fatal(err)
	}
	if !slices.ContainsFunc(sentTo("legato-LEG-7_triage"), func(line string) bool { return strings.Contains(line, "Continue your workflow assignment") }) {
		t.Fatalf("sent to the triage agent: %q", sentTo("legato-LEG-7_triage"))
	}
	if len(sentTo("legato-LEG-7")) != before {
		t.Fatalf("the card's own agent was prompted: %q", sentTo("legato-LEG-7"))
	}
}

// The run's attention reads the step's own agent: the triage agent is
// waiting while the card's agent works.
func TestAttentionReadsTheSlottedStepsAgent(t *testing.T) {
	ctx := context.Background()
	_, s, _, workflows, view, main := startSlotRunBesideTheCardsAgent(t, slotAgentWorkflow)
	triage, err := s.GetAgentSessionInSlot(ctx, "LEG-7", "triage")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.UpdateAgentActivityForSession(ctx, main.ID, store.ActivityWorking); err != nil {
		t.Fatal(err)
	}
	if err := s.UpdateAgentActivityForSession(ctx, triage.ID, store.ActivityWaiting); err != nil {
		t.Fatal(err)
	}
	got, err := workflows.Show(ctx, view.Run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if attempt := got.Attempts[0]; attempt.AgentActivity != store.ActivityWaiting || attempt.AttentionReason != "agent waiting for input" {
		t.Fatalf("attempt = %q %q", attempt.AgentActivity, attempt.AttentionReason)
	}
}

func TestASlottedAgentIsListedUnderTheCardWithItsStage(t *testing.T) {
	ctx := context.Background()
	agents, s, _, _, _, _ := startSlotRunBesideTheCardsAgent(t, slotAgentWorkflow)
	list, err := agents.ListAgents(ctx)
	if err != nil {
		t.Fatal(err)
	}
	i := slices.IndexFunc(list, func(a AgentSession) bool { return a.TaskID == "LEG-7_triage" })
	if i < 0 {
		t.Fatalf("no LEG-7_triage among %+v", list)
	}
	if row := list[i]; row.CardTaskID != "LEG-7" || row.Slot != "triage" || !strings.HasSuffix(row.Title, " · triage") {
		t.Fatalf("row = %+v", row)
	}
	// The card shows it as a chip of its own beside its own agent.
	card := SummarizeRelatedAgents(list)["LEG-7"]
	if card.Count != 1 || len(card.Slots) != 1 || card.Slots[0].Slot != "triage" || card.Slots[0].Stage == "" {
		t.Fatalf("the card's agents = %+v", card)
	}
	if stage, err := s.GetWorkStage(ctx, TaskPlace("LEG-7", "triage")); err != nil || stage == nil {
		t.Fatalf("the triage slot's stage = %+v, %v", stage, err)
	}
	triage, err := s.GetAgentSessionInSlot(ctx, "LEG-7", "triage")
	if err != nil {
		t.Fatal(err)
	}
	if place := SessionPlace(*triage); place != TaskPlace("LEG-7", "triage") {
		t.Fatalf("place = %+v", place)
	}
	if place := PlaceOf(list[i]); place != TaskPlace("LEG-7", "triage") {
		t.Fatalf("PlaceOf = %+v", place)
	}
}

// The engine checks a step's slot by the agent service's own rules.
func TestTheEngineChecksSlotsByTheAgentServicesRules(t *testing.T) {
	if workflowengine.SlotNamePattern != slotName.String() {
		t.Fatalf("engine pattern %q, agent service %q", workflowengine.SlotNamePattern, slotName.String())
	}
	if _, err := regexp.Compile(workflowengine.SlotNamePattern); err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(workflowengine.ReservedSlots, SlotReviewer) {
		t.Fatalf("reserved = %v", workflowengine.ReservedSlots)
	}
}
