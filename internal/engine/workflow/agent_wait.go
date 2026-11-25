package workflow

import (
	"fmt"
	"slices"
	"time"
)

// What ends an agent step (spec LEG-482 §5.3, LEG-485). A step's wait_for
// lists signals, and the first to come ends it:
//
//   - complete: the agent ran `legato workflow step complete` with every
//     declared output;
//   - finished: a stage event of the agent's place reached Finished after
//     the step began (above its watermark);
//   - ended: the agent's session ended, not interrupted;
//   - none: delivering the prompt is enough.
//
// timeout is a duration the step may take once it starts; the deadline it
// makes is absolute, so time Legato is down counts (§5.5).

// The wait_for signals.
const (
	SignalComplete = "complete"
	SignalFinished = "finished"
	SignalEnded    = "ended"
	SignalNone     = "none"
	// SignalTimeout is not a wait_for signal: it is the outcome of a step
	// its timeout ended.
	SignalTimeout = "timeout"
)

// waitSteps are the step types that take wait_for.
var waitSteps = map[string]bool{"agent.run": true, "agent.message": true, "agent.stop": true}

// EffectiveWaitFor is what a step waits for: its wait_for, else its
// default. agent.run waits for complete, or for ended on a task with no
// declared outputs; agent.message waits for complete when it declares
// outputs and for nothing otherwise; agent.stop stops at once (none).
// Other steps take no wait_for: nil.
func EffectiveWaitFor(step Step) []string {
	if len(step.WaitFor) > 0 {
		return append([]string(nil), step.WaitFor...)
	}
	switch step.Type {
	case "agent.run":
		if len(step.Outputs) == 0 && AgentOnTask(step.With) {
			return []string{SignalEnded}
		}
		return []string{SignalComplete}
	case "agent.message":
		if len(step.Outputs) > 0 {
			return []string{SignalComplete}
		}
		return []string{SignalNone}
	case "agent.stop":
		return []string{SignalNone}
	}
	return nil
}

// TimeoutDuration is the step's timeout, 0 when it has none.
func (s Step) TimeoutDuration() (time.Duration, error) {
	if s.Timeout == "" {
		return 0, nil
	}
	d, err := time.ParseDuration(s.Timeout)
	if err != nil || d <= 0 {
		return 0, fmt.Errorf("timeout must be a duration such as 30m or 12h")
	}
	return d, nil
}

// checkWait checks a step's wait_for and timeout.
func checkWait(step Step, version int, fromFile bool) error {
	if version == SchemaVersion && fromFile {
		if step.WaitFor != nil {
			return within("wait_for", fmt.Errorf("wait_for is a schema_version 2 key"))
		}
		if step.Timeout != "" {
			return within("timeout", fmt.Errorf("timeout is a schema_version 2 key"))
		}
	}
	if _, err := step.TimeoutDuration(); err != nil {
		return within("timeout", err)
	}
	if step.WaitFor == nil {
		return nil
	}
	if !waitSteps[step.Type] {
		return within("wait_for", fmt.Errorf("wait_for takes an agent step: agent.run, agent.message or agent.stop"))
	}
	if len(step.WaitFor) == 0 {
		return within("wait_for", fmt.Errorf("wait_for lists at least one signal"))
	}
	for i, signal := range step.WaitFor {
		switch signal {
		case SignalComplete, SignalFinished, SignalEnded, SignalNone:
		case "idle":
			return within(fmt.Sprintf("wait_for[%d]", i), fmt.Errorf("wait_for: idle is not supported yet"))
		default:
			return within(fmt.Sprintf("wait_for[%d]", i), fmt.Errorf("wait_for: unknown signal %q: complete, finished, ended or none", signal))
		}
		if slices.Index(step.WaitFor, signal) != i {
			return within(fmt.Sprintf("wait_for[%d]", i), fmt.Errorf("wait_for names %s twice", signal))
		}
	}
	if slices.Contains(step.WaitFor, SignalNone) && len(step.WaitFor) > 1 {
		return within("wait_for", fmt.Errorf("wait_for: none stands alone"))
	}
	if step.Type == "agent.stop" && !slices.Equal(step.WaitFor, []string{SignalEnded}) && !slices.Equal(step.WaitFor, []string{SignalNone}) {
		return within("wait_for", fmt.Errorf("agent.stop waits for ended (then cleans up), or for none"))
	}
	if len(step.Outputs) > 0 && !slices.Contains(step.WaitFor, SignalComplete) {
		return within("wait_for", fmt.Errorf("the step declares outputs, so wait_for needs complete: the agent fills them when it completes the step"))
	}
	if step.Type == "agent.run" && !AgentOnTask(step.With) && slices.Equal(step.WaitFor, []string{SignalNone}) && !stepKeeps(step.With) {
		return within("wait_for", fmt.Errorf("wait_for: none would stop a workflow's agent as soon as it starts: give it keep: true"))
	}
	return nil
}

func stepKeeps(with map[string]any) bool {
	keep, _ := with["keep"].(bool)
	kept, _ := with["keep_agent"].(bool)
	return keep || kept
}

// checkCloneFrom checks that an agent.run step's clone: {from: <step>}
// names an earlier step that ran an agent.
func checkCloneFrom(step Step, agentSteps map[string]bool) error {
	value, ok := step.With["clone"]
	if !ok || step.Type != "agent.run" {
		return nil
	}
	from, isFrom := AgentFrom(value)
	if !isFrom {
		return nil
	}
	runs, earlier := agentSteps[from]
	switch {
	case !earlier:
		return within("with.clone", fmt.Errorf("with clone: step %q is not an earlier step", from))
	case !runs:
		return within("with.clone", fmt.Errorf("with clone: step %q runs no agent", from))
	}
	return nil
}
