package handofftest

import (
	"context"
	"sync"
	"testing"

	"github.com/cpave3/legato/internal/service"
)

type recordingCapabilities struct {
	mu       sync.Mutex
	requests []service.CapabilityRequest
}

func (r *recordingCapabilities) Capabilities(_ context.Context, req service.CapabilityRequest) (service.AgentCapabilities, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.requests = append(r.requests, req)
	return service.AgentCapabilities{}, nil
}

func TestAHandedOffAgentIsAskedForWithItsPurpose(t *testing.T) {
	ctx := context.Background()
	source := &recordingCapabilities{}
	worker := StartWorker(t, WorkerOptions{Name: "vm-1", Capabilities: source})
	main := NewMain(t, MainOptions{Name: "workstation", Clock: worker.Clock})
	paired := Pair(t, main, worker)
	taskID, _ := localTask(t, main, "Why is login flaky?", "", "")
	away, err := main.Handoffs.HandOff(ctx, service.TaskHandoffRequest{TaskID: taskID, Worker: paired.InstanceID, Purpose: service.HandoffFindOut, Repo: "acme/widget"})
	if err != nil {
		t.Fatal(err)
	}
	worker.Settle()

	source.mu.Lock()
	defer source.mu.Unlock()
	if len(source.requests) != 1 {
		t.Fatalf("requests = %+v, want the handed-off agent's", source.requests)
	}
	got := source.requests[0]
	if got.TaskID != away.WorkerTaskID || got.HandoffPurpose != service.HandoffFindOut || got.Repository != "github.com/acme/widget" {
		t.Fatalf("the handed-off agent was asked for as %+v", got)
	}
}
