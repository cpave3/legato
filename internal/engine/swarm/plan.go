package swarm

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Plan is the YAML structure the conductor submits via `legato swarm propose-plan`.
type Plan struct {
	Swarm    PlanHeader    `yaml:"swarm" json:"swarm"`
	Subtasks []PlanSubtask `yaml:"subtasks" json:"subtasks"`
}

// PlanHeader carries the swarm-level fields.
type PlanHeader struct {
	ParentTaskID string `yaml:"parent_task_id" json:"parent_task_id"`
	WorkingDir   string `yaml:"working_dir" json:"working_dir"`
	Summary      string `yaml:"summary" json:"summary"`
}

// PlanSubtask describes one worker the conductor wants to dispatch.
type PlanSubtask struct {
	Title  string   `yaml:"title" json:"title"`
	Role   string   `yaml:"role,omitempty" json:"role,omitempty"`
	Agent  string   `yaml:"agent,omitempty" json:"agent,omitempty"`
	Scope  []string `yaml:"scope,omitempty" json:"scope,omitempty"`
	Prompt string   `yaml:"prompt,omitempty" json:"prompt,omitempty"`
}

// roleLabelPattern is what we accept for the free-form `role` field.
// Lowercase letters, digits, and dashes — keeps roles legible and shell-safe.
var roleLabelPattern = regexp.MustCompile(`^[a-z0-9-]+$`)

// ParsePlan deserializes YAML bytes into a Plan.
func ParsePlan(data []byte) (*Plan, error) {
	var p Plan
	if err := yaml.Unmarshal(data, &p); err != nil {
		return nil, fmt.Errorf("parse plan: %w", err)
	}
	return &p, nil
}

// LoadPlan reads a plan file from disk and parses it.
func LoadPlan(path string) (*Plan, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read plan %s: %w", path, err)
	}
	return ParsePlan(data)
}

// ValidatePlan returns an error if the plan is malformed. registeredAdapters
// is the set of names accepted for the per-sub-task `agent` field; pass empty
// to skip the adapter-name check (useful for tests that don't wire adapters).
// maxSubtasks caps the plan size; pass 0 to skip the cap.
func ValidatePlan(plan *Plan, registeredAdapters []string, maxSubtasks int) error {
	if plan == nil {
		return fmt.Errorf("plan is nil")
	}
	if strings.TrimSpace(plan.Swarm.ParentTaskID) == "" {
		return fmt.Errorf("swarm.parent_task_id is required")
	}
	if strings.TrimSpace(plan.Swarm.WorkingDir) == "" {
		return fmt.Errorf("swarm.working_dir is required")
	}
	if len(plan.Subtasks) == 0 {
		return fmt.Errorf("plan must contain at least one sub-task")
	}
	if maxSubtasks > 0 && len(plan.Subtasks) > maxSubtasks {
		return fmt.Errorf("plan has %d sub-tasks; max is %d", len(plan.Subtasks), maxSubtasks)
	}

	adapters := make(map[string]struct{}, len(registeredAdapters))
	for _, a := range registeredAdapters {
		adapters[a] = struct{}{}
	}

	for i, st := range plan.Subtasks {
		if strings.TrimSpace(st.Title) == "" {
			return fmt.Errorf("subtasks[%d]: title is required", i)
		}
		if st.Role != "" && !roleLabelPattern.MatchString(st.Role) {
			return fmt.Errorf("subtasks[%d]: role %q must match [a-z0-9-]+", i, st.Role)
		}
		if st.Agent != "" && len(adapters) > 0 {
			if _, ok := adapters[st.Agent]; !ok {
				return fmt.Errorf("subtasks[%d]: agent %q is not a registered adapter", i, st.Agent)
			}
		}
		if err := ValidateScope(st.Scope); err != nil {
			return fmt.Errorf("subtasks[%d]: scope: %w", i, err)
		}
	}

	return nil
}

// WriteTo serializes the plan to YAML and writes it to a canonical path under
// the swarm's working directory: `<workingDir>/.legato/plans/<parent>-<unix-ts>.yaml`.
// Returns the canonical path on success.
func (p *Plan) WriteTo(workingDir, parentTaskID string) (string, error) {
	if workingDir == "" {
		return "", fmt.Errorf("workingDir is required")
	}
	if parentTaskID == "" {
		return "", fmt.Errorf("parentTaskID is required")
	}

	dir := filepath.Join(workingDir, ".legato", "plans")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("create plans dir: %w", err)
	}

	filename := fmt.Sprintf("%s-%d.yaml", parentTaskID, time.Now().Unix())
	canonical := filepath.Join(dir, filename)

	data, err := yaml.Marshal(p)
	if err != nil {
		return "", fmt.Errorf("marshal plan: %w", err)
	}
	if err := os.WriteFile(canonical, data, 0o644); err != nil {
		return "", fmt.Errorf("write plan: %w", err)
	}
	return canonical, nil
}
