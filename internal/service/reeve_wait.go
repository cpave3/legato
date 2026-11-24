package service

import (
	"context"
	"log"
	"time"

	"github.com/cpave3/legato/internal/engine/ipc"
	"github.com/cpave3/legato/internal/engine/store"
)

// ReeveAnswerWait is how long an agent waits on the Reeve before the
// operator is told: past it, or with no Reeve session to answer, the agent
// needs the operator ("The Reeve hasn't answered"), so a question never
// sits unseen.
const ReeveAnswerWait = 10 * time.Minute

// ReeveWaitChanged is the IPC message a process sends when an agent starts
// or stops waiting on the Reeve, or a wait passes ReeveAnswerWait: each
// running Legato refreshes its agents and cards.
const ReeveWaitChanged = "reeve_wait_changed"

// NotifyReeveWaitChanged tells every running Legato, this one too, that a
// task's agent's wait on the Reeve changed.
func NotifyReeveWaitChanged(taskID string) {
	ipc.Broadcast(ipc.Message{Type: ReeveWaitChanged, TaskID: taskID})
}

// endReeveWait ends an execution's wait on the Reeve, saying why.
func endReeveWait(ctx context.Context, db *store.Store, taskID, executionID, reason string) {
	if executionID == "" {
		return
	}
	ended, err := db.ClearReeveWait(ctx, executionID, reason)
	if err != nil {
		log.Printf("reeve wait: ending %s's: %v", executionID, err)
		return
	}
	if ended {
		NotifyReeveWaitChanged(taskID)
	}
}

// endReeveWaitsForTask ends the waits on the Reeve of a task's agents: the
// Reeve answered it.
func endReeveWaitsForTask(ctx context.Context, db *store.Store, taskID string) {
	ended, err := db.ClearReeveWaitsForTask(ctx, taskID, store.ReeveWaitReeveAnswered)
	if err != nil {
		log.Printf("reeve wait: ending %s's: %v", taskID, err)
		return
	}
	if ended > 0 {
		NotifyReeveWaitChanged(taskID)
	}
}

// reeveWaitState is a wait's state at now: open while a Reeve session can
// answer and ReeveAnswerWait has not passed, else unanswered.
func reeveWaitState(since time.Time, reeveRuns bool, now time.Time) string {
	if !reeveRuns || now.Sub(since) >= ReeveAnswerWait {
		return ReeveWaitUnanswered
	}
	return ReeveWaitOpen
}

// ReeveWaitsDue says whether a wait on the Reeve passed ReeveAnswerWait in
// (after, until]: its agent's glance moved to Needs you with nothing else
// to tell the lists so.
func ReeveWaitsDue(ctx context.Context, db *store.Store, after, until time.Time) bool {
	waits, err := db.OpenReeveWaitsSince(ctx)
	if err != nil {
		return false
	}
	for _, since := range waits {
		if due := since.Add(ReeveAnswerWait); due.After(after) && !due.After(until) {
			return true
		}
	}
	return false
}

// WaitingOnReeve is the agent's wait on the Reeve when that wait is what
// its glance shows (ReeveWaitOpen: working; ReeveWaitUnanswered: needs
// you), else "": no wait, or something else decides the glance.
func (a AgentSession) WaitingOnReeve() string {
	if a.ReeveWait == "" {
		return ""
	}
	without := a
	without.ReeveWait = ""
	if without.Glance() == a.Glance() {
		return ""
	}
	return a.ReeveWait
}
