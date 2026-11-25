package service

import (
	"context"
	"strings"
	"testing"

	"github.com/cpave3/legato/internal/engine/store"
	workflowengine "github.com/cpave3/legato/internal/engine/workflow"
)

func ticketStepsFixture(t *testing.T) (*fakeBurnliteServer, SyncService, *store.Store) {
	t.Helper()
	fake := buildMilestoneBurnlite()
	svc, s := newSourcesTest(t, fake.source(t))
	if _, err := s.CreateWorkspace(context.Background(), store.Workspace{Name: "Proposals"}); err != nil {
		t.Fatal(err)
	}
	return fake, svc, s
}

func TestTheTicketImportStepPutsATicketOnTheBoardOnce(t *testing.T) {
	_, svc, s := ticketStepsFixture(t)
	step := NewTicketImportExecutor(svc.(TicketImporter), s)
	ctx := context.Background()

	result, err := step.Start(ctx, ResolvedWorkflowStep{With: map[string]any{"key": " est-10 ", "provider": "burnlite", "column": "Doing", "workspace": "proposals"}})
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "succeeded" || result.Outputs["task"] != "EST-10" || result.Outputs["created"] != true || result.Outputs["restored"] != false {
		t.Fatalf("result = %+v", result)
	}
	task, _ := s.GetTask(ctx, "EST-10")
	if task.Status != "Doing" || task.WorkspaceID == nil {
		t.Fatalf("task = %+v", task)
	}

	result, err = step.Start(ctx, ResolvedWorkflowStep{With: map[string]any{"key": "EST-10", "column": "Backlog"}})
	if err != nil || result.Outputs["created"] != false {
		t.Fatalf("again: %+v, %v", result, err)
	}
	if task, _ := s.GetTask(ctx, "EST-10"); task.Status != "Doing" {
		t.Fatalf("an existing task moved to %s", task.Status)
	}

	for name, with := range map[string]map[string]any{
		"no key":            {},
		"unknown workspace": {"key": "EST-12", "workspace": "Nowhere"},
		"unknown column":    {"key": "EST-12", "column": "Nowhere"},
		"missing ticket":    {"key": "EST-99"},
	} {
		if _, err := step.Start(ctx, ResolvedWorkflowStep{With: with}); err == nil {
			t.Errorf("%s: imported", name)
		}
	}
	if _, err := NewTicketImportExecutor(nil, s).Start(ctx, ResolvedWorkflowStep{With: map[string]any{"key": "EST-12"}}); err == nil || !strings.Contains(err.Error(), "not available") {
		t.Fatalf("without sync: %v", err)
	}
}

func TestTheTicketsListStepGivesItemsAndACount(t *testing.T) {
	_, svc, _ := ticketStepsFixture(t)
	step := NewTicketsListExecutor(svc.(TicketLister))
	ctx := context.Background()

	result, err := step.Start(ctx, ResolvedWorkflowStep{With: map[string]any{"milestone": "EST-9", "kind": "work", "frontier": true, "labels": []any{"api"}}})
	if err != nil {
		t.Fatal(err)
	}
	items, _ := result.Outputs["items"].([]any)
	if result.Outputs["count"] != 1 || len(items) != 1 {
		t.Fatalf("result = %+v", result)
	}
	item, _ := items[0].(map[string]any)
	if item["key"] != "EST-10" || item["milestone"] != "EST-9" || item["frontier"] != true || item["kind"] != "work" {
		t.Fatalf("item = %#v", item)
	}
	if blockers, ok := item["open_blockers"].([]any); !ok || len(blockers) != 0 {
		t.Fatalf("open_blockers = %#v", item["open_blockers"])
	}

	none, err := step.Start(ctx, ResolvedWorkflowStep{With: map[string]any{"project": "EST", "labels": []any{"nothing"}}})
	if err != nil || none.Outputs["count"] != 0 {
		t.Fatalf("none = %+v, %v", none, err)
	}
	if got, ok := none.Outputs["items"].([]any); !ok || len(got) != 0 {
		t.Fatalf("empty items = %#v", none.Outputs["items"])
	}

	for name, with := range map[string]any{
		"limit too high":  map[string]any{"project": "EST", "limit": 201},
		"limit zero":      map[string]any{"project": "EST", "limit": 0},
		"labels not list": map[string]any{"project": "EST", "labels": "api"},
		"ready on tasks":  map[string]any{"project": "EST", "ready": true},
	} {
		if _, err := step.Start(ctx, ResolvedWorkflowStep{With: with.(map[string]any)}); err == nil {
			t.Errorf("%s: listed", name)
		}
	}
}

func boardTask(t *testing.T, s *store.Store, id, column string, workspace *int, metadata string) {
	t.Helper()
	task := store.Task{ID: id, Title: "Task " + id, Status: column, WorkspaceID: workspace}
	if metadata != "" {
		task.Metadata = &metadata
	}
	if err := s.CreateTask(context.Background(), task); err != nil {
		t.Fatal(err)
	}
}

func TestTheTasksListStepFiltersTheBoard(t *testing.T) {
	_, _, s := ticketStepsFixture(t)
	ctx := context.Background()
	workspaces, _ := s.ListWorkspaces(ctx)
	var proposals *int
	for _, w := range workspaces {
		if w.Name == "Proposals" {
			proposals = &w.ID
		}
	}
	workflows, err := s.EnsureWorkspace(ctx, store.Workspace{Name: store.WorkflowWorkspaceName})
	if err != nil {
		t.Fatal(err)
	}
	boardTask(t, s, "T-1", "Backlog", nil, `{"labels":["bug"]}`)
	boardTask(t, s, "T-2", "Backlog", proposals, `{"triaged":true,"labels":["bug"]}`)
	boardTask(t, s, "T-3", "Backlog", proposals, `{"labels":["ui"]}`)
	boardTask(t, s, "T-4", "Doing", proposals, "")
	boardTask(t, s, "T-5", "Backlog", nil, "")
	boardTask(t, s, "T-6", "Backlog", &workflows, "")
	if err := s.ArchiveTask(ctx, "T-5"); err != nil {
		t.Fatal(err)
	}
	step := NewTasksListExecutor(s)
	list := func(with map[string]any) []string {
		t.Helper()
		result, err := step.Start(ctx, ResolvedWorkflowStep{With: with})
		if err != nil {
			t.Fatal(err)
		}
		items, _ := result.Outputs["items"].([]any)
		if result.Outputs["count"] != len(items) {
			t.Fatalf("count %v for %d items", result.Outputs["count"], len(items))
		}
		var ids []string
		for _, item := range items {
			ids = append(ids, item.(map[string]any)["id"].(string))
		}
		return ids
	}

	if got := strings.Join(list(map[string]any{}), ","); got != "T-1,T-2,T-3,T-4" {
		t.Fatalf("every task = %s", got)
	}
	if got := strings.Join(list(map[string]any{"column": "backlog"}), ","); got != "T-1,T-2,T-3" {
		t.Fatalf("backlog = %s", got)
	}
	if got := strings.Join(list(map[string]any{"workspace": "Proposals", "limit": 2}), ","); got != "T-2,T-3" {
		t.Fatalf("proposals = %s", got)
	}
	untriagedBugs := map[string]any{"all": []any{
		map[string]any{"not": map[string]any{"field": "task.metadata.triaged", "exists": true}},
		map[string]any{"field": "task.metadata.labels", "empty": false},
		map[string]any{"field": "task.metadata.labels[0]", "equals": "bug"},
	}}
	if got := strings.Join(list(map[string]any{"column": "Backlog", "where": untriagedBugs}), ","); got != "T-1" {
		t.Fatalf("untriaged bugs = %s", got)
	}
	if got := strings.Join(list(map[string]any{"where": map[string]any{"field": "task.workspace", "equals": "Proposals"}}), ","); got != "T-2,T-3,T-4" {
		t.Fatalf("where workspace = %s", got)
	}

	result, err := step.Start(ctx, ResolvedWorkflowStep{With: map[string]any{"column": "Doing"}})
	if err != nil {
		t.Fatal(err)
	}
	item := result.Outputs["items"].([]any)[0].(map[string]any)
	if item["title"] != "Task T-4" || item["status"] != "Doing" || item["workspace"] != "Proposals" {
		t.Fatalf("item = %#v", item)
	}

	for name, with := range map[string]map[string]any{
		"unknown column":    {"column": "Nowhere"},
		"unknown workspace": {"workspace": "Nowhere"},
		"bad where":         {"where": map[string]any{"field": "steps.a.outputs.b", "exists": true}},
		"limit too high":    {"limit": 500},
	} {
		if _, err := step.Start(ctx, ResolvedWorkflowStep{With: with}); err == nil {
			t.Errorf("%s: listed", name)
		}
	}
}

// buildMilestoneTicketSteps are example A's ticket steps (LEG-480), with the
// per-ticket import its loop will make taken straight from the list.
const buildMilestoneTicketSteps = `schema_version: 2
name: build-milestone
title: Build a milestone
inputs:
  milestone: {type: string, required: true}
steps:
  - id: card
    type: ticket.import
    with: {key: "${{ workflow.inputs.milestone }}", column: Doing}
  - id: find
    type: tickets.list
    with: {provider: burnlite, milestone: "${{ workflow.inputs.milestone }}", kind: work, frontier: true}
  - id: first
    if: {field: steps.find.outputs.count, gt: 0}
    type: ticket.import
    with: {key: "${{ steps.find.outputs.items[0].key }}", provider: burnlite}
  - id: second
    if: {field: steps.find.outputs.count, gt: 1}
    type: ticket.import
    with: {key: "${{ steps.find.outputs.items[1].key }}", provider: burnlite}
  - id: third
    if: {field: steps.find.outputs.count, gt: 2}
    type: ticket.import
    with: {key: "${{ steps.find.outputs.items[2].key }}", provider: burnlite}
`

// Example A runs its ticket steps on a Burnlite milestone: tickets.list
// gives the milestone's frontier, and ticket.import puts each ticket on the
// board once, however many times the workflow runs.
func TestBuildMilestonesTicketStepsRunOnABurnliteMilestone(t *testing.T) {
	_, sync, s := ticketStepsFixture(t)
	svc := NewWorkflowService(s, []WorkflowExecutor{NewTicketImportExecutor(sync.(TicketImporter), s), NewTicketsListExecutor(sync.(TicketLister))})
	definition, err := workflowengine.Parse("build-milestone.yaml", []byte(buildMilestoneTicketSteps))
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	run := func() *store.WorkflowRunView {
		t.Helper()
		view, err := svc.Start(ctx, definition, map[string]string{"milestone": "EST-9"})
		if err != nil {
			t.Fatal(err)
		}
		if view.Run.Status != "succeeded" {
			t.Fatalf("run = %s: %+v", view.Run.Status, view.Steps)
		}
		return view
	}

	view := run()
	var frontier []string
	for _, item := range outputsOf(t, stepOf(t, view, "find"))["items"].([]any) {
		frontier = append(frontier, item.(map[string]any)["key"].(string))
	}
	if got := strings.Join(frontier, ","); got != "EST-10,EST-12" {
		t.Fatalf("frontier = %s", got)
	}
	if got := outputsOf(t, stepOf(t, view, "card")); got["task"] != "EST-9" || got["created"] != true {
		t.Fatalf("card = %v", got)
	}
	if got := outputsOf(t, stepOf(t, view, "first")); got["task"] != "EST-10" || got["created"] != true {
		t.Fatalf("first = %v", got)
	}
	if got := stepOf(t, view, "third").Status; got != "skipped" {
		t.Fatalf("third = %s", got)
	}
	if card, _ := s.GetTask(ctx, "EST-9"); card.Status != "Doing" {
		t.Fatalf("milestone card in %s", card.Status)
	}

	again := run()
	for _, id := range []string{"card", "first", "second"} {
		if got := outputsOf(t, stepOf(t, again, id)); got["created"] != false {
			t.Fatalf("%s on the second run = %v", id, got)
		}
	}
	for _, key := range []string{"EST-9", "EST-10", "EST-12"} {
		if _, err := s.GetTask(ctx, key); err != nil {
			t.Fatalf("%s: %v", key, err)
		}
	}
	if _, err := s.GetTask(ctx, "EST-11"); err == nil {
		t.Fatal("EST-11, which waits on EST-10, is on the board")
	}
}

// Example A's ticket loop on the real steps: a next: loop asks tickets.list
// for the milestone's frontier before each pass and imports the first ticket
// no pass has taken, so each frontier ticket is put on the board once and
// the loop ends when nothing new is left (LEG-488 on LEG-490's loops).
const ticketLoop = `schema_version: 2
name: ticket-loop
title: Import a milestone's frontier, one ticket a pass
inputs:
  milestone: {type: string, required: true}
outputs:
  imported: {type: json, value: "${{ steps.each.outputs.results }}"}
steps:
  - id: each
    loop:
      next:
        type: tickets.list
        with: {provider: burnlite, milestone: "${{ workflow.inputs.milestone }}", kind: work, frontier: true}
      key: "${{ loop.item.key }}"
      max: 10
    outputs:
      task: {type: string, value: "${{ steps.card.outputs.task }}"}
    steps:
      - id: card
        type: ticket.import
        with: {key: "${{ loop.item.key }}"}
`

func TestANextLoopImportsEachFrontierTicketOnce(t *testing.T) {
	_, sync, s := ticketStepsFixture(t)
	svc := NewWorkflowService(s, []WorkflowExecutor{NewTicketImportExecutor(sync.(TicketImporter), s), NewTicketsListExecutor(sync.(TicketLister))})
	definition, err := workflowengine.Parse("ticket-loop.yaml", []byte(ticketLoop))
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	view, err := svc.Start(ctx, definition, map[string]string{"milestone": "EST-9"})
	if err != nil {
		t.Fatal(err)
	}
	if view.Run.Status != "succeeded" {
		t.Fatalf("run = %s: %+v", view.Run.Status, view.Steps)
	}
	loop := outputsOf(t, stepOf(t, view, "each"))
	if loop["count"] != float64(2) || loop["ended_by"] != "next_empty" {
		t.Fatalf("loop outputs = %v", loop)
	}
	var tasks []string
	for _, result := range loop["results"].([]any) {
		tasks = append(tasks, result.(map[string]any)["task"].(string))
	}
	if got := strings.Join(tasks, ","); got != "EST-10,EST-12" {
		t.Fatalf("imported = %s", got)
	}
	for _, key := range []string{"EST-10", "EST-12"} {
		if _, err := s.GetTask(ctx, key); err != nil {
			t.Fatalf("%s: %v", key, err)
		}
	}
}
