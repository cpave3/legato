package workflow_test

import (
	"strings"
	"testing"

	"github.com/cpave3/legato/internal/engine/workflow"
)

// The ticket steps of example A (build-milestone, LEG-480): the milestone's
// frontier listed, the milestone and a ticket imported, the import's task
// and the list's items readable by later steps (LEG-488).
const ticketStepsFile = `schema_version: 2
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
    with: {provider: burnlite, milestone: "${{ workflow.inputs.milestone }}", kind: work, frontier: true, limit: 10}
  - id: first
    if: {field: steps.find.outputs.count, gt: 0}
    type: ticket.import
    with: {key: "${{ steps.find.outputs.items[0].key }}", provider: burnlite, workspace: Proposals}
  - id: note
    type: task.note
    with: {task: "${{ steps.first.outputs.task }}", body: "created ${{ steps.first.outputs.created }}"}
`

func TestTheTicketStepsOfBuildMilestoneLoad(t *testing.T) {
	definition, diagnostics := workflow.Check("build.yaml", []byte(ticketStepsFile))
	if definition == nil || len(diagnostics) != 0 {
		t.Fatalf("diagnostics = %+v", diagnostics)
	}
}

func TestTicketStepsRefuseWhatTheyCannotTake(t *testing.T) {
	step := func(body string) string {
		return "schema_version: 2\nname: t\ntitle: T\nsteps:\n" + body
	}
	cases := map[string]string{
		"import without key":       "  - {id: a, type: ticket.import, with: {column: Doing}}\n",
		"import unknown provider":  "  - {id: a, type: ticket.import, with: {key: LEG-1, provider: linear}}\n",
		"list unknown kind":        "  - {id: a, type: tickets.list, with: {project: LEG, kind: epic}}\n",
		"list unknown state":       "  - {id: a, type: tickets.list, with: {project: LEG, state: done}}\n",
		"list limit not a number":  "  - {id: a, type: tickets.list, with: {project: LEG, limit: lots}}\n",
		"list frontier not a bool": "  - {id: a, type: tickets.list, with: {project: LEG, frontier: yes please}}\n",
		"tasks where unknown key":  "  - {id: a, type: tasks.list, with: {where: {field: task.title, equal: x}}}\n",
		"tasks where not task":     "  - {id: a, type: tasks.list, with: {where: {field: steps.x.outputs.y, exists: true}}}\n",
		"tasks where no field":     "  - {id: a, type: tasks.list, with: {where: {field: task.colour, exists: true}}}\n",
		"tasks where two ops":      "  - {id: a, type: tasks.list, with: {where: {field: task.title, exists: true, empty: false}}}\n",
		"tasks where a list":       "  - {id: a, type: tasks.list, with: {where: [task.title]}}\n",
	}
	for name, body := range cases {
		if _, err := workflow.Parse("t.yaml", []byte(step(body))); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestTasksListTakesAConditionOverTaskFields(t *testing.T) {
	file := `schema_version: 2
name: triage
title: Triage
inputs:
  owner: {type: string}
steps:
  - id: find
    type: tasks.list
    with:
      column: Triage
      workspace: Backend
      where:
        all:
          - {not: {field: task.metadata.triaged, exists: true}}
          - {field: "task.metadata.labels[0]", equals: bug}
          - {field: task.workspace, in: [Backend, Ops]}
          - {field: task.provider, equals: "${{ workflow.inputs.owner }}"}
      limit: 15
  - id: tell
    if: {field: steps.find.outputs.count, gt: 0}
    type: task.note
    with: {task: "${{ steps.find.outputs.items[0].id }}", body: first}
`
	if _, diagnostics := workflow.Check("triage.yaml", []byte(file)); len(diagnostics) != 0 {
		t.Fatalf("diagnostics = %+v", diagnostics)
	}
}

func TestATaskConditionMatchesTasks(t *testing.T) {
	condition, err := workflow.TaskCondition(map[string]any{"all": []any{
		map[string]any{"not": map[string]any{"field": "task.metadata.triaged", "exists": true}},
		map[string]any{"field": "task.metadata.labels[-1]", "equals": "bug"},
		map[string]any{"field": "task.priority", "in": []any{"High", "Medium"}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	task := func(priority string, metadata map[string]any) map[string]any {
		return map[string]any{"id": "T-1", "title": "A", "priority": priority, "metadata": metadata}
	}
	cases := []struct {
		name string
		task map[string]any
		want bool
	}{
		{"untriaged bug", task("High", map[string]any{"labels": []any{"ui", "bug"}}), true},
		{"triaged", task("High", map[string]any{"triaged": true, "labels": []any{"bug"}}), false},
		{"not a bug", task("High", map[string]any{"labels": []any{"bug", "ui"}}), false},
		{"low", task("Low", map[string]any{"labels": []any{"bug"}}), false},
		{"no metadata", task("High", nil), false},
	}
	for _, c := range cases {
		got, err := condition.MatchTask(c.task)
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if got != c.want {
			t.Errorf("%s: matched %v, want %v", c.name, got, c.want)
		}
	}
	if _, err := workflow.TaskCondition(map[string]any{"field": "steps.a.outputs.b", "exists": true}); err == nil || !strings.Contains(err.Error(), "task.") {
		t.Fatalf("err = %v", err)
	}
}

// The editor learns where tasks.list's where may read.
func TestTheWhereContextIsListed(t *testing.T) {
	for _, context := range workflow.ExpressionContexts() {
		if context.Location != "tasks.list.where" {
			continue
		}
		var patterns []string
		for _, root := range context.Roots {
			patterns = append(patterns, root.Pattern)
		}
		if got := strings.Join(patterns, " "); !strings.Contains(got, "task.<task field>") || !strings.Contains(got, "task.metadata.<path>") || !strings.Contains(got, "task.workspace") {
			t.Fatalf("roots = %s", got)
		}
		return
	}
	t.Fatal("no tasks.list.where context")
}

// An operator's value given as ${{ }} is checked as an expression at load
// and as the operator's type once it resolves, when the step runs.
func TestATaskConditionTakesAnExpressionForAnOperatorsValue(t *testing.T) {
	file := func(where string) string {
		return `schema_version: 2
name: triage
title: Triage
inputs:
  priorities: {type: json}
  triaged: {type: boolean}
steps:
  - id: find
    type: tasks.list
    with:
      where: ` + where + "\n"
	}
	valid := `{all: [{field: task.priority, in: "${{ workflow.inputs.priorities }}"}, {field: task.metadata.triaged, exists: "${{ workflow.inputs.triaged }}"}, {any: [{field: task.title, empty: "${{ workflow.inputs.triaged }}"}]}]}`
	definition, err := workflow.Parse("triage.yaml", []byte(file(valid)))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := workflow.Parse("triage.yaml", []byte(file(`{field: task.priority, in: "${{ workflow.inputs.nothing }}"}`))); err == nil {
		t.Fatal("an unknown input was accepted")
	}

	step, err := definition.ResolveStep("find", map[string]any{"priorities": []any{"High", "Medium"}, "triaged": false}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	condition, err := workflow.TaskCondition(step.With["where"])
	if err != nil {
		t.Fatal(err)
	}
	for priority, want := range map[string]bool{"High": true, "Low": false} {
		got, err := condition.MatchTask(map[string]any{"title": "A", "priority": priority, "metadata": map[string]any{}})
		if err != nil || got != want {
			t.Errorf("%s: matched %v, %v; want %v", priority, got, err, want)
		}
	}

	step, err = definition.ResolveStep("find", map[string]any{"priorities": "High", "triaged": false}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := workflow.TaskCondition(step.With["where"]); err == nil {
		t.Fatal("in took text once resolved")
	}
}
