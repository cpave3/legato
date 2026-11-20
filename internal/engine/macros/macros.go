// Package macros defines the shared macro type and config-related helpers.
// Macros are persisted in legato's YAML config as a top-level list.
package macros

import (
	"encoding/json"
	"fmt"

	"gopkg.in/yaml.v3"
)

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
	// Keys is the message the macro sends. Config and the API also take it
	// as "message", the name the web UI shows; it is written as "keys".
	Keys string `yaml:"keys" json:"keys"`
	// AppliesTo is "task", "pr" or "" (anything).
	AppliesTo string `yaml:"applies_to,omitempty" json:"applies_to,omitempty"`
	// Agent is the agent tool (an adapter's kind) to start; "" the default.
	Agent string `yaml:"agent,omitempty" json:"agent,omitempty"`
	// Tier is the adapter's tier to start with; "" none.
	Tier string `yaml:"tier,omitempty" json:"tier,omitempty"`
}

// macroFields is Macro as it is read, with message beside keys.
type macroFields struct {
	Name      string `yaml:"name" json:"name"`
	Keys      string `yaml:"keys" json:"keys"`
	Message   string `yaml:"message" json:"message"`
	AppliesTo string `yaml:"applies_to" json:"applies_to"`
	Agent     string `yaml:"agent" json:"agent"`
	Tier      string `yaml:"tier" json:"tier"`
}

func (f macroFields) macro() (Macro, error) {
	keys := f.Keys
	if f.Message != "" {
		if keys != "" && keys != f.Message {
			return Macro{}, fmt.Errorf("macro %q: keys and message differ; give one of them", f.Name)
		}
		keys = f.Message
	}
	return Macro{Name: f.Name, Keys: keys, AppliesTo: f.AppliesTo, Agent: f.Agent, Tier: f.Tier}, nil
}

// UnmarshalYAML reads a macro, its message as keys or message.
func (m *Macro) UnmarshalYAML(node *yaml.Node) error {
	var f macroFields
	if err := node.Decode(&f); err != nil {
		return err
	}
	out, err := f.macro()
	if err != nil {
		return err
	}
	*m = out
	return nil
}

// UnmarshalJSON reads a macro, its message as keys or message.
func (m *Macro) UnmarshalJSON(data []byte) error {
	var f macroFields
	if err := json.Unmarshal(data, &f); err != nil {
		return err
	}
	out, err := f.macro()
	if err != nil {
		return err
	}
	*m = out
	return nil
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
