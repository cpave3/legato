package service

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cpave3/legato/internal/engine/hooks"
	"github.com/cpave3/legato/internal/engine/sandbox"
)

func TestChimeraSandboxLaunchKeepsBootstrapOutsideAgent(t *testing.T) {
	for _, prompt := range []string{"", "a task prompt"} {
		t.Run(map[bool]string{true: "prompt", false: "no-prompt"}[prompt != ""], func(t *testing.T) {
			t.Setenv("LEGATO_HOME", t.TempDir())
			backend := &fakeBackend{}
			svc, s, mt := sandboxedService(t, backend)
			adapter := hooks.NewChimeraAdapter("legato")
			adapter.SetModDir(t.TempDir())
			agent := svc.(*agentService)
			agent.adapter = adapter
			agent.adapters["chimera"] = adapter
			createTask(t, s, "bootstrap")
			ctx := context.Background()
			if err := svc.SpawnAgent(ctx, "bootstrap", 0, 0, AgentSpawnOptions{Prompt: prompt}); err != nil {
				t.Fatal(err)
			}
			session, err := s.GetAgentSessionByTaskID(ctx, "bootstrap")
			if err != nil {
				t.Fatal(err)
			}
			secret, path, err := s.GetChimeraLaunchSecret(ctx, session.ID)
			if err != nil {
				t.Fatal(err)
			}
			lines := mt.sentLinesFor("legato-bootstrap")
			if len(lines) != 1 || !strings.HasPrefix(lines[0], "sh -c ") || !strings.Contains(lines[0], "SANDBOX --args 3") || strings.Contains(lines[0], secret) {
				t.Fatalf("launch did not keep bootstrap outside sandbox: %v", lines)
			}
			if len(backend.plans) != 1 {
				t.Fatalf("plans=%v", backend.plans)
			}
			plan := backend.plans[0]
			found := false
			for i, m := range plan.Mounts {
				if m.Path == filepath.Dir(path) && m.Kind == sandbox.HideDir {
					found = true
					for _, later := range plan.Mounts[i+1:] {
						if binds(later.Kind) && within(m.Path, later.Path) {
							t.Fatalf("bootstrap mask covered by later mount: %+v", later)
						}
					}
				}
			}
			if !found {
				t.Fatalf("sandbox exposes host bootstrap directory %s", filepath.Dir(path))
			}
		})
	}
}
