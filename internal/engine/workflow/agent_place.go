package workflow

import (
	"fmt"
	"strings"
)

// Where a workflow's agent lives, and naming the agent an earlier step used
// (spec LEG-482 §5.1, LEG-607).
//
// An agent.run step's with.on is AgentOnRun (the default): an agent of the
// workflow, stopped once its outputs are in unless keep is true. Anything
// else is a task, or a task ID: a plain agent in that task's main slot,
// which keeps running unless stop is true, and which reuse: true messages
// when it is running already.
//
// agent.message and agent.stop name their agent with agent: {from: <step>},
// the agent an earlier agent step used, or with a task.

// AgentOnRun is with.on of an agent of the workflow, the default.
const AgentOnRun = "run"

// AgentFromKey is the one key of an {from: <step>} agent reference.
const AgentFromKey = "from"

// agentStepTypes are the step types whose agent a later step can name
// with {from: <step>}.
var agentStepTypes = map[string]bool{"agent.run": true, "agent.dispatch": true, "agent.message": true}

// AgentOnTask reports whether an agent.run step's with.on names a task
// rather than the workflow's own agent: any value but "" and AgentOnRun.
func AgentOnTask(with map[string]any) bool {
	value, ok := with["on"]
	if !ok || value == nil {
		return false
	}
	text, isText := value.(string)
	if !isText {
		return true
	}
	text = strings.TrimSpace(text)
	return text != "" && text != AgentOnRun
}

// AgentFrom is the step an {from: <step>} reference names, and whether
// value is one.
func AgentFrom(value any) (string, bool) {
	ref, ok := value.(map[string]any)
	if !ok || len(ref) != 1 {
		return "", false
	}
	step, ok := ref[AgentFromKey].(string)
	if !ok || strings.TrimSpace(step) == "" || expressionPattern.MatchString(step) {
		return "", false
	}
	return step, true
}

// checkAgentStep checks what an agent step's with says of its agent, as far
// as the step alone tells: an agent.run step's place, and the form of an
// agent: {from: <step>}.
func checkAgentStep(step Step) error {
	if value, ok := step.With["agent"]; ok && (step.Type == "agent.message" || step.Type == "agent.stop") {
		if _, ok := AgentFrom(value); !ok {
			return within("with.agent", fmt.Errorf("with agent must be {from: <step>}"))
		}
	}
	if step.Type != "agent.run" {
		return nil
	}
	if AgentOnTask(step.With) {
		for _, key := range []string{"keep", "keep_agent"} {
			if _, ok := step.With[key]; ok {
				return within("with."+key, fmt.Errorf("with keep takes on: run; an agent on a task keeps running unless stop: true"))
			}
		}
		for _, key := range []string{"task", "slot", "working_dir", "scratch", "name"} {
			if _, ok := step.With[key]; ok {
				return within("with."+key, fmt.Errorf("with on: <task> takes no task, slot, working_dir, scratch or name"))
			}
		}
		return nil
	}
	for _, key := range []string{"reuse", "stop"} {
		if _, ok := step.With[key]; ok {
			return within("with."+key, fmt.Errorf("with reuse and stop take on: <task>; a workflow agent (on: run) is never reused, and stops unless keep: true"))
		}
	}
	return nil
}

// checkAgentFrom checks that an agent: {from: <step>} names an earlier step
// that ran an agent. agentSteps holds the earlier steps' IDs, each true when
// it runs an agent.
func checkAgentFrom(step Step, agentSteps map[string]bool) error {
	value, ok := step.With["agent"]
	if !ok || (step.Type != "agent.message" && step.Type != "agent.stop") {
		return nil
	}
	from, _ := AgentFrom(value)
	runs, earlier := agentSteps[from]
	switch {
	case !earlier:
		return within("with.agent", fmt.Errorf("with agent: step %q is not an earlier step", from))
	case !runs:
		return within("with.agent", fmt.Errorf("with agent: step %q runs no agent", from))
	}
	return nil
}
