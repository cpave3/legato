package service

import (
	"context"
	"errors"
	"fmt"

	"github.com/cpave3/legato/internal/engine/store"
)

// ErrAuthorNotResumable is why a handoff's author cannot be brought back:
// no saved conversation, no clone, or a resume that failed. The review loop
// marks the review stuck; Legato never starts a fresh author in its place.
var ErrAuthorNotResumable = errors.New("the handoff's author cannot be resumed")

// ResumeAuthor brings back the author of the open handoff of a card when it
// is not running: its saved conversation, in the main slot of the same job
// clone, with its permission prompts off, its branch guidance, and its
// branches granted to the new session as they were to the first. An author
// that runs is left alone.
func (s *HandoffWorkerService) ResumeAuthor(ctx context.Context, taskID string) error {
	return s.resumeAuthor(ctx, taskID, HandedOffRestart{})
}

// HandedOffFrom names the main a card was handed off from while its handoff
// is open (HandedOffAgents): its name, else its instance ID. An error other
// than no open handoff is returned, so a restart refuses rather than take
// the card for one of the worker's own.
func (s *HandoffWorkerService) HandedOffFrom(ctx context.Context, taskID string) (string, bool, error) {
	h, err := s.db.OpenHandoffForTask(ctx, taskID)
	if errors.Is(err, store.ErrNotFound) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	main := h.MainName
	if main == "" {
		main = h.MainInstanceID
	}
	return main, true, nil
}

// RestartHandedOff starts a handed-off card's running agent again as
// ResumeAuthor does (HandedOffAgents): it checks the conversation can be
// resumed, then stops the agent with r.Stop, then resumes it with r's note
// and resources.
func (s *HandoffWorkerService) RestartHandedOff(ctx context.Context, taskID string, r HandedOffRestart) error {
	return s.resumeAuthor(ctx, taskID, r)
}

func (s *HandoffWorkerService) resumeAuthor(ctx context.Context, taskID string, r HandedOffRestart) error {
	note := r.Note
	h, err := s.db.OpenHandoffForTask(ctx, taskID)
	if err != nil {
		return fmt.Errorf("%w: %s has no open handoff: %v", ErrAuthorNotResumable, taskID, err)
	}
	if r.Stop == nil {
		// A session whose tmux is gone (a worker that restarted, a crash)
		// is marked ended before it is looked for.
		if err := s.opts.Agents.ReconcileSessions(ctx); err != nil {
			return err
		}
		if session, err := s.opts.Agents.GetAgentSession(ctx, taskID); err == nil && session != nil {
			return nil
		}
	}
	kind := h.AgentKind
	saved, err := s.opts.Agents.GetTaskResumeSession(ctx, taskID, kind)
	if err != nil {
		return fmt.Errorf("%w: reading its saved conversation: %v", ErrAuthorNotResumable, err)
	}
	if saved == nil {
		return fmt.Errorf("%w: it has no saved conversation (its %s session never reported one)", ErrAuthorNotResumable, kind)
	}
	kind = saved.AgentKind
	// A restart stops the running agent only once it is known it can be
	// resumed as the handoff resumes it.
	identity := saved.AccountIdentity
	if r.Stop != nil {
		if session, err := s.opts.Agents.GetAgentSession(ctx, taskID); err == nil && session != nil && session.ExecutionID != "" {
			identity, err = s.db.ExecutionAccount(ctx, session.ExecutionID)
			if err != nil {
				return err
			}
		} else if err != nil && !errors.Is(err, store.ErrNotFound) {
			return err
		}
		if err := r.Stop(ctx); err != nil {
			return err
		}
	}
	opts := AgentSpawnOptions{AgentKind: kind, Tier: h.AgentTier, Repo: h.Repo, Unattended: true, Branches: handoffSessionBranches(h),
		SessionPolicy: "resume", ResumeSessionID: saved.ID, Prompt: note, Holder: h.MainTaskID}
	opts.AccountIdentity = identity
	if identity != nil {
		opts.Account = identity.Account
	}
	opts.Resources = r.Resources
	if err := s.opts.Agents.SpawnAgent(ctx, taskID, 0, 0, opts); err != nil {
		return fmt.Errorf("%w: %v", ErrAuthorNotResumable, err)
	}
	s.grant(ctx, h)
	return nil
}

// ResumeInterrupted resumes the author of a handed-off card when a crash
// took it (an InterruptedResumeRoute): through ResumeAuthor, unattended in
// its clone. A card with no open handoff is not handled.
func (s *HandoffWorkerService) ResumeInterrupted(ctx context.Context, taskID string) (bool, error) {
	if _, err := s.db.OpenHandoffForTask(ctx, taskID); err != nil {
		return false, nil
	}
	return true, s.ResumeAuthor(ctx, taskID)
}
