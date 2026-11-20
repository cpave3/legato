package service

import (
	"context"
	"log"
	"strings"

	"github.com/cpave3/legato/internal/engine/ipc"
	"github.com/cpave3/legato/internal/engine/store"
)

// IsOperatorText says whether a user turn's text is the operator's own: any
// text that does not open with a [legato …] tag. Peers', the Reeve's and
// Legato's own messages are tagged (docs/ai/dev-notes.md); macros are the
// operator's words and are not.
func IsOperatorText(text string) bool {
	text = strings.TrimSpace(text)
	return text != "" && !strings.HasPrefix(text, "[legato ")
}

// CloseOperatorItemsOnTurn closes every open operator item of an execution
// when text is the operator's own, and tells running Legatos. It says how
// many it closed.
func CloseOperatorItemsOnTurn(ctx context.Context, db *store.Store, taskID, executionID, text string) (int, error) {
	if !IsOperatorText(text) {
		return 0, nil
	}
	return CloseOperatorItemsForOperatorTurn(ctx, db, taskID, executionID)
}

// CloseOperatorItemsForOperatorTurn closes every open operator item of an
// execution on a turn already known to be the operator's.
func CloseOperatorItemsForOperatorTurn(ctx context.Context, db *store.Store, taskID, executionID string) (int, error) {
	if executionID == "" {
		return 0, nil
	}
	return closeExecutionOperatorItems(ctx, db, taskID, executionID, store.OperatorItemOperatorTurn)
}

func closeExecutionOperatorItems(ctx context.Context, db *store.Store, taskID, executionID, reason string) (int, error) {
	closed, err := db.CloseExecutionOperatorItems(ctx, executionID, reason)
	if err == nil && closed > 0 {
		NotifyOperatorItemsChanged(taskID)
	}
	return closed, err
}

// NotifyOperatorItemsChanged tells every running Legato, this one too, that
// a task's operator items changed, so each reconciles Attention.
func NotifyOperatorItemsChanged(taskID string) {
	ipc.Broadcast(ipc.Message{Type: "operator_items_changed", TaskID: taskID})
}

// OperatorTurnRecorder is told of a message the operator sent an agent
// through Legato (the web UI, the TUI, voice, a macro). It stands for the
// operator's turn for an agent tool that reports turns without their text.
type OperatorTurnRecorder interface {
	RecordOperatorMessage(ctx context.Context, taskID, text string) error
}

// RecordOperatorMessage closes the open operator items of the agent of
// taskID (in the context's slot) when text is the operator's own.
func (a *agentService) RecordOperatorMessage(ctx context.Context, taskID, text string) error {
	if !IsOperatorText(text) {
		return nil
	}
	session, err := slotSession(ctx, a.store, taskID)
	if err != nil {
		return err
	}
	_, err = CloseOperatorItemsOnTurn(ctx, a.store, session.TaskID, session.ExecutionID, text)
	return err
}

// NoteOperatorMessage tells agents that the operator sent the agent of
// taskID text, when agents keeps operator items. Each place a message the
// operator wrote enters Legato calls it once it is delivered.
func NoteOperatorMessage(ctx context.Context, agents any, taskID, text string) {
	recorder, ok := agents.(OperatorTurnRecorder)
	if !ok {
		return
	}
	if err := recorder.RecordOperatorMessage(ctx, taskID, text); err != nil {
		log.Printf("operator items: the operator's message to %s: %v", taskID, err)
	}
}

type operatorItemsKeptKey struct{}

// withOperatorItemsKept marks an execution ended by a crash: an interrupted
// agent keeps its operator items until it is resumed or dismissed.
func withOperatorItemsKept(ctx context.Context) context.Context {
	return context.WithValue(ctx, operatorItemsKeptKey{}, true)
}

func operatorItemsKept(ctx context.Context) bool {
	kept, _ := ctx.Value(operatorItemsKeptKey{}).(bool)
	return kept
}

// endOperatorItems closes what an ended execution posted for the operator.
func (a *agentService) endOperatorItems(ctx context.Context, executionID string) {
	if executionID == "" || operatorItemsKept(ctx) {
		return
	}
	taskID := ""
	if execution, err := a.store.GetExecution(ctx, executionID); err == nil {
		taskID = execution.ExecutionTaskID
		if taskID == "" {
			taskID = execution.TaskID
		}
	}
	if _, err := closeExecutionOperatorItems(ctx, a.store, taskID, executionID, store.OperatorItemAgentEnded); err != nil {
		log.Printf("operator items: closing those of ended execution %s: %v", executionID, err)
	}
}

// takeOverOperatorItems gives a resumed agent the open items its
// interrupted execution posted.
func (a *agentService) takeOverOperatorItems(ctx context.Context, taskID, fromExecutionID string) {
	if fromExecutionID == "" {
		return
	}
	session, err := a.store.GetRunningAgentSession(ctx, taskID, "")
	if err != nil {
		return
	}
	if moved, err := a.store.MoveOperatorItems(ctx, fromExecutionID, session.ExecutionID); err != nil {
		log.Printf("operator items: moving those of %s to the resumed agent: %v", fromExecutionID, err)
	} else if moved > 0 {
		NotifyOperatorItemsChanged(taskID)
	}
}

// sweepOrphanOperatorItems closes the items of agents that ended while no
// Legato ran.
func (a *agentService) sweepOrphanOperatorItems(ctx context.Context) {
	if closed, err := a.store.CloseOrphanOperatorItems(ctx); err != nil {
		log.Printf("operator items: closing those of agents that ended: %v", err)
	} else if closed > 0 {
		NotifyOperatorItemsChanged("")
	}
}
