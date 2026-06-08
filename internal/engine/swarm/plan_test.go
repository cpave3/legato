package swarm

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func validPlan() *Plan {
	return &Plan{
		Swarm: PlanHeader{
			ParentTaskID: "abc12345",
			WorkingDir:   "/tmp/work",
			Summary:      "do the thing",
		},
		Steps: []PlanStep{
			{
				Name: "Step 1",
				Subtasks: []PlanSubtask{
					{Title: "API", Role: "backend", Scope: []string{"api/**"}, Prompt: "build the API"},
				},
			},
		},
	}
}

func TestParsePlanRoundTrip(t *testing.T) {
	yaml := `swarm:
  parent_task_id: abc12345
  working_dir: /tmp/work
  summary: |
    do the thing
steps:
  - name: "Step 1"
    subtasks:
      - title: API
        role: backend
        agent: claude-code
        scope:
          - api/**
        prompt: |
          build the API
`
	p, err := ParsePlan([]byte(yaml))
	if err != nil {
		t.Fatal(err)
	}
	if p.Swarm.ParentTaskID != "abc12345" {
		t.Errorf("parent = %q", p.Swarm.ParentTaskID)
	}
	if len(p.Steps) != 1 {
		t.Fatalf("len(steps) = %d", len(p.Steps))
	}
	if p.Steps[0].Name != "Step 1" {
		t.Errorf("step name = %q", p.Steps[0].Name)
	}
	if len(p.Steps[0].Subtasks) != 1 {
		t.Fatalf("len(subtasks) = %d", len(p.Steps[0].Subtasks))
	}
	st := p.Steps[0].Subtasks[0]
	if st.Title != "API" || st.Role != "backend" || st.Agent != "claude-code" {
		t.Errorf("unexpected subtask: %+v", st)
	}
	if len(st.Scope) != 1 || st.Scope[0] != "api/**" {
		t.Errorf("scope = %v", st.Scope)
	}
}

func TestParsePlanInvalidYAML(t *testing.T) {
	if _, err := ParsePlan([]byte("not: valid: yaml: ::")); err == nil {
		t.Error("expected parse error")
	}
}

func TestValidatePlanHappyPath(t *testing.T) {
	if err := ValidatePlan(validPlan(), ValidateOptions{}); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestValidatePlanMissingParent(t *testing.T) {
	p := validPlan()
	p.Swarm.ParentTaskID = ""
	if err := ValidatePlan(p, ValidateOptions{}); err == nil {
		t.Error("expected error for missing parent")
	}
}

func TestValidatePlanMissingWorkingDir(t *testing.T) {
	p := validPlan()
	p.Swarm.WorkingDir = "  "
	if err := ValidatePlan(p, ValidateOptions{}); err == nil {
		t.Error("expected error for blank working_dir")
	}
}

func TestValidatePlanNoSteps(t *testing.T) {
	p := validPlan()
	p.Steps = nil
	if err := ValidatePlan(p, ValidateOptions{}); err == nil {
		t.Error("expected error for empty steps")
	}
}

func TestValidatePlanEmptyStepSubtasks(t *testing.T) {
	p := validPlan()
	p.Steps[0].Subtasks = nil
	if err := ValidatePlan(p, ValidateOptions{}); err == nil {
		t.Error("expected error for step with empty subtasks")
	}
}

func TestValidatePlanMissingTitle(t *testing.T) {
	p := validPlan()
	p.Steps[0].Subtasks[0].Title = ""
	if err := ValidatePlan(p, ValidateOptions{}); err == nil {
		t.Error("expected error for missing title")
	}
}

func TestValidatePlanInvalidRole(t *testing.T) {
	p := validPlan()
	p.Steps[0].Subtasks[0].Role = "Backend Specialist"
	err := ValidatePlan(p, ValidateOptions{})
	if err == nil {
		t.Error("expected error for role with spaces")
	}
}

func TestValidatePlanRoleEmptyAccepted(t *testing.T) {
	p := validPlan()
	p.Steps[0].Subtasks[0].Role = ""
	if err := ValidatePlan(p, ValidateOptions{}); err != nil {
		t.Errorf("empty role should be allowed, got %v", err)
	}
}

func TestValidatePlanUnknownAgent(t *testing.T) {
	p := validPlan()
	p.Steps[0].Subtasks[0].Agent = "ghost-tool"
	err := ValidatePlan(p, ValidateOptions{RegisteredAdapters: []string{"claude-code", "chimera", "codex"}})
	if err == nil {
		t.Error("expected error for unknown agent")
	}
}

func TestValidatePlanAgentSkippedWhenNoAdapters(t *testing.T) {
	p := validPlan()
	p.Steps[0].Subtasks[0].Agent = "ghost-tool"
	if err := ValidatePlan(p, ValidateOptions{}); err != nil {
		t.Errorf("agent check should be skipped when adapters empty: %v", err)
	}
}

func TestValidatePlanMalformedScope(t *testing.T) {
	p := validPlan()
	p.Steps[0].Subtasks[0].Scope = []string{"["}
	err := ValidatePlan(p, ValidateOptions{})
	if err == nil {
		t.Error("expected error for malformed glob")
	}
}

func TestValidatePlanExceedsPerStepCap(t *testing.T) {
	p := validPlan()
	p.Steps[0].Subtasks = nil
	for i := 0; i < 5; i++ {
		p.Steps[0].Subtasks = append(p.Steps[0].Subtasks, PlanSubtask{Title: "x"})
	}
	err := ValidatePlan(p, ValidateOptions{MaxSubtasks: 3})
	if err == nil {
		t.Error("expected error for step over cap")
	}
	if !strings.Contains(err.Error(), "per step") {
		t.Errorf("expected per-step error, got: %v", err)
	}
}

func TestValidatePlanExceedsTotalCap(t *testing.T) {
	p := validPlan()
	p.Steps = []PlanStep{
		{Name: "Step 1", Subtasks: []PlanSubtask{{Title: "a"}, {Title: "b"}}},
		{Name: "Step 2", Subtasks: []PlanSubtask{{Title: "c"}, {Title: "d"}}},
	}
	err := ValidatePlan(p, ValidateOptions{MaxSubtasks: 3})
	if err == nil {
		t.Error("expected error for total subtasks over cap")
	}
	if !strings.Contains(err.Error(), "max is") {
		t.Errorf("expected total cap error, got: %v", err)
	}
}

func TestValidatePlanMultiStepHappyPath(t *testing.T) {
	p := validPlan()
	p.Steps = []PlanStep{
		{Name: "Setup", Subtasks: []PlanSubtask{{Title: "Init"}}},
		{Name: "Build", Subtasks: []PlanSubtask{{Title: "API"}, {Title: "UI"}}},
	}
	if err := ValidatePlan(p, ValidateOptions{}); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestValidatePlanExceedsMaxSteps(t *testing.T) {
	p := validPlan()
	p.Steps = []PlanStep{
		{Name: "S1", Subtasks: []PlanSubtask{{Title: "a"}}},
		{Name: "S2", Subtasks: []PlanSubtask{{Title: "b"}}},
	}
	err := ValidatePlan(p, ValidateOptions{MaxSteps: 1})
	if err == nil {
		t.Fatal("expected error when steps exceed maxSteps")
	}
	if !strings.Contains(err.Error(), "max is 1") {
		t.Errorf("expected max cap error, got: %v", err)
	}
}

func TestPlanWriteToCanonicalPath(t *testing.T) {
	root := t.TempDir()
	t.Setenv("LEGATO_HOME", root)
	p := validPlan()
	p.Swarm.WorkingDir = root

	canonical, err := p.WriteTo(root, "abc12345")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(canonical, filepath.Join(root, "plans")) {
		t.Errorf("canonical path %q not under plans/", canonical)
	}
	if _, err := os.Stat(canonical); err != nil {
		t.Errorf("plan file not on disk: %v", err)
	}
	data, _ := os.ReadFile(canonical)
	loaded, err := ParsePlan(data)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Swarm.ParentTaskID != "abc12345" {
		t.Errorf("roundtrip parent = %q", loaded.Swarm.ParentTaskID)
	}
	if len(loaded.Steps) != 1 || loaded.Steps[0].Name != "Step 1" {
		t.Errorf("roundtrip steps = %+v", loaded.Steps)
	}
	if len(loaded.Steps[0].Subtasks) != 1 {
		t.Errorf("roundtrip subtask count = %d", len(loaded.Steps[0].Subtasks))
	}
}

func TestPlanWriteToRequiresArgs(t *testing.T) {
	p := validPlan()
	if _, err := p.WriteTo("", "abc"); err == nil {
		t.Error("expected error for empty workingDir")
	}
	if _, err := p.WriteTo(t.TempDir(), ""); err == nil {
		t.Error("expected error for empty parentTaskID")
	}
}

func TestLoadPlanFromDisk(t *testing.T) {
	root := t.TempDir()
	t.Setenv("LEGATO_HOME", root)
	p := validPlan()
	canonical, err := p.WriteTo(root, "abc12345")
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadPlan(canonical)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Steps[0].Subtasks[0].Title != "API" {
		t.Errorf("title = %q", loaded.Steps[0].Subtasks[0].Title)
	}
}

func TestLoadPlanFileMissing(t *testing.T) {
	if _, err := LoadPlan("/no/such/file"); err == nil {
		t.Error("expected error for missing file")
	}
}

func TestParsePlanIncludesTier(t *testing.T) {
	yaml := `swarm:
  parent_task_id: abc12345
  working_dir: /tmp/work
steps:
  - name: "Step 1"
    subtasks:
      - title: API
        agent: claude-code
        tier: small
`
	p, err := ParsePlan([]byte(yaml))
	if err != nil {
		t.Fatal(err)
	}
	if got := p.Steps[0].Subtasks[0].Tier; got != "small" {
		t.Errorf("tier = %q, want small", got)
	}
}

func tierRegistry(adapterTiers map[string][]string) map[string]map[string]struct{} {
	out := make(map[string]map[string]struct{}, len(adapterTiers))
	for adapter, tiers := range adapterTiers {
		set := make(map[string]struct{}, len(tiers))
		for _, t := range tiers {
			set[t] = struct{}{}
		}
		out[adapter] = set
	}
	return out
}

func TestValidatePlanTierKnownAccepted(t *testing.T) {
	p := validPlan()
	p.Steps[0].Subtasks[0].Agent = "claude-code"
	p.Steps[0].Subtasks[0].Tier = "small"
	opts := ValidateOptions{
		RegisteredAdapters: []string{"claude-code"},
		AdapterTiers:       tierRegistry(map[string][]string{"claude-code": {"small", "large"}}),
	}
	if err := ValidatePlan(p, opts); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestValidatePlanTierUnknownRejected(t *testing.T) {
	p := validPlan()
	p.Steps[0].Subtasks[0].Agent = "claude-code"
	p.Steps[0].Subtasks[0].Tier = "ghost"
	opts := ValidateOptions{
		RegisteredAdapters: []string{"claude-code"},
		AdapterTiers:       tierRegistry(map[string][]string{"claude-code": {"small", "large"}}),
	}
	err := ValidatePlan(p, opts)
	if err == nil {
		t.Fatal("expected error for unknown tier")
	}
	if !strings.Contains(err.Error(), "tier \"ghost\"") {
		t.Errorf("error should mention the bad tier: %v", err)
	}
}

func TestValidatePlanTierEmptyAlwaysAccepted(t *testing.T) {
	p := validPlan()
	p.Steps[0].Subtasks[0].Agent = "claude-code"
	p.Steps[0].Subtasks[0].Tier = ""
	opts := ValidateOptions{
		RegisteredAdapters: []string{"claude-code"},
		AdapterTiers:       tierRegistry(map[string][]string{"claude-code": {"small"}}),
	}
	if err := ValidatePlan(p, opts); err != nil {
		t.Errorf("empty tier should always pass, got %v", err)
	}
}

func TestValidatePlanTierFallsBackToDefaultAgent(t *testing.T) {
	p := validPlan()
	// Sub-task omits agent entirely; tier should be resolved against DefaultAgent.
	p.Steps[0].Subtasks[0].Agent = ""
	p.Steps[0].Subtasks[0].Tier = "large"
	opts := ValidateOptions{
		RegisteredAdapters: []string{"claude-code"},
		DefaultAgent:       "claude-code",
		AdapterTiers:       tierRegistry(map[string][]string{"claude-code": {"small", "large"}}),
	}
	if err := ValidatePlan(p, opts); err != nil {
		t.Errorf("unexpected error when default agent provides the tier: %v", err)
	}
}

func TestValidatePlanTierRejectedWhenAdapterHasNoTiers(t *testing.T) {
	p := validPlan()
	p.Steps[0].Subtasks[0].Agent = "chimera"
	p.Steps[0].Subtasks[0].Tier = "small"
	opts := ValidateOptions{
		RegisteredAdapters: []string{"claude-code", "chimera", "codex"},
		AdapterTiers:       tierRegistry(map[string][]string{"claude-code": {"small"}}),
	}
	err := ValidatePlan(p, opts)
	if err == nil {
		t.Fatal("expected error when adapter has no tiers configured")
	}
	if !strings.Contains(err.Error(), "no tiers configured") {
		t.Errorf("expected 'no tiers configured' error, got: %v", err)
	}
}

func TestValidatePlanTierNoAgentNoDefault(t *testing.T) {
	// Tier set, registry non-empty, but neither sub-task `Agent` nor
	// `DefaultAgent` is provided — validation cannot resolve which adapter's
	// tier set to consult and must reject.
	p := validPlan()
	p.Steps[0].Subtasks[0].Agent = ""
	p.Steps[0].Subtasks[0].Tier = "small"
	opts := ValidateOptions{
		AdapterTiers: tierRegistry(map[string][]string{"claude-code": {"small"}}),
	}
	err := ValidatePlan(p, opts)
	if err == nil {
		t.Fatal("expected error when tier set but no agent or default_agent")
	}
	if !strings.Contains(err.Error(), "no agent or default_agent") {
		t.Errorf("expected error to mention missing agent/default_agent, got: %v", err)
	}
}

func TestValidatePlanTierSkippedWhenRegistryEmpty(t *testing.T) {
	// Tests that don't wire AdapterTiers can still call ValidatePlan
	// without tripping over a `tier` field.
	p := validPlan()
	p.Steps[0].Subtasks[0].Tier = "anything"
	if err := ValidatePlan(p, ValidateOptions{}); err != nil {
		t.Errorf("tier check should be skipped when AdapterTiers empty, got: %v", err)
	}
}

func TestValidatePlanMissingWorkingDirRejectedByDefault(t *testing.T) {
	p := validPlan()
	p.Swarm.WorkingDir = ""
	err := ValidatePlan(p, ValidateOptions{})
	if err == nil {
		t.Fatal("expected error for missing working_dir without AllowMissingWorkingDir")
	}
	if !strings.Contains(err.Error(), "working_dir is required") {
		t.Errorf("expected 'working_dir is required' error, got: %v", err)
	}
}

func TestValidatePlanMissingWorkingDirAllowedWithOption(t *testing.T) {
	p := validPlan()
	p.Swarm.WorkingDir = ""
	if err := ValidatePlan(p, ValidateOptions{AllowMissingWorkingDir: true}); err != nil {
		t.Fatalf("unexpected error with AllowMissingWorkingDir: %v", err)
	}
}
