package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"testing"

	"github.com/cpave3/legato/internal/engine/store"
)

func TestCompileLearningPromptRequiresExplicitActivationAndScope(t *testing.T) {
	db := newServiceTestStore(t)
	svc := NewLearningService(db, nil)
	ctx := context.Background()

	insertPromptLearning(t, db, "learn-user", "user", "", "validated", "Keep diffs focused.", true, 3)
	insertPromptLearning(t, db, "learn-project", "project", "repo:widget", "validated", "Run widget contract tests.", true, 2)
	insertPromptLearning(t, db, "learn-disabled", "user", "", "validated", "Do not inject this yet.", false, 1)
	insertPromptLearning(t, db, "learn-doc", "project", "repo:widget", "incorporated", "This belongs only in docs.", false, 1)
	insertPromptLearning(t, db, "learn-stale", "project", "repo:widget", "stale", "Stale instruction.", true, 4)
	insertPromptLearning(t, db, "learn-other", "project", "repo:other", "validated", "Other project instruction.", true, 1)

	got, err := svc.CompileLearningPrompt(ctx, LearningPromptCompileInput{TaskID: "task-1", Adapter: "codex", Role: "worker",
		ProjectKey: "repo:widget", MaxItems: 10, MaxBytes: 1024})
	if err != nil {
		t.Fatal(err)
	}
	want := "## Learned context\n- Keep diffs focused.\n- Run widget contract tests."
	if got.Rendered != want {
		t.Fatalf("rendered = %q, want %q", got.Rendered, want)
	}
	if len(got.Included) != 2 || got.Included[0].ID != "learn-user" || got.Included[0].Revision != 3 || got.Included[1].ID != "learn-project" {
		t.Fatalf("included = %+v", got.Included)
	}
	assertPromptExclusions(t, got.Excluded, map[string]string{
		"learn-disabled": "disabled", "learn-doc": "disabled", "learn-stale": "inactive",
	})
	manifest, err := db.GetLearningPromptManifest(ctx, got.Manifest.ID)
	if err != nil {
		t.Fatal(err)
	}
	if manifest.ProjectKey != "repo:widget" || manifest.Adapter != "codex" || manifest.TaskID != "task-1" {
		t.Fatalf("manifest = %+v", manifest)
	}
	sum := sha256.Sum256([]byte(got.Rendered))
	if manifest.RenderedDigest != "sha256:"+hex.EncodeToString(sum[:]) {
		t.Fatalf("digest = %s", manifest.RenderedDigest)
	}
	var included []LearningPromptVersion
	if err := json.Unmarshal([]byte(manifest.IncludedJSON), &included); err != nil {
		t.Fatal(err)
	}
	if len(included) != 2 || included[0].Revision != 3 {
		t.Fatalf("persisted included = %+v", included)
	}
}

func TestCompileLearningPromptDeterministicItemAndByteBudgets(t *testing.T) {
	db := newServiceTestStore(t)
	svc := NewLearningService(db, nil)
	ctx := context.Background()
	insertPromptLearning(t, db, "learn-b", "user", "", "validated", "second", true, 1)
	insertPromptLearning(t, db, "learn-a", "user", "", "validated", "first", true, 1)
	insertPromptLearning(t, db, "learn-c", "project", "repo:widget", "validated", "third", true, 1)

	one, err := svc.CompileLearningPrompt(ctx, LearningPromptCompileInput{TaskID: "t", Adapter: "chimera", ProjectKey: "repo:widget", MaxItems: 2, MaxBytes: 1024})
	if err != nil {
		t.Fatal(err)
	}
	if one.Rendered != "## Learned context\n- first\n- second" {
		t.Fatalf("ordering/render = %q", one.Rendered)
	}
	assertPromptExclusions(t, one.Excluded, map[string]string{"learn-c": "item_limit"})

	// The header and first item fit exactly; the next whole item does not.
	limit := len("## Learned context\n- first")
	two, err := svc.CompileLearningPrompt(ctx, LearningPromptCompileInput{TaskID: "t", Adapter: "chimera", ProjectKey: "repo:widget", MaxItems: 3, MaxBytes: limit})
	if err != nil {
		t.Fatal(err)
	}
	if two.Rendered != "## Learned context\n- first" || len([]byte(two.Rendered)) > limit {
		t.Fatalf("byte-bounded render = %q", two.Rendered)
	}
	assertPromptExclusions(t, two.Excluded, map[string]string{"learn-b": "byte_limit", "learn-c": "byte_limit"})
}

func TestCompileLearningPromptPersistsEmptyResolutionAndListsBySession(t *testing.T) {
	db := newServiceTestStore(t)
	svc := NewLearningService(db, nil)
	session := 42
	got, err := svc.CompileLearningPrompt(context.Background(), LearningPromptCompileInput{TaskID: "t", AgentSessionID: &session,
		Adapter: "claude-code", MaxItems: 2, MaxBytes: 64})
	if err != nil {
		t.Fatal(err)
	}
	if got.Rendered != "" || got.Manifest.RenderedDigest != "sha256:e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855" {
		t.Fatalf("empty compile = %+v", got)
	}
	listed, err := db.ListLearningPromptManifestsBySession(context.Background(), session)
	if err != nil || len(listed) != 1 || listed[0].ID != got.Manifest.ID {
		t.Fatalf("listed = %+v, err = %v", listed, err)
	}
}

func insertPromptLearning(t *testing.T, db *store.Store, id, scope, project, status, claim string, promptEnabled bool, revision int) {
	t.Helper()
	_, err := db.DB().Exec(`INSERT INTO learnings (id, scope, project_key, kind, claim, status, prompt_enabled, revision) VALUES (?, ?, ?, 'preference', ?, ?, ?, ?)`,
		id, scope, project, claim, status, promptEnabled, revision)
	if err != nil {
		t.Fatal(err)
	}
}

func assertPromptExclusions(t *testing.T, got []LearningPromptExclusion, want map[string]string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("exclusions = %+v, want %v", got, want)
	}
	for _, item := range got {
		if want[item.ID] != item.Reason {
			t.Fatalf("exclusions = %+v, want %v", got, want)
		}
	}
}
