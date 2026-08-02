package service

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/cpave3/legato/internal/engine/store"
)

// LearningAttentionSource is the public learning surface used by Attention.
// Keeping the adapter on this interface ensures actions retain the learning
// service's revision checks and event publication.
type LearningAttentionSource interface {
	List(context.Context, LearningFilter) ([]store.Learning, error)
	ListIncorporations(context.Context, ...string) ([]store.LearningIncorporation, error)
	Show(context.Context, string) (*LearningView, error)
	Accept(context.Context, string, int, string, string, string) (*LearningView, error)
	Reject(context.Context, string, int) (*LearningView, error)
	VerifyIncorporation(context.Context, string, int, bool, string) (*store.LearningIncorporation, error)
	ListProposals(context.Context, string) ([]store.LearningProposal, error)
	DecideProposal(context.Context, string, int, bool) error
	SetPromptEnabled(context.Context, string, int, bool) (*LearningView, error)
}

type LearningAttentionAdapter struct {
	learnings LearningAttentionSource
}

func NewLearningAttentionAdapter(learnings LearningAttentionSource) *LearningAttentionAdapter {
	return &LearningAttentionAdapter{learnings: learnings}
}

func (a *LearningAttentionAdapter) Producer() string { return "learnings" }

func (a *LearningAttentionAdapter) Snapshot(ctx context.Context) ([]AttentionInput, error) {
	candidates, err := a.learnings.List(ctx, LearningFilter{Status: "candidate", Limit: 200})
	if err != nil {
		return nil, err
	}
	validated, err := a.learnings.List(ctx, LearningFilter{Status: "validated", Limit: 200})
	if err != nil {
		return nil, err
	}
	incorporated, err := a.learnings.List(ctx, LearningFilter{Status: "incorporated", Limit: 200})
	if err != nil {
		return nil, err
	}
	validated = append(validated, incorporated...)
	stale, err := a.learnings.List(ctx, LearningFilter{Status: "stale", Limit: 200})
	if err != nil {
		return nil, err
	}
	incorporations, err := a.learnings.ListIncorporations(ctx, "verification_pending", "stale")
	if err != nil {
		return nil, err
	}
	proposals, err := a.learnings.ListProposals(ctx, "pending")
	if err != nil {
		return nil, err
	}

	items := make([]AttentionInput, 0, len(candidates)+len(validated)+len(stale)+len(incorporations)+len(proposals))
	for _, learning := range append(candidates, stale...) {
		actions := []AttentionAction{{ID: "open", Label: "Open learning", Kind: "navigate", Intent: "primary"}}
		reason := "Learning proposal is awaiting your decision"
		urgency := "normal"
		if learning.Status == "candidate" {
			actions = append(actions,
				AttentionAction{ID: "accept", Label: "Accept", Kind: "execute", RequiresConfirm: true},
				AttentionAction{ID: "reject", Label: "Reject", Kind: "execute", RequiresConfirm: true},
			)
		} else {
			reason = "Learning is stale and needs review"
			urgency = "high"
		}
		items = append(items, AttentionInput{
			SourceKey:      "learning:" + learning.ID,
			SourceRevision: learningAttentionRevision("learning", learning.Status, learning.Revision),
			Title:          learning.Claim, Reason: reason, Detail: learning.Rationale,
			Category: "learning", Urgency: urgency,
			Navigation: AttentionNavigation{Kind: "learning", Target: learning.ID}, Actions: actions,
		})
	}
	for _, learning := range validated {
		if learning.PromptDecided {
			continue
		}
		items = append(items, AttentionInput{
			SourceKey: "activation:" + learning.ID, SourceRevision: learningAttentionRevision("activation", learning.Status, learning.Revision),
			Title: learning.Claim, Reason: "Choose whether this accepted learning should affect future agent prompts", Detail: learning.Rationale,
			Category: "learning", Urgency: "normal", Navigation: AttentionNavigation{Kind: "learning", Target: learning.ID},
			Actions: []AttentionAction{{ID: "open", Label: "Open learning", Kind: "navigate", Intent: "primary"},
				{ID: "enable", Label: "Enable for prompts", Kind: "execute", RequiresConfirm: true},
				{ID: "leave_disabled", Label: "Leave disabled", Kind: "execute"}},
		})
	}
	for _, proposal := range proposals {
		title := "Curator proposal: " + proposal.Operation
		detail := proposal.Reason
		items = append(items, AttentionInput{
			SourceKey: "proposal:" + proposal.ID, SourceRevision: learningAttentionRevision("proposal", proposal.Status, proposal.Revision),
			Title: title, Reason: "Curator recommendation is awaiting your decision", Detail: detail,
			Category: "learning", Urgency: "normal", Navigation: AttentionNavigation{Kind: "learning", Target: proposal.LearningID},
			Actions: []AttentionAction{{ID: "open", Label: "Open learning", Kind: "navigate", Intent: "primary"},
				{ID: "accept", Label: "Accept", Kind: "execute", RequiresConfirm: true}, {ID: "reject", Label: "Reject", Kind: "execute", RequiresConfirm: true}},
		})
	}
	for _, incorporation := range incorporations {
		view, err := a.learnings.Show(ctx, incorporation.LearningID)
		if err != nil {
			return nil, err
		}
		reason := "Learning incorporation is awaiting verification"
		urgency := "normal"
		actions := []AttentionAction{{ID: "open", Label: "Open learning", Kind: "navigate", Intent: "primary"}}
		if incorporation.Status == "verification_pending" {
			actions = append(actions,
				AttentionAction{ID: "accept", Label: "Accept verification", Kind: "execute", RequiresConfirm: true},
				AttentionAction{ID: "reject", Label: "Reject verification", Kind: "execute", RequiresConfirm: true},
			)
		} else {
			reason = "Learning incorporation is stale and needs review"
			urgency = "high"
		}
		detail := incorporation.Destination
		if incorporation.Target != "" {
			detail += ": " + incorporation.Target
		}
		items = append(items, AttentionInput{
			SourceKey:      "incorporation:" + incorporation.ID,
			SourceRevision: learningAttentionRevision("incorporation", incorporation.Status, incorporation.Revision),
			Title:          view.Learning.Claim, Reason: reason, Detail: detail,
			Category: "learning", Urgency: urgency,
			Navigation: AttentionNavigation{Kind: "learning", Target: incorporation.LearningID}, Actions: actions,
		})
	}
	return items, nil
}

func learningAttentionRevision(kind, status string, revision int) string {
	return fmt.Sprintf("%s:%s:%d", kind, status, revision)
}

func parseLearningAttentionRevision(value, kind string) (int, error) {
	parts := strings.Split(value, ":")
	if len(parts) != 3 || parts[0] != kind {
		return 0, fmt.Errorf("invalid %s attention revision %q", kind, value)
	}
	revision, err := strconv.Atoi(parts[2])
	if err != nil || revision < 1 {
		return 0, fmt.Errorf("invalid %s attention revision %q", kind, value)
	}
	return revision, nil
}

func (a *LearningAttentionAdapter) Execute(ctx context.Context, request AttentionActionRequest) error {
	kind, id, ok := strings.Cut(request.SourceKey, ":")
	if !ok || id == "" {
		return fmt.Errorf("invalid learning attention source key %q", request.SourceKey)
	}
	revision, err := parseLearningAttentionRevision(request.ExpectedRevision, kind)
	if err != nil {
		return err
	}
	switch kind {
	case "learning":
		switch request.ActionID {
		case "accept":
			_, err = a.learnings.Accept(ctx, id, revision, "", "", "")
		case "reject":
			_, err = a.learnings.Reject(ctx, id, revision)
		default:
			err = fmt.Errorf("unsupported learning attention action %q", request.ActionID)
		}
	case "activation":
		switch request.ActionID {
		case "enable":
			_, err = a.learnings.SetPromptEnabled(ctx, id, revision, true)
		case "leave_disabled":
			_, err = a.learnings.SetPromptEnabled(ctx, id, revision, false)
		default:
			err = fmt.Errorf("unsupported learning activation action %q", request.ActionID)
		}
	case "proposal":
		switch request.ActionID {
		case "accept":
			err = a.learnings.DecideProposal(ctx, id, revision, true)
		case "reject":
			err = a.learnings.DecideProposal(ctx, id, revision, false)
		default:
			err = fmt.Errorf("unsupported learning proposal attention action %q", request.ActionID)
		}
	case "incorporation":
		switch request.ActionID {
		case "accept":
			_, err = a.learnings.VerifyIncorporation(ctx, id, revision, true, "Accepted through Attention")
		case "reject":
			_, err = a.learnings.VerifyIncorporation(ctx, id, revision, false, "Rejected through Attention")
		default:
			err = fmt.Errorf("unsupported learning attention action %q", request.ActionID)
		}
	default:
		err = fmt.Errorf("invalid learning attention source key %q", request.SourceKey)
	}
	return err
}
