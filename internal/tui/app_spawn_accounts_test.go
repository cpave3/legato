package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/cpave3/legato/config"
	"github.com/cpave3/legato/internal/service"
	"github.com/cpave3/legato/internal/tui/overlay"
)

func TestSpawnOverlayAccountsReachTaskAndEphemeralServices(t *testing.T) {
	for _, task := range []string{"TASK-1", ""} {
		t.Run(task, func(t *testing.T) {
			svc := &fakeAgentService{defaultAdapter: "claude-code"}
			app := newTestApp()
			app.agentSvc = svc
			app.workDir = "/work"
			app.SetClaudeAccounts([]config.ClaudeAccount{{Name: "work", Label: "Work subscription", ConfigDir: "/secret/account-root"}})
			model, _ := app.openAgentSpawnOverlay(task, "Task")
			app = model.(App)
			if strings.Contains(app.activeOverlay.View(), "/secret/account-root") {
				t.Fatal("account path displayed")
			}
			if task == "" {
				app.activeOverlay, _ = app.activeOverlay.Update(tea.KeyMsg{Type: tea.KeyTab})
			}
			// Choose explicit Claude, then Work on its account row.
			for _, key := range []tea.KeyMsg{{Type: tea.KeyRight}, {Type: tea.KeyTab}, {Type: tea.KeyRight}} {
				app.activeOverlay, _ = app.activeOverlay.Update(key)
			}
			if !strings.Contains(app.activeOverlay.View(), "Work subscription") {
				t.Fatal("configured label missing")
			}
			_, submit := app.activeOverlay.Update(tea.KeyMsg{Type: tea.KeyEnter})
			_, cmd := app.Update(submit())
			runSpawnCommand(t, cmd)
			if svc.lastSpawnOpts == nil || svc.lastSpawnOpts.Account != "work" || svc.lastSpawnOpts.AgentKind != "claude-code" {
				t.Fatalf("spawn opts = %+v", svc.lastSpawnOpts)
			}
		})
	}
}

func TestSpawnAccountPolicyRemainsUnspecifiedOnResumeAndFreshStart(t *testing.T) {
	for _, choice := range []string{"resume", "new", "ephemeral"} {
		t.Run(choice, func(t *testing.T) {
			svc := &fakeAgentService{defaultAdapter: "claude-code", resumeSession: &service.TaskResumeSession{ID: "saved", AgentKind: "claude-code", CWD: "/saved"}}
			app := newTestApp()
			app.agentSvc = svc
			app.workDir = "/work"
			app.SetClaudeAccounts([]config.ClaudeAccount{{Name: "work"}})
			task := "TASK-1"
			if choice == "ephemeral" {
				task = ""
			}
			model, _ := app.openAgentSpawnOverlay(task, "Task")
			app = model.(App)
			_, submit := app.activeOverlay.Update(tea.KeyMsg{Type: tea.KeyEnter})
			got := submit().(overlay.AgentSpawnSubmitMsg)
			if got.Account != "" || got.AgentKind != "" {
				t.Fatalf("policy materialized too early: %+v", got)
			}
			model, cmd := app.Update(got)
			app = model.(App)
			if choice != "ephemeral" {
				key := tea.KeyMsg{Type: tea.KeyEnter}
				if choice == "new" {
					key = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}}
				}
				_, choose := app.activeOverlay.Update(key)
				_, cmd = app.Update(choose())
			}
			runSpawnCommand(t, cmd)
			if svc.lastSpawnOpts == nil || svc.lastSpawnOpts.Account != "" {
				t.Fatalf("account no longer implicit: %+v", svc.lastSpawnOpts)
			}
			if choice == "resume" && svc.lastSpawnOpts.SessionPolicy != "resume" {
				t.Fatal("resume policy missing")
			}
			if choice != "resume" && svc.lastSpawnOpts.AgentKind != "" {
				t.Fatal("fresh policy overridden")
			}
		})
	}
}
