package service

import (
	"context"
	"fmt"
	"testing"

	"github.com/cpave3/legato/internal/engine/store"
)

func TestLearningListAndSearchApplyScopedFilters(t *testing.T) {
	db := newServiceTestStore(t)
	svc := NewLearningService(db, nil)
	ctx := context.Background()

	user := recordQueryLearning(t, svc, "user", "", "validated", "Prefer focused diffs", "1")
	widget := recordQueryLearning(t, svc, "project", "repo:github.com/acme/widget", "validated", "Widget output uses a quartz cache", "2")
	rejected := recordQueryLearning(t, svc, "project", "repo:github.com/acme/widget", "rejected", "Rejected quartz workaround", "3")
	recordQueryLearning(t, svc, "project", "repo:github.com/acme/other", "validated", "Other quartz cache", "4")

	listed, err := svc.List(ctx, LearningFilter{Scope: "project", ProjectKey: "repo:github.com/acme/widget", Status: "validated", Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	assertLearningIDs(t, listed, widget.ID)

	found, err := svc.Search(ctx, "quartz", LearningFilter{ProjectKey: "repo:github.com/acme/widget", Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	assertLearningIDSet(t, found, rejected.ID, widget.ID)

	found, err = svc.Search(ctx, "quartz", LearningFilter{ProjectKey: "repo:github.com/acme/widget", Status: "validated", Limit: 1})
	if err != nil {
		t.Fatal(err)
	}
	assertLearningIDs(t, found, widget.ID)

	users, err := svc.List(ctx, LearningFilter{Scope: "user", Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	assertLearningIDs(t, users, user.ID)
}

func TestLearningContextIncludesOnlyActiveUserAndMatchingProject(t *testing.T) {
	db := newServiceTestStore(t)
	svc := NewLearningService(db, nil)

	user := recordQueryLearning(t, svc, "user", "", "validated", "User preference", "u")
	proposed := recordQueryLearning(t, svc, "project", "repo:github.com/acme/widget", "action_proposed", "Proposed widget fact", "p")
	incorporated := recordQueryLearning(t, svc, "project", "repo:github.com/acme/widget", "incorporated", "Incorporated widget fact", "i")
	for i, status := range []string{"candidate", "rejected", "stale"} {
		recordQueryLearning(t, svc, "project", "repo:github.com/acme/widget", status, status+" widget fact", fmt.Sprint(i))
	}
	recordQueryLearning(t, svc, "project", "repo:github.com/acme/other", "validated", "Unrelated project fact", "o")

	got, err := svc.Context(context.Background(), "repo:github.com/acme/widget", 10)
	if err != nil {
		t.Fatal(err)
	}
	// User context is consistently first; ties are resolved by ID.
	if len(got) != 3 || got[0].ID != user.ID {
		t.Fatalf("context = %+v", got)
	}
	ids := map[string]bool{}
	for _, item := range got {
		ids[item.ID] = true
	}
	if !ids[proposed.ID] || !ids[incorporated.ID] {
		t.Fatalf("context IDs = %v", ids)
	}

	limited, err := svc.Context(context.Background(), "repo:github.com/acme/widget", 1)
	if err != nil {
		t.Fatal(err)
	}
	assertLearningIDs(t, limited, user.ID)
}

func TestLearningContextForTaskUsesRepositoryIdentity(t *testing.T) {
	db := newServiceTestStore(t)
	svc := NewLearningService(db, nil)
	ctx := context.Background()
	createQueryTask(t, db, "task-query")
	if err := db.SetCodeContext(ctx, "task-query", &store.CodeContext{Repo: "https://github.com/Acme/Widget.git"}); err != nil {
		t.Fatal(err)
	}
	user := recordQueryLearning(t, svc, "user", "", "validated", "Global preference", "g")
	project := recordQueryLearning(t, svc, "project", "repo:github.com/acme/widget", "validated", "Widget fact", "w")
	recordQueryLearning(t, svc, "project", "repo:github.com/acme/other", "validated", "Other fact", "o")

	got, err := svc.ContextForTask(ctx, "task-query", 10)
	if err != nil {
		t.Fatal(err)
	}
	assertLearningIDs(t, got, user.ID, project.ID)
}

func TestLearningContextForTaskFallsBackWithoutRemote(t *testing.T) {
	db := newServiceTestStore(t)
	ctx := context.Background()
	repo := initIdentityTestRepository(t)
	createQueryTask(t, db, "task-local-repo")
	if err := db.SetCodeContext(ctx, "task-local-repo", &store.CodeContext{WorkingDir: repo}); err != nil {
		t.Fatal(err)
	}
	resolver := NewProjectIdentityResolver(CheckoutInspectFunc(func(string) (*store.CodeContext, error) {
		return nil, fmt.Errorf("no remote")
	}))
	svc := NewLearningService(db, nil, resolver)
	identity, err := resolver.Resolve(nil, repo)
	if err != nil {
		t.Fatal(err)
	}
	project := recordQueryLearning(t, svc, "project", identity.Key, "validated", "Local repository fact", "local")
	recordQueryLearning(t, svc, "project", "repo:github.com/acme/other", "validated", "Other fact", "other-local")

	got, err := svc.ContextForTask(ctx, "task-local-repo", 10)
	if err != nil {
		t.Fatal(err)
	}
	assertLearningIDs(t, got, project.ID)
}

func recordQueryLearning(t *testing.T, svc *LearningService, scope, project, status, claim, ref string) store.Learning {
	t.Helper()
	view, err := svc.Record(context.Background(), LearningRecordInput{Scope: scope, ProjectKey: project, Kind: "technical_fact", Claim: claim, SourceKind: "explicit_user_entry", SourceRef: "query:" + ref + ":" + status, Author: "user"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.store.DB().Exec(`UPDATE learnings SET status = ? WHERE id = ?`, status, view.Learning.ID); err != nil {
		t.Fatal(err)
	}
	view.Learning.Status = status
	return view.Learning
}

func assertLearningIDs(t *testing.T, got []store.Learning, want ...string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("got IDs %v, want %v", learningIDs(got), want)
	}
	for i := range want {
		if got[i].ID != want[i] {
			t.Fatalf("got IDs %v, want %v", learningIDs(got), want)
		}
	}
}

func assertLearningIDSet(t *testing.T, got []store.Learning, want ...string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("got IDs %v, want set %v", learningIDs(got), want)
	}
	set := make(map[string]bool, len(got))
	for _, item := range got {
		set[item.ID] = true
	}
	for _, id := range want {
		if !set[id] {
			t.Fatalf("got IDs %v, want set %v", learningIDs(got), want)
		}
	}
}

func learningIDs(items []store.Learning) []string {
	ids := make([]string, len(items))
	for i := range items {
		ids[i] = items[i].ID
	}
	return ids
}

func createQueryTask(t *testing.T, db *store.Store, id string) {
	t.Helper()
	if err := db.CreateTask(context.Background(), store.Task{ID: id, Title: id, Status: "todo", Priority: "medium"}); err != nil {
		t.Fatal(err)
	}
}
