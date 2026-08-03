package service

import (
	"context"
	"fmt"
	"testing"

	"github.com/cpave3/legato/internal/engine/events"
)

func TestLearningServiceRecordCreatesCandidateWithEvidence(t *testing.T) {
	db := newServiceTestStore(t)
	bus := events.New()
	svc := NewLearningService(db, bus)
	changes := bus.Subscribe(events.EventLearningChanged)

	view, err := svc.Record(context.Background(), LearningRecordInput{
		Scope:      "user",
		Kind:       "working_preference",
		Claim:      "Finish migrations across every call site.",
		TaskID:     "task-1",
		SourceKind: "explicit_user_entry",
		SourceRef:  "conversation:1",
		Author:     "user",
	})
	if err != nil {
		t.Fatal(err)
	}
	if view.Learning.Status != "candidate" || view.Learning.Scope != "user" || view.Learning.Revision != 1 {
		t.Fatalf("learning = %+v", view.Learning)
	}
	if len(view.Evidence) != 1 || view.Evidence[0].Author != "user" || view.Evidence[0].SourceRef != "conversation:1" {
		t.Fatalf("evidence = %+v", view.Evidence)
	}

	stored, err := svc.Show(context.Background(), view.Learning.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Learning.Claim != view.Learning.Claim || len(stored.Evidence) != 1 {
		t.Fatalf("stored = %+v", stored)
	}

	select {
	case event := <-changes:
		payload := event.Payload.(events.LearningChangedPayload)
		if payload.LearningID != view.Learning.ID || payload.Kind != "recorded" {
			t.Fatalf("event = %+v", payload)
		}
	default:
		t.Fatal("expected learning changed event")
	}
}

func TestLearningServiceRecordsCategorizesTagsAndEditsValidatedKnowledge(t *testing.T) {
	db := newServiceTestStore(t)
	svc := NewLearningService(db, nil)
	view, err := svc.Record(context.Background(), LearningRecordInput{
		Scope: "project", ProjectKey: "repo:github.com/acme/widget", Kind: "technical_fact",
		Category: " Workflow ", Tags: []string{"Agents", " callbacks ", "agents"},
		Claim: "Callbacks carry attempt identity.", Rationale: "Prevents stale completion.",
		SourceKind: "explicit_user_entry", SourceRef: "feedback:kb", Author: "user",
	})
	if err != nil {
		t.Fatal(err)
	}
	if view.Learning.Category != "workflow" || fmt.Sprint(view.Learning.Tags) != "[agents callbacks]" {
		t.Fatalf("classification = category %q tags %v", view.Learning.Category, view.Learning.Tags)
	}
	accepted, err := svc.Accept(context.Background(), view.Learning.ID, view.Learning.Revision, "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	edited, err := svc.Edit(context.Background(), accepted.Learning.ID, accepted.Learning.Revision, LearningEditInput{
		Claim: "Callbacks must carry the active attempt identity.", Rationale: "Rejects stale completion.",
		Scope: "project", ProjectKey: "repo:github.com/acme/widget", Category: "workflow-runtime", Tags: []string{"callbacks", "retries"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if edited.Learning.Status != "validated" || edited.Learning.Claim != "Callbacks must carry the active attempt identity." || edited.Learning.Rationale != "Rejects stale completion." || edited.Learning.Category != "workflow-runtime" || fmt.Sprint(edited.Learning.Tags) != "[callbacks retries]" {
		t.Fatalf("edited = %+v", edited.Learning)
	}
}

func TestLearningServiceAcceptRequiresCurrentRevision(t *testing.T) {
	db := newServiceTestStore(t)
	svc := NewLearningService(db, nil)
	view, err := svc.Record(context.Background(), LearningRecordInput{Scope: "user", Kind: "working_preference", Claim: "Prefer focused diffs.", SourceKind: "explicit_user_entry", SourceRef: "feedback:1", Author: "user"})
	if err != nil {
		t.Fatal(err)
	}
	accepted, err := svc.Accept(context.Background(), view.Learning.ID, 1, "Prefer small, focused diffs.", "user", "")
	if err != nil {
		t.Fatal(err)
	}
	if accepted.Learning.Status != "validated" || accepted.Learning.Revision != 2 || accepted.Learning.Claim != "Prefer small, focused diffs." {
		t.Fatalf("accepted = %+v", accepted.Learning)
	}
	if _, err := svc.Reject(context.Background(), view.Learning.ID, 1); err != ErrLearningConflict {
		t.Fatalf("stale reject error = %v, want ErrLearningConflict", err)
	}
	if _, err := svc.Accept(context.Background(), accepted.Learning.ID, accepted.Learning.Revision, "", "", ""); err != ErrLearningConflict {
		t.Fatalf("repeat accept error = %v, want ErrLearningConflict", err)
	}
}

func TestLearningServiceRequiresVerifiedIncorporation(t *testing.T) {
	db := newServiceTestStore(t)
	svc := NewLearningService(db, nil)
	view, err := svc.Record(context.Background(), LearningRecordInput{Scope: "project", ProjectKey: "repo:github.com/acme/widget", Kind: "technical_fact", Claim: "Outputs are limited to 10 MiB.", SourceKind: "explicit_user_entry", SourceRef: "task:T-1", Author: "user"})
	if err != nil {
		t.Fatal(err)
	}
	view, err = svc.Accept(context.Background(), view.Learning.ID, 1, "", "project", "repo:github.com/acme/widget")
	if err != nil {
		t.Fatal(err)
	}
	inc, err := svc.ProposeIncorporation(context.Background(), view.Learning.ID, view.Learning.Revision, "documentation", "docs/workflows.md", "Document the output limit")
	if err != nil {
		t.Fatal(err)
	}
	if inc.Status != "proposed" {
		t.Fatalf("incorporation = %+v", inc)
	}
	pending, err := svc.SubmitIncorporation(context.Background(), inc.ID, inc.Revision, []string{"docs/workflows.md:208"}, "source_comparison", "matches implementation")
	if err != nil {
		t.Fatal(err)
	}
	if pending.Status != "verification_pending" {
		t.Fatalf("pending = %+v", pending)
	}
	before, _ := svc.Show(context.Background(), view.Learning.ID)
	if before.Learning.Status == "incorporated" {
		t.Fatal("agent submission incorporated learning before verification")
	}
	verified, err := svc.VerifyIncorporation(context.Background(), pending.ID, pending.Revision, true, "confirmed")
	if err != nil {
		t.Fatal(err)
	}
	if verified.Status != "verified" {
		t.Fatalf("verified = %+v", verified)
	}
	after, _ := svc.Show(context.Background(), view.Learning.ID)
	if after.Learning.Status != "incorporated" {
		t.Fatalf("learning status = %q", after.Learning.Status)
	}
}

func TestLearningServiceRecordIsIdempotentForSameSourceAndContent(t *testing.T) {
	db := newServiceTestStore(t)
	svc := NewLearningService(db, nil)
	input := LearningRecordInput{Scope: "project", ProjectKey: "repo:github.com/acme/widget", Kind: "technical_fact", Claim: "Outputs are limited to 10 MiB.", SourceKind: "explicit_user_entry", SourceRef: "task:T-1:note:2", Author: "user"}

	first, err := svc.Record(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	second, err := svc.Record(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	if first.Learning.ID != second.Learning.ID || len(second.Evidence) != 1 {
		t.Fatalf("first=%+v second=%+v", first, second)
	}
}
