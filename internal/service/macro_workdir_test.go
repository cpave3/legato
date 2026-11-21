package service

import (
	"context"
	"testing"
	"time"

	"github.com/cpave3/legato/internal/engine/store"
)

func startedIn(t *testing.T, svc *MacroService, spy *macroAgentsSpy, pr *MacroPR) string {
	t.Helper()
	spy.spawned = nil
	if _, err := svc.Start(context.Background(), MacroStart{Name: "Address feedback", TaskID: "abc12345", PR: pr}); err != nil {
		t.Fatal(err)
	}
	if len(spy.spawned) != 1 {
		t.Fatalf("spawned %v", spy.spawned)
	}
	return spy.spawned[0].WorkingDir
}

func TestAMacrosNewAgentStartsInTheTasksCheckout(t *testing.T) {
	ctx := context.Background()
	pr := &MacroPR{Repo: "acme/app", Number: 7}

	t.Run("with nothing known, the server's default", func(t *testing.T) {
		svc, spy := newMacroStartFixture(t)
		if got := startedIn(t, svc, spy, pr); got != "" {
			t.Fatalf("working dir = %q, want the default", got)
		}
	})

	t.Run("its remembered working directory", func(t *testing.T) {
		svc, spy := newMacroStartFixture(t)
		if err := svc.settings.db.SetCodeContext(ctx, "abc12345", &store.CodeContext{Repo: "acme/app", WorkingDir: "/src/app"}); err != nil {
			t.Fatal(err)
		}
		if got := startedIn(t, svc, spy, pr); got != "/src/app" {
			t.Fatalf("working dir = %q", got)
		}
	})

	t.Run("its live job clone of the PR's repo before the remembered directory", func(t *testing.T) {
		svc, spy := newMacroStartFixture(t)
		db := svc.settings.db
		if err := db.SetCodeContext(ctx, "abc12345", &store.CodeContext{Repo: "acme/app", WorkingDir: "/src/app"}); err != nil {
			t.Fatal(err)
		}
		if _, err := db.CreateJobClone(ctx, store.JobClone{ExecutionID: "ex-1", TaskID: "abc12345", Repo: "acme/other", Path: "/jobs/ex-1/other", CreatedAt: time.Now()}); err != nil {
			t.Fatal(err)
		}
		if _, err := db.CreateJobClone(ctx, store.JobClone{ExecutionID: "ex-2", TaskID: "abc12345", Repo: "acme/app", Path: "/jobs/ex-2/app", CreatedAt: time.Now()}); err != nil {
			t.Fatal(err)
		}
		if got := startedIn(t, svc, spy, pr); got != "/jobs/ex-2/app" {
			t.Fatalf("working dir = %q", got)
		}
		// A task macro uses the clone of the task's own repository.
		if got := startedIn(t, svc, spy, nil); got != "/jobs/ex-2/app" {
			t.Fatalf("task macro working dir = %q", got)
		}
		// Another repo's PR, with no clone of it, falls back to the
		// remembered directory.
		if got := startedIn(t, svc, spy, &MacroPR{Repo: "acme/sdk", Number: 1}); got != "/src/app" {
			t.Fatalf("other repo working dir = %q", got)
		}
	})

	t.Run("its worktree before anything else", func(t *testing.T) {
		svc, spy := newMacroStartFixture(t)
		db := svc.settings.db
		if _, err := db.CreateJobClone(ctx, store.JobClone{ExecutionID: "ex-2", TaskID: "abc12345", Repo: "acme/app", Path: "/jobs/ex-2/app", CreatedAt: time.Now()}); err != nil {
			t.Fatal(err)
		}
		if err := db.SetTaskWorktree(ctx, "abc12345", &store.TaskWorktree{PrimaryDir: "/src/app", Path: "/src/app/.worktrees/abc", Branch: "abc", BaseBranch: "main"}); err != nil {
			t.Fatal(err)
		}
		if got := startedIn(t, svc, spy, pr); got != "/src/app/.worktrees/abc" {
			t.Fatalf("working dir = %q", got)
		}
	})

	t.Run("a resumed conversation keeps its own directory", func(t *testing.T) {
		svc, spy := newMacroStartFixture(t)
		if err := svc.settings.db.SetTaskWorktree(ctx, "abc12345", &store.TaskWorktree{PrimaryDir: "/src/app", Path: "/src/app/.worktrees/abc", Branch: "abc", BaseBranch: "main"}); err != nil {
			t.Fatal(err)
		}
		spy.saved = &TaskResumeSession{ID: "s-1", CWD: "/where/it/was"}
		if got := startedIn(t, svc, spy, pr); got != "/where/it/was" {
			t.Fatalf("working dir = %q", got)
		}
		if spy.spawned[0].ResumeWorkingDir != "/where/it/was" {
			t.Fatalf("resume dir = %q", spy.spawned[0].ResumeWorkingDir)
		}
	})
}
