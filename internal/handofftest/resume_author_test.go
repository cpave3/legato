package handofftest

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/cpave3/legato/internal/service"
)

// A handoff's author that died (its worker restarted) comes back as it was:
// its conversation resumed, prompts still off, in the same clone, allowed to
// push its branches.
func TestAHandoffsAuthorIsResumedWithEverythingItHad(t *testing.T) {
	for _, kind := range []string{"claude-code", "codex"} {
		t.Run(kind, func(t *testing.T) {
			ctx := context.Background()
			worker := StartWorker(t, WorkerOptions{Name: "vm-1", GitHubRepositories: []string{"acme/*"}})
			main := NewMain(t, MainOptions{Name: "workstation", Clock: worker.Clock})
			paired := Pair(t, main, worker)
			taskID, _ := localTask(t, main, "Fix the flaky login test", "", "")
			away, err := main.Handoffs.HandOff(ctx, service.TaskHandoffRequest{TaskID: taskID, Worker: paired.InstanceID,
				Purpose: service.HandoffMakeChange, Repo: "acme/widget", AgentKind: kind})
			if err != nil {
				t.Fatal(err)
			}
			worker.Settle()
			card := away.WorkerTaskID
			first, err := worker.Store.GetAgentSessionByTaskID(ctx, card)
			if err != nil {
				t.Fatal(err)
			}
			clone := worker.Tmux.Spawned()[0].WorkDir
			// Its bridge saved the conversation, as a live author's does.
			if _, err := worker.Store.RecordTaskNativeSession(ctx, card, first.ID, first.ExecutionID, kind, "author-conversation", clone, ""); err != nil {
				t.Fatal(err)
			}

			// The author dies, and the worker restarts.
			_ = worker.Tmux.Kill("legato-" + card)
			worker.Restart()

			if err := worker.Handoffs.ResumeAuthor(ctx, card); err != nil {
				t.Fatalf("resuming the author: %v", err)
			}

			spawns := worker.Tmux.Spawned()
			resumed := spawns[len(spawns)-1]
			env := strings.Join(resumed.Env, "\n")
			if resumed.Name != "legato-"+card || resumed.WorkDir != clone || !strings.Contains(env, "LEGATO_RESUME_SESSION_ID=author-conversation") {
				t.Fatalf("resumed in %s (%s) with env %s; want the author's conversation in clone %s", resumed.Name, resumed.WorkDir, env, clone)
			}
			typed := strings.Join(worker.Tmux.Typed(resumed.Name), "\n")
			if !strings.Contains(typed, "--"+kind+"-prompts-off") {
				t.Fatalf("launch = %q; want its prompts still off", typed)
			}
			session, err := worker.Store.GetAgentSessionByTaskID(ctx, card)
			if err != nil || session.ExecutionID == first.ExecutionID || !session.Sandboxed {
				t.Fatalf("resumed session = %+v, %v", session, err)
			}
			grants, _ := worker.Credentials.Grants(ctx)
			pushes := false
			for _, g := range grants {
				pushes = pushes || (g.ExecutionID == session.ExecutionID && strings.Contains(strings.Join(g.Operations, ","), "git.push"))
			}
			if !pushes {
				t.Fatalf("grants %+v; want the resumed author allowed to push", grants)
			}
			if got, ok := service.HandoffSessionBranches(ctx, worker.Store, card); !ok || !got.Granted {
				t.Fatalf("branch guidance %+v, %v", got, ok)
			}

			// Running: nothing to do.
			if err := worker.Handoffs.ResumeAuthor(ctx, card); err != nil {
				t.Fatal(err)
			}
			if len(worker.Tmux.Spawned()) != len(spawns) {
				t.Fatal("an author that runs was started again")
			}
		})
	}
}

func TestAnAuthorWithNoSavedConversationIsNotStartedAfresh(t *testing.T) {
	ctx := context.Background()
	worker, away, _ := handOffAndSettle(t, service.HandoffMakeChange)
	_ = worker.Tmux.Kill("legato-" + away.WorkerTaskID)
	spawned := len(worker.Tmux.Spawned())

	err := worker.Handoffs.ResumeAuthor(ctx, away.WorkerTaskID)

	if !errors.Is(err, service.ErrAuthorNotResumable) || !strings.Contains(err.Error(), "no saved conversation") {
		t.Fatalf("err = %v", err)
	}
	if len(worker.Tmux.Spawned()) != spawned {
		t.Fatal("a fresh author was started")
	}
}
