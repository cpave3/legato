package service

import (
	"context"
	"testing"

	"github.com/cpave3/legato/internal/engine/sandbox"
)

func TestAReadOnlySpawnIsSandboxedWithItsCheckoutReadOnly(t *testing.T) {
	backend := &fakeBackend{}
	svc, s, _ := sandboxedService(t, backend)
	createTask(t, s, "task-1")
	dir := realTempDir(t)
	// Asked to run outside the sandbox, it runs inside it all the same.
	if err := svc.SpawnAgent(context.Background(), "task-1", 0, 0, AgentSpawnOptions{WorkingDir: dir, Unsandboxed: true, ReadOnlyCheckout: true}); err != nil {
		t.Fatal(err)
	}
	if len(backend.plans) != 1 {
		t.Fatalf("plans = %d, want 1", len(backend.plans))
	}
	found := false
	for _, m := range backend.plans[0].Mounts {
		if m.Path == dir || m.Dest == dir {
			found = true
			if m.Kind != sandbox.ReadOnly {
				t.Fatalf("checkout mount = %+v, want read-only", m)
			}
		}
	}
	if !found {
		t.Fatal("the checkout is not mounted")
	}
	session, err := svc.GetAgentSession(context.Background(), "task-1")
	if err != nil || !session.Sandboxed {
		t.Fatalf("session = %+v, %v", session, err)
	}
	readOnly, err := s.ExecutionReadOnly(context.Background(), session.ExecutionID)
	if err != nil || !readOnly {
		t.Fatalf("execution read-only = %v, %v", readOnly, err)
	}
}
