package store

import (
	"context"
	"testing"
)

func TestMarkLearningIncorporationStaleUpdatesBothRecordsAndPreservesHistory(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	if _, err := s.DB().ExecContext(ctx, `INSERT INTO learnings
		(id, scope, kind, claim, status, revision) VALUES ('learn-drift', 'user', 'technical_fact', 'fact', 'incorporated', 4)`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.DB().ExecContext(ctx, `INSERT INTO learning_incorporations
		(id, learning_id, destination, target, status, revision, references_json, verification_result, target_digest, verified_at)
		VALUES ('inc-drift', 'learn-drift', 'documentation', 'docs/fact.md', 'verified', 3, '["docs/fact.md:1"]', 'confirmed', 'sha256:old', datetime('now'))`); err != nil {
		t.Fatal(err)
	}

	verified, err := s.ListVerifiedLearningIncorporations(ctx)
	if err != nil || len(verified) != 1 || verified[0].ID != "inc-drift" {
		t.Fatalf("verified=%+v err=%v", verified, err)
	}
	if err := s.MarkLearningIncorporationStale(ctx, "inc-drift", 3); err != nil {
		t.Fatal(err)
	}
	inc, err := s.GetLearningIncorporation(ctx, "inc-drift")
	if err != nil {
		t.Fatal(err)
	}
	view, err := s.GetLearningView(ctx, "learn-drift")
	if err != nil {
		t.Fatal(err)
	}
	if inc.Status != "stale" || inc.Revision != 4 || view.Learning.Status != "stale" || view.Learning.Revision != 5 {
		t.Fatalf("inc=%+v learning=%+v", inc, view.Learning)
	}
	if inc.ReferencesJSON != `["docs/fact.md:1"]` || inc.VerificationResult != "confirmed" || inc.TargetDigest != "sha256:old" || inc.VerifiedAt == nil {
		t.Fatalf("history was changed: %+v", inc)
	}
	if err := s.MarkLearningIncorporationStale(ctx, "inc-drift", 3); err != ErrConflict {
		t.Fatalf("second mark error = %v, want ErrConflict", err)
	}
}
