package handofftest

import (
	"context"
	"strings"
	"testing"

	"github.com/cpave3/legato/internal/service"
)

// handOffAndSettle hands a new local task to the worker for purpose and
// waits for its agent to start.
func handOffAndSettle(t *testing.T, purpose string) (*Worker, service.AwayTask, string) {
	t.Helper()
	worker := StartWorker(t, WorkerOptions{Name: "vm-1", GitHubRepositories: []string{"acme/*"}})
	main := NewMain(t, MainOptions{Name: "workstation", Clock: worker.Clock})
	paired := Pair(t, main, worker)
	taskID, _ := localTask(t, main, "Fix the flaky login test", "", "")
	away, err := main.Handoffs.HandOff(context.Background(), service.TaskHandoffRequest{TaskID: taskID, Worker: paired.InstanceID,
		Purpose: purpose, Repo: "acme/widget", AgentKind: "claude-code"})
	if err != nil {
		t.Fatal(err)
	}
	worker.Settle()
	return worker, away, taskID
}

func TestAMakeAChangeHandoffsSessionMayPushTheMainTasksBranchesAndOpenDrafts(t *testing.T) {
	ctx := context.Background()
	worker, away, taskID := handOffAndSettle(t, service.HandoffMakeChange)

	session, err := worker.Store.GetAgentSessionByTaskID(ctx, away.WorkerTaskID)
	if err != nil {
		t.Fatal(err)
	}
	grants, err := worker.Credentials.Grants(ctx)
	if err != nil {
		t.Fatal(err)
	}
	branch := "legato/" + strings.ToLower(taskID)
	if len(grants) != 1 {
		t.Fatalf("grants %+v", grants)
	}
	g := grants[0]
	if g.Credential != "github" || g.TaskID != away.WorkerTaskID || g.ExecutionID != session.ExecutionID || g.Mode != "session" ||
		strings.Join(g.Operations, ",") != "git.push,pr.create-draft" ||
		strings.Join(g.Resources, " ") != "github:acme/widget@refs/heads/"+branch+" github:acme/widget@refs/heads/"+branch+"-*" {
		t.Fatalf("grant %+v", g)
	}
	// Its guidance names the main task's branches, and says they are
	// already allowed.
	if got, ok := service.HandoffSessionBranches(ctx, worker.Store, away.WorkerTaskID); !ok || got.Base != branch || !got.Granted {
		t.Fatalf("session branches %+v, %v", got, ok)
	}
}

func TestAFindOutHandoffsSessionGetsNoGrant(t *testing.T) {
	ctx := context.Background()
	worker, away, taskID := handOffAndSettle(t, service.HandoffFindOut)

	if _, err := worker.Store.GetAgentSessionByTaskID(ctx, away.WorkerTaskID); err != nil {
		t.Fatalf("the agent did not start: %v", err)
	}
	if grants, err := worker.Credentials.Grants(ctx); err != nil || len(grants) != 0 {
		t.Fatalf("grants %+v, %v", grants, err)
	}
	if got, ok := service.HandoffSessionBranches(ctx, worker.Store, away.WorkerTaskID); !ok || got.Base != "legato/"+strings.ToLower(taskID) || got.Granted {
		t.Fatalf("session branches %+v, %v", got, ok)
	}
}
