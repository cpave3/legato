package hooks_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/cpave3/legato/internal/engine/hooks"
)

func TestNativeSessionLaunchRejectsConflictingLaunchAndTierFlags(t *testing.T) {
	cases := []struct {
		kind, flag string
		resumeOnly bool
	}{
		{"claude-code", "--resume", false}, {"claude-code", "-rother", false},
		{"claude-code", "--continue", false}, {"claude-code", "--session-id=other", false},
		{"claude-code", "--fork-session", false}, {"claude-code", "--worktree", true},
		{"claude-code", "-wother", true}, {"claude-code", "--background", true},
		{"codex", "--last", false}, {"codex", "--all", false},
		{"codex", "resume", false}, {"codex", "fork", false},
		{"codex", "--cd=/other", true}, {"codex", "-C/other", true},
		{"codex", "--ephemeral", true}, {"codex", "--remote", true},
	}
	for _, tc := range cases {
		for _, tierArgs := range []bool{false, true} {
			for _, policy := range []string{"resume", "new"} {
				t.Run(tc.kind+"/"+tc.flag+"/"+policy+"/tier="+fmt.Sprint(tierArgs), func(t *testing.T) {
					var validate func(string, string) error
					if tc.kind == "claude-code" {
						a := hooks.NewClaudeCodeAdapter("legato")
						if tierArgs {
							a.SetTiers(map[string][]string{"selected": {tc.flag}})
						} else {
							a.SetLaunchArgs([]string{tc.flag})
						}
						validate = a.ValidateSessionLaunch
					} else {
						a := hooks.NewCodexAdapter("legato")
						if tierArgs {
							a.SetTiers(map[string][]string{"selected": {tc.flag}})
						} else {
							a.SetLaunchArgs([]string{tc.flag})
						}
						validate = a.ValidateSessionLaunch
					}
					err := validate(policy, "selected")
					if tc.resumeOnly && policy == "new" {
						if err != nil {
							t.Fatalf("fresh workspace option rejected: %v", err)
						}
					} else if err == nil || !strings.Contains(err.Error(), tc.flag) {
						t.Fatalf("conflicting flag error = %v", err)
					}
				})
			}
		}
	}
}

func TestNativeSessionLaunchPreservesNormalConfiguration(t *testing.T) {
	claude := hooks.NewClaudeCodeAdapter("legato")
	claude.SetLaunchArgs([]string{"--permission-mode", "acceptEdits"})
	claude.SetTiers(map[string][]string{"selected": {"--model", "selected-model"}})
	codex := hooks.NewCodexAdapter("legato")
	codex.SetLaunchArgs([]string{"-c", "sandbox_mode=workspace-write", "--ask-for-approval", "on-request"})
	codex.SetTiers(map[string][]string{"selected": {"--model", "selected-model"}})
	modelAlias := hooks.NewCodexAdapter("legato")
	modelAlias.SetLaunchArgs([]string{"--model", "resume"})
	if err := modelAlias.ValidateSessionLaunch("new", ""); err != nil {
		t.Fatalf("model alias was mistaken for a subcommand: %v", err)
	}
	for _, policy := range []string{"new", "resume"} {
		if err := claude.ValidateSessionLaunch(policy, "selected"); err != nil {
			t.Fatal(err)
		}
		if err := codex.ValidateSessionLaunch(policy, "selected"); err != nil {
			t.Fatal(err)
		}
		env := map[string]string{"LEGATO_SESSION_POLICY": policy, "LEGATO_RESUME_SESSION_ID": "exact-id"}
		if command := claude.LaunchCommand(env, "", "", "selected"); !strings.Contains(command, "--permission-mode acceptEdits") || !strings.Contains(command, "--model selected-model") {
			t.Fatal(command)
		}
		if command := codex.LaunchCommand(env, "", "", "selected"); !strings.Contains(command, "sandbox_mode=workspace-write") || !strings.Contains(command, "--ask-for-approval on-request") || !strings.Contains(command, "--model selected-model") {
			t.Fatal(command)
		}
	}
}
