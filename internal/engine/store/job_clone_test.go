package store

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestJobClonesLifecycle(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	clone := JobClone{ExecutionID: "ex-1", TaskID: "task1", Repo: "acme/widget", Ref: "feature/x", Branch: "feature/x",
		Path: "/data/jobs/ex-1/widget", CreatedAt: time.Date(2026, 11, 2, 10, 0, 0, 0, time.UTC)}
	if _, err := s.CreateJobClone(ctx, clone); err != nil {
		t.Fatalf("CreateJobClone: %v", err)
	}

	got, err := s.JobCloneByPath(ctx, "/data/jobs/ex-1/widget")
	if err != nil {
		t.Fatalf("JobCloneByPath: %v", err)
	}
	if got.TaskID != "task1" || got.Repo != "acme/widget" || got.Branch != "feature/x" || !got.CreatedAt.Equal(clone.CreatedAt) || got.RemoveAfter != nil {
		t.Fatalf("got %+v", got)
	}

	keep := time.Date(2026, 11, 9, 10, 0, 0, 0, time.UTC)
	if err := s.KeepJobCloneUntil(ctx, got.ID, keep); err != nil {
		t.Fatal(err)
	}
	live, err := s.ListLiveJobClones(ctx)
	if err != nil || len(live) != 1 || live[0].RemoveAfter == nil || !live[0].RemoveAfter.Equal(keep) {
		t.Fatalf("live = %+v, %v", live, err)
	}

	if err := s.MarkJobCloneRemoved(ctx, got.ID, keep); err != nil {
		t.Fatal(err)
	}
	if live, _ := s.ListLiveJobClones(ctx); len(live) != 0 {
		t.Fatalf("a removed clone is still live: %+v", live)
	}
	if _, err := s.JobCloneByPath(ctx, "/data/jobs/ex-1/widget"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("a removed clone is still found by its path: %v", err)
	}
}

func TestAJobCloneHoldsTheTasksThatJoinedIt(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	made := time.Date(2026, 11, 2, 10, 0, 0, 0, time.UTC)
	if _, err := s.CreateJobClone(ctx, JobClone{ExecutionID: "ex-1", TaskID: "base", Repo: "acme/widget", Branch: "legato/base",
		Path: "/data/jobs/ex-1/widget", CreatedAt: made}); err != nil {
		t.Fatal(err)
	}
	clone, err := s.JobCloneByPath(ctx, "/data/jobs/ex-1/widget")
	if err != nil {
		t.Fatal(err)
	}
	if len(clone.TaskIDs) != 1 || clone.TaskIDs[0] != "base" || !clone.LastUsedAt.Equal(made) {
		t.Fatalf("a new clone = %+v", clone)
	}

	joined := made.Add(time.Hour)
	if err := s.AttachJobCloneTask(ctx, clone.ID, "next", joined); err != nil {
		t.Fatalf("AttachJobCloneTask: %v", err)
	}
	if err := s.AttachJobCloneTask(ctx, clone.ID, "next", joined); err != nil {
		t.Fatalf("joining twice: %v", err)
	}
	got, err := s.JobCloneByID(ctx, clone.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.TaskIDs) != 2 || got.TaskIDs[1] != "next" || !got.LastUsedAt.Equal(joined) {
		t.Fatalf("after a second task joined = %+v", got)
	}

	forTask, err := s.LiveJobCloneForTask(ctx, "next", "acme/widget")
	if err != nil || forTask.ID != clone.ID {
		t.Fatalf("LiveJobCloneForTask = %+v, %v", forTask, err)
	}
	if _, err := s.LiveJobCloneForTask(ctx, "next", "acme/other"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("a clone of another repository: %v", err)
	}

	used := joined.Add(time.Hour)
	if err := s.TouchJobClone(ctx, clone.ID, used); err != nil {
		t.Fatal(err)
	}
	live, _ := s.ListLiveJobClones(ctx)
	if len(live) != 1 || !live[0].LastUsedAt.Equal(used) || len(live[0].TaskIDs) != 2 {
		t.Fatalf("live = %+v", live)
	}

	if err := s.MarkJobCloneRemoved(ctx, clone.ID, used); err != nil {
		t.Fatal(err)
	}
	if _, err := s.LiveJobCloneForTask(ctx, "next", "acme/widget"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("a removed clone is still the task's: %v", err)
	}
	if _, err := s.JobCloneByID(ctx, 999); !errors.Is(err, ErrNotFound) {
		t.Fatalf("JobCloneByID of nothing: %v", err)
	}
}
