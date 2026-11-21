package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/cpave3/legato/internal/engine/store"
)

// RestoredNote is the first message of a conversation restored from the
// session history: when and where it last ran, and that much may have
// changed since.
func RestoredNote(lastRan time.Time, folder string) string {
	when := "earlier"
	if !lastRan.IsZero() {
		when = "on " + lastRan.Local().Format("2 Jan 2006 at 15:04")
	}
	return fmt.Sprintf("[legato restored] This conversation was restored from Legato's session history. It last ran %s in %s; files, branches and other agents may have changed since. Check the state before you go on.", when, folder)
}

// ConversationRestore is a conversation of the history to start again in
// its task's main slot, as its latest run had it.
type ConversationRestore struct {
	ConversationID    int64
	TaskID            string
	AgentKind         string
	ProviderSessionID string
	CWD               string
	StorageDir        string
	// ExecutionID is the latest run's, which the restore copies; Sandboxed
	// is whether that run was sandboxed, nil when not known.
	ExecutionID string
	Sandboxed   *bool
	// LastRanAt is when the latest run ended, or started while it had no end.
	LastRanAt time.Time
}

// ConversationRestorer starts a conversation of the history again.
type ConversationRestorer interface {
	RestoreConversation(ctx context.Context, r ConversationRestore) error
}

// RestoreConversation starts a conversation again in its task's main slot:
// the same tool, folder, sandbox choice, engram (in a new engram session)
// and resources, those removed since left out. The task's interrupted
// agent is resumed when it is this conversation, and dismissed first when
// it is another. A refusal is a *RestoreRefusal, and starts nothing.
func (a *agentService) RestoreConversation(ctx context.Context, r ConversationRestore) error {
	if err := RefuseAway(ctx, a.store, r.TaskID); err != nil {
		return &RestoreRefusal{Reason: err.Error()}
	}
	if session, err := a.store.GetAgentSessionInSlot(ctx, r.TaskID, store.SlotMain); err == nil && session.Status == "running" {
		return &RestoreRefusal{Reason: "the task's agent is running: stop it first, or restore into a new task", NewTaskPossible: true}
	}
	if interrupted, err := a.store.GetInterruptedAgentSession(ctx, r.TaskID); err == nil {
		if interrupted.ExecutionID != "" && interrupted.ExecutionID == r.ExecutionID {
			err := a.ResumeInterruptedAgent(ctx, r.TaskID, false)
			if errors.Is(err, ErrAgentNotResumable) {
				return &RestoreRefusal{Reason: reasonOf(err)}
			}
			return err
		}
		if err := a.DismissInterruptedAgent(ctx, r.TaskID); err != nil {
			return err
		}
	}

	opt, _ := a.runSpawnOptions(ctx, r.AgentKind, r.Sandboxed, false, r.ExecutionID)
	// The engram session ended with the run; the engram starts a new one.
	opt.EngramSession = ""
	opt.Slot = store.SlotMain
	opt.SessionPolicy = "resume"
	opt.ResumeSessionID = r.ProviderSessionID
	opt.ResumeWorkingDir = r.CWD
	opt.WorkingDir = r.CWD
	opt.Prompt = RestoredNote(r.LastRanAt, r.CWD)
	chosen, _ := a.store.ListChosenExecutionResources(ctx, r.ExecutionID)
	opt.Resources = a.resolveResources(ctx, chosen)
	if task, err := a.store.GetTask(ctx, r.TaskID); err == nil {
		opt.Ephemeral = task.Ephemeral
	}
	check := opt
	if err := a.prepareTaskResume(ctx, r.TaskID, &check); err != nil {
		if errors.Is(err, ErrTaskResumeConflict) {
			return &RestoreRefusal{Reason: err.Error()}
		}
		return err
	}
	return a.SpawnAgent(ctx, r.TaskID, 0, 0, opt)
}
