package service

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/cpave3/legato/internal/engine/store"
)

func TestLaunchMessagesAreKeptUntilDelivered(t *testing.T) {
	svc, s, _ := newNativeLaunchService(t, t.TempDir())
	createTask(t, s, "parent-1")
	if err := svc.SpawnAgent(context.Background(), "parent-1", 0, 0, AgentSpawnOptions{
		Role: "conductor", ParentTaskID: "parent-1", Brief: "kick off the swarm",
	}); err != nil {
		t.Fatal(err)
	}
	// The agent tool is still starting: the kickoff waits, kept.
	if kept, _ := s.ListLaunchMessages(context.Background()); len(kept) != 1 || kept[0].Message != briefKickoffMessage {
		t.Fatalf("kept = %+v, want the kickoff", kept)
	}
	commands := bindBridge(t, s, "parent-1")
	awaitBridgeCommand(t, commands)
	awaitLaunchMessagesDelivered(t, s)
	if kept, _ := s.ListLaunchMessages(context.Background()); len(kept) != 0 {
		t.Fatalf("kept after delivery = %+v", kept)
	}
}

// Legato restarts between the launch and the agent tool being ready: the
// Legato that starts next delivers the kickoff, once.
func TestLaunchMessagesAreDeliveredAfterARestart(t *testing.T) {
	before, s, mt := newNativeLaunchService(t, t.TempDir())
	createTask(t, s, "parent-1")
	if err := before.SpawnAgent(context.Background(), "parent-1", 0, 0, AgentSpawnOptions{
		Role: "conductor", ParentTaskID: "parent-1", Brief: "kick off the swarm",
	}); err != nil {
		t.Fatal(err)
	}

	after := NewAgentService(s, mt, t.TempDir(), AgentServiceOptions{Adapter: before.(*agentService).adapter, BriefKickoffDelay: time.Millisecond})
	if n := after.(LaunchMessageResumer).ResumeLaunchMessages(context.Background()); n != 1 {
		t.Fatalf("resumed %d agents, want 1", n)
	}
	commands := bindBridge(t, s, "parent-1")

	if command := awaitBridgeCommand(t, commands); command["text"] != briefKickoffMessage {
		t.Fatalf("command = %v, want the kickoff", command)
	}
	select {
	case again := <-commands:
		t.Fatalf("delivered twice: %v", again)
	case <-time.After(200 * time.Millisecond):
	}
}

func TestLaunchMessagesOfAnAgentThatEndedAreDropped(t *testing.T) {
	before, s, mt := newNativeLaunchService(t, t.TempDir())
	createTask(t, s, "parent-1")
	if err := before.SpawnAgent(context.Background(), "parent-1", 0, 0, AgentSpawnOptions{
		Role: "conductor", ParentTaskID: "parent-1", Brief: "kick off the swarm",
	}); err != nil {
		t.Fatal(err)
	}
	mt.mu.Lock()
	delete(mt.sessions, "legato-parent-1")
	mt.mu.Unlock()

	after := NewAgentService(s, mt, t.TempDir(), AgentServiceOptions{Adapter: before.(*agentService).adapter})
	if n := after.(LaunchMessageResumer).ResumeLaunchMessages(context.Background()); n != 0 {
		t.Fatalf("resumed %d agents, want none", n)
	}
	if kept, _ := s.ListLaunchMessages(context.Background()); len(kept) != 0 {
		t.Fatalf("kept = %+v, want them dropped", kept)
	}
}

func awaitLaunchMessagesDelivered(t *testing.T, s *store.Store) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		kept, err := s.ListLaunchMessages(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if len(kept) == 0 {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("launch messages still pending: %+v", kept)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

type unavailableLaunchTmux struct{ *mockTmux }

func (m unavailableLaunchTmux) IsAlive(string) (bool, error) {
	return false, fmt.Errorf("tmux probe failed")
}

func TestLaunchRecoveryKeepsMessagesWhenThePaneCannotBeChecked(t *testing.T) {
	before, s, mt := newNativeLaunchService(t, t.TempDir())
	ctx := context.Background()
	message := store.LaunchMessage{ExecutionID: "ex-kept", TaskID: "task", TmuxSession: "legato-task", Message: "do the task", Owner: "previous-process"}
	if err := s.InsertLaunchMessages(ctx, []store.LaunchMessage{message}); err != nil {
		t.Fatal(err)
	}
	after := NewAgentService(s, unavailableLaunchTmux{mt}, t.TempDir(), AgentServiceOptions{Adapter: before.(*agentService).adapter})
	if n := after.(LaunchMessageResumer).ResumeLaunchMessages(ctx); n != 0 {
		t.Fatalf("resumed=%d", n)
	}
	kept, err := s.ListLaunchMessages(ctx)
	if err != nil || len(kept) != 1 || kept[0].Message != message.Message {
		t.Fatalf("pending after tmux probe failure=%+v, %v", kept, err)
	}
}
