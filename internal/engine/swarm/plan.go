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
	Swarm PlanHeader `yaml:"swarm" json:"swarm"`
	Steps []PlanStep `yaml:"steps" json:"steps"`
}

// PlanHeader carries the swarm-level fields.
type PlanHeader struct {
	ParentTaskID string `yaml:"parent_task_id" json:"parent_task_id"`
	WorkingDir   string `yaml:"working_dir" json:"working_dir"`
	Summary      string `yaml:"summary" json:"summary"`
}

// PlanStep is a named group of sub-tasks that execute together.
type PlanStep struct {
	Name     string        `yaml:"name" json:"name"`
	Subtasks []PlanSubtask `yaml:"subtasks" json:"subtasks"`
}

// PlanSubtask describes one worker the conductor wants to dispatch.
type PlanSubtask struct {
	Title  string   `yaml:"title" json:"title"`
	Role   string   `yaml:"role,omitempty" json:"role,omitempty"`
	Agent  string   `yaml:"agent,omitempty" json:"agent,omitempty"`
	Tier   string   `yaml:"tier,omitempty" json:"tier,omitempty"`
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

// ValidateOptions configures the plan validation rules. Zero-valued fields
// disable their respective check (e.g. an empty RegisteredAdapters skips the
// adapter-name check; MaxSubtasks == 0 skips the cap).
type ValidateOptions struct {
	// RegisteredAdapters is the set of accepted names for the `agent` field.
	// Empty disables the check (useful for tests that don't wire adapters).
	RegisteredAdapters []string
	// AdapterTiers maps adapter name → the set of tier names configured for
	// that adapter. Used to validate per-sub-task `tier` against the
	// resolved adapter (sub-task `agent` if set, otherwise DefaultAgent).
	// Plans referencing an unknown tier are rejected.
	AdapterTiers map[string]map[string]struct{}
	// DefaultAgent is the adapter name used to resolve `tier` when a sub-task
	// omits `agent`. When empty and a sub-task with `tier` also omits
	// `agent`, validation rejects the sub-task because we can't determine
	// which adapter's tier set to consult.
	DefaultAgent string
	// MaxSubtasks caps the plan size; 0 skips the cap.
	MaxSubtasks int
	// MaxSteps caps the number of steps; 0 skips the cap.
	MaxSteps int
	// AllowMissingWorkingDir permits a plan with an empty working_dir. Used by
	// ExtendApprovedPlan where the working directory is inherited from the
	// existing swarm.
	AllowMissingWorkingDir bool
}

// ValidatePlan returns an error if the plan is malformed.
func ValidatePlan(plan *Plan, opts ValidateOptions) error {
	if plan == nil {
		return fmt.Errorf("plan is nil")
	}
	if strings.TrimSpace(plan.Swarm.ParentTaskID) == "" {
		return fmt.Errorf("swarm.parent_task_id is required")
	}
	if strings.TrimSpace(plan.Swarm.WorkingDir) == "" && !opts.AllowMissingWorkingDir {
		return fmt.Errorf("swarm.working_dir is required")
	}
	if len(plan.Steps) == 0 {
		return fmt.Errorf("plan must contain at least one step")
	}
	if opts.MaxSteps > 0 && len(plan.Steps) > opts.MaxSteps {
		return fmt.Errorf("plan has %d steps; max is %d", len(plan.Steps), opts.MaxSteps)
	}

	adapters := make(map[string]struct{}, len(opts.RegisteredAdapters))
	for _, a := range opts.RegisteredAdapters {
		adapters[a] = struct{}{}
	}

	totalSubtasks := 0
	for si, step := range plan.Steps {
		if len(step.Subtasks) == 0 {
			return fmt.Errorf("step[%d]: must contain at least one sub-task", si)
		}
		if opts.MaxSubtasks > 0 && len(step.Subtasks) > opts.MaxSubtasks {
			return fmt.Errorf("step[%d] has %d sub-tasks; max per step is %d", si, len(step.Subtasks), opts.MaxSubtasks)
		}
		totalSubtasks += len(step.Subtasks)
		for i, st := range step.Subtasks {
			if strings.TrimSpace(st.Title) == "" {
				return fmt.Errorf("step[%d].subtasks[%d]: title is required", si, i)
			}
			if st.Role != "" && !roleLabelPattern.MatchString(st.Role) {
				return fmt.Errorf("step[%d].subtasks[%d]: role %q must match [a-z0-9-]+", si, i, st.Role)
			}
			if st.Agent != "" && len(adapters) > 0 {
				if _, ok := adapters[st.Agent]; !ok {
					return fmt.Errorf("step[%d].subtasks[%d]: agent %q is not a registered adapter", si, i, st.Agent)
				}
			}
			if err := validateTier(st, opts, si, i); err != nil {
				return err
			}
			if err := ValidateScope(st.Scope); err != nil {
				return fmt.Errorf("step[%d].subtasks[%d]: scope: %w", si, i, err)
			}
		}
	}

	if opts.MaxSubtasks > 0 && totalSubtasks > opts.MaxSubtasks {
		return fmt.Errorf("plan has %d sub-tasks; max is %d", totalSubtasks, opts.MaxSubtasks)
	}

	return nil
}

// validateTier ensures a sub-task's `tier` is configured under the adapter
// that will spawn it. The adapter is resolved as st.Agent → opts.DefaultAgent.
// Skipped entirely when AdapterTiers is empty (no tier registry wired) or
// when the sub-task doesn't set `tier`.
func validateTier(st PlanSubtask, opts ValidateOptions, si, i int) error {
	if st.Tier == "" {
		return nil
	}
	if len(opts.AdapterTiers) == 0 {
		return nil
	}
	resolved := st.Agent
	if resolved == "" {
		resolved = opts.DefaultAgent
	}
	if resolved == "" {
		return fmt.Errorf("step[%d].subtasks[%d]: tier %q set but no agent or default_agent to resolve it against", si, i, st.Tier)
	}
	tiers, ok := opts.AdapterTiers[resolved]
	if !ok || len(tiers) == 0 {
		return fmt.Errorf("step[%d].subtasks[%d]: tier %q set but adapter %q has no tiers configured", si, i, st.Tier, resolved)
	}
	if _, ok := tiers[st.Tier]; !ok {
		return fmt.Errorf("step[%d].subtasks[%d]: tier %q is not configured for adapter %q", si, i, st.Tier, resolved)
	}
	return nil
}

// WriteTo serializes the plan to YAML and writes it to a canonical path under
// ~/.legato/plans/<parent>-<unix-ts>.yaml. The workingDir parameter is still
// validated but no longer used to derive the output path.
// Returns the canonical path on success.
func (p *Plan) WriteTo(workingDir, parentTaskID string) (string, error) {
	if workingDir == "" {
		return "", fmt.Errorf("workingDir is required")
	}
	if parentTaskID == "" {
		return "", fmt.Errorf("parentTaskID is required")
	}

	dir, err := PlansDir()
	if err != nil {
		return "", fmt.Errorf("resolve plans dir: %w", err)
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
