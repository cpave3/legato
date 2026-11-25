package workflow_test

import (
	"strings"
	"testing"

	"github.com/cpave3/legato/internal/engine/workflow"
)

// agentStepsFile is a version 2 file of the steps given, with a task input.
func agentStepsFile(steps string) []byte {
	return []byte("schema_version: 2\nname: s\ntitle: S\ninputs:\n  task: {type: task, required: true}\nsteps:\n" + steps)
}

// The shape ticket-to-merged-pr uses (LEG-607): an agent on the card that
// declares pr_number, then agent.stop on the agent that step used.
func TestAnAgentOnATaskDeclaresOutputsAndALaterStepStopsIt(t *testing.T) {
	definition, err := workflow.Parse("ship.yaml", agentStepsFile(`
  - id: implement
    title: Implement
    type: agent.run
    with: {on: "${{ workflow.inputs.task }}", macro: Implement, repo: a/b}
    outputs:
      pr_number: {type: integer}
  - id: tell
    title: Tell it
    type: agent.message
    with: {agent: {from: implement}, prompt: "Merged in #${{ steps.implement.outputs.pr_number }}"}
  - id: stop
    title: Stop it
    type: agent.stop
    with: {agent: {from: implement}}
  - id: stop-card
    title: Stop the card's agent
    type: agent.stop
    with: {task: "${{ workflow.inputs.task }}"}
`))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if got := definition.Steps[2].With["agent"]; got.(map[string]any)["from"] != "implement" {
		t.Fatalf("agent.stop's agent = %#v", got)
	}
}

func TestOnRunIsTheDefaultAndTakesKeep(t *testing.T) {
	for _, with := range []string{`{prompt: hi}`, `{on: run, prompt: hi}`, `{on: run, prompt: hi, keep: true}`, `{prompt: hi, keep: true}`} {
		if _, err := workflow.Parse("s.yaml", agentStepsFile("  - {id: a, title: A, type: agent.run, with: "+with+"}\n")); err != nil {
			t.Errorf("%s: %v", with, err)
		}
	}
	for _, with := range []string{`{on: "${{ workflow.inputs.task }}", prompt: hi, reuse: true}`, `{on: "${{ workflow.inputs.task }}", prompt: hi, stop: true}`, `{on: "${{ workflow.inputs.task }}", prompt: hi, reuse: true, stop: true}`} {
		if _, err := workflow.Parse("s.yaml", agentStepsFile("  - {id: a, title: A, type: agent.run, with: "+with+"}\n")); err != nil {
			t.Errorf("%s: %v", with, err)
		}
	}
}

// What only one place takes is refused at the other when the file loads.
func TestWhereAnAgentLivesIsCheckedWhenTheFileLoads(t *testing.T) {
	const task = `on: "${{ workflow.inputs.task }}", prompt: hi`
	cases := []struct{ name, with, want string }{
		{"reuse on run", `{prompt: hi, reuse: true}`, `with reuse and stop take on: <task>; a workflow agent (on: run) is never reused, and stops unless keep: true`},
		{"stop on run", `{on: run, prompt: hi, stop: true}`, `with reuse and stop take on: <task>; a workflow agent (on: run) is never reused, and stops unless keep: true`},
		{"keep on a task", `{` + task + `, keep: true}`, `with keep takes on: run; an agent on a task keeps running unless stop: true`},
		{"keep_agent on a task", `{` + task + `, keep_agent: true}`, `with keep takes on: run; an agent on a task keeps running unless stop: true`},
		{"task and on", `{` + task + `, task: "${{ workflow.inputs.task }}"}`, `with on: <task> takes no task, slot, working_dir, scratch or name`},
		{"name on a task", `{` + task + `, name: Bob}`, `with on: <task> takes no task, slot, working_dir, scratch or name`},
		{"working_dir on a task", `{` + task + `, working_dir: /tmp}`, `with on: <task> takes no task, slot, working_dir, scratch or name`},
		{"keep and keep_agent", `{prompt: hi, keep: true, keep_agent: true}`, `with takes keep or keep_agent, not both`},
		{"reuse not a boolean", `{` + task + `, reuse: "${{ workflow.inputs.task.id }}"}`, `with reuse must be true or false`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := workflow.Parse("s.yaml", agentStepsFile("  - {id: a, title: A, type: agent.run, with: "+tc.with+"}\n"))
			if err == nil || !strings.HasSuffix(err.Error(), tc.want) {
				t.Fatalf("err = %v, want it to end %q", err, tc.want)
			}
		})
	}
}

// {from: <step>} names an earlier step that ran an agent.
func TestAnAgentFromAStepIsCheckedWhenTheFileLoads(t *testing.T) {
	first := "  - {id: note, title: N, type: task.note, with: {task: \"${{ workflow.inputs.task }}\", body: hi}}\n" +
		"  - {id: build, title: B, type: agent.run, with: {on: \"${{ workflow.inputs.task }}\", prompt: hi}}\n"
	cases := []struct{ name, step, want string }{
		{"a later step", "  - {id: m, title: M, type: agent.message, with: {agent: {from: later}, prompt: hi}}\n  - {id: later, title: L, type: agent.run, with: {prompt: hi}}\n", `with agent: step "later" is not an earlier step`},
		{"itself", "  - {id: m, title: M, type: agent.message, with: {agent: {from: m}, prompt: hi}}\n", `with agent: step "m" is not an earlier step`},
		{"a step with no agent", "  - {id: m, title: M, type: agent.message, with: {agent: {from: note}, prompt: hi}}\n", `with agent: step "note" runs no agent`},
		{"not a map", "  - {id: m, title: M, type: agent.stop, with: {agent: build}}\n", `with agent must be {from: <step>}`},
		{"other keys", "  - {id: m, title: M, type: agent.stop, with: {agent: {from: build, slot: x}}}\n", `with agent must be {from: <step>}`},
		{"an expression", "  - {id: m, title: M, type: agent.stop, with: {agent: {from: \"${{ workflow.inputs.task.id }}\"}}}\n", `with agent must be {from: <step>}`},
		{"agent and task", "  - {id: m, title: M, type: agent.stop, with: {agent: {from: build}, task: \"${{ workflow.inputs.task }}\"}}\n", `with takes task or agent, not both`},
		{"neither", "  - {id: m, title: M, type: agent.message, with: {prompt: hi}}\n", `with requires task or agent`},
		{"stop with neither", "  - {id: m, title: M, type: agent.stop, with: {}}\n", `with requires task or agent`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := workflow.Parse("s.yaml", agentStepsFile(first+tc.step))
			if err == nil || !strings.HasSuffix(err.Error(), tc.want) {
				t.Fatalf("err = %v, want it to end %q", err, tc.want)
			}
			if path := workflow.ErrorPath(err); !strings.HasPrefix(path, "steps[2].with") {
				t.Fatalf("path = %q", path)
			}
		})
	}
}

// An agent.message, agent.stop or agent.dispatch step's agent can be named
// in turn: {from} follows it to the agent it used.
func TestAnAgentFromAMessageStepIsThatStepsAgent(t *testing.T) {
	_, err := workflow.Parse("s.yaml", agentStepsFile(`
  - {id: build, title: B, type: agent.run, with: {on: "${{ workflow.inputs.task }}", prompt: hi}}
  - {id: push, title: P, type: agent.message, with: {agent: {from: build}, prompt: push}}
  - {id: again, title: A, type: agent.message, with: {agent: {from: push}, macro: Implement}}
  - {id: send, title: S, type: agent.dispatch, with: {task: "${{ workflow.inputs.task }}", prompt: hi}}
  - {id: stop, title: S, type: agent.stop, with: {agent: {from: send}}}
`))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
}

func TestAgentMessageTakesAPromptOrAMacro(t *testing.T) {
	base := "  - {id: build, title: B, type: agent.run, with: {prompt: hi}}\n"
	for with, want := range map[string]string{
		`{agent: {from: build}}`:                      `with requires prompt or macro`,
		`{agent: {from: build}, prompt: a, macro: b}`: `with takes prompt or macro, not both`,
	} {
		_, err := workflow.Parse("s.yaml", agentStepsFile(base+"  - {id: m, title: M, type: agent.message, with: "+with+"}\n"))
		if err == nil || !strings.HasSuffix(err.Error(), want) {
			t.Errorf("%s: err = %v, want %q", with, err, want)
		}
	}
	spec, ok := workflow.LookupExecutor("agent.message")
	if !ok {
		t.Fatal("agent.message is not in the catalog")
	}
	for _, name := range []string{"task", "session_id"} {
		if _, ok := spec.Output(name); !ok {
			t.Errorf("agent.message has no %s output", name)
		}
	}
}
