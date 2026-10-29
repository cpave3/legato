package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/cpave3/legato/internal/engine/store"
)

// ApprovalAttentionAdapter shows each pending approval in Attention, with
// the decisions that need no more input as actions.
type ApprovalAttentionAdapter struct {
	approvals *ApprovalService
}

func NewApprovalAttentionAdapter(approvals *ApprovalService) *ApprovalAttentionAdapter {
	return &ApprovalAttentionAdapter{approvals: approvals}
}

func (a *ApprovalAttentionAdapter) Producer() string { return "approvals" }

func (a *ApprovalAttentionAdapter) Snapshot(ctx context.Context) ([]AttentionInput, error) {
	pending, err := a.approvals.List(ctx, store.ApprovalFilter{Status: store.ApprovalPending})
	if err != nil {
		return nil, err
	}
	items := make([]AttentionInput, 0, len(pending))
	for _, approval := range pending {
		items = append(items, AttentionInput{
			SignalKind: AttentionSignalApproval,
			SourceKey:  approval.ID, SourceRevision: strconv.Itoa(approval.Revision),
			TaskID: approval.TaskID, Title: approval.Title, Reason: approvalReason(approval), Detail: approval.Summary,
			Category: "approval", Urgency: "normal",
			Navigation: AttentionNavigation{Kind: "approval", Target: approval.ID},
			Actions:    approvalActions(approval),
		})
	}
	return items, nil
}

func (a *ApprovalAttentionAdapter) Execute(ctx context.Context, request AttentionActionRequest) error {
	decision, err := decisionForAction(ctx, a.approvals, request)
	if err != nil {
		return err
	}
	decision.By = "attention"
	_, err = a.approvals.Decide(ctx, request.SourceKey, decision)
	return err
}

// approvalReason says who asks, for the line under the title.
func approvalReason(approval Approval) string {
	var details struct {
		TaskTitle string `json:"task_title"`
		AgentKind string `json:"agent_kind"`
	}
	_ = json.Unmarshal(approval.Details, &details)
	who := approval.TaskID
	if details.TaskTitle != "" {
		who = details.TaskTitle
	}
	parts := []string{}
	if who != "" {
		parts = append(parts, who)
	}
	if details.AgentKind != "" {
		parts = append(parts, details.AgentKind)
	}
	if len(parts) == 0 {
		return "Waiting for your decision"
	}
	return strings.Join(parts, " · ") + " asks"
}

// approvalQuestion is one question of an approval's details.
type approvalQuestion struct {
	ID          string `json:"id"`
	Question    string `json:"question"`
	MultiSelect bool   `json:"multiSelect"`
	Options     []struct {
		Label string `json:"label"`
	} `json:"options"`
}

func approvalQuestions(approval Approval) []approvalQuestion {
	var details struct {
		Questions []approvalQuestion `json:"questions"`
	}
	_ = json.Unmarshal(approval.Details, &details)
	return details.Questions
}

// approvalActions are an approval's options, as Attention actions: allow:<id>
// for each allow option, deny, and for a single single-choice question
// answer:<index> for each of its options.
func approvalActions(approval Approval) []AttentionAction {
	actions := []AttentionAction{}
	for _, option := range approval.Options {
		switch option.Kind {
		case ApprovalAllow:
			actions = append(actions, AttentionAction{ID: "allow:" + option.ID, Label: option.Label, Kind: "execute"})
		case ApprovalDeny:
			actions = append(actions, AttentionAction{ID: "deny", Label: option.Label, Kind: "execute"})
		}
	}
	if questions := approvalQuestions(approval); len(questions) > 0 {
		if len(questions) == 1 && !questions[0].MultiSelect {
			for i, option := range questions[0].Options {
				actions = append(actions, AttentionAction{ID: "answer:" + strconv.Itoa(i), Label: option.Label, Kind: "execute"})
			}
		}
		actions = append(actions, AttentionAction{ID: "dismiss", Label: "Dismiss", Kind: "execute"})
	}
	if len(actions) > 0 {
		actions[0].Intent = "primary"
	}
	return actions
}

// decisionForAction turns an action from approvalActions back into a decision.
func decisionForAction(ctx context.Context, approvals *ApprovalService, request AttentionActionRequest) (ApprovalDecision, error) {
	switch {
	case strings.HasPrefix(request.ActionID, "allow:"):
		return ApprovalDecision{Decision: ApprovalAllow, Option: strings.TrimPrefix(request.ActionID, "allow:")}, nil
	case request.ActionID == "deny":
		return ApprovalDecision{Decision: ApprovalDeny, Feedback: request.Text}, nil
	case request.ActionID == "dismiss":
		return ApprovalDecision{Decision: ApprovalDismiss, Feedback: request.Text}, nil
	case strings.HasPrefix(request.ActionID, "answer:"):
		approval, err := approvals.Get(ctx, request.SourceKey)
		if err != nil {
			return ApprovalDecision{}, err
		}
		index, err := strconv.Atoi(strings.TrimPrefix(request.ActionID, "answer:"))
		questions := approvalQuestions(*approval)
		if err != nil || len(questions) != 1 || index < 0 || index >= len(questions[0].Options) {
			return ApprovalDecision{}, fmt.Errorf("%w: unknown answer %q", ErrApprovalInvalid, request.ActionID)
		}
		return ApprovalDecision{Decision: ApprovalAnswer, Answers: map[string][]string{questions[0].ID: {questions[0].Options[index].Label}}}, nil
	}
	return ApprovalDecision{}, fmt.Errorf("%w: unknown approval action %q", ErrApprovalInvalid, request.ActionID)
}
