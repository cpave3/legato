package service_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/cpave3/legato/internal/cli"
	"github.com/cpave3/legato/internal/engine/store"
	workflowengine "github.com/cpave3/legato/internal/engine/workflow"
	"github.com/cpave3/legato/internal/service"
)

type waitingAgentExecutor struct{}

func (waitingAgentExecutor) Type() string { return "agent.run" }
func (waitingAgentExecutor) Start(_ context.Context, step service.ResolvedWorkflowStep) (service.WorkflowExecutionResult, error) {
	return service.WorkflowExecutionResult{
		Status: "waiting_agent",
		AgentSession: &store.AgentSession{
			TaskID:      step.TaskID,
			TmuxSession: "legato-" + step.TaskID,
			AgentKind:   "chimera",
			Status:      "running",
		},
	}, nil
}
func (waitingAgentExecutor) Cancel(context.Context, store.WorkflowStepAttempt) error { return nil }

func TestSubagentCompletionDoesNotCreateWorkflowIdleAttention(t *testing.T) {
	ctx := context.Background()
	db, err := store.New(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	definition, err := workflowengine.Parse("test.yaml", []byte("schema_version: 1\nname: subagent\ntitle: Parent task\nsteps:\n  - id: run\n    title: Run\n    type: agent.run\n    with: {prompt: run}\n"))
	if err != nil {
		t.Fatal(err)
	}
	svc := service.NewWorkflowService(db, []service.WorkflowExecutor{waitingAgentExecutor{}})
	started, err := svc.Start(ctx, definition, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.InsertAgentSession(ctx, store.AgentSession{TaskID: started.Run.TaskID, TmuxSession: "legato-" + started.Run.TaskID, AgentKind: "chimera", Status: "running"}); err != nil {
		t.Fatal(err)
	}
	if err := db.UpdateAgentActivity(ctx, started.Run.TaskID, "working"); err != nil {
		t.Fatal(err)
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `[{"subagentId":"sub-1","sessionId":"child-1","purpose":"Review tests","status":"running"}]`)
	}))
	defer srv.Close()
	if err := db.UpdateChimeraDiscovery(ctx, started.Run.TaskID, "parent-1", srv.URL); err != nil {
		t.Fatal(err)
	}
	if err := cli.AgentState(db, cli.AgentStateReport{TaskID: started.Run.TaskID, Activity: "", SessionID: "child-1"}, nil, nil); err != nil {
		t.Fatal(err)
	}

	view, err := svc.Show(ctx, started.Run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if view.Attempts[0].NeedsAttention || view.Attempts[0].AgentActivity != "working" {
		t.Fatalf("attempt = %+v", view.Attempts[0])
	}
	items, err := service.NewWorkflowAttentionAdapter(svc).Snapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range items {
		if item.Navigation.Target == started.Run.ID {
			t.Fatalf("child completion created workflow attention: %+v", item)
		}
	}
}
