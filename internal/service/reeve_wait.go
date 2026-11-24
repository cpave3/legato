package service

import (
	"context"
	"log"
	"time"

	"github.com/cpave3/legato/internal/engine/ipc"
	"github.com/cpave3/legato/internal/engine/store"
)

// ReeveAnswerWait is how long the Reeve has to answer an agent once it was
// told the agent's question: past it the agent needs the operator
// ("Waiting on the Reeve for N min"), so a question never sits unseen. It
// runs from when the Reeve was told, not from when the agent asked: the
// watcher holds a message while the Reeve is busy (LEG-623).
const ReeveAnswerWait = 15 * time.Minute

// ReeveUntoldWait is the backstop for a question the Reeve was never told
// (one stuck mid-turn, say): past it from the asking, the agent needs the
// operator.
const ReeveUntoldWait = time.Hour

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

// endReeveWaitOf ends the wait on the Reeve of the one agent an address
// names, as legato agent message delivers to it: a running execution's ID,
// else an agent ID (a task ID for its main slot, <task>_<slot> for a named
// one). The Reeve answered it; a task's other slots keep theirs.
func endReeveWaitOf(ctx context.Context, db *store.Store, address string) {
	if execution, err := db.GetExecution(ctx, address); err == nil && execution.Status == store.ExecutionRunning {
		endReeveWait(ctx, db, execution.ExecutionTaskID, execution.ID, store.ReeveWaitReeveAnswered)
		return
	}
	if session, err := slotSession(ctx, db, address); err == nil {
		endReeveWait(ctx, db, session.TaskID, session.ExecutionID, store.ReeveWaitReeveAnswered)
	}
}

// reeveWaitState is a wait's state at now: unanswered when no Reeve can
// answer (no session, and none started for the question), when the Reeve
// was told at toldAt and ReeveAnswerWait has passed since, or when it was
// never told and ReeveUntoldWait has passed since the agent asked; else
// open.
func reeveWaitState(since time.Time, toldAt *time.Time, reachable bool, now time.Time) string {
	if !reachable || !now.Before(reeveWaitDue(since, toldAt)) {
		return ReeveWaitUnanswered
	}
	return ReeveWaitOpen
}

// reeveWaitDue is when a wait the Reeve can answer goes unanswered.
func reeveWaitDue(since time.Time, toldAt *time.Time) time.Time {
	if toldAt != nil {
		return toldAt.Add(ReeveAnswerWait)
	}
	return since.Add(ReeveUntoldWait)
}

// ReeveWaitsDue says whether a wait on the Reeve went unanswered in
// (after, until]: its agent's glance moved to Needs you with nothing else
// to tell the lists so.
func ReeveWaitsDue(ctx context.Context, db *store.Store, after, until time.Time) bool {
	waits, err := db.OpenReeveWaits(ctx)
	if err != nil {
		return false
	}
	for _, w := range waits {
		if due := reeveWaitDue(w.Since, w.ToldAt); due.After(after) && !due.After(until) {
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
