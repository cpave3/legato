package hooks

import (
	"strings"
	"testing"
)

func TestBuiltinRolePromptConductor(t *testing.T) {
	got := builtinRolePrompt("conductor")
	if got == "" {
		t.Error("conductor prompt empty")
	}
	if !strings.Contains(got, "conductor") {
		t.Error("conductor prompt missing expected keyword")
	}
	if !strings.Contains(got, "legato task show $LEGATO_TASK_ID") {
		t.Error("conductor prompt missing task context lookup guidance")
	}
}

func TestBuiltinRolePromptWorkerFallback(t *testing.T) {
	// Free-form labels fall back to the worker brief.
	for _, role := range []string{"backend", "frontend", "migrations", "anything-else"} {
		got := builtinRolePrompt(role)
		if got == "" {
			t.Errorf("role %q returned empty prompt", role)
		}
		if !strings.Contains(got, "worker") {
			t.Errorf("role %q didn't fall back to worker prompt", role)
		}
		if !strings.Contains(got, "legato task show $LEGATO_TASK_ID") {
			t.Errorf("role %q worker prompt missing task context lookup guidance", role)
		}
	}
}

func TestBuiltinRolePromptEmptyRole(t *testing.T) {
	if got := builtinRolePrompt(""); got != "" {
		t.Errorf("empty role should return empty prompt, got %q", got)
	}
}

func TestResolveRolePromptOverrideTakesPrecedence(t *testing.T) {
	overrides := RolePromptOverrides{"backend": "OVERRIDE"}
	if got := resolveRolePrompt(overrides, "backend"); got != "OVERRIDE" {
		t.Errorf("resolveRolePrompt = %q, want OVERRIDE", got)
	}
	// Unknown role + no matching override → falls back to worker prompt.
	if got := resolveRolePrompt(overrides, "frontend"); got == "" || !strings.Contains(got, "worker") {
		t.Errorf("frontend without override should yield worker fallback, got %q", got)
	}
}

func TestClaudeAdapterImplementsRolePrompts(t *testing.T) {
	a := NewClaudeCodeAdapter("/usr/bin/legato")
	if got := a.RoleSystemPrompt("conductor"); got == "" {
		t.Error("ClaudeCode conductor prompt empty")
	}
	if got := a.RoleSystemPrompt("backend"); got == "" {
		t.Error("ClaudeCode worker fallback empty")
	}
	a.SetRoleOverrides(RolePromptOverrides{"backend": "X"})
	if got := a.RoleSystemPrompt("backend"); got != "X" {
		t.Errorf("override = %q, want X", got)
	}
}

func TestChimeraAdapterImplementsRolePrompts(t *testing.T) {
	a := NewChimeraAdapter("/usr/bin/legato")
	if got := a.RoleSystemPrompt("conductor"); got == "" {
		t.Error("Chimera conductor prompt empty")
	}
	if got := a.RoleSystemPrompt("backend"); got == "" {
		t.Error("Chimera worker fallback empty")
	}
}

func TestClaudeAdapterLaunchCommand(t *testing.T) {
	a := NewClaudeCodeAdapter("/usr/bin/legato")

	got := a.LaunchCommand(map[string]string{"LEGATO_ROLE_PROMPT_FILE": "/tmp/role.md"}, "", "")
	if !strings.Contains(got, "claude") {
		t.Errorf("expected claude in launch command, got %q", got)
	}
	if !strings.Contains(got, "--append-system-prompt") {
		t.Errorf("expected --append-system-prompt flag, got %q", got)
	}
	if !strings.Contains(got, "$LEGATO_ROLE_PROMPT_FILE") {
		t.Errorf("expected file path env var reference, got %q", got)
	}
	if !strings.Contains(got, "cat ") {
		t.Errorf("expected `cat` substitution, got %q", got)
	}

	// Without LEGATO_ROLE_PROMPT_FILE in env — bare claude launch.
	if got := a.LaunchCommand(map[string]string{}, "", ""); got != "claude" {
		t.Errorf("expected bare claude command without role prompt file env, got %q", got)
	}

	// Nil env returns empty.
	if got := a.LaunchCommand(nil, "", ""); got != "" {
		t.Errorf("expected empty command for nil env, got %q", got)
	}
}

func TestChimeraAdapterLaunchCommand(t *testing.T) {
	a := NewChimeraAdapter("/usr/bin/legato")

	got := a.LaunchCommand(map[string]string{"LEGATO_ROLE_PROMPT_FILE": "/tmp/role.md"}, "", "")
	if !strings.Contains(got, "chimera") {
		t.Errorf("expected chimera in launch command, got %q", got)
	}
	if !strings.Contains(got, "--prompt") {
		t.Errorf("expected --prompt flag, got %q", got)
	}
	if !strings.Contains(got, "$LEGATO_ROLE_PROMPT_FILE") {
		t.Errorf("expected file path env var reference, got %q", got)
	}

	if got := a.LaunchCommand(map[string]string{}, "", ""); got != "chimera" {
		t.Errorf("expected bare chimera command without role prompt file env, got %q", got)
	}
}

func TestClaudeAdapterLaunchCommandWithTier(t *testing.T) {
	a := NewClaudeCodeAdapter("/usr/bin/legato")
	a.SetLaunchArgs([]string{"--dangerously-skip-permissions"})
	a.SetTiers(map[string][]string{
		"small": {"--model", "claude-haiku-4-5"},
		"large": {"--model", "claude-opus-4-7"},
	})

	got := a.LaunchCommand(map[string]string{"LEGATO_ROLE_PROMPT_FILE": "/tmp/role.md"}, "", "small")
	if !strings.Contains(got, "--dangerously-skip-permissions") {
		t.Errorf("expected base launch_args, got %q", got)
	}
	if !strings.Contains(got, "--model claude-haiku-4-5") {
		t.Errorf("expected tier args, got %q", got)
	}

	// Tier args must come AFTER base args so a tier-specified flag wins.
	baseIdx := strings.Index(got, "--dangerously-skip-permissions")
	tierIdx := strings.Index(got, "--model")
	if baseIdx == -1 || tierIdx == -1 || tierIdx <= baseIdx {
		t.Errorf("expected tier args to follow base args, got %q", got)
	}

	// Empty tier → no tier args.
	got = a.LaunchCommand(map[string]string{"LEGATO_ROLE_PROMPT_FILE": "/tmp/role.md"}, "", "")
	if strings.Contains(got, "--model") {
		t.Errorf("empty tier should not inject --model, got %q", got)
	}

	// Unknown tier → no tier args (validation rejects unknown tiers earlier;
	// the adapter just looks up by name and treats a miss as a no-op).
	got = a.LaunchCommand(map[string]string{"LEGATO_ROLE_PROMPT_FILE": "/tmp/role.md"}, "", "ghost")
	if strings.Contains(got, "--model") {
		t.Errorf("unknown tier should not inject anything, got %q", got)
	}
}

func TestChimeraAdapterLaunchCommandWithTier(t *testing.T) {
	a := NewChimeraAdapter("/usr/bin/legato")
	a.SetLaunchArgs([]string{"--sandbox"})
	a.SetTiers(map[string][]string{
		"quick": {"--model", "haiku"},
	})

	got := a.LaunchCommand(map[string]string{"LEGATO_ROLE_PROMPT_FILE": "/tmp/role.md"}, "", "quick")
	if !strings.Contains(got, "--sandbox") {
		t.Errorf("expected base launch_args, got %q", got)
	}
	if !strings.Contains(got, "--model haiku") {
		t.Errorf("expected tier args, got %q", got)
	}

	got = a.LaunchCommand(map[string]string{"LEGATO_ROLE_PROMPT_FILE": "/tmp/role.md"}, "", "")
	if strings.Contains(got, "--model") {
		t.Errorf("empty tier should not inject --model, got %q", got)
	}
}
