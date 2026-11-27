package service

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/cpave3/legato/internal/engine/gitrepo"
	"github.com/cpave3/legato/internal/engine/store"
)

// Real repository cleanup and agent resume share the same durable store;
// only tmux and GitHub are local stand-ins.
func interruptedCloneFixture(t *testing.T) (restartFixture, *RepositoryService, *time.Time) {
	t.Helper()
	f := newRestartFixture(t)
	gh := newRepoUpstream(t, "acme/widget", "acme/other")
	now := time.Now()
	repos := NewRepositoryService(RepositoryServiceOptions{
		Store: f.store, DataDir: t.TempDir(), KeepIdle: 2 * time.Hour,
		Git: gitrepo.Runner{AskPass: "/bin/false"}, Tokens: fakeTokens{gh: gh, proxy: true},
		Now: func() time.Time { return now },
	})
	f.svc.(*agentService).SetRepositories(repos)
	return f, repos, &now
}

func TestAutomaticCleanupKeepsAnInterruptedCloneResumable(t *testing.T) {
	f, repos, now := interruptedCloneFixture(t)
	ctx := context.Background()
	createTask(t, f.store, "task-i")
	if err := f.svc.SpawnAgent(ctx, "task-i", 0, 0, AgentSpawnOptions{AgentKind: "claude-code", Repo: "acme/widget"}); err != nil {
		t.Fatal(err)
	}
	clones, err := repos.ListJobClones(ctx)
	if err != nil || len(clones) != 1 {
		t.Fatalf("clones = %+v, %v", clones, err)
	}
	clone := clones[0]
	f.reportConversation(t, "task-i", clone.Path)
	bundle := filepath.Join(clone.Path, ".legato", "plans", "local", "plan.md")
	if err := os.MkdirAll(filepath.Dir(bundle), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(clone.Path, ".git", "info", "exclude"), []byte(".legato/\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(bundle, []byte("local plan"), 0600); err != nil {
		t.Fatal(err)
	}
	f.crash()
	*now = now.Add(24 * time.Hour)
	// A sweep before startup reconciliation still sees a running execution.
	if err := repos.SweepJobClones(ctx); err != nil {
		t.Fatal(err)
	}
	if n, err := f.interrupted().SweepInterruptedAgents(ctx); err != nil || n != 1 {
		t.Fatalf("startup = %d, %v", n, err)
	}
	// Repeated periodic sweeps must not age the interrupted agent out.
	for range 2 {
		*now = now.Add(24 * time.Hour)
		if err := repos.SweepJobClones(ctx); err != nil {
			t.Fatal(err)
		}
		if !exists(bundle) {
			t.Fatal("automatic cleanup deleted the interrupted agent's local plan")
		}
	}
	if err := f.interrupted().ResumeInterruptedAgent(ctx, "task-i", false); err != nil {
		t.Fatalf("resume after cleanup: %v", err)
	}
	sentLineEventually(t, f.tmux, "task-i", "env -u CLAUDE_CONFIG_DIR claude --resume conversation-1")
	session, err := f.store.GetAgentSessionByTaskID(ctx, "task-i")
	if err != nil {
		t.Fatal(err)
	}
	execution, err := f.store.GetExecution(ctx, session.ExecutionID)
	if err != nil || execution.WorkingDir != clone.Path {
		t.Fatalf("resumed execution = %+v, %v", execution, err)
	}
	*now = now.Add(24 * time.Hour)
	if err := repos.SweepJobClones(ctx); err != nil {
		t.Fatal(err)
	}
	if !exists(bundle) {
		t.Fatal("resumed agent lost its clone")
	}
}

func TestDismissalReleasesAutomaticCleanupHoldWithoutResettingRetention(t *testing.T) {
	for _, unpushed := range []bool{false, true} {
		t.Run(fmt.Sprintf("unpushed=%t", unpushed), func(t *testing.T) {
			f, repos, now := interruptedCloneFixture(t)
			ctx := context.Background()
			clone, err := repos.JobClone(ctx, JobCloneRequest{ExecutionID: "ex-original", TaskID: "task-i", Repo: "acme/widget"})
			if err != nil {
				t.Fatal(err)
			}
			if unpushed {
				gitIn(t, clone.Path, "commit", "-q", "--allow-empty", "-m", "unpushed")
				*now = now.Add(3 * time.Hour)
				if err := repos.SweepJobClones(ctx); err != nil {
					t.Fatal(err)
				}
				kept, err := f.store.JobCloneByID(ctx, clone.ID)
				if err != nil || kept.RemoveAfter == nil {
					t.Fatalf("unpushed retention = %+v, %v", kept, err)
				}
			}
			f.interruptedAgent(t, "task-i", clone.Path, true)
			*now = now.Add(10 * 24 * time.Hour)
			if err := repos.SweepJobClones(ctx); err != nil {
				t.Fatal(err)
			}
			if !exists(clone.Path) {
				t.Fatal("interrupted hold lost to retention deadline")
			}
			if err := f.interrupted().DismissInterruptedAgent(ctx, "task-i"); err != nil {
				t.Fatal(err)
			}
			if err := repos.SweepJobClones(ctx); err != nil {
				t.Fatal(err)
			}
			if exists(clone.Path) {
				t.Fatal("dismissal did not release the expired clone")
			}
		})
	}
}

func TestSharedInterruptedCloneIsHeldThroughCanonicalAliasesUntilEveryDismissal(t *testing.T) {
	f, repos, now := interruptedCloneFixture(t)
	ctx := context.Background()
	clone, err := repos.JobClone(ctx, JobCloneRequest{ExecutionID: "ex-original", TaskID: "task-a", Repo: "acme/widget"})
	if err != nil {
		t.Fatal(err)
	}
	// Like /home -> /var/home, two spellings name the same directory.
	alias := filepath.Join(t.TempDir(), "home")
	if err := os.Symlink(filepath.Dir(clone.Path), alias); err != nil {
		t.Fatal(err)
	}
	aliasCWD := filepath.Join(alias, filepath.Base(clone.Path), "nested")
	if err := os.MkdirAll(aliasCWD, 0700); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"task-a", "task-b"} {
		createTask(t, f.store, id)
		if _, err := repos.JobClone(ctx, JobCloneRequest{ExecutionID: "ex-join-" + id, TaskID: id, CloneID: clone.ID}); err != nil {
			t.Fatal(err)
		}
		cwd := clone.Path
		if id == "task-b" {
			cwd = aliasCWD
		}
		if err := f.svc.SpawnAgent(ctx, id, 0, 0, AgentSpawnOptions{AgentKind: "claude-code", WorkingDir: cwd}); err != nil {
			t.Fatal(err)
		}
		f.reportConversation(t, id, cwd)
	}
	// Another clone belonging to task-a is not held just because the task
	// is interrupted: only its actual execution/folder holds a clone.
	other, err := repos.JobClone(ctx, JobCloneRequest{ExecutionID: "ex-other", TaskID: "task-a", Repo: "acme/other"})
	if err != nil {
		t.Fatal(err)
	}
	f.crash()
	if n, err := f.interrupted().SweepInterruptedAgents(ctx); err != nil || n != 2 {
		t.Fatalf("startup = %d, %v", n, err)
	}
	*now = now.Add(24 * time.Hour)
	if err := f.interrupted().DismissInterruptedAgent(ctx, "task-a"); err != nil {
		t.Fatal(err)
	}
	if err := repos.SweepJobClones(ctx); err != nil {
		t.Fatal(err)
	}
	if !exists(clone.Path) {
		t.Fatal("dismissing one agent released the other agent's aliased shared clone")
	}
	if exists(other.Path) {
		t.Fatal("an interrupted task held an unrelated clone")
	}
	if err := f.interrupted().ResumeInterruptedAgent(ctx, "task-b", false); err != nil {
		t.Fatalf("resume through canonical alias: %v", err)
	}
	sentLineEventually(t, f.tmux, "task-b", "env -u CLAUDE_CONFIG_DIR claude --resume conversation-1")
	// Interrupt that resumed execution too, then dismiss the last holder.
	f.crash()
	if _, err := f.interrupted().SweepInterruptedAgents(ctx); err != nil {
		t.Fatal(err)
	}
	if err := f.interrupted().DismissInterruptedAgent(ctx, "task-b"); err != nil {
		t.Fatal(err)
	}
	if err := repos.SweepJobClones(ctx); err != nil {
		t.Fatal(err)
	}
	if exists(clone.Path) {
		t.Fatal("shared clone remains after the last holder was dismissed")
	}
}

func TestInterruptedHoldDoesNotRequireAConversationOrPreventExplicitRemoval(t *testing.T) {
	f, repos, now := interruptedCloneFixture(t)
	ctx := context.Background()
	clone, err := repos.JobClone(ctx, JobCloneRequest{ExecutionID: "ex-original", TaskID: "task-i", Repo: "acme/widget"})
	if err != nil {
		t.Fatal(err)
	}
	f.interruptedAgent(t, "task-i", clone.Path, false)
	*now = now.Add(24 * time.Hour)
	if err := repos.SweepJobClones(ctx); err != nil {
		t.Fatal(err)
	}
	if !exists(clone.Path) {
		t.Fatal("an interrupted agent without a conversation lost its clone")
	}
	list, err := f.interrupted().ListInterruptedAgents(ctx)
	if err != nil || len(list) != 1 || !list[0].CanStartNew {
		t.Fatalf("interrupted = %+v, %v", list, err)
	}
	if err := repos.RemoveJobClone(ctx, clone.ID); err != nil {
		t.Fatalf("intentional removal: %v", err)
	}
	if exists(clone.Path) {
		t.Fatal("automatic hold changed explicit removal policy")
	}
}

func TestAutomaticCleanupHoldsTheCloneDuringStartupTerminalization(t *testing.T) {
	f, repos, now := interruptedCloneFixture(t)
	ctx := context.Background()
	createTask(t, f.store, "task-i")
	if err := f.svc.SpawnAgent(ctx, "task-i", 0, 0, AgentSpawnOptions{AgentKind: "claude-code", Repo: "acme/widget"}); err != nil {
		t.Fatal(err)
	}
	session, err := f.store.GetAgentSessionByTaskID(ctx, "task-i")
	if err != nil {
		t.Fatal(err)
	}
	clones, err := repos.ListJobClones(ctx)
	if err != nil || len(clones) != 1 {
		t.Fatalf("clones = %+v, %v", clones, err)
	}
	f.crash()
	// endGoneSession ends the execution before changing the session's
	// status. Another process's periodic sweep can run between those writes.
	if err := f.svc.(*agentService).EndExecution(ctx, session.ExecutionID, store.ExecutionLost); err != nil {
		t.Fatal(err)
	}
	*now = now.Add(24 * time.Hour)
	if err := repos.SweepJobClones(ctx); err != nil {
		t.Fatal(err)
	}
	if !exists(clones[0].Path) {
		t.Fatal("startup terminalization exposed the clone to a periodic sweep")
	}
	if n, err := f.interrupted().SweepInterruptedAgents(ctx); err != nil || n != 1 {
		t.Fatalf("startup = %d, %v", n, err)
	}
}
