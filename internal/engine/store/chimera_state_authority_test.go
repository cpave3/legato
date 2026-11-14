package store

import (
	"context"
	"testing"
)

func TestLegacyActivityWritesCannotOverwriteModState(t *testing.T) {
	for _, byID := range []bool{false, true} {
		t.Run(map[bool]string{false: "pane", true: "script"}[byID], func(t *testing.T) {
			s := newTestStore(t)
			ctx := context.Background()
			createTestTask(t, s, "mod-task")
			if err := s.InsertAgentSession(ctx, AgentSession{TaskID: "mod-task", TmuxSession: "legato-mod-task", Command: "chimera", AgentKind: "chimera", Status: "running"}); err != nil {
				t.Fatal(err)
			}
			a, err := s.GetAgentSessionByTaskID(ctx, "mod-task")
			if err != nil {
				t.Fatal(err)
			}
			if err := s.SetAgentHookState(ctx, a.ID, ActivityWaiting, nil, HookSourceMod); err != nil {
				t.Fatal(err)
			}
			// The old caller may have read the source before the mod's first report.
			if byID {
				err = s.UpdateAgentActivityForSession(ctx, a.ID, ActivityWorking)
			} else {
				err = s.UpdateAgentActivity(ctx, "mod-task", ActivityWorking)
			}
			if err != nil {
				t.Fatal(err)
			}
			a, err = s.GetAgentSessionByTaskID(ctx, "mod-task")
			if err != nil {
				t.Fatal(err)
			}
			if a.Activity != ActivityWaiting || a.HookSource != HookSourceMod {
				t.Fatalf("stale writer won: %+v", a)
			}
		})
	}
}

func TestLegacyIdentityCannotOverwriteModSelector(t *testing.T) {
	for _, value := range []string{"stale", ""} {
		t.Run("legacy="+value, func(t *testing.T) {
			s := newTestStore(t)
			ctx := context.Background()
			createTestTask(t, s, "mod-task")
			if err := s.InsertAgentSession(ctx, AgentSession{TaskID: "mod-task", TmuxSession: "legato-mod-task", Command: "chimera", AgentKind: "chimera", Status: "running"}); err != nil {
				t.Fatal(err)
			}
			if err := s.SetChimeraResumeInSlot(ctx, "mod-task", SlotMain, "actual"); err != nil {
				t.Fatal(err)
			}
			a, err := s.GetAgentSessionByTaskID(ctx, "mod-task")
			if err != nil {
				t.Fatal(err)
			}
			if err := s.SetAgentHookState(ctx, a.ID, ActivityWorking, nil, HookSourceMod); err != nil {
				t.Fatal(err)
			}
			if err := s.SetChimeraResumeInSlot(ctx, "mod-task", SlotMain, value); err != nil {
				t.Fatal(err)
			}
			saved, err := s.GetTaskNativeSession(ctx, "mod-task", "chimera")
			if err != nil || saved == nil || saved.ProviderSessionID != "actual" {
				t.Fatalf("legacy writer replaced mod selector: %+v %v", saved, err)
			}
		})
	}
}
