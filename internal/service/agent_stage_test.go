package service

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/cpave3/legato/internal/engine/events"
	"github.com/cpave3/legato/internal/engine/store"
)

type stageFixture struct {
	stages *WorkStageService
	store  *store.Store
	bus    *events.Bus
}

func newStageFixture(t *testing.T) stageFixture {
	t.Helper()
	db, err := store.New(filepath.Join(t.TempDir(), "stages.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	bus := events.New()
	return stageFixture{stages: NewWorkStageService(db, bus), store: db, bus: bus}
}

func (f stageFixture) stage(t *testing.T, place WorkStagePlace) store.WorkStage {
	t.Helper()
	got, err := f.stages.Get(context.Background(), place)
	if err != nil {
		t.Fatal(err)
	}
	if got == nil {
		return store.WorkStage{}
	}
	return *got
}

func (f stageFixture) milestone(t *testing.T, place WorkStagePlace, stage, reason string, feedback bool) bool {
	t.Helper()
	changed, err := f.stages.Milestone(context.Background(), place, WorkMilestone{Stage: stage, Reason: reason, Feedback: feedback})
	if err != nil {
		t.Fatal(err)
	}
	return changed
}

// Milestones move a place forward only; review feedback may take it back to
// Implementing, with the pass in its reason.
func TestMilestonesMoveForwardOnlyExceptFeedback(t *testing.T) {
	f := newStageFixture(t)
	ctx := context.Background()
	createTask(t, f.store, "LEG-1")
	place := TaskPlace("LEG-1", "")
	if err := f.stages.Start(ctx, place, "ex-1"); err != nil {
		t.Fatal(err)
	}
	if got := f.stage(t, place); got.Stage != store.WorkStageExploring || got.Source != store.WorkStageSourceStart {
		t.Fatalf("a new conversation starts at %+v", got)
	}

	f.milestone(t, place, store.WorkStageReviewing, "PR #12 ready", false)
	if f.milestone(t, place, store.WorkStageImplementing, "plan approved", false) {
		t.Fatal("a milestone moved the stage back")
	}
	if got := f.stage(t, place); got.Stage != store.WorkStageReviewing || got.Reason != "PR #12 ready" {
		t.Fatalf("after a backward milestone: %+v", got)
	}

	f.milestone(t, place, store.WorkStageImplementing, "fixing review feedback · pass 2", true)
	got := f.stage(t, place)
	if got.Stage != store.WorkStageImplementing || got.Reason != "fixing review feedback · pass 2" || got.Source != store.WorkStageSourceMilestone {
		t.Fatalf("after feedback: %+v", got)
	}

	history, err := f.stages.History(ctx, place, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(history) != 3 || history[0].Stage != store.WorkStageImplementing || history[0].FromStage != store.WorkStageReviewing || history[2].Stage != store.WorkStageExploring {
		t.Fatalf("history = %+v", history)
	}
}

// Evidence of an edit moves a place to Implementing, from any stage, and
// never further; a reviewer slot ignores it.
func TestEvidenceMovesToImplementingAndNoFurther(t *testing.T) {
	f := newStageFixture(t)
	ctx := context.Background()
	createTask(t, f.store, "LEG-2")
	place := TaskPlace("LEG-2", "")
	_ = f.stages.Start(ctx, place, "ex-1")

	if changed, err := f.stages.Evidence(ctx, place, "ex-1", "edited main.go", false); err != nil || !changed {
		t.Fatalf("evidence while exploring: %v, %v", changed, err)
	}
	if changed, _ := f.stages.Evidence(ctx, place, "ex-1", "edited util.go", false); changed {
		t.Fatal("a second edit changed an implementing stage")
	}

	// An edit after Finished gives Implementing again.
	f.milestone(t, place, store.WorkStageFinished, "PR merged", false)
	if changed, _ := f.stages.Evidence(ctx, place, "ex-1", "edited main.go", false); !changed {
		t.Fatal("an edit after Finished did not move the stage")
	}
	if got := f.stage(t, place); got.Stage != store.WorkStageImplementing || got.Source != store.WorkStageSourceEvidence {
		t.Fatalf("after an edit after Finished: %+v", got)
	}

	reviewer := TaskPlace("LEG-2", SlotReviewer)
	_ = f.stages.Start(ctx, reviewer, "ex-2")
	if got := f.stage(t, reviewer); got.Stage != store.WorkStageReviewing {
		t.Fatalf("a reviewer slot starts at %q", got.Stage)
	}
	if changed, _ := f.stages.Evidence(ctx, reviewer, "ex-2", "edited", false); changed {
		t.Fatal("evidence moved a reviewer slot")
	}
	if changed, _ := f.stages.Evidence(ctx, TaskPlace("LEG-2", "other"), "ex-3", "edited", true); changed {
		t.Fatal("evidence moved a read-only place")
	}
}

// The operator and the agent may set any stage, back or forward.
func TestTheOperatorAndTheAgentSetAnyStage(t *testing.T) {
	f := newStageFixture(t)
	ctx := context.Background()
	createTask(t, f.store, "LEG-3")
	place := TaskPlace("LEG-3", "")
	ch := f.bus.Subscribe(events.EventAgentStageChanged)

	f.milestone(t, place, store.WorkStageFinished, "PR merged", false)
	<-ch
	got, err := f.stages.SetByOperator(ctx, place, "Implementing", "one more fix")
	if err != nil {
		t.Fatal(err)
	}
	if got.Stage != store.WorkStageImplementing || got.Source != store.WorkStageSourceOperator || got.Reason != "one more fix" {
		t.Fatalf("set by the operator: %+v", got)
	}
	select {
	case ev := <-ch:
		p := ev.Payload.(events.AgentStageChangedPayload)
		if p.TaskID != "LEG-3" || p.Slot != store.SlotMain || p.Stage != store.WorkStageImplementing || p.Reason != "one more fix" {
			t.Fatalf("published %+v", p)
		}
	case <-time.After(time.Second):
		t.Fatal("no agent_stage_changed")
	}

	if changed, err := f.stages.Report(ctx, place, "ex-1", "exploring", "answering a question"); err != nil || !changed {
		t.Fatalf("report: %v, %v", changed, err)
	}
	if got := f.stage(t, place); got.Stage != store.WorkStageExploring || got.Source != store.WorkStageSourceAgent {
		t.Fatalf("reported: %+v", got)
	}

	if _, err := f.stages.SetByOperator(ctx, place, "done", ""); !errors.Is(err, ErrInvalidWorkStage) {
		t.Fatalf("an unknown stage: %v", err)
	}
}

// The Reeve has no stage: signals about it are ignored and the operator is
// refused.
func TestTheReeveHasNoStage(t *testing.T) {
	f := newStageFixture(t)
	ctx := context.Background()
	reeve, err := f.store.CreateReeveTask(ctx, "Reeve")
	if err != nil {
		t.Fatal(err)
	}
	place := TaskPlace(reeve, "")
	_ = f.stages.Start(ctx, place, "ex-1")
	_, _ = f.stages.Evidence(ctx, place, "ex-1", "edited", false)
	if got := f.stage(t, place); got.Stage != "" {
		t.Fatalf("the Reeve has stage %q", got.Stage)
	}
	if _, err := f.stages.SetByOperator(ctx, place, "finished", ""); !errors.Is(err, ErrNoWorkStage) {
		t.Fatalf("setting the Reeve's stage: %v", err)
	}
}

// Two workflow agents of one task each keep a stage of their own.
func TestWorkflowAgentsAreKeyedByExecution(t *testing.T) {
	f := newStageFixture(t)
	ctx := context.Background()
	createTask(t, f.store, "LEG-4")
	first := WorkStagePlace{TaskID: "LEG-4", Slot: "main", PlaceExecutionID: "ex-a"}
	second := WorkStagePlace{TaskID: "LEG-4", Slot: "main", PlaceExecutionID: "ex-b"}
	_ = f.stages.Start(ctx, first, "ex-a")
	_ = f.stages.Start(ctx, second, "ex-b")
	f.milestone(t, first, store.WorkStageFinished, "step succeeded", false)
	if a, b := f.stage(t, first), f.stage(t, second); a.Stage != store.WorkStageFinished || b.Stage != store.WorkStageExploring {
		t.Fatalf("first %q, second %q", a.Stage, b.Stage)
	}
	if task := f.stage(t, TaskPlace("LEG-4", "")); task.Stage != "" {
		t.Fatalf("the task's main slot took %q from a workflow agent", task.Stage)
	}
}

func TestGlance(t *testing.T) {
	cases := []struct {
		name string
		in   GlanceInput
		want string
	}{
		{"a done note is finished, not needs you", GlanceInput{Status: "running", Stage: store.WorkStageFinished}, GlanceFinished},
		{"an idle agent with a proposed plan needs you", GlanceInput{Status: "running", Stage: store.WorkStagePlanning, PlanPending: true}, GlanceNeedsYou},
		{"a plan sent back, idle, is your turn", GlanceInput{Status: "running", Stage: store.WorkStagePlanning}, GlanceYourTurn},
		{"a question needs you", GlanceInput{Status: "running", Activity: store.ActivityWorking, NeedsItems: 1}, GlanceNeedsYou},
		{"waiting needs you", GlanceInput{Status: "running", Activity: store.ActivityWaiting, Stage: store.WorkStageFinished}, GlanceNeedsYou},
		{"an error needs you", GlanceInput{Status: "running", Activity: store.ActivityError}, GlanceNeedsYou},
		{"working", GlanceInput{Status: "running", Activity: store.ActivityWorking, Stage: store.WorkStageFinished}, GlanceWorking},
		{"background is working", GlanceInput{Status: "running", Activity: store.ActivityBackground}, GlanceWorking},
		{"idle mid-work is your turn", GlanceInput{Status: "running", Stage: store.WorkStageImplementing}, GlanceYourTurn},
		{"no stage recorded, idle, is your turn", GlanceInput{Status: "running"}, GlanceYourTurn},
		{"ended", GlanceInput{Status: "dead", Activity: store.ActivityWaiting}, GlanceEnded},
	}
	for _, c := range cases {
		if got := Glance(c.in); got != c.want {
			t.Errorf("%s: %q, want %q", c.name, got, c.want)
		}
	}
}

// A fresh spawn resets the stage; a resumed one keeps it. The listed agent
// carries its stage and glance, and a done note leaves it Finished.
func TestSpawnResetsTheStageUnlessItResumes(t *testing.T) {
	f := newSlotFixture(t)
	ctx := context.Background()
	stages := NewWorkStageService(f.store, nil)
	f.svc.(*agentService).SetWorkStages(stages)
	createTask(t, f.store, "task-r")
	place := TaskPlace("task-r", "")

	if err := f.svc.SpawnAgent(ctx, "task-r", 0, 0, AgentSpawnOptions{AgentKind: "shell", WorkingDir: t.TempDir()}); err != nil {
		t.Fatal(err)
	}
	if _, err := stages.Milestone(ctx, place, WorkMilestone{Stage: store.WorkStageFinished, Reason: "PR merged"}); err != nil {
		t.Fatal(err)
	}
	agent := onlyAgent(t, f.svc)
	if _, err := f.store.PostOperatorItem(ctx, store.OperatorItem{
		ID: "oi-1", TaskID: "task-r", AttributionTaskID: "task-r", ExecutionID: agent.ExecutionID, Kind: "done", Title: "Merged",
	}, nil); err != nil {
		t.Fatal(err)
	}
	agent = onlyAgent(t, f.svc)
	if agent.Stage != store.WorkStageFinished || agent.StageReason != "PR merged" || agent.StageSince == nil {
		t.Fatalf("listed agent's stage: %q %q %v", agent.Stage, agent.StageReason, agent.StageSince)
	}
	if agent.Glance() != GlanceFinished || agent.NeedsOperator() || agent.ShownActivity() == store.ActivityWaiting {
		t.Fatalf("a done note: glance %q, needs operator %v, shown %q", agent.Glance(), agent.NeedsOperator(), agent.ShownActivity())
	}

	// Resumed: the stage stays.
	if err := f.svc.KillAgent(ctx, "task-r"); err != nil {
		t.Fatal(err)
	}
	if err := f.svc.SpawnAgent(ctx, "task-r", 0, 0, AgentSpawnOptions{AgentKind: "chimera", WorkingDir: t.TempDir(), SessionPolicy: "resume"}); err != nil {
		t.Fatal(err)
	}
	if got := onlyAgent(t, f.svc); got.Stage != store.WorkStageFinished {
		t.Fatalf("a resumed agent's stage: %q", got.Stage)
	}

	// Fresh: back to Exploring.
	if err := f.svc.KillAgent(ctx, "task-r"); err != nil {
		t.Fatal(err)
	}
	if err := f.svc.SpawnAgent(ctx, "task-r", 0, 0, AgentSpawnOptions{AgentKind: "shell", WorkingDir: t.TempDir()}); err != nil {
		t.Fatal(err)
	}
	if got := onlyAgent(t, f.svc); got.Stage != store.WorkStageExploring || got.StageSource != store.WorkStageSourceStart {
		t.Fatalf("a fresh agent's stage: %q from %q", got.Stage, got.StageSource)
	}
}

// A task's proposed plan makes its main slot's agent need the operator.
func TestAProposedPlanMakesTheAgentNeedTheOperator(t *testing.T) {
	f := newSlotFixture(t)
	ctx := context.Background()
	createTask(t, f.store, "task-p")
	if err := f.svc.SpawnAgent(ctx, "task-p", 0, 0, AgentSpawnOptions{AgentKind: "shell", WorkingDir: t.TempDir()}); err != nil {
		t.Fatal(err)
	}
	if got := onlyAgent(t, f.svc).Glance(); got != GlanceYourTurn {
		t.Fatalf("idle with no plan: %q", got)
	}
	planOfTask(t, f.store, "task-p", "", "proposed", "# Plan")
	if got := onlyAgent(t, f.svc).Glance(); got != GlanceNeedsYou {
		t.Fatalf("idle with a proposed plan: %q", got)
	}
}

func onlyAgent(t *testing.T, svc AgentService) AgentSession {
	t.Helper()
	agents, err := svc.ListAgents(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var running []AgentSession
	for _, a := range agents {
		if a.Status == "running" {
			running = append(running, a)
		}
	}
	if len(running) != 1 {
		t.Fatalf("running agents = %+v", running)
	}
	return running[0]
}
