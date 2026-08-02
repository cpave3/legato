package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/cpave3/legato/internal/engine/store"
)

func TestPrepareLearningPacketIsDeterministicBoundedAndRecordsExclusions(t *testing.T) {
	db := newServiceTestStore(t)
	ctx := context.Background()
	seedCurationEvidence(t, db, "lev-b", "TASK-1", "repo:one", "second evidence")
	seedCurationEvidence(t, db, "lev-a", "TASK-1", "repo:one", "first evidence")
	seedCurationLearning(t, db, "learn-project", "project", "repo:one", "Project fact")
	seedCurationLearning(t, db, "learn-user", "user", "", "User preference")
	seedCurationLearning(t, db, "learn-other", "project", "repo:other", "Must be excluded by scope")

	svc := NewLearningService(db, nil)
	input := PrepareLearningPacketInput{WorkflowRunID: "run-1", AttemptID: "attempt-1", TaskID: "TASK-1", ProjectKey: "repo:one", ProjectLabel: "one", MaxItems: 2, MaxBytes: 32 * 1024}
	first, err := svc.PrepareLearningPacket(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	second, err := svc.PrepareLearningPacket(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	if first.Generation.ID != second.Generation.ID || first.Generation.PacketJSON != second.Generation.PacketJSON || first.Packet.PacketDigest != second.Packet.PacketDigest {
		t.Fatalf("retry was not deterministic/idempotent: first=%+v second=%+v", first.Generation, second.Generation)
	}
	if len(first.Packet.Evidence)+len(first.Packet.Learnings) != 2 {
		t.Fatalf("included items = %+v", first.Packet)
	}
	if len(first.Packet.Exclusions) != 2 {
		t.Fatalf("exclusions = %+v", first.Packet.Exclusions)
	}
	for _, excluded := range first.Packet.Exclusions {
		if excluded.ID == "learn-other" {
			t.Fatalf("irrelevant learning leaked into exclusions: %+v", excluded)
		}
		if excluded.Reason != "item_limit" {
			t.Fatalf("reason = %q", excluded.Reason)
		}
	}
	var persisted LearningPacket
	if err := json.Unmarshal([]byte(first.Generation.PacketJSON), &persisted); err != nil {
		t.Fatal(err)
	}
	digest, encoded, err := learningPacketJSON(persisted)
	if err != nil {
		t.Fatal(err)
	}
	if digest != persisted.PacketDigest || string(encoded) != first.Generation.PacketJSON {
		t.Fatalf("packet digest/canonical JSON mismatch")
	}
}

func TestPrepareLearningPacketAutomaticallyResolvesTaskProject(t *testing.T) {
	db := newServiceTestStore(t)
	ctx := context.Background()
	createQueryTask(t, db, "TASK-AUTO")
	if err := db.SetCodeContext(ctx, "TASK-AUTO", &store.CodeContext{Repo: "git@github.com:Acme/Widget.git"}); err != nil {
		t.Fatal(err)
	}
	seedCurationLearning(t, db, "learn-widget", "project", "repo:github.com/acme/widget", "Widget fact")
	seedCurationLearning(t, db, "learn-other", "project", "repo:github.com/acme/other", "Other fact")

	packet, err := NewLearningService(db, nil).PrepareLearningPacket(ctx, PrepareLearningPacketInput{
		AttemptID: "automatic-project", TaskID: "TASK-AUTO", MaxItems: 10, MaxBytes: 32 * 1024,
	})
	if err != nil {
		t.Fatal(err)
	}
	if packet.Packet.Context.ProjectKey != "repo:github.com/acme/widget" || packet.Packet.Context.ProjectLabel != "github.com/acme/widget" {
		t.Fatalf("context = %+v", packet.Packet.Context)
	}
	if len(packet.Packet.Learnings) != 1 || packet.Packet.Learnings[0].ID != "learn-widget" {
		t.Fatalf("learnings = %+v", packet.Packet.Learnings)
	}
}

func TestSubmitCuratorOperationsRejectsProjectKeyUnrelatedToPacket(t *testing.T) {
	db := newServiceTestStore(t)
	ctx := context.Background()
	svc := NewLearningService(db, nil)
	seedCurationEvidence(t, db, "lev-project", "TASK", "repo:one", "evidence")
	packet, err := svc.PrepareLearningPacket(ctx, PrepareLearningPacketInput{
		AttemptID: "project-scope", TaskID: "TASK", ProjectKey: "repo:one", MaxItems: 10, MaxBytes: 32 * 1024,
	})
	if err != nil {
		t.Fatal(err)
	}
	doc := CuratorOperationsDocument{SchemaVersion: 1, PacketDigest: packet.Packet.PacketDigest, Operations: []CuratorOperation{{
		Operation: "create", NewLearningID: "learn-unrelated", EvidenceIDs: []string{"lev-project"},
		Claim: "Fact", Kind: "technical_fact", Scope: "project", ProjectKey: "repo:other",
	}}}
	if _, err := svc.SubmitCuratorOperations(ctx, "project-scope", doc); !errors.Is(err, ErrInvalidCuratorOperations) {
		t.Fatalf("error = %v", err)
	}
}

func TestPrepareLearningPacketUsesWholeRecordsAndByteExclusions(t *testing.T) {
	db := newServiceTestStore(t)
	ctx := context.Background()
	seedCurationEvidence(t, db, "lev-small", "TASK", "", "small")
	seedCurationEvidence(t, db, "lev-large", "TASK", "", string(make([]byte, 4096)))
	svc := NewLearningService(db, nil)
	result, err := svc.PrepareLearningPacket(ctx, PrepareLearningPacketInput{AttemptID: "bytes", TaskID: "TASK", MaxItems: 10, MaxBytes: 2500})
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range result.Packet.Evidence {
		if item.ID == "lev-large" {
			t.Fatal("oversized evidence was truncated/included")
		}
	}
	found := false
	for _, item := range result.Packet.Exclusions {
		if item.ID == "lev-large" && item.Reason == "byte_limit" {
			found = true
		}
	}
	if !found {
		t.Fatalf("exclusions = %+v", result.Packet.Exclusions)
	}
	if len(result.Generation.PacketJSON) > 2500 {
		t.Fatalf("packet bytes = %d", len(result.Generation.PacketJSON))
	}
}

func TestSubmitCuratorOperationsValidatesReferencesAndPersistsAtomically(t *testing.T) {
	db := newServiceTestStore(t)
	ctx := context.Background()
	svc := NewLearningService(db, nil)
	seedCurationEvidence(t, db, "lev-1", "TASK", "", "supported observation")
	seedCurationLearning(t, db, "learn-1", "user", "", "Existing")
	packet, err := svc.PrepareLearningPacket(ctx, PrepareLearningPacketInput{AttemptID: "curate-1", TaskID: "TASK", MaxItems: 10, MaxBytes: 32 * 1024})
	if err != nil {
		t.Fatal(err)
	}

	bad := CuratorOperationsDocument{SchemaVersion: 1, PacketDigest: packet.Packet.PacketDigest, Operations: []CuratorOperation{
		{Operation: "create", NewLearningID: "learn-new", EvidenceIDs: []string{"lev-1"}, Claim: "Candidate", Kind: "fact", Scope: "user"},
		{Operation: "reinforce", LearningID: "learn-1", EvidenceIDs: []string{"lev-unknown"}},
	}}
	if _, err := svc.SubmitCuratorOperations(ctx, "curate-1", bad); !errors.Is(err, ErrInvalidCuratorOperations) {
		t.Fatalf("error = %v", err)
	}
	if _, err := db.GetLearningView(ctx, "learn-new"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("partial candidate persisted: %v", err)
	}

	good := CuratorOperationsDocument{SchemaVersion: 1, PacketDigest: packet.Packet.PacketDigest, Operations: []CuratorOperation{
		{Operation: "create", NewLearningID: "learn-new", EvidenceIDs: []string{"lev-1"}, Claim: "Candidate", Kind: "technical_fact", Scope: "user", Confidence: .7},
	}}
	result, err := svc.SubmitCuratorOperations(ctx, "curate-1", good)
	if err != nil {
		t.Fatal(err)
	}
	if result.Replayed {
		t.Fatal("first submission marked replay")
	}
	view, err := db.GetLearningView(ctx, "learn-new")
	if err != nil {
		t.Fatal(err)
	}
	if view.Learning.Status != "candidate" || len(view.Evidence) != 1 {
		t.Fatalf("view = %+v", view)
	}
	replay, err := svc.SubmitCuratorOperations(ctx, "curate-1", good)
	if err != nil || !replay.Replayed {
		t.Fatalf("replay=%+v err=%v", replay, err)
	}
	if len(view.Evidence) != 1 {
		t.Fatal("replay duplicated relation")
	}
}

func TestSubmitCuratorOperationsPersistsHumanDecisionProposals(t *testing.T) {
	db := newServiceTestStore(t)
	ctx := context.Background()
	svc := NewLearningService(db, nil)
	seedCurationEvidence(t, db, "lev-discard", "TASK", "", "task-local detail")
	seedCurationLearning(t, db, "learn-old", "user", "", "Old wording")
	seedCurationLearning(t, db, "learn-canonical", "user", "", "Canonical wording")
	packet, err := svc.PrepareLearningPacket(ctx, PrepareLearningPacketInput{AttemptID: "proposal-ops", TaskID: "TASK", MaxItems: 20, MaxBytes: 64 * 1024})
	if err != nil {
		t.Fatal(err)
	}
	doc := CuratorOperationsDocument{SchemaVersion: 1, PacketDigest: packet.Packet.PacketDigest, Operations: []CuratorOperation{
		{Operation: "duplicate", LearningID: "learn-old", CanonicalLearningID: "learn-canonical", Reason: "same preference"},
		{Operation: "discard", EvidenceIDs: []string{"lev-discard"}, Reason: "task local"},
	}}
	if _, err := svc.SubmitCuratorOperations(ctx, "proposal-ops", doc); err != nil {
		t.Fatal(err)
	}
	proposals, err := db.ListLearningProposals(ctx, "pending")
	if err != nil {
		t.Fatal(err)
	}
	if len(proposals) != 2 || proposals[0].Operation == "" || proposals[1].Operation == "" {
		t.Fatalf("proposals = %+v", proposals)
	}
	var duplicate store.LearningProposal
	for _, proposal := range proposals {
		if proposal.Operation == "duplicate" {
			duplicate = proposal
		}
	}
	if duplicate.TargetLearningRevision != 1 {
		t.Fatalf("target learning revision = %d", duplicate.TargetLearningRevision)
	}
	if err := db.ApplyLearningProposalDecision(ctx, duplicate.ID, duplicate.Revision, true); err != nil {
		t.Fatal(err)
	}
	view, err := db.GetLearningView(ctx, "learn-old")
	if err != nil {
		t.Fatal(err)
	}
	if view.Learning.Status != "duplicate" || view.Learning.CanonicalLearningID == nil || *view.Learning.CanonicalLearningID != "learn-canonical" {
		t.Fatalf("learning = %+v", view.Learning)
	}
}

func TestLearningServiceAcceptsReviseProposalAndPreservesEvidence(t *testing.T) {
	db := newServiceTestStore(t)
	ctx := context.Background()
	svc := NewLearningService(db, nil)
	seedCurationEvidence(t, db, "lev-revise", "TASK", "repo:one", "specific incident evidence")
	seedCurationLearning(t, db, "learn-revise", "project", "repo:one", "After ticket TASK, change WidgetView.")
	if _, err := db.DB().Exec(`INSERT INTO learning_evidence_links (learning_id,evidence_id,relation) VALUES ('learn-revise','lev-revise','support')`); err != nil {
		t.Fatal(err)
	}
	packet, err := svc.PrepareLearningPacket(ctx, PrepareLearningPacketInput{
		AttemptID: "revise-proposal", TaskID: "TASK", ProjectKey: "repo:one", MaxItems: 20, MaxBytes: 64 * 1024,
	})
	if err != nil {
		t.Fatal(err)
	}
	doc := CuratorOperationsDocument{SchemaVersion: 1, PacketDigest: packet.Packet.PacketDigest, Operations: []CuratorOperation{{
		Operation: "revise", LearningID: "learn-revise", Claim: "Refresh canonical server state after lifecycle mutations.",
		Rationale: "State-dependent controls otherwise render stale transitions.", Kind: "process_improvement",
		Scope: "project", ProjectKey: "repo:one", Reason: "remove incident-only detail",
	}}}
	if _, err := svc.SubmitCuratorOperations(ctx, "revise-proposal", doc); err != nil {
		t.Fatal(err)
	}
	proposals, err := svc.ListProposals(ctx, "pending")
	if err != nil || len(proposals) != 1 {
		t.Fatalf("proposals = %+v, err = %v", proposals, err)
	}
	if err := svc.DecideProposal(ctx, proposals[0].ID, proposals[0].Revision, true); err != nil {
		t.Fatal(err)
	}
	view, err := svc.Show(ctx, "learn-revise")
	if err != nil {
		t.Fatal(err)
	}
	if view.Learning.Claim != doc.Operations[0].Claim || view.Learning.Rationale != doc.Operations[0].Rationale || view.Learning.Kind != "process_improvement" || view.Learning.Revision != 2 {
		t.Fatalf("revised learning = %+v", view.Learning)
	}
	if len(view.Evidence) != 1 || view.Evidence[0].ID != "lev-revise" {
		t.Fatalf("revised evidence = %+v", view.Evidence)
	}
	found, err := svc.Search(ctx, "canonical server state", LearningFilter{Limit: 10})
	if err != nil || len(found) != 1 || found[0].ID != "learn-revise" {
		t.Fatalf("revised search = %+v, err = %v", found, err)
	}
	old, err := svc.Search(ctx, "WidgetView", LearningFilter{Limit: 10})
	if err != nil || len(old) != 0 {
		t.Fatalf("old wording remained searchable = %+v, err = %v", old, err)
	}
}

func TestLearningServiceRejectsReviseWithoutEvidence(t *testing.T) {
	db := newServiceTestStore(t)
	ctx := context.Background()
	svc := NewLearningService(db, nil)
	seedCurationLearning(t, db, "learn-no-evidence", "user", "", "specific")
	packet, err := svc.PrepareLearningPacket(ctx, PrepareLearningPacketInput{AttemptID: "no-evidence-revise", MaxItems: 20, MaxBytes: 64 * 1024})
	if err != nil {
		t.Fatal(err)
	}
	doc := CuratorOperationsDocument{SchemaVersion: 1, PacketDigest: packet.Packet.PacketDigest, Operations: []CuratorOperation{{
		Operation: "revise", LearningID: "learn-no-evidence", Claim: "general", Kind: "pitfall", Scope: "user", Reason: "generalize",
	}}}
	if _, err := svc.SubmitCuratorOperations(ctx, "no-evidence-revise", doc); !errors.Is(err, ErrInvalidCuratorOperations) {
		t.Fatalf("error = %v, want invalid curator operations", err)
	}
}

func TestLearningServiceRejectsReviseThatBroadensProjectLearningToUserScope(t *testing.T) {
	db := newServiceTestStore(t)
	ctx := context.Background()
	svc := NewLearningService(db, nil)
	seedCurationLearning(t, db, "learn-project-revise", "project", "repo:one", "project rule")
	seedCurationEvidence(t, db, "lev-project-linked", "OTHER", "repo:one", "linked evidence")
	if _, err := db.DB().Exec(`INSERT INTO learning_evidence_links (learning_id,evidence_id,relation) VALUES ('learn-project-revise','lev-project-linked','support')`); err != nil {
		t.Fatal(err)
	}
	packet, err := svc.PrepareLearningPacket(ctx, PrepareLearningPacketInput{AttemptID: "broad-revise", ProjectKey: "repo:one", MaxItems: 20, MaxBytes: 64 * 1024})
	if err != nil {
		t.Fatal(err)
	}
	doc := CuratorOperationsDocument{SchemaVersion: 1, PacketDigest: packet.Packet.PacketDigest, Operations: []CuratorOperation{{
		Operation: "revise", LearningID: "learn-project-revise", Claim: "global rule", Kind: "pitfall", Scope: "user", Reason: "generalize",
	}}}
	if _, err := svc.SubmitCuratorOperations(ctx, "broad-revise", doc); !errors.Is(err, ErrInvalidCuratorOperations) {
		t.Fatalf("error = %v, want invalid curator operations", err)
	}
}

func TestLearningServiceRejectsOversizedReviseProposal(t *testing.T) {
	db := newServiceTestStore(t)
	ctx := context.Background()
	svc := NewLearningService(db, nil)
	seedCurationLearning(t, db, "learn-oversized-revise", "user", "", "specific")
	seedCurationEvidence(t, db, "lev-oversized-linked", "OTHER", "", "linked evidence")
	if _, err := db.DB().Exec(`INSERT INTO learning_evidence_links (learning_id,evidence_id,relation) VALUES ('learn-oversized-revise','lev-oversized-linked','support')`); err != nil {
		t.Fatal(err)
	}
	packet, err := svc.PrepareLearningPacket(ctx, PrepareLearningPacketInput{AttemptID: "oversized-revise", MaxItems: 20, MaxBytes: 64 * 1024})
	if err != nil {
		t.Fatal(err)
	}
	doc := CuratorOperationsDocument{SchemaVersion: 1, PacketDigest: packet.Packet.PacketDigest, Operations: []CuratorOperation{{
		Operation: "revise", LearningID: "learn-oversized-revise", Claim: strings.Repeat("x", MaxLearningClaimBytes+1), Kind: "pitfall", Scope: "user", Reason: "generalize",
	}}}
	if _, err := svc.SubmitCuratorOperations(ctx, "oversized-revise", doc); !errors.Is(err, ErrInvalidCuratorOperations) {
		t.Fatalf("error = %v, want invalid curator operations", err)
	}
}

func TestLearningServiceStaleReviseProposalConflictsAndStaysPending(t *testing.T) {
	db := newServiceTestStore(t)
	ctx := context.Background()
	svc := NewLearningService(db, nil)
	seedCurationEvidence(t, db, "lev-stale-revise", "TASK", "repo:one", "evidence")
	seedCurationLearning(t, db, "learn-stale-revise", "project", "repo:one", "specific")
	seedCurationEvidence(t, db, "lev-stale-linked", "OTHER", "repo:one", "linked evidence")
	if _, err := db.DB().Exec(`INSERT INTO learning_evidence_links (learning_id,evidence_id,relation) VALUES ('learn-stale-revise','lev-stale-linked','support')`); err != nil {
		t.Fatal(err)
	}
	packet, err := svc.PrepareLearningPacket(ctx, PrepareLearningPacketInput{AttemptID: "stale-revise", TaskID: "TASK", ProjectKey: "repo:one", MaxItems: 20, MaxBytes: 64 * 1024})
	if err != nil {
		t.Fatal(err)
	}
	doc := CuratorOperationsDocument{SchemaVersion: 1, PacketDigest: packet.Packet.PacketDigest, Operations: []CuratorOperation{{
		Operation: "revise", LearningID: "learn-stale-revise", Claim: "general", Kind: "pitfall", Scope: "project", ProjectKey: "repo:one", Reason: "generalize",
	}}}
	if _, err := svc.SubmitCuratorOperations(ctx, "stale-revise", doc); err != nil {
		t.Fatal(err)
	}
	proposals, err := svc.ListProposals(ctx, "pending")
	if err != nil || len(proposals) != 1 {
		t.Fatalf("proposals = %+v, err = %v", proposals, err)
	}
	if _, err := svc.Accept(ctx, "learn-stale-revise", 1, "concurrent edit", "", ""); err != nil {
		t.Fatal(err)
	}
	if err := svc.DecideProposal(ctx, proposals[0].ID, proposals[0].Revision, true); !errors.Is(err, ErrLearningConflict) {
		t.Fatalf("error = %v, want learning conflict", err)
	}
	pending, err := svc.ListProposals(ctx, "pending")
	if err != nil || len(pending) != 1 || pending[0].ID != proposals[0].ID || pending[0].Revision != 1 {
		t.Fatalf("pending proposals = %+v, err = %v", pending, err)
	}
}

func TestSubmitCuratorOperationsRejectsForbiddenAndUnknownJSONFields(t *testing.T) {
	db := newServiceTestStore(t)
	ctx := context.Background()
	svc := NewLearningService(db, nil)
	seedCurationEvidence(t, db, "lev-1", "TASK", "", "evidence")
	packet, err := svc.PrepareLearningPacket(ctx, PrepareLearningPacketInput{AttemptID: "curate-json", TaskID: "TASK", MaxItems: 10, MaxBytes: 32 * 1024})
	if err != nil {
		t.Fatal(err)
	}
	for _, operation := range []string{"accept", "incorporate", "verify"} {
		doc := fmt.Sprintf(`{"schema_version":1,"packet_digest":%q,"operations":[{"operation":%q,"evidence_ids":["lev-1"]}]}`, packet.Packet.PacketDigest, operation)
		if _, err := svc.SubmitCuratorOperationsJSON(ctx, "curate-json", []byte(doc)); !errors.Is(err, ErrInvalidCuratorOperations) {
			t.Fatalf("%s error=%v", operation, err)
		}
	}
	unknown := fmt.Sprintf(`{"schema_version":1,"packet_digest":%q,"operations":[],"accepted":true}`, packet.Packet.PacketDigest)
	if _, err := svc.SubmitCuratorOperationsJSON(ctx, "curate-json", []byte(unknown)); !errors.Is(err, ErrInvalidCuratorOperations) {
		t.Fatalf("unknown field error=%v", err)
	}
	generation, err := db.GetLearningCurationGenerationByAttempt(ctx, "curate-json")
	if err != nil {
		t.Fatal(err)
	}
	if generation.Status != store.CurationGenerationPrepared {
		t.Fatalf("generation=%+v", generation)
	}
}

func seedCurationEvidence(t *testing.T, db *store.Store, id, task, project, content string) {
	t.Helper()
	_, err := db.DB().Exec(`INSERT INTO learning_evidence (id,source_kind,source_ref,content,content_digest,author,task_id,project_key,metadata_json) VALUES (?,?,?,?,?,?,?,?,?)`, id, "test", id, content, "sha256:"+id, "tester", task, project, "{}")
	if err != nil {
		t.Fatal(err)
	}
}

func seedCurationLearning(t *testing.T, db *store.Store, id, scope, project, claim string) {
	t.Helper()
	_, err := db.DB().Exec(`INSERT INTO learnings (id,scope,project_key,kind,claim,status,confidence,revision) VALUES (?,?,?,?,?,'candidate',0,1)`, id, scope, project, "fact", claim)
	if err != nil {
		t.Fatal(err)
	}
}
