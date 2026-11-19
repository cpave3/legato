package workflow

import "fmt"

// The kinds of pull request trigger.
const (
	TriggerPRChangesRequested = "pr.changes_requested"
	TriggerPRReviewRequested  = "pr.review_requested"
)

// PRTrigger starts a workflow on a pull request event: a review that
// requests changes on a PR a task owns (pr.changes_requested), or a review
// requested of the operator (pr.review_requested). Its inputs may read
// ${{ event.repo }}, ${{ event.pr_number }}, ${{ event.pr_url }} and, for
// changes requested, ${{ event.task }} and ${{ event.reviewer }}.
type PRTrigger struct {
	Enabled       *bool          `yaml:"enabled,omitempty" json:"enabled,omitempty"`
	Confirm       bool           `yaml:"confirm,omitempty" json:"confirm,omitempty"`
	MaxRunsPerDay int            `yaml:"max_runs_per_day,omitempty" json:"max_runs_per_day,omitempty"`
	Inputs        map[string]any `yaml:"inputs,omitempty" json:"inputs,omitempty"`
}

// Validate checks the trigger's limit and that its inputs are declared.
func (t *PRTrigger) Validate(kind string, inputs map[string]Input) error {
	if err := validateMaxRunsPerDay(t.MaxRunsPerDay); err != nil {
		return fmt.Errorf("%s %w", kind, err)
	}
	for key := range t.Inputs {
		if _, ok := inputs[key]; !ok {
			return fmt.Errorf("trigger input %q is not declared", key)
		}
	}
	return nil
}

func (t *PRTrigger) common() TriggerCommon {
	return TriggerCommon{Enabled: t.Enabled, Confirm: t.Confirm, MaxRunsPerDay: t.MaxRunsPerDay, Inputs: t.Inputs}
}
