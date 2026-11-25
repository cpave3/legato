package workflow_test

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/cpave3/legato/internal/engine/workflow"
)

// Agent waits, timeouts and clones (LEG-485, spec LEG-482 §5.2–§5.6).

// The acceptance shape: an agent on the card waits for Finished, then a
// message to the same agent waits for Finished again; both time out.
func TestAnAgentStepWaitsForASignalWithATimeout(t *testing.T) {
	definition, err := workflow.Parse("build.yaml", agentStepsFile(`
  - id: build
    title: Build
    type: agent.run
    with: {on: "${{ workflow.inputs.task }}", prompt: Build it, repo: a/b, branch: feature}
    wait_for: [finished]
    timeout: 12h
  - id: push
    title: Push
    type: agent.message
    with: {agent: {from: build}, prompt: "Push ${{ steps.build.outputs.branch }} and open its PRs"}
    wait_for: [finished]
    timeout: 1h
  - id: stop
    title: Stop
    type: agent.stop
    with: {agent: {from: build}}
    wait_for: [ended]
`))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	build := definition.Steps[0]
	if !slices.Equal(build.WaitFor, []string{"finished"}) || build.Timeout != "12h" {
		t.Fatalf("build = %+v", build)
	}
	if got, _ := build.TimeoutDuration(); got != 12*time.Hour {
		t.Fatalf("timeout = %v", got)
	}
	if got := workflow.EffectiveWaitFor(definition.Steps[2]); !slices.Equal(got, []string{"ended"}) {
		t.Fatalf("agent.stop waits for %v", got)
	}
}

// What a step waits for when wait_for is not given: complete when it
// declares outputs, ended for an agent on a task, none for a message or a
// stop, complete otherwise.
func TestWhatAnAgentStepWaitsForByDefault(t *testing.T) {
	definition, err := workflow.Parse("s.yaml", agentStepsFile(`
  - {id: run, title: R, type: agent.run, with: {prompt: hi}}
  - {id: run-out, title: R, type: agent.run, with: {prompt: hi}, outputs: {n: {type: integer}}}
  - {id: task, title: T, type: agent.run, with: {on: "${{ workflow.inputs.task }}", prompt: hi}}
  - {id: task-out, title: T, type: agent.run, with: {on: "${{ workflow.inputs.task }}", prompt: hi}, outputs: {n: {type: integer}}}
  - {id: tell, title: M, type: agent.message, with: {agent: {from: task}, prompt: hi}}
  - {id: tell-out, title: M, type: agent.message, with: {agent: {from: task}, prompt: hi}, outputs: {n: {type: integer}}}
  - {id: stop, title: S, type: agent.stop, with: {agent: {from: task}}}
  - {id: note, title: N, type: task.note, with: {task: "${{ workflow.inputs.task }}", body: hi}}
`))
	if err != nil {
		t.Fatal(err)
	}
	want := map[string][]string{
		"run": {"complete"}, "run-out": {"complete"}, "task": {"ended"}, "task-out": {"complete"},
		"tell": {"none"}, "tell-out": {"complete"}, "stop": {"none"}, "note": nil,
	}
	for _, step := range definition.Steps {
		if got := workflow.EffectiveWaitFor(step); !slices.Equal(got, want[step.ID]) {
			t.Errorf("%s waits for %v, want %v", step.ID, got, want[step.ID])
		}
	}
}

func TestWaitForAndTimeoutAreCheckedWhenTheFileLoads(t *testing.T) {
	for name, tc := range map[string]struct{ step, want string }{
		"an unknown signal":       {`{id: a, title: A, type: agent.run, with: {prompt: hi}, wait_for: [done]}`, "wait_for: unknown signal \"done\""},
		"idle, a later slice":     {`{id: a, title: A, type: agent.run, with: {prompt: hi}, wait_for: [idle]}`, "idle is not supported yet"},
		"none with another":       {`{id: a, title: A, type: agent.run, with: {prompt: hi, keep: true}, wait_for: [none, finished]}`, "none stands alone"},
		"a signal twice":          {`{id: a, title: A, type: agent.run, with: {prompt: hi}, wait_for: [finished, finished]}`, "twice"},
		"an empty list":           {`{id: a, title: A, type: agent.run, with: {prompt: hi}, wait_for: []}`, "at least one signal"},
		"not an agent step":       {`{id: a, title: A, type: task.note, with: {task: "${{ workflow.inputs.task }}", body: hi}, wait_for: [complete]}`, "wait_for takes an agent step"},
		"a stop waits for ended":  {`{id: a, title: A, type: agent.stop, with: {task: "${{ workflow.inputs.task }}"}, wait_for: [finished]}`, "agent.stop waits for ended"},
		"outputs need complete":   {`{id: a, title: A, type: agent.run, with: {prompt: hi}, wait_for: [finished], outputs: {n: {type: integer}}}`, "declares outputs, so wait_for needs complete"},
		"none stops a run agent":  {`{id: a, title: A, type: agent.run, with: {prompt: hi}, wait_for: [none]}`, "keep: true"},
		"a timeout not a time":    {`{id: a, title: A, type: agent.run, with: {prompt: hi}, timeout: soon}`, "timeout must be a duration"},
		"a timeout of nothing":    {`{id: a, title: A, type: agent.run, with: {prompt: hi}, timeout: 0s}`, "timeout must be a duration"},
		"clone from a later step": {`{id: a, title: A, type: agent.run, with: {prompt: hi, repo: a/b, clone: {from: b}}}`, "step \"b\" is not an earlier step"},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := workflow.Parse("s.yaml", agentStepsFile("  - "+tc.step+"\n"))
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want %q", err, tc.want)
			}
		})
	}
}

func TestWaitForAndTimeoutAreSchemaVersion2Keys(t *testing.T) {
	for _, key := range []string{"wait_for: [complete]", "timeout: 1h"} {
		_, err := workflow.Parse("s.yaml", []byte("schema_version: 1\nname: s\ntitle: S\nsteps:\n  - id: a\n    title: A\n    type: agent.run\n    with: {prompt: hi}\n    "+key+"\n"))
		if err == nil || !strings.Contains(err.Error(), "schema_version 2") {
			t.Errorf("%s: err = %v", key, err)
		}
	}
}

// A clone is explicit: an earlier agent step's ({from}), or an ID such as a
// loop's previous clone_id, which Legato fills on every agent step.
func TestACloneIsAnEarlierAgentStepsOrAnID(t *testing.T) {
	_, err := workflow.Parse("s.yaml", agentStepsFile(`
  - {id: build, title: B, type: agent.run, with: {prompt: hi, repo: a/b, branch: one}}
  - {id: next, title: N, type: agent.run, with: {prompt: hi, repo: a/b, branch: two, clone: {from: build}}}
  - {id: again, title: A, type: agent.run, with: {prompt: hi, repo: a/b, clone: "${{ steps.next.outputs.clone_id }}"}}
  - {id: tell, title: T, type: agent.message, with: {agent: {from: build}, prompt: "${{ steps.build.outputs.top_branch }} ${{ steps.build.outputs.prs }} ${{ steps.build.outputs.last_message }}"}}
`))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if _, err := workflow.Parse("s.yaml", agentStepsFile(`
  - {id: note, title: N, type: task.note, with: {task: "${{ workflow.inputs.task }}", body: hi}}
  - {id: next, title: N, type: agent.run, with: {prompt: hi, repo: a/b, clone: {from: note}}}
`)); err == nil || !strings.Contains(err.Error(), "runs no agent") {
		t.Fatalf("clone from a step with no agent: err = %v", err)
	}
}
