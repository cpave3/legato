package store

import (
	"context"
	"testing"
)

func TestFallbackBindingDoesNotReplaceNativeBinding(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	createTestTask(t, s, "launch")
	id, err := s.InsertAgentSessionReturningID(ctx, AgentSession{TaskID: "launch", Status: "running", TmuxSession: "legato-launch"})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.UpdateChimeraDiscoveryForSession(ctx, id, "mod", "http://127.0.0.1:1/private"); err != nil {
		t.Fatal(err)
	}
	bound, err := s.BindChimeraDiscoveryIfUnbound(ctx, id, "legacy", "http://127.0.0.1:2")
	if err != nil || bound {
		t.Fatalf("fallback bound=%v err=%v", bound, err)
	}
	got, err := s.GetAgentSessionByTaskID(ctx, "launch")
	if err != nil || *got.ChimeraSessionID != "mod" {
		t.Fatalf("binding=%+v err=%v", got, err)
	}
}
