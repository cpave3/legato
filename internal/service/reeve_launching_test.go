package service

import (
	"context"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cpave3/legato/internal/engine/store"
)

// slowReeveAgents starts an agent only when let, and binds its native
// endpoint some time after: a Reeve still starting.
type slowReeveAgents struct {
	fakeReeveAgents
	mu      sync.Mutex
	let     chan struct{}
	bound   bool
	queued  []string
	boundAt []bool
}

func (f *slowReeveAgents) SpawnAgent(ctx context.Context, taskID string, a, b int, opts ...AgentSpawnOptions) error {
	<-f.let
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.fakeReeveAgents.SpawnAgent(ctx, taskID, a, b, opts...)
}

func (f *slowReeveAgents) HasChimeraEndpoint(context.Context, string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.bound
}

func (f *slowReeveAgents) QueueMessage(_ context.Context, _ string, text string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.queued = append(f.queued, text)
	f.boundAt = append(f.boundAt, f.bound)
	return nil
}

func (f *slowReeveAgents) bind() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.bound = true
}

func (f *slowReeveAgents) sent() ([]string, []bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.queued...), append([]bool(nil), f.boundAt...)
}

func newSlowReeve(t *testing.T) (*ReeveService, *store.Store, *slowReeveAgents, string) {
	t.Helper()
	dir := t.TempDir()
	db, err := store.New(filepath.Join(dir, "legato.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	agents := &slowReeveAgents{let: make(chan struct{}, 8)}
	svc := NewReeveService(ReeveServiceOptions{Store: db, Agents: agents, Folder: filepath.Join(dir, "reeve"),
		Defaults: func() ReeveDefaults { return ReeveDefaults{Agent: "claude-code"} }})
	ctx := context.Background()
	agents.let <- struct{}{}
	session, err := svc.Start(ctx, ReeveStart{StartedBy: "web"})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.SetReeveSessionState(ctx, session.TaskID, store.ReeveSuspended); err != nil {
		t.Fatal(err)
	}
	agents.bound = false
	return svc, db, agents, session.TaskID
}

func TestAMessageDuringAResumeWaitsForTheReeveAndIsDelivered(t *testing.T) {
	svc, db, agents, taskID := newSlowReeve(t)
	ctx := context.Background()

	// The warm-up resumes the Reeve; its agent is still starting.
	resumed := make(chan error, 1)
	go func() { _, err := svc.Resume(ctx, taskID, ReeveStart{}); resumed <- err }()
	waitUntil(t, func() bool {
		current, _ := db.CurrentReeveSession(ctx)
		return current != nil && current.State == store.ReeveRunning
	})

	messaged := make(chan error, 1)
	go func() { messaged <- svc.Message(ctx, "is the sync fixed?", "web") }()
	time.Sleep(50 * time.Millisecond)
	if queued, _ := agents.sent(); len(queued) != 0 {
		t.Fatalf("sent while the Reeve was starting: %q", queued)
	}

	agents.let <- struct{}{}
	if err := <-resumed; err != nil {
		t.Fatal(err)
	}
	time.Sleep(50 * time.Millisecond)
	agents.bind()
	if err := <-messaged; err != nil {
		t.Fatal(err)
	}
	queued, bound := agents.sent()
	if len(queued) != 1 || queued[0] != "is the sync fixed?" || !bound[0] {
		t.Fatalf("queued %q (bound %v), want the message once the endpoint was bound", queued, bound)
	}
}

func TestAMessageThatFindsTheReeveSuspendedDuringAResumeIsNotDropped(t *testing.T) {
	svc, _, agents, taskID := newSlowReeve(t)
	ctx := context.Background()

	resumed := make(chan error, 1)
	go func() { _, err := svc.Resume(ctx, taskID, ReeveStart{}); resumed <- err }()
	// The message reads the session before the resume marks it running.
	messaged := make(chan error, 1)
	go func() { messaged <- svc.Message(ctx, "is the sync fixed?", "web") }()
	time.Sleep(50 * time.Millisecond)

	agents.let <- struct{}{}
	agents.let <- struct{}{}
	if err := <-resumed; err != nil {
		t.Fatal(err)
	}
	agents.bind()
	if err := <-messaged; err != nil {
		t.Fatal(err)
	}
	queued, _ := agents.sent()
	inPrompt := false
	for _, o := range agents.spawned[1:] {
		if strings.Contains(o.Prompt, "is the sync fixed?") {
			inPrompt = true
		}
	}
	if !inPrompt && (len(queued) != 1 || queued[0] != "is the sync fixed?") {
		t.Fatalf("the message was dropped: queued %q, spawns %+v", queued, agents.spawned)
	}
}
