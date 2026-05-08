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
		Subtasks: []PlanSubtask{
			{Title: "API", Role: "backend", Scope: []string{"api/**"}, Prompt: "build the API"},
		},
	}
}

func TestParsePlanRoundTrip(t *testing.T) {
	yaml := `swarm:
  parent_task_id: abc12345
  working_dir: /tmp/work
  summary: |
    do the thing
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
	if len(p.Subtasks) != 1 {
		t.Fatalf("len(subtasks) = %d", len(p.Subtasks))
	}
	st := p.Subtasks[0]
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
	if err := ValidatePlan(validPlan(), nil, 0); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestValidatePlanMissingParent(t *testing.T) {
	p := validPlan()
	p.Swarm.ParentTaskID = ""
	if err := ValidatePlan(p, nil, 0); err == nil {
		t.Error("expected error for missing parent")
	}
}

func TestValidatePlanMissingWorkingDir(t *testing.T) {
	p := validPlan()
	p.Swarm.WorkingDir = "  "
	if err := ValidatePlan(p, nil, 0); err == nil {
		t.Error("expected error for blank working_dir")
	}
}

func TestValidatePlanNoSubtasks(t *testing.T) {
	p := validPlan()
	p.Subtasks = nil
	if err := ValidatePlan(p, nil, 0); err == nil {
		t.Error("expected error for empty subtasks")
	}
}

func TestValidatePlanMissingTitle(t *testing.T) {
	p := validPlan()
	p.Subtasks[0].Title = ""
	if err := ValidatePlan(p, nil, 0); err == nil {
		t.Error("expected error for missing title")
	}
}

func TestValidatePlanInvalidRole(t *testing.T) {
	p := validPlan()
	p.Subtasks[0].Role = "Backend Specialist"
	err := ValidatePlan(p, nil, 0)
	if err == nil {
		t.Error("expected error for role with spaces")
	}
}

func TestValidatePlanRoleEmptyAccepted(t *testing.T) {
	p := validPlan()
	p.Subtasks[0].Role = ""
	if err := ValidatePlan(p, nil, 0); err != nil {
		t.Errorf("empty role should be allowed, got %v", err)
	}
}

func TestValidatePlanUnknownAgent(t *testing.T) {
	p := validPlan()
	p.Subtasks[0].Agent = "ghost-tool"
	err := ValidatePlan(p, []string{"claude-code", "chimera"}, 0)
	if err == nil {
		t.Error("expected error for unknown agent")
	}
}

func TestValidatePlanAgentSkippedWhenNoAdapters(t *testing.T) {
	p := validPlan()
	p.Subtasks[0].Agent = "ghost-tool"
	if err := ValidatePlan(p, nil, 0); err != nil {
		t.Errorf("agent check should be skipped when adapters empty: %v", err)
	}
}

func TestValidatePlanMalformedScope(t *testing.T) {
	p := validPlan()
	p.Subtasks[0].Scope = []string{"["}
	err := ValidatePlan(p, nil, 0)
	if err == nil {
		t.Error("expected error for malformed glob")
	}
}

func TestValidatePlanExceedsCap(t *testing.T) {
	p := validPlan()
	p.Subtasks = nil
	for i := 0; i < 5; i++ {
		p.Subtasks = append(p.Subtasks, PlanSubtask{Title: "x"})
	}
	err := ValidatePlan(p, nil, 3)
	if err == nil {
		t.Error("expected error for plan over cap")
	}
}

func TestPlanWriteToCanonicalPath(t *testing.T) {
	root := t.TempDir()
	p := validPlan()
	p.Swarm.WorkingDir = root

	canonical, err := p.WriteTo(root, "abc12345")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(canonical, filepath.Join(root, ".legato", "plans")) {
		t.Errorf("canonical path %q not under .legato/plans/", canonical)
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
	p := validPlan()
	canonical, err := p.WriteTo(root, "abc12345")
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadPlan(canonical)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Subtasks[0].Title != "API" {
		t.Errorf("title = %q", loaded.Subtasks[0].Title)
	}
}

func TestLoadPlanFileMissing(t *testing.T) {
	if _, err := LoadPlan("/no/such/file"); err == nil {
		t.Error("expected error for missing file")
	}
}
