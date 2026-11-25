package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/cpave3/legato/internal/engine/mcpserver"
	workflowengine "github.com/cpave3/legato/internal/engine/workflow"
	"github.com/cpave3/legato/internal/service"
)

type draftWorkflowInput struct {
	Reason string `json:"reason" jsonschema:"one line saying what the workflow is for, the approval's title"`
	YAML   string `json:"yaml" jsonschema:"the whole workflow definition, schema_version 2 (a version 1 draft is upgraded), as legato workflow validate takes it"`
}

func (m *mcpTools) draftWorkflow() mcpserver.Tool {
	return mcpserver.NewTool(mcpserver.Definition{
		Name: "draft_workflow", Title: "Draft a workflow and save it paused",
		Description: "Saves a workflow you wrote from the operator's description to their workflows folder, once they approve it. Legato checks it as legato workflow validate does and gives you the errors to fix; then it sets enabled: false on every trigger, so nothing runs until the operator switches it on in the Workflows tab, and proposes the save. A name a workflow has is refused: nothing is overwritten. Read the user manual's using/workflows page for the format, and start from a template where one fits.",
	}, func(ctx context.Context, in draftWorkflowInput) (mcpserver.Result, error) {
		if err := m.mustBeReeve(ctx); err != nil {
			return mcpserver.Result{}, err
		}
		paused, definition, err := workflowengine.PausedDraft([]byte(in.YAML))
		if err != nil {
			return mcpserver.Result{}, fmt.Errorf("the draft does not validate: %w", err)
		}
		if m.opts.Reeve.WorkflowNames != nil {
			for _, name := range m.opts.Reeve.WorkflowNames(ctx) {
				if name == definition.Name {
					return mcpserver.Result{}, fmt.Errorf("a workflow named %q exists; choose another name (drafts never overwrite)", name)
				}
			}
		}
		body, _ := json.Marshal(map[string]string{"yaml": string(paused)})
		reason := strings.TrimSpace(in.Reason)
		if reason == "" {
			reason = "save the workflow " + definition.Name + ", paused"
		}
		return m.propose(ctx, reason, []service.ChangeCall{{Kind: service.CallAPI, Method: "POST", Path: "/api/workflow-drafts", Body: body}})
	})
}
