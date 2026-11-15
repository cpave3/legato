package service

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/cpave3/legato/internal/engine/store"
)

// planOfTask stores a plan with one revision, without the validator.
func planOfTask(t *testing.T, db *store.Store, taskID, name, status, markdown string) store.Plan {
	t.Helper()
	plan := store.Plan{ID: "pl-" + taskID + "-" + name, TaskID: taskID, Name: name, Title: "Plan " + name, Summary: "about " + name, Status: status, LatestRevision: 1}
	revision := store.PlanRevision{ID: plan.ID + "-r1", PlanID: plan.ID, Revision: 1, Markdown: markdown, ManifestJSON: "{}"}
	if err := db.InsertPlanRevision(context.Background(), plan, revision, nil); err != nil {
		t.Fatal(err)
	}
	return plan
}

func planReadStore(t *testing.T) *store.Store {
	t.Helper()
	db, err := store.New(filepath.Join(t.TempDir(), "legato.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	for _, id := range []string{"LEG-160", "LEG-1"} {
		if err := db.CreateTask(context.Background(), store.Task{ID: id, Title: id, Status: "Doing"}); err != nil {
			t.Fatal(err)
		}
	}
	return db
}

// A task's plans are listed newest first, whatever their status.
func TestTaskPlansListsEveryPlanOfTheTaskNewestFirst(t *testing.T) {
	db := planReadStore(t)
	planOfTask(t, db, "LEG-160", "first", "approved", "# First")
	planOfTask(t, db, "LEG-160", "macos-seatbelt-sandbox", "proposed", "# Seatbelt")
	planOfTask(t, db, "LEG-1", "other", "proposed", "# Other")
	svc := NewPlanService(db, nil, nil)

	got, err := svc.TaskPlans(context.Background(), "LEG-160")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].PlanID != "pl-LEG-160-macos-seatbelt-sandbox" || got[1].PlanID != "pl-LEG-160-first" {
		t.Fatalf("plans = %+v", got)
	}
	if got[0].Status != "proposed" || got[0].Revision != 1 || got[0].Name != "macos-seatbelt-sandbox" || got[0].TaskID != "LEG-160" {
		t.Fatalf("latest = %+v", got[0])
	}
	none, err := svc.TaskPlans(context.Background(), "LEG-1")
	if err != nil || none == nil || len(none) != 1 {
		t.Fatalf("a task with one plan = %v, %v", none, err)
	}
	if _, err := svc.TaskPlans(context.Background(), "LEG-404"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("a task that does not exist = %v", err)
	}
}

// Reading a task's plan without naming it finds its latest plan; a name
// still finds exactly that plan.
func TestResolveForReadFindsTheTasksLatestPlanWithoutAName(t *testing.T) {
	db := planReadStore(t)
	planOfTask(t, db, "LEG-160", "first", "approved", "# First")
	planOfTask(t, db, "LEG-160", "macos-seatbelt-sandbox", "proposed", "# Seatbelt")
	svc := NewPlanService(db, nil, nil)
	ctx := context.Background()

	plan, err := svc.ResolveForRead(ctx, "LEG-160", "")
	if err != nil || plan.ID != "pl-LEG-160-macos-seatbelt-sandbox" {
		t.Fatalf("latest = %+v, %v", plan, err)
	}
	plan, err = svc.ResolveForRead(ctx, "LEG-160", "first")
	if err != nil || plan.ID != "pl-LEG-160-first" {
		t.Fatalf("named = %+v, %v", plan, err)
	}
	if _, err := svc.ResolveForRead(ctx, "LEG-160", "missing"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("a missing name = %v", err)
	}
	if _, err := svc.ResolveForRead(ctx, "LEG-1", ""); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("a task with no plans = %v", err)
	}
}

// A plan submitted without a name is the one found without a name, even
// when a named plan is newer: that is what the agent of the task means.
func TestResolveForReadPrefersTheUnnamedPlan(t *testing.T) {
	db := planReadStore(t)
	planOfTask(t, db, "LEG-160", "", "proposed", "# Default")
	planOfTask(t, db, "LEG-160", "later", "proposed", "# Later")
	svc := NewPlanService(db, nil, nil)
	plan, err := svc.ResolveForRead(context.Background(), "LEG-160", "")
	if err != nil || plan.Name != "" {
		t.Fatalf("plan = %+v, %v", plan, err)
	}
}
