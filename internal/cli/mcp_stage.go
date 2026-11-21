package cli

import (
	"context"
	"fmt"

	"github.com/cpave3/legato/internal/engine/mcpserver"
	"github.com/cpave3/legato/internal/service"
)

type reportStageInput struct {
	Stage string `json:"stage" jsonschema:"exploring (reading, searching, answering; nothing changed yet), planning (a plan waits on the operator), implementing (changing the code), reviewing (the work is up for review) or finished (the work is done)"`
	Note  string `json:"note,omitempty" jsonschema:"why, as a short phrase the operator sees beside the stage, such as \"PR #12 open\" or \"answered the question\""`
}

// reportStage is the report_stage tool: the agent says where its work
// stands. See docs/ai/agent-states.md § Stages.
func (m *mcpTools) reportStage() mcpserver.Tool {
	return mcpserver.NewTool(mcpserver.Definition{
		Name:  "report_stage",
		Title: "Report where your work stands",
		Description: "Tells Legato the stage of your work, which the operator sees beside you in every list of agents: " +
			"exploring, planning, implementing, reviewing or finished. Report each change: finished when you answered a question that needed no change, " +
			"reviewing when you open a pull request or mark a review tour ready, finished when the work is done. " +
			"Your report wins over what Legato infers, until something later happens to the work.",
	}, func(ctx context.Context, in reportStageInput) (mcpserver.Result, error) {
		session, err := m.session(ctx)
		if err != nil {
			return mcpserver.Result{}, err
		}
		stage, err := service.ParseWorkStage(in.Stage)
		if err != nil {
			return mcpserver.Result{}, err
		}
		if service.IsReeveTask(ctx, m.opts.Store, session.TaskID) {
			return mcpserver.Result{Text: "The Reeve has no stage, so nothing was recorded."}, nil
		}
		if _, err := service.ReportWorkStage(ctx, m.opts.Store, *session, stage, in.Note); err != nil {
			return mcpserver.Result{}, err
		}
		return mcpserver.Result{
			Text: fmt.Sprintf("Your stage is %s.", stage),
			Data: map[string]any{"stage": stage},
		}, nil
	})
}
