package service

import (
	"context"
	"errors"
	"fmt"
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
	h, err := s.db.OpenHandoffForTask(ctx, taskID)
	if err != nil {
		return fmt.Errorf("%w: %s has no open handoff: %v", ErrAuthorNotResumable, taskID, err)
	}
	// A session whose tmux is gone (a worker that restarted, a crash) is
	// marked ended before it is looked for.
	if err := s.opts.Agents.ReconcileSessions(ctx); err != nil {
		return err
	}
	if session, err := s.opts.Agents.GetAgentSession(ctx, taskID); err == nil && session != nil {
		return nil
	}
	kind := h.AgentKind
	if kind == "" {
		kind = s.opts.Agents.DefaultAdapter()
	}
	saved, err := s.opts.Agents.GetTaskResumeSession(ctx, taskID, kind)
	if err != nil {
		return fmt.Errorf("%w: reading its saved conversation: %v", ErrAuthorNotResumable, err)
	}
	if saved == nil {
		return fmt.Errorf("%w: it has no saved conversation (its %s session never reported one)", ErrAuthorNotResumable, kind)
	}
	opts := AgentSpawnOptions{AgentKind: kind, Repo: h.Repo, Unattended: true, Branches: handoffSessionBranches(h),
		SessionPolicy: "resume", ResumeSessionID: saved.ID}
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
