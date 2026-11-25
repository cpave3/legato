package workflow

import "strings"

// ReviewLoopWorkflowName is the built-in workflow every caller runs a review
// loop through (spec LEG-559 § review-loop; LEG-587): the review loop steps
// strung together with a repeat loop, called with workflow.run. The name is
// reserved: a file may not take it (copy the built-in under another name to
// change it), and a definition of that name never replaces the built-in.
const ReviewLoopWorkflowName = "review-loop"

// reservedWorkflowNames are the built-ins a file may not name itself.
var reservedWorkflowNames = map[string]bool{ReviewLoopWorkflowName: true}

// builtInSourcePrefix marks a definition Legato itself carries.
const builtInSourcePrefix = "builtin:"

// reviewLoopYAML is the built-in review-loop: open, a repeat loop of a pass
// then, when clean, a confirm or, with changes, a wait for the author,
// until approved, closed or stuck; then close. The ready step and the
// steward are later additions (LEG-588, LEG-590).
const reviewLoopYAML = `schema_version: 2
name: review-loop
title: Review loop
run_title: Review loop on ${{ workflow.inputs.repo }}
description: >-
  Review open pull requests in passes, each posted as GitHub reviews from
  Legato's reviewer App, until a clean pass is confirmed at GitHub's heads
  (approved), the pull requests close, or the operator ends a stuck loop.
  Call it with workflow.run; its outputs say how it ended.
inputs:
  repo:
    type: string
    required: true
    description: GitHub repository (owner/name)
  pr_number:
    type: integer
    default: 0
    description: The pull request to review, with its stack (scope); or give prs
  prs:
    type: json
    description: The pull requests to review, bottom of the stack first, at most 20; or give pr_number
  scope:
    type: string
    default: ""
    description: "With pr_number: stack (the default) for every open pull request of its stack, pr for that pull request alone"
  agent_author:
    type: boolean
    default: false
    description: Have the agent of the board task each pull request belongs to fix and answer the findings
  reviewer:
    type: string
    default: ""
    description: Agent tool that reviews, or tool:tier; this Legato's setting when empty
  skill:
    type: string
    default: ""
    description: The review skill the reviewer is told to use; this Legato's setting when empty
  max_passes:
    type: integer
    default: 0
    description: Passes before the loop goes stuck; 0 for this Legato's setting
  max_reentries:
    type: integer
    default: 0
    description: Times a stewarded pull request may go back to review after approval; 0 for this Legato's setting, which it may only lower
outputs:
  outcome: {type: string, value: "${{ steps.close.outputs.outcome }}", description: "approved, closed or stuck (ended when the loop stopped otherwise)"}
  loop_id: {type: string, value: "${{ steps.close.outputs.loop_id }}", description: The review loop's ID}
  passes: {type: integer, value: "${{ steps.close.outputs.passes }}", description: The passes posted}
  clean_pass: {type: integer, value: "${{ steps.close.outputs.clean_pass }}", description: "The clean pass that approved the loop, or 0"}
  confirmed_at: {type: string, value: "${{ steps.close.outputs.confirmed_at }}", description: "When GitHub's heads were confirmed at the clean pass, or empty"}
  findings: {type: json, value: "${{ steps.close.outputs.findings }}", description: "total, blocking, nits and open"}
  prs: {type: json, value: "${{ steps.close.outputs.prs }}", description: "Each pull request: number, url, state, draft, author_task, approved, head_sha and review_url"}
  approval_withheld: {type: boolean, value: "${{ steps.close.outputs.approval_withheld }}", description: "true when a pull request whose author is outside the repository got a COMMENT instead of an APPROVE"}
  summary_path: {type: string, value: "${{ steps.close.outputs.summary_path }}", description: A Markdown summary of every pass}
  linked_task: {type: string, value: "${{ steps.close.outputs.linked_task }}", description: "The board task the pull requests belong to, or empty"}
steps:
  - id: open
    title: Open the review loop
    type: github.pr.review.open
    with:
      repo: ${{ workflow.inputs.repo }}
      pr_number: ${{ workflow.inputs.pr_number }}
      prs: ${{ workflow.inputs.prs }}
      scope: ${{ workflow.inputs.scope }}
      agent_author: ${{ workflow.inputs.agent_author }}
      reviewer: ${{ workflow.inputs.reviewer }}
      skill: ${{ workflow.inputs.skill }}
      max_passes: ${{ workflow.inputs.max_passes }}
      max_reentries: ${{ workflow.inputs.max_reentries }}
  - id: review
    title: Review in passes
    # A failed step fails review-loop, so the caller's workflow.run step
    # fails and its on_failure decides; Retry resumes this loop.
    loop: {repeat: true, max: 100, until: {field: loop.last.state, in: [approved, closed, stuck]}, on_failure: fail}
    outputs:
      state: {type: string, value: "${{ steps.confirm.outputs.state ?? steps.author.outputs.state ?? steps.pass.outputs.state }}"}
    steps:
      - id: pass
        title: Review pass
        type: github.pr.review.pass
        with: {loop: "${{ steps.open.outputs.loop_id }}"}
      - id: confirm
        title: Confirm at GitHub's heads
        if: {field: steps.pass.outputs.verdict, equals: clean}
        type: github.pr.review.confirm
        with: {loop: "${{ steps.open.outputs.loop_id }}"}
      - id: author
        title: Author fixes
        if: {field: steps.pass.outputs.verdict, equals: changes}
        type: github.pr.review.wait
        with: {loop: "${{ steps.open.outputs.loop_id }}", mode: author}
  - id: close
    title: Close the review loop
    type: github.pr.review.close
    with: {loop: "${{ steps.open.outputs.loop_id }}"}
`

// BuiltInReviewLoop is the built-in review-loop, parsed anew each call.
func BuiltInReviewLoop() *Definition {
	definition, err := Parse(builtInSourcePrefix+ReviewLoopWorkflowName, []byte(reviewLoopYAML))
	if err != nil {
		panic("built-in review-loop workflow is invalid: " + err.Error())
	}
	return definition
}

// ReviewLoopYAML is the built-in review-loop's file, for an operator who
// copies it under another name to change it.
func ReviewLoopYAML() string { return reviewLoopYAML }

// reservedNameProblem refuses a file that takes a built-in's reserved name.
func (d *Definition) reservedNameProblem() error {
	if !reservedWorkflowNames[strings.TrimSpace(d.Name)] || strings.HasPrefix(d.Source, builtInSourcePrefix) {
		return nil
	}
	return &reservedNameError{name: d.Name}
}

type reservedNameError struct{ name string }

func (e *reservedNameError) Error() string {
	return e.name + " is Legato's built-in workflow: a file may not take its name. Copy it under another name to change it"
}

// reserved says whether a built-in's name is reserved, so that no definition
// of that name replaces it.
func reserved(name string) bool { return reservedWorkflowNames[name] }

// ReservedWorkflowName says whether a file may not take name: a reserved
// built-in's (review-loop). A copy of it takes a name of its own.
func ReservedWorkflowName(name string) bool { return reserved(strings.TrimSpace(name)) }
