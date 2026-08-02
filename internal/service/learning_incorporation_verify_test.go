package service

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cpave3/legato/internal/engine/store"
)

func TestVerifyIncorporationTargetFileContentAndDigest(t *testing.T) {
	for _, tc := range []struct {
		name, kind, evidence string
	}{
		{"exact content", "exact_content", "approved contents"},
		{"digest", "digest", sha256Digest([]byte("approved contents"))},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db := newServiceTestStore(t)
			svc := NewLearningService(db, nil)
			root := t.TempDir()
			if err := os.WriteFile(filepath.Join(root, "guide.md"), []byte("approved contents"), 0600); err != nil {
				t.Fatal(err)
			}
			inc := seedPendingTargetIncorporation(t, svc, "documentation", "guide.md", tc.kind, tc.evidence)
			verified, err := svc.VerifyIncorporationTarget(context.Background(), inc.ID, inc.Revision, root, false)
			if err != nil {
				t.Fatal(err)
			}
			if verified.Status != "verified" {
				t.Fatalf("incorporation = %+v", verified)
			}
			if verified.TargetDigest != sha256Digest([]byte("approved contents")) {
				t.Fatalf("target digest = %q", verified.TargetDigest)
			}
			if verified.VerificationKind != tc.kind || verified.VerificationResult != tc.evidence {
				t.Fatalf("submitted verification evidence was replaced: %+v", verified)
			}
		})
	}
}

func TestVerifyIncorporationTargetDigestDrivesLaterDrift(t *testing.T) {
	db := newServiceTestStore(t)
	svc := NewLearningService(db, nil)
	root := t.TempDir()
	path := filepath.Join(root, "guide.md")
	if err := os.WriteFile(path, []byte("approved contents"), 0600); err != nil {
		t.Fatal(err)
	}
	inc := seedPendingTargetIncorporation(t, svc, "documentation", "guide.md", "exact_content", "approved contents")
	if inc.Status != "verification_pending" {
		t.Fatalf("submission must remain pending: %+v", inc)
	}
	verified, err := svc.VerifyIncorporationTarget(context.Background(), inc.ID, inc.Revision, root, false)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("drifted contents"), 0600); err != nil {
		t.Fatal(err)
	}
	results, err := svc.CheckIncorporationDrift(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0].Outcome != "stale" || results[0].Reason != "target digest mismatch" {
		t.Fatalf("digest %q did not detect drift: %+v", verified.TargetDigest, results)
	}
}

func TestVerifyIncorporationTargetRejectsUnsafePathWithoutMutation(t *testing.T) {
	db := newServiceTestStore(t)
	svc := NewLearningService(db, nil)
	inc := seedPendingTargetIncorporation(t, svc, "existing_source", "../outside", "source", "exists")
	_, err := svc.VerifyIncorporationTarget(context.Background(), inc.ID, inc.Revision, t.TempDir(), false)
	if err == nil || !strings.Contains(err.Error(), "unsafe incorporation target") {
		t.Fatalf("error = %v", err)
	}
	stored, _ := db.GetLearningIncorporation(context.Background(), inc.ID)
	if stored.Status != "verification_pending" || stored.Revision != inc.Revision {
		t.Fatalf("unsafe verification mutated record: %+v", stored)
	}
}

func TestVerifyIncorporationTargetNamedSkillAndExplicitCheck(t *testing.T) {
	db := newServiceTestStore(t)
	svc := NewLearningService(db, nil)
	root := t.TempDir()
	skillDir := filepath.Join(root, ".claude", "skills", "focused")
	if err := os.MkdirAll(skillDir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("skill"), 0600); err != nil {
		t.Fatal(err)
	}
	skill := seedPendingTargetIncorporation(t, svc, "named_skill", "focused", "source", "exists")
	if got, err := svc.VerifyIncorporationTarget(context.Background(), skill.ID, skill.Revision, root, false); err != nil || got.Status != "verified" {
		t.Fatalf("skill verification = %+v, %v", got, err)
	}

	check := seedPendingTargetIncorporation(t, svc, "test_or_lint", "go test ./...", "manual", "passed")
	if got, err := svc.VerifyIncorporationTarget(context.Background(), check.ID, check.Revision, root, true); err != nil || got.Status != "verified" {
		t.Fatalf("check verification = %+v, %v", got, err)
	}
}

func TestVerifyNamedResourceDigestDrivesDriftAtResolvedTarget(t *testing.T) {
	db := newServiceTestStore(t)
	svc := NewLearningService(db, nil)
	root := t.TempDir()
	skillDir := filepath.Join(root, ".claude", "skills", "focused")
	if err := os.MkdirAll(skillDir, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(skillDir, "SKILL.md")
	if err := os.WriteFile(path, []byte("original skill"), 0600); err != nil {
		t.Fatal(err)
	}
	inc := seedPendingTargetIncorporation(t, svc, "named_skill", "focused", "source", "exists")
	verified, err := svc.VerifyIncorporationTarget(context.Background(), inc.ID, inc.Revision, root, false)
	if err != nil {
		t.Fatal(err)
	}
	if verified.TargetDigest != sha256Digest([]byte("original skill")) {
		t.Fatalf("target digest = %q", verified.TargetDigest)
	}
	if err := os.WriteFile(path, []byte("changed skill"), 0600); err != nil {
		t.Fatal(err)
	}
	results, err := svc.CheckIncorporationDrift(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0].Outcome != "stale" || results[0].Reason != "target digest mismatch" {
		t.Fatalf("results = %+v", results)
	}
}

func TestVerifyIncorporationTargetPromptProfileUsesApprovedClaim(t *testing.T) {
	db := newServiceTestStore(t)
	svc := NewLearningService(db, nil)
	inc := seedPendingTargetIncorporation(t, svc, "prompt_profile", "claim prompt_profile", "wording", "submitted")
	got, err := svc.VerifyIncorporationTarget(context.Background(), inc.ID, inc.Revision, t.TempDir(), false)
	if err != nil || got.Status != "verified" {
		t.Fatalf("prompt verification = %+v, %v", got, err)
	}
}

func seedPendingTargetIncorporation(t *testing.T, svc *LearningService, destination, target, kind, result string) *store.LearningIncorporation {
	t.Helper()
	ctx := context.Background()
	view, err := svc.Record(ctx, LearningRecordInput{Scope: "user", Kind: "technical_fact", Claim: "claim " + destination, SourceKind: "test", SourceRef: destination, Author: "user"})
	if err != nil {
		t.Fatal(err)
	}
	view, err = svc.Accept(ctx, view.Learning.ID, view.Learning.Revision, "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	inc, err := svc.ProposeIncorporation(ctx, view.Learning.ID, view.Learning.Revision, destination, target, "update")
	if err != nil {
		t.Fatal(err)
	}
	inc, err = svc.SubmitIncorporation(ctx, inc.ID, inc.Revision, []string{target}, kind, result)
	if err != nil {
		t.Fatal(err)
	}
	return inc
}
