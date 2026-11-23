package cli_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/cpave3/legato/internal/engine/store"
)

// treeOf draws an agent's background jobs as their tree: each job as
// label=state (a shell's state is blank), its children indented under it
// in the order they were listed, and lines joined by " | ".
func treeOf(s step) string {
	known := map[string]bool{}
	for _, job := range s.jobs {
		known[job.ID] = true
	}
	// A job whose parent is not listed is drawn at the top.
	parentOf := func(job store.BackgroundJob) string {
		if known[job.Parent] {
			return job.Parent
		}
		return ""
	}
	var lines []string
	var walk func(parent string, depth int)
	walk = func(parent string, depth int) {
		for _, job := range s.jobs {
			if parentOf(job) != parent || depth > 8 {
				continue
			}
			lines = append(lines, strings.Repeat("  ", depth)+job.Label+"="+job.State)
			walk(job.ID, depth+1)
		}
	}
	walk("", 0)
	return strings.Join(lines, " | ")
}

// In an interactive session a background subagent that starts its own and
// ends its turn waits on it: Claude Code wakes it when its child ends, with
// a SubagentStart of the same agent, and tells the main loop only after
// that. Every level is listed, under its parent, until the last ends.
func TestClaudeHook_ASubagentsOwnSubagentsAreListedUnderIt(t *testing.T) {
	agent := newHookAgent(t)

	steps := agent.replay("nested-woken-parent")

	const l1, l2 = "general-purpose: L1 orchestrator", "general-purpose: L2 worker"
	for _, c := range []struct {
		line int
		want string
	}{
		{5, l1 + "=working"},                            // the main loop's Stop
		{8, l1 + "=working |   " + l2 + "=working"},     // L1 started L2
		{11, l1 + "=background |   " + l2 + "=working"}, // L1 ended its turn
		{13, l1 + "=background"},                        // L2 ended: L1 is about to be woken
		{14, l1 + "=working"},                           // L1 woken
		{17, ""},                                        // L1 ended with nothing left
		{19, ""},                                        // the main loop's last Stop
	} {
		if got := treeOf(steps[c.line]); got != c.want {
			t.Errorf("after line %d (%s):\n got %q\nwant %q", c.line, steps[c.line].event, got, c.want)
		}
	}
	// A parent that ends its turn keeps its place in the list.
	if got := jobsOf(steps[11]); got != "subagent:"+l1+", subagent:"+l2 {
		t.Errorf("order once L1 ended its turn = %q, want L1 first", got)
	}
	// The agent stays in the background until the last level ends.
	for _, i := range []int{5, 8, 11, 13, 14} {
		if steps[i].activity != store.ActivityBackground {
			t.Errorf("activity after line %d = %q, want background", i, steps[i].activity)
		}
	}
	if got := steps[19].activity; got != "" {
		t.Errorf("activity after the last Stop = %q, want idle", got)
	}
}

// In `claude -p` Claude Code tells the main loop of a subagent as soon as
// its turn ends, even while its own subagent runs, and a nested subagent's
// end goes to the main loop too. The parent stays listed while its child
// runs, and the child while the shell it left runs.
func TestClaudeHook_AParentThatEndsBeforeItsChildrenStaysUntilTheyDo(t *testing.T) {
	agent := newHookAgent(t)

	steps := agent.replay("nested-background")

	const l1, quick, slow = "general-purpose: Level1 orchestrator", "general-purpose: Level2 quick", "general-purpose: Level2 slow"
	const shell = "Sleep for 25 seconds in background"
	for _, c := range []struct {
		line int
		want string
	}{
		{11, l1 + "=working |   " + quick + "=working |   " + slow + "=working"},
		{12, l1 + "=working |   " + slow + "=working"},    // quick ended
		{13, l1 + "=background |   " + slow + "=working"}, // L1 ended its turn
		{15, l1 + "=background |   " + slow + "=working"}, // the main loop was told L1 ended
		{17, l1 + "=background |   " + slow + "=working"}, // the main loop's Stop does not list L1
		{19, l1 + "=background |   " + slow + "=background |     " + shell + "="},
		{21, l1 + "=background |   " + slow + "=background |     " + shell + "="},
		{22, ""},
	} {
		if got := treeOf(steps[c.line]); got != c.want {
			t.Errorf("after line %d (%s):\n got %q\nwant %q", c.line, steps[c.line].event, got, c.want)
		}
	}
	if got := steps[21].activity; got != store.ActivityBackground {
		t.Errorf("activity with the nested shell running = %q, want background", got)
	}
}

// writeSubagentMeta writes what Claude Code keeps of a subagent beside the
// session's transcript, and returns the transcript's path.
func writeSubagentMeta(t *testing.T, metas map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	transcript := filepath.Join(dir, "11111111-2222-3333-4444-555555555555.jsonl")
	subagents := filepath.Join(dir, "11111111-2222-3333-4444-555555555555", "subagents")
	if err := os.MkdirAll(subagents, 0o755); err != nil {
		t.Fatal(err)
	}
	for id, meta := range metas {
		if err := os.WriteFile(filepath.Join(subagents, "agent-"+id+".meta.json"), []byte(meta), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return transcript
}

// Each hook is a process of its own, so a list can reach Legato before the
// report of the start it lists. A subagent's parent is then read from what
// Claude Code wrote beside the transcript, and the parent stays.
func TestClaudeHook_HooksThatArriveOutOfOrderStillMakeTheTree(t *testing.T) {
	agent := newHookAgent(t)
	transcript := writeSubagentMeta(t, map[string]string{
		"a1": `{"agentType":"general-purpose","description":"Orchestrate","spawnDepth":1,"finished":true}`,
		"a2": `{"agentType":"general-purpose","description":"Implement D1","parentAgentId":"a1","spawnDepth":2}`,
		"a3": `{"agentType":"general-purpose","description":"Check D1","parentAgentId":"a2","spawnDepth":3}`,
	})
	list := func(ids ...string) string {
		labels := map[string]string{"a1": "Orchestrate", "a2": "Implement D1", "a3": "Check D1"}
		var tasks []string
		for _, id := range ids {
			tasks = append(tasks, fmt.Sprintf(`{"id":%q,"type":"subagent","status":"running","description":%q,"agent_type":"general-purpose"}`, id, labels[id]))
		}
		return "[" + strings.Join(tasks, ",") + "]"
	}
	send := func(payload string) step {
		return agent.send([]byte(strings.ReplaceAll(payload, "TRANSCRIPT", transcript)))
	}

	send(`{"hook_event_name":"UserPromptSubmit","prompt":"go","transcript_path":"TRANSCRIPT"}`)
	send(`{"hook_event_name":"PostToolUse","tool_name":"Agent","tool_input":{"description":"Orchestrate","subagent_type":"general-purpose"},"tool_response":{"isAsync":true,"agentId":"a1"},"transcript_path":"TRANSCRIPT"}`)
	send(`{"hook_event_name":"Stop","background_tasks":` + list("a1") + `,"transcript_path":"TRANSCRIPT"}`)
	// a1 starts a2, a2 starts a3, and a1 ends its turn; a1's list comes
	// before either start is reported.
	send(`{"hook_event_name":"SubagentStart","agent_id":"a2","agent_type":"general-purpose","transcript_path":"TRANSCRIPT"}`)
	send(`{"hook_event_name":"SubagentStart","agent_id":"a3","agent_type":"general-purpose","transcript_path":"TRANSCRIPT"}`)
	early := send(`{"hook_event_name":"SubagentStop","agent_id":"a1","agent_type":"general-purpose","background_tasks":` + list("a1", "a2", "a3") + `,"transcript_path":"TRANSCRIPT"}`)
	send(`{"hook_event_name":"PostToolUse","agent_id":"a1","tool_name":"Agent","tool_input":{"description":"Implement D1","subagent_type":"general-purpose"},"tool_response":{"isAsync":true,"agentId":"a2"},"transcript_path":"TRANSCRIPT"}`)
	late := send(`{"hook_event_name":"PostToolUse","agent_id":"a2","tool_name":"Agent","tool_input":{"description":"Check D1","subagent_type":"general-purpose"},"tool_response":{"isAsync":true,"agentId":"a3"},"transcript_path":"TRANSCRIPT"}`)
	// a2 stops before its child does, and its child's stop comes last.
	waiting := send(`{"hook_event_name":"SubagentStop","agent_id":"a2","agent_type":"general-purpose","background_tasks":` + list("a2", "a3") + `,"transcript_path":"TRANSCRIPT"}`)
	last := send(`{"hook_event_name":"SubagentStop","agent_id":"a3","agent_type":"general-purpose","background_tasks":` + list("a3") + `,"transcript_path":"TRANSCRIPT"}`)

	const o, d1, c1 = "general-purpose: Orchestrate", "general-purpose: Implement D1", "general-purpose: Check D1"
	for _, c := range []struct {
		name string
		got  step
		want string
	}{
		{"a1's list before the starts", early, o + "=background |   " + d1 + "=working |     " + c1 + "=working"},
		{"the starts reported late", late, o + "=background |   " + d1 + "=working |     " + c1 + "=working"},
		{"a2 ended before a3", waiting, o + "=background |   " + d1 + "=background |     " + c1 + "=working"},
		// a3 has ended; a2 and a1 stay through it, as Claude Code wakes a2.
		{"a3 ended", last, o + "=background |   " + d1 + "=background"},
	} {
		if got := treeOf(c.got); got != c.want {
			t.Errorf("%s:\n got %q\nwant %q", c.name, got, c.want)
		}
	}
}

// A foreground subagent runs inside its caller's work: only background
// ones are listed, at any depth.
func TestClaudeHook_OnlyBackgroundSubagentsAreListed(t *testing.T) {
	agent := newHookAgent(t)

	steps := agent.replay("nested-foreground")

	const l1, l2 = "general-purpose: L1 orchestrator", "general-purpose: L2 background"
	for _, c := range []struct {
		line int
		want string
	}{
		{10, l1 + "=working |   " + l2 + "=working"},
		{12, l1 + "=working |   " + l2 + "=working"}, // the foreground child has ended
		{14, l1 + "=background |   " + l2 + "=working"},
		{16, l1 + "=background |   " + l2 + "=working"},
		{18, l1 + "=background"},
		{20, ""},
	} {
		if got := treeOf(steps[c.line]); got != c.want {
			t.Errorf("after line %d (%s):\n got %q\nwant %q", c.line, steps[c.line].event, got, c.want)
		}
	}
	if got := steps[20].activity; got != "" {
		t.Errorf("activity at the end = %q, want idle", got)
	}
}

// A subagent whose stop never came (an interrupt fires none) leaves at the
// next list that does not name it, and so does its parent once nothing it
// started runs.
func TestClaudeHook_ASubagentTheListNoLongerNamesLeaves(t *testing.T) {
	agent := newHookAgent(t)
	agent.send([]byte(`{"hook_event_name":"UserPromptSubmit","prompt":"go"}`))
	agent.send([]byte(`{"hook_event_name":"PostToolUse","tool_name":"Agent","tool_input":{"description":"outer"},"tool_response":{"isAsync":true,"agentId":"s1"}}`))
	agent.send([]byte(`{"hook_event_name":"Stop","background_tasks":[{"id":"s1","type":"subagent","status":"running","description":"outer"}]}`))
	agent.send([]byte(`{"hook_event_name":"PostToolUse","agent_id":"s1","tool_name":"Agent","tool_input":{"description":"inner"},"tool_response":{"isAsync":true,"agentId":"s2"}}`))
	if got := treeOf(agent.now("")); got != "outer=working |   inner=working" {
		t.Fatalf("before = %q", got)
	}

	gone := agent.send([]byte(`{"hook_event_name":"SubagentStop","agent_id":"s1","background_tasks":[{"id":"s1","type":"subagent","status":"running","description":"outer"}]}`))

	if got := treeOf(gone); got != "" {
		t.Fatalf("after a list without the inner subagent = %q, want nothing", got)
	}
}

// A subagent's ID names a job's parent, so it is kept to what Legato can
// show: one line, clipped.
func TestClaudeHook_ATreeIsKeptToWhatLegatoCanShowSafely(t *testing.T) {
	agent := newHookAgent(t)
	long := strings.Repeat("p", 300)
	agent.send([]byte(`{"hook_event_name":"UserPromptSubmit","prompt":"go"}`))
	agent.send([]byte(`{"hook_event_name":"PostToolUse","agent_id":"` + long + `\u001b[31m","tool_name":"Agent","tool_input":{"description":"inner"},"tool_response":{"isAsync":true,"agentId":"s2"}}`))

	got := agent.now("").jobs
	if len(got) != 1 {
		t.Fatalf("jobs = %+v", got)
	}
	if n := utf8.RuneCountInString(got[0].Parent); n > 64 || strings.ContainsAny(got[0].Parent, "\x1b") {
		t.Fatalf("parent = %q (%d runes), want one clean line of at most 64", got[0].Parent, n)
	}
}

// A nested subagent's time is recorded under the subagent that started it.
func TestClaudeHook_ANestedSubagentIsTimedUnderItsParent(t *testing.T) {
	agent := newHookAgent(t)

	agent.replay("nested-woken-parent")

	parents, err := agent.s.SubagentParents(context.Background(), agent.execution)
	if err != nil {
		t.Fatal(err)
	}
	if got := parents["a1f248af89a364333"]; got != "a17f99dc869e3d03e" {
		t.Fatalf("L2's parent = %q, want L1", got)
	}
	if got, ok := parents["a17f99dc869e3d03e"]; !ok || got != "" {
		t.Fatalf("L1's parent = %q (recorded %v), want the top-level agent", got, ok)
	}
}
