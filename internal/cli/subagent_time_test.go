package cli

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/cpave3/legato/internal/engine/analytics"
	"github.com/cpave3/legato/internal/engine/store"
	"github.com/cpave3/legato/internal/service"
)

// subagentTimeAgent is a Claude Code agent whose hooks a test sends at the
// moments it chooses.
type subagentTimeAgent struct {
	t         *testing.T
	s         *store.Store
	taskID    string
	execution string
}

func newSubagentTimeAgent(t *testing.T, started time.Time) *subagentTimeAgent {
	t.Helper()
	ctx := context.Background()
	s, err := store.New(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	if err := s.CreateColumnMapping(ctx, store.ColumnMapping{ColumnName: "Doing"}); err != nil {
		t.Fatal(err)
	}
	if err := s.CreateTask(ctx, store.Task{ID: "tree1", Title: "Tree", Status: "Doing", CreatedAt: "2024-01-01T00:00:00Z", UpdatedAt: "2024-01-01T00:00:00Z"}); err != nil {
		t.Fatal(err)
	}
	if err := s.InsertAgentSession(ctx, store.AgentSession{TaskID: "tree1", TmuxSession: "legato-tree1", AgentKind: "claude-code", Status: "running"}); err != nil {
		t.Fatal(err)
	}
	session, err := s.GetAgentSessionByTaskID(ctx, "tree1")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.CreateExecution(ctx, store.Execution{ID: "exec-tree1", Kind: store.ExecutionInteractive, Status: store.ExecutionRunning, TaskID: "tree1", ExecutionTaskID: "tree1", AgentSessionID: &session.ID, AgentKind: "claude-code", SubagentTelemetry: true, StartedAt: started}); err != nil {
		t.Fatal(err)
	}
	return &subagentTimeAgent{t: t, s: s, taskID: "tree1", execution: "exec-tree1"}
}

func (a *subagentTimeAgent) hook(at time.Time, payload string) {
	a.t.Helper()
	hook, err := ParseClaudeHook([]byte(payload))
	if err != nil {
		a.t.Fatal(err)
	}
	if err := applyClaudeHookAt(a.s, ClaudeHookReport{TaskID: a.taskID, ExecutionID: a.execution, Hook: hook}, at, nil, nil); err != nil {
		a.t.Fatalf("applying %s: %v", hook.Event, err)
	}
}

func (a *subagentTimeAgent) activity() string {
	a.t.Helper()
	session, err := a.s.GetAgentSessionByTaskID(context.Background(), a.taskID)
	if err != nil {
		a.t.Fatal(err)
	}
	return session.Activity
}

func (a *subagentTimeAgent) report(start, end time.Time) (*service.Report, service.TaskStats) {
	a.t.Helper()
	report, err := service.NewReportService(a.s).GenerateReport(context.Background(), analytics.TimeRange{Start: start, End: end})
	if err != nil {
		a.t.Fatal(err)
	}
	for _, task := range report.ByTask {
		if task.TaskID == a.taskID {
			return report, task
		}
	}
	a.t.Fatalf("the task is not in the report: %+v", report.ByTask)
	return nil, service.TaskStats{}
}

var fiveSubagents = []string{"sa1", "sa2", "sa3", "sa4", "sa5"}

// The parent works 10 minutes, starts five background sub-agents and ends its
// turn; they work 10 minutes together while it waits in the background.
func TestSubagentTime_FromClaudeCodeHooksToTheReport(t *testing.T) {
	t0 := time.Now().UTC().Add(-3 * time.Hour).Truncate(time.Minute)
	agent := newSubagentTimeAgent(t, t0)
	agent.hook(t0, `{"hook_event_name":"UserPromptSubmit","prompt":"split the work"}`)
	running := ""
	for i, id := range fiveSubagents {
		agent.hook(t0.Add(10*time.Minute), fmt.Sprintf(`{"hook_event_name":"SubagentStart","agent_id":%q,"agent_type":"general-purpose"}`, id))
		if i > 0 {
			running += ","
		}
		running += fmt.Sprintf(`{"id":%q,"type":"subagent","status":"running","description":"part %d","agent_type":"general-purpose"}`, id, i)
	}
	agent.hook(t0.Add(10*time.Minute), `{"hook_event_name":"Stop","background_tasks":[`+running+`]}`)
	if got := agent.activity(); got != store.ActivityBackground {
		t.Fatalf("the parent while its sub-agents work: %q, want background", got)
	}
	// One of them asks the operator for a minute, then goes on.
	agent.hook(t0.Add(12*time.Minute), `{"hook_event_name":"PermissionRequest","agent_id":"sa1","tool_name":"Bash"}`)
	agent.hook(t0.Add(13*time.Minute), `{"hook_event_name":"PostToolUse","agent_id":"sa1","tool_name":"Bash"}`)
	for _, id := range fiveSubagents {
		agent.hook(t0.Add(20*time.Minute), fmt.Sprintf(`{"hook_event_name":"SubagentStop","agent_id":%q,"agent_type":"general-purpose"}`, id))
	}
	if err := agent.s.TerminalizeExecutionAt(context.Background(), agent.execution, store.ExecutionSucceeded, t0.Add(30*time.Minute)); err != nil {
		t.Fatal(err)
	}

	whole, task := agent.report(t0, t0.Add(time.Hour))

	// 10 parent minutes and 5 × 10 sub-agent minutes, one of which waited.
	if task.Working != 10*time.Minute || task.SubagentWorking != 49*time.Minute || task.SubagentWaiting != time.Minute {
		t.Fatalf("own working %v, sub-agents working %v and waiting %v; want 10m, 49m, 1m", task.Working, task.SubagentWorking, task.SubagentWaiting)
	}
	if task.AggregateWorking != 59*time.Minute || task.Elapsed != 20*time.Minute || task.SubagentCount != 5 {
		t.Fatalf("aggregate %v, elapsed %v, sub-agents %d; want 59m, 20m, 5", task.AggregateWorking, task.Elapsed, task.SubagentCount)
	}
	if task.SubagentCoverage != store.SubagentCoverageComplete || whole.Summary.AggregateWorking != 59*time.Minute {
		t.Fatalf("coverage %q, summary aggregate %v", task.SubagentCoverage, whole.Summary.AggregateWorking)
	}

	// The window the five worked together: 50 agent-minutes in 10.
	_, window := agent.report(t0.Add(10*time.Minute), t0.Add(20*time.Minute))
	if window.Working != 0 || window.AggregateWorking+window.SubagentWaiting != 50*time.Minute || window.Elapsed != 10*time.Minute {
		t.Fatalf("window: own %v, aggregate %v (+%v waiting), elapsed %v; want 0, 50m in all, 10m", window.Working, window.AggregateWorking, window.SubagentWaiting, window.Elapsed)
	}
}

// Without the sub-agent's wait: the ticket's sixty agent-minutes.
func TestSubagentTime_SixtyAgentMinutesFromTenParentAndFiveSubagentMinutes(t *testing.T) {
	t0 := time.Now().UTC().Add(-3 * time.Hour).Truncate(time.Minute)
	agent := newSubagentTimeAgent(t, t0)
	agent.hook(t0, `{"hook_event_name":"UserPromptSubmit","prompt":"split the work"}`)
	for _, id := range fiveSubagents {
		agent.hook(t0.Add(10*time.Minute), fmt.Sprintf(`{"hook_event_name":"SubagentStart","agent_id":%q}`, id))
	}
	agent.hook(t0.Add(10*time.Minute), `{"hook_event_name":"Stop","background_tasks":[`+
		`{"id":"sa1","type":"subagent"},{"id":"sa2","type":"subagent"},{"id":"sa3","type":"subagent"},{"id":"sa4","type":"subagent"},{"id":"sa5","type":"subagent"}]}`)
	for _, id := range fiveSubagents {
		agent.hook(t0.Add(20*time.Minute), fmt.Sprintf(`{"hook_event_name":"SubagentStop","agent_id":%q}`, id))
	}

	_, task := agent.report(t0, t0.Add(time.Hour))
	if task.AggregateWorking != 60*time.Minute || task.Elapsed != 20*time.Minute {
		t.Fatalf("aggregate %v, elapsed %v; want 60m and 20m", task.AggregateWorking, task.Elapsed)
	}
	_, window := agent.report(t0.Add(10*time.Minute), t0.Add(20*time.Minute))
	if window.AggregateWorking != 50*time.Minute || window.Elapsed != 10*time.Minute {
		t.Fatalf("window: aggregate %v, elapsed %v; want 50m and 10m", window.AggregateWorking, window.Elapsed)
	}
}
