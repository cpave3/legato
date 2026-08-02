package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
)

func TestCheckIncorporationDriftMarksMissingTargetAndLearningStale(t *testing.T) {
	db := newServiceTestStore(t)
	svc := NewLearningService(db, nil)
	root := t.TempDir()
	learningID, incID := seedVerifiedDriftIncorporation(t, svc, "docs/missing.md", "")

	results, err := svc.CheckIncorporationDrift(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0].Outcome != "stale" || results[0].Reason != "target missing" {
		t.Fatalf("results = %+v", results)
	}
	inc, err := db.GetLearningIncorporation(context.Background(), incID)
	if err != nil {
		t.Fatal(err)
	}
	view, err := svc.Show(context.Background(), learningID)
	if err != nil {
		t.Fatal(err)
	}
	if inc.Status != "stale" || view.Learning.Status != "stale" {
		t.Fatalf("incorporation=%q learning=%q", inc.Status, view.Learning.Status)
	}
	if inc.VerifiedAt == nil || inc.ReferencesJSON != `["docs/missing.md:1"]` || inc.VerificationResult != "confirmed" {
		t.Fatalf("verification history was not preserved: %+v", inc)
	}
}

func TestCheckIncorporationDriftChecksOptionalDigest(t *testing.T) {
	db := newServiceTestStore(t)
	svc := NewLearningService(db, nil)
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "tracked.md"), []byte("changed"), 0600); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256([]byte("original"))
	_, incID := seedVerifiedDriftIncorporation(t, svc, "tracked.md", "sha256:"+hex.EncodeToString(sum[:]))

	results, err := svc.CheckIncorporationDrift(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0].Outcome != "stale" || results[0].Reason != "target digest mismatch" {
		t.Fatalf("results = %+v", results)
	}
	inc, _ := db.GetLearningIncorporation(context.Background(), incID)
	if inc.TargetDigest == "" || inc.Status != "stale" {
		t.Fatalf("incorporation = %+v", inc)
	}
}

func TestCheckIncorporationDriftLeavesMatchingAndSemanticTargetsUnchanged(t *testing.T) {
	db := newServiceTestStore(t)
	svc := NewLearningService(db, nil)
	root := t.TempDir()
	content := []byte("same")
	if err := os.WriteFile(filepath.Join(root, "tracked.md"), content, 0600); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(content)
	_, matchingID := seedVerifiedDriftIncorporation(t, svc, "tracked.md", hex.EncodeToString(sum[:]))
	_, semanticID := seedVerifiedDriftIncorporation(t, svc, "architecture://decision/retries", "")

	results, err := svc.CheckIncorporationDrift(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 2 {
		t.Fatalf("results = %+v", results)
	}
	outcomes := map[string]string{}
	for _, result := range results {
		outcomes[result.IncorporationID] = result.Outcome
	}
	if outcomes[matchingID] != "unchanged" || outcomes[semanticID] != "unverifiable" {
		t.Fatalf("results = %+v", results)
	}
	for _, id := range []string{matchingID, semanticID} {
		inc, _ := db.GetLearningIncorporation(context.Background(), id)
		if inc.Status != "verified" || inc.Revision != 3 {
			t.Fatalf("incorporation changed: %+v", inc)
		}
	}
}

func TestCheckIncorporationDriftRejectsTargetsOutsideRoot(t *testing.T) {
	db := newServiceTestStore(t)
	svc := NewLearningService(db, nil)
	root := t.TempDir()
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(root, "escape")); err != nil {
		t.Fatal(err)
	}
	_, incID := seedVerifiedDriftIncorporation(t, svc, "escape/file.md", "")

	results, err := svc.CheckIncorporationDrift(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0].Outcome != "unverifiable" {
		t.Fatalf("results = %+v", results)
	}
	inc, _ := db.GetLearningIncorporation(context.Background(), incID)
	if inc.Status != "verified" {
		t.Fatalf("unsafe target changed: %+v", inc)
	}
}

func seedVerifiedDriftIncorporation(t *testing.T, svc *LearningService, target, digest string) (string, string) {
	t.Helper()
	ctx := context.Background()
	view, err := svc.Record(ctx, LearningRecordInput{Scope: "user", Kind: "technical_fact", Claim: "claim " + target, SourceKind: "test", SourceRef: target, Author: "user"})
	if err != nil {
		t.Fatal(err)
	}
	view, err = svc.Accept(ctx, view.Learning.ID, view.Learning.Revision, "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	inc, err := svc.ProposeIncorporation(ctx, view.Learning.ID, view.Learning.Revision, "documentation", target, "document")
	if err != nil {
		t.Fatal(err)
	}
	inc, err = svc.SubmitIncorporation(ctx, inc.ID, inc.Revision, []string{target + ":1"}, "source_comparison", "submitted")
	if err != nil {
		t.Fatal(err)
	}
	inc, err = svc.VerifyIncorporation(ctx, inc.ID, inc.Revision, true, "confirmed")
	if err != nil {
		t.Fatal(err)
	}
	if digest != "" {
		if _, err := svc.store.DB().ExecContext(ctx, `UPDATE learning_incorporations SET target_digest = ? WHERE id = ?`, digest, inc.ID); err != nil {
			t.Fatal(err)
		}
	}
	return view.Learning.ID, inc.ID
}
