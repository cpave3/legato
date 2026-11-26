package workflow

import (
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

// The inline review step is deprecated (spec LEG-559 § Deprecation, M2, M3,
// A1; LEG-599). It is frozen through v1, with today's inputs and outputs,
// and still runs; a file using it gets a warning, and the upgrade rewrites
// it to a workflow.run step that calls the built-in review-loop.

// ReviewPassesStepType is the deprecated inline review step.
const ReviewPassesStepType = "github.pr.review_passes"

// reviewPassesDeprecation is the warning on each inline review step.
const reviewPassesDeprecation = "github.pr.review_passes is deprecated: call the built-in review-loop with workflow.run instead. `legato workflow upgrade <file>` makes the change"

// deprecatedSteps warns of each inline review step, a loop body's included,
// and offers the upgrade as its fix.
func deprecatedSteps(root *yaml.Node, definition *Definition) []Diagnostic {
	message := reviewPassesDeprecation + "."
	if definition.SchemaVersion == SchemaVersion {
		message = reviewPassesDeprecation + " (it upgrades the file to v2)."
	}
	var diagnostics []Diagnostic
	eachStep(definition.Steps, "steps", func(step Step, where string, _ []Step, _ map[string]any) {
		if step.Type != ReviewPassesStepType {
			return
		}
		d := diagnosticAt(root, where+".type", message)
		d.Severity = SeverityWarning
		d.Fix = FixUpgrade
		diagnostics = append(diagnostics, d)
	})
	return diagnostics
}

// reviewPassesInputs are the inline step's inputs, which review-loop takes
// under the same names and with the same meaning.
var reviewPassesInputs = map[string]bool{"repo": true, "pr_number": true, "agent_author": true, "max_passes": true, "reviewer": true, "scope": true}

// rewriteReviewPasses rewrites an inline review step to call review-loop:
// type workflow.run, its with under with.inputs beside workflow:
// review-loop, and its declared outputs dropped, since a workflow.run
// step's are the child's. An input review-loop does not take is left out,
// with a note. findings, an integer on the inline step, is review-loop's
// findings.total; reviews has no equal.
func rewriteReviewPasses(step *UpgradeStep) {
	step.SetType(WorkflowRunStepType)
	step.WrapWith("workflow", ReviewLoopWorkflowName, "inputs", func(key string) bool {
		if reviewPassesInputs[key] {
			return true
		}
		step.Note(fmt.Sprintf("step %q: review-loop takes no input %q, so the upgrade left it out", step.ID, key))
		return false
	})
	step.Drop("outputs")
	step.MapOutput("findings", "steps."+step.ID+".outputs.findings.total")
	step.Unmapped("reviews", "review-loop has no reviews output (each pull request's review_url is in prs, and the reviews are on the run page)")
}

func init() { stepRewrites[ReviewPassesStepType] = rewriteReviewPasses }

// isBlank says whether text is only spaces.
func isBlank(text string) bool { return strings.TrimSpace(text) == "" }
