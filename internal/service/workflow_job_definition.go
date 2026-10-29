package service

import (
	workflowengine "github.com/cpave3/legato/internal/engine/workflow"
)

// JobWorkflowName is the built-in workflow a client starts to run one agent
// job with a durable run record.
const JobWorkflowName = "legato-job"

// BuiltInJobWorkflow returns legato-job: prepare the job's checkout, then run
// an agent on the prompt there. The agent stays after it completes the step,
// so the person can read its answer and go on talking to it.
func BuiltInJobWorkflow() *workflowengine.Definition {
	definition := &workflowengine.Definition{
		SchemaVersion: workflowengine.SchemaVersion,
		Name:          JobWorkflowName,
		Title:         "Agent job",
		Description:   "Run one agent on a prompt, in a repository checkout or a directory.",
		Source:        "builtin:" + JobWorkflowName,
		Inputs: map[string]workflowengine.Input{
			"prompt":      {Type: "string", Required: true, Description: "What the agent is asked to do"},
			"repo":        {Type: "string", Default: "", Description: "GitHub repository (owner/name) to check out; empty for none"},
			"ref":         {Type: "string", Default: "", Description: "Branch, tag or commit to check out; the default branch when empty"},
			"agent":       {Type: "string", Default: "", Description: "Agent tool (claude-code, codex, chimera); the default when empty"},
			"working_dir": {Type: "string", Default: "", Description: "Directory the agent works in when no repository is given"},
		},
		Steps: []workflowengine.Step{
			{ID: "checkout", Title: "Prepare checkout", Type: "repo.ensure", With: map[string]any{
				"repo": "${{ workflow.inputs.repo }}", "ref": "${{ workflow.inputs.ref }}", "working_dir": "${{ workflow.inputs.working_dir }}",
			}, Outputs: map[string]workflowengine.Output{"working_dir": {Type: "string"}}},
			{ID: "agent", Title: "Run agent", Type: "agent.run", With: map[string]any{
				"name": "Job", "agent": "${{ workflow.inputs.agent }}", "working_dir": "${{ steps.checkout.outputs.working_dir }}",
				"keep_agent": true,
				"prompt":     "${{ workflow.inputs.prompt }}\n\nWhen you are done, complete the step with a short summary of what you did and found as the summary output.",
			}, Outputs: map[string]workflowengine.Output{"summary": {Type: "string", Description: "What the agent did and found"}}},
		},
	}
	if err := definition.Validate(); err != nil {
		panic("built-in legato-job workflow is invalid: " + err.Error())
	}
	return definition
}
