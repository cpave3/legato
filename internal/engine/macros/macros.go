// Package macros defines the shared macro type and config-related helpers.
// Macros are persisted in legato's YAML config as a top-level list.
package macros

import "fmt"

// What a macro applies to, when it says.
const (
	AppliesToTask = "task"
	AppliesToPR   = "pr"
)

// Macro is a named piece of text the operator sends to an agent. Sent to a
// running agent from a picker it is typed as it is; started on a task or a
// pull request it may also start the task's agent, with Agent and Tier.
type Macro struct {
	Name string `yaml:"name" json:"name"`
	Keys string `yaml:"keys" json:"keys"`
	// AppliesTo is "task", "pr" or "" (anything).
	AppliesTo string `yaml:"applies_to,omitempty" json:"applies_to,omitempty"`
	// Agent is the agent tool (an adapter's kind) to start; "" the default.
	Agent string `yaml:"agent,omitempty" json:"agent,omitempty"`
	// Tier is the adapter's tier to start with; "" none.
	Tier string `yaml:"tier,omitempty" json:"tier,omitempty"`
}

// Validate checks the fields a macro may leave empty.
func (m Macro) Validate() error {
	switch m.AppliesTo {
	case "", AppliesToTask, AppliesToPR:
	default:
		return fmt.Errorf("macro %q: applies_to must be task or pr, got %q", m.Name, m.AppliesTo)
	}
	return nil
}

// ListResult is the JSON shape returned by GET /api/macros.
type ListResult struct {
	Macros []Macro `json:"macros"`
}
