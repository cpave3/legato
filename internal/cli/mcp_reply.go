package cli

import (
	"context"
	"fmt"

	"github.com/cpave3/legato/internal/engine/mcpserver"
	"github.com/cpave3/legato/internal/service"
)

// MCPCaseReplies sends an agent's reply to a delegation back to the
// integration that made it (Argus).
type MCPCaseReplies interface {
	Reply(ctx context.Context, taskID, executionID string, in service.CaseReplyInput) (service.CaseReplyResult, error)
}

type replyCaseInput struct {
	DelegationID string `json:"delegation_id" jsonschema:"the delegation ID from the [argus case …] message"`
	Outcome      string `json:"outcome" jsonschema:"done, finding, question or cannot"`
	Text         string `json:"text" jsonschema:"what you did, found, need to know, or why you cannot, as Markdown"`
}

func (m *mcpTools) replyCase() mcpserver.Tool {
	return mcpserver.NewTool(mcpserver.Definition{
		Name:  "reply_case",
		Title: "Reply to an Argus case",
		Description: "Answers a message that starts [argus case … · delegation dl-… · …]: work Argus handed you. " +
			"Use it only for such a message, with its delegation ID. outcome is done (you did what was asked), finding (what you found out), " +
			"question (you need an answer to go on; it comes back as another [argus case …] message) or cannot (why you cannot do it). " +
			"A reply Argus cannot take at once is queued and sent later.",
	}, func(ctx context.Context, in replyCaseInput) (mcpserver.Result, error) {
		session, err := m.session(ctx)
		if err != nil {
			return mcpserver.Result{}, err
		}
		executionID := m.opts.ExecutionID
		if executionID == "" {
			executionID = session.ExecutionID
		}
		result, err := m.opts.CaseReplies.Reply(ctx, m.opts.TaskID, executionID, service.CaseReplyInput{DelegationID: in.DelegationID, Outcome: in.Outcome, Text: in.Text})
		if err != nil {
			return mcpserver.Result{}, err
		}
		text := "Reply sent to Argus."
		if result.State == "queued" {
			text = "The reply is queued; Legato sends it to Argus as soon as Argus takes it."
		}
		return mcpserver.Result{Text: fmt.Sprintf("%s (%s)", text, result.ID), Data: map[string]any{"reply_id": result.ID, "state": result.State}}, nil
	})
}
