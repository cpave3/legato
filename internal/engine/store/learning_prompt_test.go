package store

import (
	"context"
	"path/filepath"
	"testing"
)

func TestLearningPromptCandidatesAndManifestPersistence(t *testing.T) {
	db, err := New(filepath.Join(t.TempDir(), "prompt.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	for _, item := range []Learning{
		{ID: "learn-user", Scope: "user", Kind: "preference", Claim: "user", Status: "validated", PromptEnabled: true, PromptDecided: true, Revision: 3},
		{ID: "learn-project", Scope: "project", ProjectKey: "repo:one", Kind: "fact", Claim: "project", Status: "validated", Revision: 2},
		{ID: "learn-other", Scope: "project", ProjectKey: "repo:two", Kind: "fact", Claim: "other", Status: "incorporated", Revision: 1},
	} {
		if _, err := db.DB().NamedExec(`INSERT INTO learnings
			(id, scope, project_key, kind, claim, status, prompt_enabled, prompt_decided, revision)
			VALUES (:id, :scope, :project_key, :kind, :claim, :status, :prompt_enabled, :prompt_decided, :revision)`, item); err != nil {
			t.Fatal(err)
		}
	}
	candidates, err := db.ListLearningPromptCandidates(ctx, "repo:one")
	if err != nil {
		t.Fatal(err)
	}
	if len(candidates) != 2 || candidates[0].ID != "learn-user" || !candidates[0].PromptEnabled ||
		candidates[1].ID != "learn-project" || candidates[1].PromptEnabled {
		t.Fatalf("candidates = %+v", candidates)
	}

	session := 7
	manifest := LearningPromptManifest{ID: "lpm-one", AgentSessionID: &session, TaskID: "task", Adapter: "codex", Role: "worker",
		ProjectKey: "repo:one", IncludedJSON: `[{"id":"learn-user","revision":3}]`,
		ExcludedJSON: `[{"id":"learn-project","reason":"non_prompt_incorporation"}]`, RenderedDigest: "sha256:abc"}
	if err := db.CreateLearningPromptManifest(ctx, manifest); err != nil {
		t.Fatal(err)
	}
	stored, err := db.GetLearningPromptManifest(ctx, manifest.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.IncludedJSON != manifest.IncludedJSON || stored.ExcludedJSON != manifest.ExcludedJSON || stored.RenderedDigest != manifest.RenderedDigest {
		t.Fatalf("stored manifest = %+v", stored)
	}
	listed, err := db.ListLearningPromptManifestsBySession(ctx, session)
	if err != nil || len(listed) != 1 || listed[0].ID != manifest.ID {
		t.Fatalf("listed = %+v, err = %v", listed, err)
	}
}
