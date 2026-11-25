package workflow

import (
	"fmt"

	"gopkg.in/yaml.v3"
)

// The review loop steps (spec LEG-559 § The five steps): trusted steps of
// the review engine that act on one loop record, which github.pr.review.open
// opens and hands to the run's later steps by its ID
// (steps.<open>.outputs.loop_id). What a workflow file may set of a loop is
// fixed here; the engine refuses the rest when the step runs.

// The review loop step types.
const (
	ReviewOpenStepType    = "github.pr.review.open"
	ReviewPassStepType    = "github.pr.review.pass"
	ReviewWaitStepType    = "github.pr.review.wait"
	ReviewConfirmStepType = "github.pr.review.confirm"
	ReviewCloseStepType   = "github.pr.review.close"
)

// MaxReviewLoopPRs is the most pull requests one review loop takes.
const MaxReviewLoopPRs = 20

// reviewLoopField is the loop a step acts on: the open step's loop_id.
var reviewLoopField = Field{Name: "loop", Kind: KindString, Label: "Review loop", Description: "The loop's ID: ${{ steps.<open step>.outputs.loop_id }}", Required: true, Essential: true, Order: 1}

var reviewOpenSpec = ExecutorSpec{
	Type: ReviewOpenStepType, Group: "Code", Title: "Open a review loop",
	Description: "Open a review loop on an open pull request, its stack, or a list of pull requests: Legato's reviewer App reviews them in passes, and the loop holds them so that no other loop reviews them. The run's later review steps act on the loop by its ID. Needs review passes turned on.",
	With: []Field{
		{Name: "repo", Kind: KindRepository, Label: "Repository", Description: "GitHub repository (owner/name)", Required: true, Essential: true, Order: 1},
		{Name: "pr_number", Kind: KindInteger, Label: "Pull request number", Description: "The pull request's number; or give prs", Essential: true, Order: 2},
		{Name: "prs", Kind: KindJSON, Label: "Pull requests", Description: "The pull requests' numbers, bottom of the stack first, at most 20; or give pr_number", Order: 3},
		{Name: "scope", Kind: KindString, Label: "Reviews", Description: "With pr_number, stack: every open pull request of its stack; pr: this pull request only", Default: "stack", Enum: []string{"stack", "pr"}, Checked: true, Invalid: "must be stack or pr", Order: 4},
		{Name: "agent_author", Kind: KindBoolean, Label: "The author's agent fixes findings", Description: "Have the agent of the board task each pull request belongs to fix and answer the findings", Default: false, Checked: true, Invalid: "must be true or false", Essential: true, Order: 5},
		{Name: "reviewer", Kind: KindAgent, Label: "Reviewer", Description: "Agent tool that reviews, or tool:tier, from those this Legato offers; its setting when empty", Order: 6},
		{Name: "skill", Kind: KindString, Label: "Reviewer's skill", Description: "The review skill the reviewer is told to use, from those this Legato offers; its setting when empty", Order: 7},
		{Name: "max_passes", Kind: KindInteger, Label: "Most passes", Description: "Passes before the loop goes stuck; 0 for this Legato's setting", Default: 0, Checked: true, Invalid: "must be 0 (this Legato's setting) or a number of passes", Order: 8},
		{Name: "max_reentries", Kind: KindInteger, Label: "Most re-entries", Description: "Times a stewarded pull request may go back to review after approval; 0 for this Legato's setting, which a workflow may lower but never raise", Default: 0, Checked: true, Invalid: "must be 0 (this Legato's setting) or a number of re-entries", Order: 9},
		{Name: "ready", Kind: KindBoolean, Label: "Mark the stack ready", Description: "Mark the stack's draft pull requests ready once the loop approves them", Default: false, Checked: true, Invalid: "must be true or false", Order: 10},
		{Name: "steward", Kind: KindBoolean, Label: "Look after it until it merges", Description: "After approval, watch an agent-authored pull request until it merges, closes or goes idle, and send its author what GitHub reports; needs agent_author", Default: false, Checked: true, Invalid: "must be true or false", Order: 11},
	},
	// pr_number and prs: exactly one, scope only with pr_number. A file
	// that writes them as values is checked when it loads
	// (checkReviewLoopStep); the step checks what they resolve to when it
	// runs (invalid_subject), since a workflow such as review-loop passes
	// both on from its inputs.
	Constraints: []Constraint{
		{Kind: ExactlyOne, Fields: []string{"pr_number", "prs"}},
		{Kind: Excludes, Fields: []string{"prs", "scope"}},
	},
	Summary: []string{"repo"},
	Outputs: []OutputSpec{
		{Name: "loop_id", Type: "string", Description: "The loop's ID, which the run's later review steps take as loop"},
		{Name: "linked_task", Type: "string", Description: "The board task the pull requests belong to, or empty"},
		{Name: "prs", Type: "json", Description: "The pull requests the loop reviews, bottom first: number, url, branch, base, author_task"},
		{Name: "warnings", Type: "json", Description: "What Legato warns of, such as a reviewer on the author's own tool"},
	},
}

var reviewPassSpec = ExecutorSpec{
	Type: ReviewPassStepType, Group: "Code", Title: "Run a review pass",
	Description: "Run one review pass of a loop: Legato pins every open pull request's head, a read-only reviewer reviews them, and the pass is posted as one GitHub review per pull request from the reviewer App: APPROVE when clean, else COMMENT. A pass whose head changed on GitHub before it posted is pinned again; a reviewer that dies runs the pass once more. When the loop is stuck the step waits with Run another pass, Message the author and End.",
	With:        []Field{reviewLoopField},
	Summary:     []string{"loop"},
	Outputs: []OutputSpec{
		{Name: "verdict", Type: "string", Description: "clean or changes; empty when the loop ended"},
		{Name: "state", Type: "string", Description: "posted; stuck when the operator ended the stuck loop; closed when its pull requests closed"},
		{Name: "pass", Type: "integer", Description: "The pass's number"},
		{Name: "loop_id", Type: "string", Description: "The loop's ID"},
	},
}

var reviewWaitSpec = ExecutorSpec{
	Type: ReviewWaitStepType, Group: "Code", Title: "Wait for the author",
	Description: "Wait while the author works on a pass's findings: Legato tells an agent author of them once it is idle (resuming it when it has stopped), and the wait ends when the author pushes, disputes a blocking finding, or the pull requests close. A person author gets a quiet time after a push.",
	With: []Field{
		reviewLoopField,
		{Name: "mode", Kind: KindString, Label: "Waits for", Description: "author: the author's round", Default: "author", Enum: []string{"author"}, Checked: true, Invalid: "must be author", Order: 2},
	},
	Summary: []string{"loop"},
	Outputs: []OutputSpec{
		{Name: "state", Type: "string", Description: "ready (the author is done), closed, or stuck when the operator ended the stuck loop"},
		{Name: "loop_id", Type: "string", Description: "The loop's ID"},
	},
}

var reviewConfirmSpec = ExecutorSpec{
	Type: ReviewConfirmStepType, Group: "Code", Title: "Confirm the clean pass",
	Description: "Read GitHub's heads after a clean pass: when each is the head the pass reviewed, the loop is approved; when one changed, the loop goes on to a new pass. It does not wait for pending checks.",
	With:        []Field{reviewLoopField},
	Summary:     []string{"loop"},
	Outputs: []OutputSpec{
		{Name: "state", Type: "string", Description: "approved, or moved (another pass is needed); closed or stuck when the loop ended"},
		{Name: "pass", Type: "integer", Description: "The clean pass confirmed, else the loop's last pass"},
		{Name: "confirmed_at", Type: "string", Description: "When the heads were confirmed (RFC 3339), or empty"},
		{Name: "loop_id", Type: "string", Description: "The loop's ID"},
	},
}

var reviewCloseSpec = ExecutorSpec{
	Type: ReviewCloseStepType, Group: "Code", Title: "Close a review loop",
	Description: "Close a review loop and write its summary: its pull requests are free for another loop. A loop also closes by itself when its run ends; closing a closed loop changes nothing.",
	With:        []Field{reviewLoopField},
	Summary:     []string{"loop"},
	Outputs: []OutputSpec{
		{Name: "outcome", Type: "string", Description: "approved, closed, stuck or ended"},
		{Name: "passes", Type: "integer", Description: "The passes posted"},
		{Name: "clean_pass", Type: "integer", Description: "The clean pass that approved the loop, or 0"},
		{Name: "confirmed_at", Type: "string", Description: "When GitHub's heads were confirmed at the clean pass (RFC 3339), or empty"},
		{Name: "findings", Type: "json", Description: "total, blocking, nits and open: the findings of every pass"},
		{Name: "prs", Type: "json", Description: "Each pull request: number, url, state, draft, author_task, approved, head_sha (the confirmed head, else the last pinned) and review_url"},
		{Name: "approval_withheld", Type: "boolean", Description: "true when the loop is approved but a pull request got a COMMENT instead of an APPROVE, because its author is outside the repository"},
		{Name: "summary_path", Type: "string", Description: "A Markdown summary of every pass"},
		{Name: "linked_task", Type: "string", Description: "The board task the pull requests belong to, or empty"},
		{Name: "loop_id", Type: "string", Description: "The loop's ID"},
	},
}

// checkReviewLoopStep checks what a workflow file may not set of a review
// loop (spec LEG-559 § Trust boundaries): the handoff binding, which only
// Legato's handoff service sets, and a steward without an agent author; and
// the shape of prs.
func checkReviewLoopStep(step Step) error {
	if step.Type != ReviewOpenStepType {
		return nil
	}
	literal := func(name string) bool {
		value, ok := step.With[name]
		if !ok {
			return false
		}
		text, isText := value.(string)
		return !isText || !HasExpression(text)
	}
	_, hasNumber := step.With["pr_number"]
	_, hasList := step.With["prs"]
	switch {
	case !hasNumber && !hasList:
		return within("with", fmt.Errorf("with requires pr_number or prs"))
	case literal("pr_number") && literal("prs"):
		return within("with", fmt.Errorf("with takes pr_number or prs, not both"))
	case literal("prs") && literal("scope"):
		return within("with.scope", fmt.Errorf("with scope goes with pr_number: prs names every pull request the loop reviews"))
	}
	if _, ok := step.With["handoff"]; ok {
		return within("with.handoff", fmt.Errorf("with handoff is set by Legato's handoff service, never by a workflow"))
	}
	if steward, _ := step.With["steward"].(bool); steward {
		if author, _ := step.With["agent_author"].(bool); !author {
			if text, ok := step.With["agent_author"].(string); !ok || !HasExpression(text) {
				return within("with.steward", fmt.Errorf("with steward needs agent_author: true: Legato looks after a pull request only for the agent that wrote it"))
			}
		}
	}
	if raw, ok := step.With["prs"]; ok {
		if text, isText := raw.(string); isText && HasExpression(text) {
			return nil
		}
		list, isList := raw.([]any)
		if !isList || len(list) == 0 || len(list) > MaxReviewLoopPRs {
			return within("with.prs", fmt.Errorf("with prs must be a list of at most %d pull request numbers", MaxReviewLoopPRs))
		}
		for _, item := range list {
			switch n := item.(type) {
			case int:
				if n > 0 {
					continue
				}
			case string:
				if HasExpression(n) {
					continue
				}
			}
			return within("with.prs", fmt.Errorf("with prs must be a list of at most %d pull request numbers", MaxReviewLoopPRs))
		}
	}
	for _, name := range []string{"max_passes", "max_reentries"} {
		if n, ok := step.With[name].(int); ok && n < 0 {
			return within("with."+name, fmt.Errorf("with %s must be 0 (this Legato's setting) or more", name))
		}
	}
	return nil
}

// ReviewLoopDiagnostics checks d's review loop steps against this Legato's
// review settings: a max_reentries above the instance's review_passes
// .max_reentries is an error, since a workflow may lower it, never raise
// it. The open step refuses the same when it runs.
func ReviewLoopDiagnostics(data []byte, d *Definition, maxReentries int) []Diagnostic {
	var root yaml.Node
	if d == nil || yaml.Unmarshal(data, &root) != nil {
		return nil
	}
	var out []Diagnostic
	eachStep(d.Steps, "steps", func(step Step, where string, _ []Step, _ map[string]any) {
		if step.Type != ReviewOpenStepType {
			return
		}
		if n, ok := step.With["max_reentries"].(int); ok && n > maxReentries {
			out = append(out, diagnosticAt(&root, where+".with.max_reentries",
				fmt.Sprintf("step %q: max_reentries %d is more than this Legato's review_passes.max_reentries (%d): a workflow may lower it, never raise it", step.ID, n, maxReentries)))
		}
	})
	return out
}
