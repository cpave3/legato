package service

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cpave3/legato/internal/engine/store"
)

// The worker's default reviewer skill, a managed one, is named as Claude
// Code knows it, as an override naming it is.
func TestTheReviewerBriefNamesAManagedDefaultSkillAsItsToolKnowsIt(t *testing.T) {
	ctx := context.Background()
	db, err := store.New(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err := db.CreateTask(ctx, store.Task{ID: "card-1", Title: "Fix it", Status: "Done"}); err != nil {
		t.Fatal(err)
	}
	advert := &ReviewPassesAdvert{Skills: []string{"my-review"}, ManagedSkills: []string{"my-review"}}
	svc := NewReviewPassService(db, ReviewPassOptions{Advert: func() *ReviewPassesAdvert { return advert }})
	caller := NewHandoffReviewCaller(svc, HandoffReviewCallerOptions{})
	brief, err := svc.brief(ctx, ReviewLoop{TaskID: "card-1", Repo: "acme/widget", AuthorTaskID: "card-1", caller: caller}, 1, nil,
		ReviewPassSettings{Enabled: true, Agent: "claude-code", Skill: "my-review", MaxPasses: 3})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(brief, "with the legato-skills:my-review skill") {
		t.Fatalf("the brief:\n%s", brief)
	}
}
