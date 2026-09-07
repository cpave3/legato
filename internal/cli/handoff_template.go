package cli

import (
	"context"
	"fmt"
	"io"

	"github.com/cpave3/legato/internal/service"
)

// ReviewHandoffTemplate provides the authoring contract in every task repository,
// without relying on this project's documentation being present in that checkout.
func ReviewHandoffTemplate(svc *service.ReviewService, tourID string, w io.Writer) error {
	ctx := context.Background()
	review, err := svc.Tour(ctx, tourID)
	if err != nil {
		return err
	}
	if review.Tour.Status != "ready" || len(review.Passes) == 0 {
		return &service.HandoffValidationError{Message: "prepare a clean ready review before requesting a template"}
	}
	handoff, err := svc.Handoff(ctx, tourID)
	if err != nil {
		return err
	}
	pass := review.Passes[len(review.Passes)-1].Pass
	input := service.HandoffInput{SchemaVersion: 1, PassID: pass.ID, BaseSHA: review.Tour.BaseSHA, HeadSHA: review.Tour.HeadSHA,
		Overview:  service.HandoffOverview{Before: "Describe the previous user-visible behavior.", After: "Describe the new user-visible behavior.", Why: "Explain why this change is needed.", NonGoals: "State what does not change.", Decision: "Explain the main design decision.", Alternative: "Explain the rejected alternative and trade-off.", Risk: "Identify the highest-risk assumption.", VerificationGaps: "State what has not been checked.", ReviewQuestion: "State the judgment the reviewer should make.", Rollback: "Explain rollback limits, or state that rollback is not established."},
		Concepts:  []service.HandoffConcept{{ID: "critical-path", Title: "Replace with the main behavior", Claim: "Replace with a concrete behavioral claim.", Flow: []string{"Name the entry point.", "Trace the behavior to its observable outcome."}, Invariant: "Describe the constraint to preserve.", FailureCase: "Describe the critical failure boundary.", ChangeGuide: "Identify where a maintainer should start.", SourceIDs: []string{"entry"}, ChapterIDs: []string{}, EvidenceIDs: []string{"check-example"}, Routes: []string{"owner", "reviewer", "maintainer", "colleague"}}},
		Sources:   []service.HandoffSourceInput{{ID: "entry", Path: "REPLACE_WITH_REPOSITORY_RELATIVE_SOURCE", Side: "head", LineStart: 1, LineEnd: 1}},
		Evidence:  []service.HandoffEvidence{{ID: "check-example", Description: "Replace with a real imported check, or remove this entry and its concept reference.", SourceIDs: []string{"entry"}, Command: "REPLACE_WITH_ACTUAL_COMMAND", HeadSHA: review.Tour.HeadSHA, RecordedAt: "REPLACE_WITH_RFC3339_TIME", Outcome: "skipped", Reporter: "agent", Limitations: "Placeholder only. Outcomes are passed, failed, or skipped. Never invent a result."}},
		Scenarios: []service.HandoffScenario{{ID: "maintenance-example", ConceptID: "critical-path", Prompt: "Replace with a practical maintenance question, or remove this optional scenario.", Explanation: "Replace with an authored explanation supported by the pinned sources.", SourceIDs: []string{"entry"}}},
		Coverage:  []service.HandoffCoverage{{Path: "REPLACE_WITH_CHANGED_PATH", Category: "uncategorized"}}, Changes: "Describe changes since the previous handoff, or state that this is the first packet."}
	if len(handoff.Packets) > 0 {
		input.ExpectedRevision = handoff.Packets[0].Revision
	}
	for _, step := range review.Passes[len(review.Passes)-1].Steps {
		if step.Kind == "chapter" {
			input.Concepts[0].ChapterIDs = append(input.Concepts[0].ChapterIDs, step.ID)
			break
		}
	}
	if input.PassID == "" || input.HeadSHA == "" || input.BaseSHA == "" {
		return fmt.Errorf("ready review is missing its capture boundary")
	}
	return writeJSON(w, input)
}
