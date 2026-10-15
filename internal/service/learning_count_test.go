package service

import (
	"context"
	"testing"

	"github.com/cpave3/legato/internal/engine/events"
)

func TestCandidateCountCountsLearningsWaitingForADecision(t *testing.T) {
	ctx := context.Background()
	learning := NewLearningService(newServiceTestStore(t), events.New())
	record := func(claim string) *LearningView {
		view, err := learning.Record(ctx, LearningRecordInput{
			Scope: "user", Kind: "preference", Claim: claim, SourceKind: "user_feedback", SourceRef: "test:" + claim, Author: "user",
		})
		if err != nil {
			t.Fatal(err)
		}
		return view
	}
	record("Prefer small diffs.")
	accepted := record("Name tests after behaviour.")
	if _, err := learning.Accept(ctx, accepted.Learning.ID, accepted.Learning.Revision, "", "", ""); err != nil {
		t.Fatal(err)
	}

	count, err := learning.CandidateCount(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("candidates = %d, want 1", count)
	}
}
