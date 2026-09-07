package service

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cpave3/legato/internal/engine/store"
)

func readyHandoffFixture(t *testing.T) (*reviewFixture, HandoffInput) {
	t.Helper()
	f := newReviewFixture(t)
	writeRepoFile(t, f.repo, "entry.go", "package entry\n// persist before delivery\n")
	gitCommitAll(t, f.repo, "Add entry")
	chapter, err := f.svc.CreateChapter(context.Background(), f.tourID, ChapterArgs{Title: "Persist questions", Narration: "Saving is independent of delivery.", Includes: []ChapterInclude{{FilePath: "entry.go", Hunk: 1}}})
	if err != nil {
		t.Fatal(err)
	}
	if err := f.svc.Ready(context.Background(), f.tourID, "Question persistence"); err != nil {
		t.Fatal(err)
	}
	tour, err := f.svc.Tour(context.Background(), f.tourID)
	if err != nil {
		t.Fatal(err)
	}
	input := HandoffInput{SchemaVersion: 1, PassID: tour.Passes[len(tour.Passes)-1].Pass.ID, BaseSHA: tour.Tour.BaseSHA, HeadSHA: tour.Tour.HeadSHA,
		Overview:  HandoffOverview{Before: "Questions could be lost.", After: "Save before delivery.", Why: "Retain questions offline.", NonGoals: "No reply deadline.", Decision: "Persist first.", Alternative: "Deliver first loses context.", Risk: "Saving does not guarantee delivery.", VerificationGaps: "No live delivery checked.", ReviewQuestion: "Does failure preserve the question?", Rollback: "Data removal is not a safe rollback."},
		Concepts:  []HandoffConcept{{ID: "persist", Title: "Persist first", Claim: "Questions survive offline agents.", Flow: []string{"Request", "Save", "Deliver"}, Invariant: "Persist before delivery.", FailureCase: "Agent is offline.", ChangeGuide: "Start at entry.go.", SourceIDs: []string{"entry", "unchanged"}, ChapterIDs: []string{chapter}, EvidenceIDs: []string{"check"}, Routes: []string{"owner", "reviewer"}}},
		Sources:   []HandoffSourceInput{{ID: "entry", Path: "entry.go", Side: "head", LineStart: 1, LineEnd: 2}, {ID: "unchanged", Path: "README.md", Side: "base", LineStart: 1, LineEnd: 1}},
		Evidence:  []HandoffEvidence{{ID: "check", Description: "Test persistence", SourceIDs: []string{"entry"}, Command: "go test ./...", HeadSHA: tour.Tour.HeadSHA, RecordedAt: "2026-09-07T12:00:00Z", Outcome: "passed", Reporter: "agent", Limitations: "Test only, not live delivery."}},
		Scenarios: []HandoffScenario{{ID: "offline", ConceptID: "persist", Prompt: "What survives an offline agent?", Explanation: "The stored question, not a guaranteed reply.", SourceIDs: []string{"entry"}}}}
	return f, input
}

func submitHandoff(t *testing.T, f *reviewFixture, input HandoffInput) *HandoffPacket {
	t.Helper()
	packet, err := f.svc.SubmitHandoff(context.Background(), f.tourID, input)
	if err != nil {
		t.Fatal(err)
	}
	return packet
}

func TestHandoffRetainsPinnedContextAndHumanNotesWithoutRepository(t *testing.T) {
	f, input := readyHandoffFixture(t)
	ctx := context.Background()
	packet := submitHandoff(t, f, input)
	if packet.Revision != 1 || packet.Note.Revision != 0 || len(packet.Input.Coverage) != 1 || packet.Input.Coverage[0].Category != "uncategorized" {
		t.Fatalf("packet=%+v", packet)
	}
	if packet.Sources[0].Text != "package entry\n// persist before delivery" || packet.Sources[1].Text != "hello" || !strings.HasPrefix(packet.Sources[0].Digest, "sha256:") {
		t.Fatalf("sources=%+v", packet.Sources)
	}
	if _, err := f.svc.SubmitHandoff(ctx, f.tourID, input); !errors.Is(err, ErrHandoffConflict) {
		t.Fatalf("stale submit=%v", err)
	}
	note := HandoffNote{Readiness: "need_help", Body: "private owner note", Answers: map[string]string{"offline": "Question persists."}}
	saved, err := f.svc.SaveHandoffNote(ctx, f.tourID, packet.ID, note)
	if err != nil || saved.Revision != 1 {
		t.Fatalf("note=%+v err=%v", saved, err)
	}
	if _, err = f.svc.SaveHandoffNote(ctx, f.tourID, packet.ID, note); !errors.Is(err, ErrHandoffConflict) {
		t.Fatalf("stale note=%v", err)
	}
	if _, err = f.svc.SaveHandoffNote(ctx, "other", packet.ID, note); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("cross-tour note=%v", err)
	}
	input.ExpectedRevision = 1
	second := submitHandoff(t, f, input)
	if second.Note.Revision != 0 || second.Note.Body != "" {
		t.Fatalf("inherited note=%+v", second.Note)
	}
	if err := os.Rename(f.repo, f.repo+"-offline"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Rename(f.repo+"-offline", f.repo) })
	view, err := f.svc.Handoff(ctx, f.tourID)
	if err != nil {
		t.Fatal(err)
	}
	if len(view.Packets) != 2 || view.CheckedAt == "" || view.Packets[1].Note.Body != "private owner note" {
		t.Fatalf("view=%+v", view)
	}
	exported, err := f.svc.ExportHandoff(ctx, f.tourID, HandoffExportInput{PacketID: packet.ID, Format: "github"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(exported.Markdown, "private owner note") || strings.Contains(exported.Markdown, "package entry") {
		t.Fatal("default export leaked note/excerpt")
	}
	exported, err = f.svc.ExportHandoff(ctx, f.tourID, HandoffExportInput{PacketID: packet.ID, Format: "maintenance", IncludeNotes: true, IncludeSources: true})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(exported.Markdown, "private owner note") || !strings.Contains(exported.Markdown, "package entry") {
		t.Fatal("explicit export omitted selection")
	}
	tour, err := f.store.GetReviewTour(ctx, f.tourID)
	if err != nil || tour.Status != "ready" {
		t.Fatalf("note changed review: %+v %v", tour, err)
	}
}

func TestHandoffLifecycleKeepsHistoryAndInvalidatesOnlyOnChange(t *testing.T) {
	for _, action := range []string{"sync-head", "sync-dirty", "reviewed-head", "restart", "advance", "chapter", "membership", "delete"} {
		t.Run(action, func(t *testing.T) {
			f, input := readyHandoffFixture(t)
			ctx := context.Background()
			p := submitHandoff(t, f, input)
			if err := f.svc.Sync(ctx, f.tourID); err != nil {
				t.Fatal(err)
			}
			before, err := f.svc.Handoff(ctx, f.tourID)
			if err != nil || before.Packets[0].Outdated {
				t.Fatalf("unchanged sync invalidated: %+v %v", before, err)
			}
			switch action {
			case "reviewed-head":
				if err := f.svc.Complete(ctx, f.tourID); err != nil {
					t.Fatal(err)
				}
				complete, err := f.svc.Handoff(ctx, f.tourID)
				if err != nil || complete.Packets[0].Outdated {
					t.Fatalf("completion invalidated %+v %v", complete, err)
				}
				fallthrough
			case "sync-head":
				writeRepoFile(t, f.repo, "entry.go", "package entry\n// changed ordering\n")
				gitCommitAll(t, f.repo, "Follow-up")
				if err := f.svc.Sync(ctx, f.tourID); err != nil {
					t.Fatal(err)
				}
			case "sync-dirty":
				writeRepoFile(t, f.repo, "entry.go", "dirty\n")
				if err := f.svc.Sync(ctx, f.tourID); err != nil {
					t.Fatal(err)
				}
			case "restart":
				if err := f.svc.Restart(ctx, f.tourID); err != nil {
					t.Fatal(err)
				}
			case "advance":
				if err := f.svc.AdvancePass(ctx, f.tourID, "follow up"); err != nil {
					t.Fatal(err)
				}
			case "chapter":
				title := "Explain delivery"
				if _, err := f.svc.EditChapter(ctx, f.tourID, input.Concepts[0].ChapterIDs[0], ChapterEditArgs{Title: &title}); err != nil {
					t.Fatal(err)
				}
			case "membership":
				if _, err := f.svc.RemoveChapter(ctx, f.tourID, input.Concepts[0].ChapterIDs[0]); err != nil {
					t.Fatal(err)
				}
			case "delete":
				if err := f.svc.Delete(ctx, f.tourID); err != nil {
					t.Fatal(err)
				}
				if _, err := f.svc.Handoff(ctx, f.tourID); !errors.Is(err, store.ErrNotFound) {
					t.Fatalf("deleted read=%v", err)
				}
				if _, err := f.store.GetHandoffPacket(ctx, f.tourID, p.ID); !errors.Is(err, store.ErrNotFound) {
					t.Fatalf("packet survived deletion: %v", err)
				}
				return
			}
			view, err := f.svc.Handoff(ctx, f.tourID)
			if err != nil {
				t.Fatal(err)
			}
			if len(view.Packets) != 1 || !view.Packets[0].Outdated || view.Packets[0].Sources[0].Text != p.Sources[0].Text || len(view.Packets[0].Chapters) != 1 {
				t.Fatalf("history=%+v", view.Packets)
			}
			if action == "reviewed-head" {
				tour, _ := f.store.GetReviewTour(ctx, f.tourID)
				if tour.Status != "reviewed" {
					t.Fatal("sync reopened completed review")
				}
			}
		})
	}
}

func TestHandoffValidationRejectsEmptyAndMisleadingContent(t *testing.T) {
	mutations := map[string]func(*HandoffInput){
		"blank-overview":      func(i *HandoffInput) { i.Overview = HandoffOverview{} },
		"blank-claim":         func(i *HandoffInput) { i.Concepts[0].Claim = " " },
		"invalid-id":          func(i *HandoffInput) { i.Concepts[0].ID = "<hidden id>" },
		"missing-command":     func(i *HandoffInput) { i.Evidence[0].Command = "" },
		"invalid-outcome":     func(i *HandoffInput) { i.Evidence[0].Outcome = "certified" },
		"missing-time":        func(i *HandoffInput) { i.Evidence[0].RecordedAt = "" },
		"bad-revision":        func(i *HandoffInput) { i.Evidence[0].HeadSHA = "main" },
		"missing-explanation": func(i *HandoffInput) { i.Scenarios[0].Explanation = "" },
		"unknown-source":      func(i *HandoffInput) { i.Concepts[0].SourceIDs = []string{"missing"} },
		"unknown-chapter":     func(i *HandoffInput) { i.Concepts[0].ChapterIDs = []string{"other"} },
		"non-diff-coverage":   func(i *HandoffInput) { i.Coverage = []HandoffCoverage{{Path: "README.md", Category: "tests"}} },
		"path-traversal":      func(i *HandoffInput) { i.Sources[0].Path = "../secret" },
		"wrong-head":          func(i *HandoffInput) { i.HeadSHA = i.BaseSHA },
	}
	for name, mutate := range mutations {
		t.Run(name, func(t *testing.T) {
			f, input := readyHandoffFixture(t)
			mutate(&input)
			_, err := f.svc.SubmitHandoff(context.Background(), f.tourID, input)
			var invalid *HandoffValidationError
			if !errors.As(err, &invalid) {
				t.Fatalf("expected validation error; got %v", err)
			}
			view, _ := f.svc.Handoff(context.Background(), f.tourID)
			if len(view.Packets) != 0 {
				t.Fatal("invalid packet persisted")
			}
		})
	}
}

func TestHandoffRejectsDirtyAndCapturingReview(t *testing.T) {
	for _, state := range []string{"dirty", "capturing"} {
		t.Run(state, func(t *testing.T) {
			f, input := readyHandoffFixture(t)
			if state == "dirty" {
				writeRepoFile(t, f.repo, "untracked", "not committed")
			} else {
				if err := f.svc.Restart(context.Background(), f.tourID); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := f.svc.SubmitHandoff(context.Background(), f.tourID, input); err == nil {
				t.Fatal("accepted non-ready snapshot")
			}
		})
	}
}

func TestHandoffNotesValidateScenarioOwnershipAndBounds(t *testing.T) {
	f, input := readyHandoffFixture(t)
	p := submitHandoff(t, f, input)
	for _, note := range []HandoffNote{{Answers: map[string]string{"missing": "answer"}}, {Skipped: []string{"missing"}}, {Answers: map[string]string{"offline": ""}, Skipped: []string{"offline"}}, {Body: strings.Repeat("x", MaxHandoffBytes)}, {Readiness: "approved"}} {
		if _, err := f.svc.SaveHandoffNote(context.Background(), f.tourID, p.ID, note); err == nil {
			t.Fatalf("accepted %+v", note)
		}
	}
}

func TestHandoffDecodeRejectsUntrustedFieldsAndTrailingDocuments(t *testing.T) {
	for _, raw := range []string{`null`, `{}`, `{"schema_version":1,"note":{}}`, `{} {}`, strings.Repeat(" ", MaxHandoffBytes+1)} {
		input, err := DecodeHandoff(strings.NewReader(raw))
		if err == nil {
			_, err = (&ReviewService{}).SubmitHandoff(context.Background(), "", input)
		}
		if err == nil {
			t.Fatalf("accepted %s", raw[:min(len(raw), 50)])
		}
	}
}

func TestHandoffComparisonIncludesBehaviorAndEvidenceChanges(t *testing.T) {
	f, input := readyHandoffFixture(t)
	ctx := context.Background()
	submitHandoff(t, f, input)
	input.ExpectedRevision = 1
	input.Overview.Decision = "Use an outbox instead."
	input.Changes = "The delivery decision changed."
	input.Evidence[0].Outcome = "failed"
	second := submitHandoff(t, f, input)
	if !second.Comparison.Safe || len(second.Comparison.EvidenceToRecheck) == 0 {
		t.Fatalf("comparison=%+v", second.Comparison)
	}
	export, err := f.svc.ExportHandoff(ctx, f.tourID, HandoffExportInput{PacketID: second.ID, Format: "github"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(export.Markdown, input.Changes) {
		t.Fatal("export lost author change summary")
	}
	writeRepoFile(t, f.repo, "entry.go", "package entry\n// use outbox\n")
	gitCommitAll(t, f.repo, "outbox")
	if err := f.svc.Ready(ctx, f.tourID, "outbox"); err != nil {
		t.Fatal(err)
	}
	view, _ := f.svc.Tour(ctx, f.tourID)
	input.ExpectedRevision = 2
	input.HeadSHA = view.Tour.HeadSHA
	third := submitHandoff(t, f, input)
	if !third.Comparison.Safe || len(third.Comparison.ChangedFiles) != 1 || third.Comparison.ChangedFiles[0] != "entry.go" || len(third.Comparison.ChangedConcepts) == 0 {
		t.Fatalf("code-only concept change not reported: %+v", third.Comparison)
	}
}

func TestHandoffQuestionUsesExistingDurableOfflineFlow(t *testing.T) {
	f, input := readyHandoffFixture(t)
	p := submitHandoff(t, f, input)
	ctx := context.Background()
	err := f.svc.AskQuestion(ctx, f.tourID, p.Chapters[0].ID, ReviewQuestion{Text: "Packet " + p.ID + ": why does this preserve questions?"})
	if !errors.Is(err, ErrAgentOffline) {
		t.Fatalf("offline warning=%v", err)
	}
	tour, err := f.svc.Tour(ctx, f.tourID)
	if err != nil {
		t.Fatal(err)
	}
	if len(tour.Messages) != 1 || !strings.Contains(tour.Messages[0].Body, p.ID) {
		t.Fatal("question lost")
	}
}

func TestHandoffSnapshotBoundaryRejectsConcurrentChapterChange(t *testing.T) {
	f, input := readyHandoffFixture(t)
	ctx := context.Background()
	boundary, err := f.store.ReviewHandoffBoundary(ctx, f.tourID)
	if err != nil {
		t.Fatal(err)
	}
	title := "New chapter title"
	if _, err := f.svc.EditChapter(ctx, f.tourID, input.Concepts[0].ChapterIDs[0], ChapterEditArgs{Title: &title}); err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(input)
	record := store.HandoffPacketRecord{ID: "raced", TourID: f.tourID, Revision: 1, PassID: input.PassID, BaseSHA: input.BaseSHA, HeadSHA: input.HeadSHA, Repository: "test", BoundaryFingerprint: boundary.Fingerprint, PacketJSON: string(raw)}
	if err := f.store.InsertHandoffPacket(ctx, record, 0, *boundary); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("stale boundary=%v", err)
	}
	view, _ := f.svc.Handoff(ctx, f.tourID)
	if len(view.Packets) != 0 {
		t.Fatal("partial publication")
	}
}

func TestReviewedTourWithoutHandoffDoesNotNeedRepository(t *testing.T) {
	f, _ := readyHandoffFixture(t)
	if err := f.svc.Complete(context.Background(), f.tourID); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(f.repo, filepath.Join(filepath.Dir(f.repo), "moved")); err != nil {
		t.Fatal(err)
	}
	if err := f.svc.Sync(context.Background(), f.tourID); err != nil {
		t.Fatalf("existing completed review requires repository: %v", err)
	}
}
