package service

import (
	"context"
	"github.com/cpave3/legato/internal/engine/hooks"
	"path/filepath"
	"strings"
	"testing"
)

func TestNamedAccountRejectsConfiguredAdapterAuthOverrides(t *testing.T) {
	for _, tier := range []bool{false, true} {
		t.Run(map[bool]string{false: "base", true: "tier"}[tier], func(t *testing.T) {
			svc, mt, _, work := namedAuthFixture(t)
			adapter := svc.AdapterFor("claude-code").(*hooks.ClaudeCodeAdapter)
			opt := AgentSpawnOptions{Account: "work", WorkingDir: work}
			args := []string{"--settings", "synthetic-only"}
			if tier {
				adapter.SetTiers(map[string][]string{"fixture": args})
				opt.Tier = "fixture"
			} else {
				adapter.SetLaunchArgs(args)
			}
			err := svc.SpawnAgent(context.Background(), "auth-source", 0, 0, opt)
			if err == nil || !strings.Contains(err.Error(), "--settings") || strings.Contains(err.Error(), "synthetic-only") {
				t.Fatalf("configured override not refused: %v", err)
			}
			if mt.sessionAlive("legato-auth-source") {
				t.Fatal("configured auth override launched")
			}
		})
	}
}

func TestNamedAccountRejectsManagedFileAuthOverrides(t *testing.T) {
	for _, file := range []string{"managed-settings.json", "managed-settings.d/10-company.json"} {
		t.Run(file, func(t *testing.T) {
			svc, mt, _, work := namedAuthFixture(t)
			policy := t.TempDir()
			svc.(*agentService).claudeManagedSettingsDir = policy
			writeNamedAuthFixture(t, filepath.Join(policy, file), `{"apiKeyHelper":"synthetic-only"}`)
			err := svc.SpawnAgent(context.Background(), "auth-source", 0, 0, AgentSpawnOptions{Account: "work", WorkingDir: work})
			if err == nil || !strings.Contains(err.Error(), "apiKeyHelper") || strings.Contains(err.Error(), "synthetic-only") {
				t.Fatalf("managed override not refused: %v", err)
			}
			if mt.sessionAlive("legato-auth-source") {
				t.Fatal("managed auth override launched")
			}
		})
	}
}
