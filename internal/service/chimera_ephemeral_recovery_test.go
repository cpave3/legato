package service

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/cpave3/legato/internal/engine/store"
)

func TestEphemeralChimeraDiscoveryDoesNotRequireATaskSessionName(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	instances := filepath.Join(os.Getenv("HOME"), ".chimera", "instances")
	if err := os.MkdirAll(instances, 0700); err != nil {
		t.Fatal(err)
	}
	svc, s, _ := newDiscoveryTestService(t)
	ctx := context.Background()
	taskID, err := s.CreateEphemeralTask(ctx, "standalone")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.InsertAgentSession(ctx, store.AgentSession{TaskID: taskID, TmuxSession: "legato-" + taskID, Command: "chimera", AgentKind: "chimera", Status: "running"}); err != nil {
		t.Fatal(err)
	}
	endpoint := nativeEndpoint(t, "ephemeral-conversation", "A name chosen in Chimera")
	writeChimeraLockfile(t, instances, "ephemeral.json", "ephemeral-conversation", endpoint)
	if err := svc.DiscoverChimera(ctx, taskID); err != nil {
		t.Fatal(err)
	}
	id, bound := binding(t, s, taskID)
	if id != "ephemeral-conversation" || bound != endpoint {
		t.Fatalf("binding=%q %q", id, bound)
	}
}
