package service

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cpave3/legato/config"
	"github.com/cpave3/legato/internal/engine/hooks"
	workflowengine "github.com/cpave3/legato/internal/engine/workflow"
)

func TestWorkflowAgentAccountReachesBothSpawnPlaces(t *testing.T) {
	for _, on := range []string{"run", "LEG-7"} {
		t.Run(on, func(t *testing.T) {
			for _, key := range namedAccountAuthVars {
				t.Setenv(key, "")
			}
			t.Setenv("HOME", t.TempDir())
			t.Setenv("CLAUDE_CONFIG_DIR", "")
			root := filepath.Join(t.TempDir(), "work")
			if err := os.MkdirAll(root, 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(root, ".credentials.json"), []byte(`{"claudeAiOauth":{"accessToken":"fixture-only"}}`), 0600); err != nil {
				t.Fatal(err)
			}
			f := newPlaceFixture(t)
			agents := NewAgentService(f.store, f.tmux, t.TempDir(), AgentServiceOptions{Adapter: hooks.NewClaudeCodeAdapter("legato"), DisablePeerAwareness: true, ClaudeAccounts: []config.ClaudeAccount{{Name: "work", ConfigDir: root}}})
			f.workflows = NewWorkflowService(f.store, []WorkflowExecutor{NewAgentRunExecutor(NewWorkflowAgentRunner(agents, f.store))})
			def, err := workflowengine.Parse("account.yaml", []byte(fmt.Sprintf(`schema_version: 2
name: account
title: Account
inputs:
  account: {type: string, required: true}
steps:
 - id: work
   title: Work
   type: agent.run
   with: {on: %s, prompt: Work, agent: claude-code, account: "${{ workflow.inputs.account }}"}
`, on)))
			if err != nil {
				t.Fatal(err)
			}
			ctx := context.Background()
			view, err := f.workflows.Start(ctx, def, map[string]string{"account": "work"})
			if err != nil {
				t.Fatal(err)
			}
			if view.Run.Status != "running" {
				t.Fatalf("run=%+v attempts=%+v", view.Run, view.Attempts)
			}
			session, err := f.store.GetAgentSessionByTaskID(ctx, view.Attempts[0].TaskID)
			if err != nil {
				t.Fatal(err)
			}
			identity, err := f.store.ExecutionAccount(ctx, session.ExecutionID)
			if err != nil || identity == nil || identity.Account != "work" || identity.ConfigDir != root {
				t.Fatalf("identity=%+v err=%v", identity, err)
			}
			if got := f.tmux.envVarsFor(session.TmuxSession)["CLAUDE_CONFIG_DIR"]; got != root {
				t.Fatalf("launch root=%q", got)
			}
			if !strings.Contains(view.Steps[0].ResolvedInputsJSON, `"account":"work"`) {
				t.Fatalf("remembered inputs=%s", view.Steps[0].ResolvedInputsJSON)
			}
		})
	}
}

func TestWorkflowAgentRejectsNonTextAccountBeforeLaunch(t *testing.T) {
	agents := &fakeWorkflowAgentService{}
	_, err := NewAgentRunExecutor(agents).Start(context.Background(), ResolvedWorkflowStep{TaskID: "run-task", With: map[string]any{"prompt": "work", "account": 42}})
	if err == nil || len(agents.calls) != 0 {
		t.Fatalf("err=%v calls=%v", err, agents.calls)
	}
}

func TestWorkflowMacroAccountReachesBothLaunchPaths(t *testing.T) {
	for _, on := range []string{"run", "local01"} {
		for _, tc := range []struct{ name, macroAgent, agent, account, wantAgent, wantAccount string }{
			{"inherited", "claude-code", "", "", "claude-code", "work"},
			{"account only macro", "", "", "", "claude-code", "work"},
			{"different explicit tool", "claude-code", "codex", "", "codex", ""},
			{"explicit default account", "claude-code", "", "default", "", "default"},
		} {
			t.Run(on+"/"+tc.name, func(t *testing.T) {
				macro, _ := newMacroStartFixture(t)
				if err := macro.Save(context.Background(), "account", config.Macro{Keys: "Work", Agent: tc.macroAgent, Account: "work"}, "test"); err != nil {
					t.Fatal(err)
				}
				runner := &fakePlaceRunner{fakeWorkflowAgentService: &fakeWorkflowAgentService{}}
				executor := NewAgentRunExecutor(runner)
				executor.SetMacros(macro)
				with := map[string]any{"on": on, "macro": "account"}
				if tc.agent != "" {
					with["agent"] = tc.agent
				}
				if tc.account != "" {
					with["account"] = tc.account
				}
				_, err := executor.Start(context.Background(), ResolvedWorkflowStep{TaskID: "run-task", With: with})
				if err != nil {
					t.Fatal(err)
				}
				if runner.opts.Account != tc.wantAccount || runner.opts.AgentKind != tc.wantAgent {
					t.Fatalf("spawn=%+v; want agent=%q account=%q", runner.opts, tc.wantAgent, tc.wantAccount)
				}
			})
		}
	}
}
