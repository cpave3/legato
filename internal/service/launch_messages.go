package service

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"log"

	"github.com/cpave3/legato/internal/engine/store"
)

// LaunchMessageResumer delivers the launch messages a restart of Legato
// interrupted.
type LaunchMessageResumer interface {
	ResumeLaunchMessages(ctx context.Context) int
}

func newLaunchOwner() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// launchWith keeps the messages an agent is launched with, then delivers
// them in the background once it can take them. Kept first, they outlive a
// restart of Legato before the agent tool is ready.
func (a *agentService) launchWith(ctx context.Context, executionID, taskID, slot, session, agentKind string, adapter AIToolAdapter, messages []string, promptFile string) {
	kept := make([]store.LaunchMessage, len(messages))
	for i, m := range messages {
		kept[i] = store.LaunchMessage{ExecutionID: executionID, Position: i, TaskID: taskID, Slot: slot, TmuxSession: session,
			AgentKind: agentKind, PromptFile: promptFile, Message: m, Owner: a.launchOwner}
	}
	if err := a.store.InsertLaunchMessages(ctx, kept); err != nil {
		log.Printf("keeping launch messages for %s: %v", session, err)
		return
	}
	go a.deliverLaunchMessages(WithAgentSlot(context.Background(), slot), executionID, taskID, session, adapter, kept, promptFile)
}

// ResumeLaunchMessages takes over the launch messages not yet delivered:
// those of agents still running are delivered by this process; those of
// agents that ended are dropped. It reports how many agents it resumed.
func (a *agentService) ResumeLaunchMessages(ctx context.Context) int {
	pending, err := a.store.ListLaunchMessages(ctx)
	if err != nil {
		log.Printf("launch messages: %v", err)
		return 0
	}
	byExecution := map[string][]store.LaunchMessage{}
	var order []string
	for _, m := range pending {
		if _, seen := byExecution[m.ExecutionID]; !seen {
			order = append(order, m.ExecutionID)
		}
		byExecution[m.ExecutionID] = append(byExecution[m.ExecutionID], m)
	}
	resumed := 0
	for _, executionID := range order {
		messages := byExecution[executionID]
		first := messages[0]
		if alive, err := a.tmux.IsAlive(first.TmuxSession); err != nil || !alive {
			_ = a.store.DeleteLaunchMessages(ctx, executionID)
			continue
		}
		// Every Legato process resumes at start; the first to adopt an
		// agent's messages delivers them.
		if adopted, err := a.store.AdoptLaunchMessages(ctx, executionID, first.Owner, a.launchOwner); err != nil || !adopted {
			continue
		}
		for i := range messages {
			messages[i].Owner = a.launchOwner
		}
		resumed++
		log.Printf("delivering %d launch message(s) to %s after a restart", len(messages), first.TmuxSession)
		go a.deliverLaunchMessages(WithAgentSlot(context.Background(), first.Slot), executionID, first.TaskID, first.TmuxSession,
			a.resolveAdapter(first.AgentKind), messages, first.PromptFile)
	}
	return resumed
}
