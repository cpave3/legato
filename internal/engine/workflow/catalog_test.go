package workflow_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/cpave3/legato/internal/engine/workflow"
)

// The step types schema v1 accepts. A new executor is added to the catalog,
// and here.
var acceptedStepTypes = []string{
	"agent.compact", "agent.dispatch", "agent.interview", "agent.message", "agent.run", "agent.stop",
	"burnlite.task.close", "chat.message.send", "chat.reaction.add", "chat.reaction.remove", "chat.thread.read", "chimera.session.remove", "file.remove",
	"gate",
	"github.pr.prepare", "github.pr.review.close", "github.pr.review.confirm", "github.pr.review.open", "github.pr.review.pass", "github.pr.review.wait", "github.pr.review_passes", "github.pr.wait",
	"learning.packet.prepare", "learning.proposals.submit",
	"repo.ensure", "task.context", "task.metadata.set", "task.move", "task.note",
	"task.working_dir.resolve", "tasks.list", "ticket.comment", "ticket.create", "ticket.import", "tickets.list", "workflow.run", "worktree.remove",
}

func TestCatalogListsEveryAcceptedStepType(t *testing.T) {
	var types []string
	for _, spec := range workflow.Catalog() {
		types = append(types, spec.Type)
	}
	slices.Sort(types)
	if !slices.Equal(types, acceptedStepTypes) {
		t.Fatalf("catalog types = %v, want %v", types, acceptedStepTypes)
	}
}

func TestCatalogEntriesAreWellFormed(t *testing.T) {
	kinds := []workflow.FieldKind{
		workflow.KindString, workflow.KindText, workflow.KindInteger, workflow.KindBoolean,
		workflow.KindTask, workflow.KindTaskOrID, workflow.KindRepository, workflow.KindPath,
		workflow.KindColumn, workflow.KindMacro, workflow.KindAgent, workflow.KindTier, workflow.KindJSON,
		workflow.KindChatBot, workflow.KindChatButtons,
	}
	outputTypes := []string{"string", "integer", "boolean", "json"}
	for _, spec := range workflow.Catalog() {
		if strings.TrimSpace(spec.Title) == "" || strings.TrimSpace(spec.Description) == "" {
			t.Errorf("%s: needs a title and a description", spec.Type)
		}
		names := map[string]bool{}
		for _, field := range spec.With {
			if names[field.Name] {
				t.Errorf("%s: field %q twice", spec.Type, field.Name)
			}
			names[field.Name] = true
			if !slices.Contains(kinds, field.Kind) {
				t.Errorf("%s.%s: unknown kind %q", spec.Type, field.Name, field.Kind)
			}
			if strings.TrimSpace(field.Description) == "" {
				t.Errorf("%s.%s: needs a description", spec.Type, field.Name)
			}
			if field.Required && field.RequiredAtRun {
				t.Errorf("%s.%s: Required already implies RequiredAtRun", spec.Type, field.Name)
			}
		}
		for _, constraint := range spec.Constraints {
			if len(constraint.Fields) < 2 {
				t.Errorf("%s: constraint %s names fewer than two fields", spec.Type, constraint.Kind)
			}
			for _, name := range constraint.Fields {
				if !names[name] {
					t.Errorf("%s: constraint %s names %q, which is not a field", spec.Type, constraint.Kind, name)
				}
			}
		}
		for _, output := range spec.Outputs {
			if !slices.Contains(outputTypes, output.Type) {
				t.Errorf("%s output %q: unknown type %q", spec.Type, output.Name, output.Type)
			}
		}
	}
}

func TestLookupExecutor(t *testing.T) {
	spec, ok := workflow.LookupExecutor("task.note")
	if !ok {
		t.Fatal("task.note is not in the catalog")
	}
	for _, name := range []string{"note_id", "task_id", "skipped", "away_on", "warning"} {
		if _, ok := spec.Output(name); !ok {
			t.Errorf("task.note has no output %q", name)
		}
	}
	if field, ok := spec.Field("optional"); !ok || field.Kind != workflow.KindBoolean || !field.Literal {
		t.Errorf("task.note optional = %+v, %v; want a literal boolean", field, ok)
	}
	if _, ok := workflow.LookupExecutor("agent.nope"); ok {
		t.Error("an unknown type is in the catalog")
	}
}

func TestCatalogIsACopy(t *testing.T) {
	first := workflow.Catalog()
	first[0].With[0].Name = "changed"
	first[0].Type = "changed"
	if second := workflow.Catalog(); second[0].Type == "changed" || second[0].With[0].Name == "changed" {
		t.Fatal("changing Catalog()'s result changed the catalog")
	}
}

func TestDispatchDeclaresItsOutputs(t *testing.T) {
	spec, _ := workflow.LookupExecutor("agent.dispatch")
	for _, name := range []string{"agent_session_id", "created", "working_dir"} {
		if _, ok := spec.Output(name); !ok {
			t.Errorf("agent.dispatch has no output %q", name)
		}
	}
	if !slices.ContainsFunc(spec.Constraints, func(c workflow.Constraint) bool {
		return c.Kind == workflow.ExactlyOne && slices.Equal(c.Fields, []string{"prompt", "macro"})
	}) {
		t.Error("agent.dispatch does not take exactly one of prompt and macro")
	}
}

// The catalog drives Validate: a field the catalog checks rejects a value
// of the wrong kind with the field's own words.
func TestCatalogDrivesStepValidation(t *testing.T) {
	cases := []struct {
		name, steps, want string
	}{
		{"unknown type", `type: agent.nope
    with: {}`, `step "s" has unsupported type "agent.nope"`},
		{"required", `type: task.move
    with: {task: "${{ workflow.inputs.task }}"}`, `step "s": with requires column`},
		{"whole task", `type: task.context
    with: {task: "id-${{ workflow.inputs.task.id }}"}`, `step "s": with task must be a whole task reference`},
		{"literal boolean", `type: task.note
    with: {task: "${{ workflow.inputs.task }}", body: hi, optional: "${{ workflow.inputs.task.id }}"}`, `step "s": with optional must be true or false`},
		{"boolean or expression", `type: github.pr.review_passes
    with: {repo: a/b, pr_number: 1, agent_author: "yes"}`, `step "s": with agent_author must be true or false`},
		{"exactly one", `type: agent.run
    with: {prompt: hi, macro: m}`, `step "s": with takes prompt or macro, not both`},
		{"exactly one missing", `type: agent.run
    with: {}`, `step "s": with requires prompt or macro`},
		{"at least one", `type: learning.proposals.submit
    with: {operations: []}`, `step "s": with requires attempt_id or generation`},
		{"macro name", `type: agent.run
    with: {macro: " "}`, `step "s": with macro must be the name of a macro`},
		{"column", `type: task.move
    with: {task: "${{ workflow.inputs.task }}", column: " "}`, `step "s": with column must be a non-empty string`},
		{"engram", `type: agent.run
    with: {prompt: hi, engram: 3}`, `step "s": with engram must be the name of an engram`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := workflow.Parse("c.yaml", []byte("schema_version: 1\nname: c\ntitle: C\ninputs:\n  task: {type: task, required: true}\nsteps:\n  - id: s\n    title: S\n    "+tc.steps+"\n"))
			if err == nil || !strings.HasSuffix(err.Error(), tc.want) {
				t.Fatalf("err = %v, want it to end %q", err, tc.want)
			}
		})
	}
}

// Fields the engine checks only when the step runs still load, as they did
// before the catalog.
func TestUncheckedFieldsStillLoad(t *testing.T) {
	_, err := workflow.Parse("c.yaml", []byte(`schema_version: 1
name: c
title: C
steps:
  - id: a
    title: A
    type: repo.ensure
    with: {repo: owner/name, working_dir: /tmp, misspelt: true}
  - id: b
    title: B
    type: github.pr.prepare
    with: {}
`))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
}

// The inspector shows a step's essential fields first, under plain labels
// in the catalog's order, and a step row is summed up by one of its fields.
func TestOfferedStepTypesSayWhatTheInspectorAndTheirRowShow(t *testing.T) {
	for _, spec := range workflow.Catalog() {
		essential := 0
		orders := map[int]string{}
		for _, field := range spec.With {
			if field.Essential {
				essential++
			}
			if strings.TrimSpace(field.Label) == "" {
				t.Errorf("%s.%s: needs a label", spec.Type, field.Name)
			}
			if field.Order <= 0 {
				t.Errorf("%s.%s: needs an order", spec.Type, field.Name)
			} else if other, taken := orders[field.Order]; taken {
				t.Errorf("%s.%s: order %d is %s's too", spec.Type, field.Name, field.Order, other)
			}
			orders[field.Order] = field.Name
		}
		if len(spec.With) > 0 && essential == 0 {
			t.Errorf("%s: no essential field", spec.Type)
		}
		if len(spec.Summary) == 0 {
			t.Errorf("%s: no summary field", spec.Type)
		}
		for _, name := range spec.Summary {
			if _, ok := spec.Field(name); !ok {
				t.Errorf("%s: summary names %q, which is not a field", spec.Type, name)
			}
		}
	}
}

// The step picker groups the offered step types (LEG-507).
func TestOfferedStepTypesAreInAPickerGroup(t *testing.T) {
	for _, spec := range workflow.Catalog() {
		if !slices.Contains(workflow.StepGroups, spec.Group) {
			t.Errorf("%s: group %q is not one of %v", spec.Type, spec.Group, workflow.StepGroups)
		}
	}
}
