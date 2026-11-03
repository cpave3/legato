package store

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestACloneStackIsListedForItsTasksAndOutlivesTheClone(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	at := time.Date(2026, 11, 3, 9, 0, 0, 0, time.UTC)
	id, err := s.CreateJobClone(ctx, JobClone{ExecutionID: "ex-1", TaskID: "t1", Repo: "acme/widget", Path: "/data/jobs/ex-1/widget", CreatedAt: at})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.AttachJobCloneTask(ctx, id, "t2", at); err != nil {
		t.Fatal(err)
	}
	if got, err := s.ListTaskStacks(ctx, "t1"); err != nil || len(got) != 0 {
		t.Fatalf("stacks before any read = %+v, %v", got, err)
	}

	first := CloneStack{CloneID: id, Root: "main", Branches: `[{"name":"a"}]`, ReadAt: at}
	if err := s.PutCloneStack(ctx, first); err != nil {
		t.Fatal(err)
	}
	second := CloneStack{CloneID: id, Root: "main", Branches: `[{"name":"a"},{"name":"b"}]`, Problem: "p", ReadAt: at.Add(time.Hour)}
	if err := s.PutCloneStack(ctx, second); err != nil {
		t.Fatal(err)
	}
	for _, task := range []string{"t1", "t2"} {
		got, err := s.ListTaskStacks(ctx, task)
		if err != nil || len(got) != 1 {
			t.Fatalf("%s: stacks = %+v, %v", task, got, err)
		}
		if g := got[0]; g.Branches != second.Branches || g.Problem != second.Problem || !g.ReadAt.Equal(second.ReadAt) ||
			g.Repo != "acme/widget" || g.Path != "/data/jobs/ex-1/widget" || !g.Live {
			t.Fatalf("%s: stack = %+v", task, g)
		}
	}
	if got, _ := s.ListTaskStacks(ctx, "t3"); len(got) != 0 {
		t.Fatalf("a task outside the clone sees its stack: %+v", got)
	}

	if live, err := s.ListLiveTaskJobClones(ctx, "t2"); err != nil || len(live) != 1 || live[0].ID != id {
		t.Fatalf("live clones of t2 = %+v, %v", live, err)
	}
	if err := s.MarkJobCloneRemoved(ctx, id, at.Add(2*time.Hour)); err != nil {
		t.Fatal(err)
	}
	got, err := s.ListTaskStacks(ctx, "t1")
	if err != nil || len(got) != 1 || got[0].Live {
		t.Fatalf("after removal: %+v, %v", got, err)
	}
	if live, _ := s.ListLiveTaskJobClones(ctx, "t2"); len(live) != 0 {
		t.Fatalf("a removed clone is live: %+v", live)
	}
	if one, err := s.CloneStackByID(ctx, id); err != nil || one.Root != "main" {
		t.Fatalf("CloneStackByID = %+v, %v", one, err)
	}

	if err := s.DeleteCloneStack(ctx, id); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.ListTaskStacks(ctx, "t1"); len(got) != 0 {
		t.Fatalf("a deleted stack is listed: %+v", got)
	}
	if _, err := s.CloneStackByID(ctx, id); !errors.Is(err, ErrNotFound) {
		t.Fatalf("CloneStackByID after delete: %v", err)
	}
}
