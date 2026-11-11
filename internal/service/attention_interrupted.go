package service

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
)

// InterruptedAttentionProducer names the Attention producer of interrupted
// agents.
const InterruptedAttentionProducer = "interrupted_agents"

// InterruptedAttentionAdapter shows the agents a crash interrupted as one
// Attention item, with a link to them and an action that resumes them all.
type InterruptedAttentionAdapter struct {
	agents InterruptedAgents
}

func NewInterruptedAttentionAdapter(agents InterruptedAgents) *InterruptedAttentionAdapter {
	return &InterruptedAttentionAdapter{agents: agents}
}

func (a *InterruptedAttentionAdapter) Producer() string { return InterruptedAttentionProducer }

func (a *InterruptedAttentionAdapter) Snapshot(ctx context.Context) ([]AttentionInput, error) {
	list, err := a.agents.ListInterruptedAgents(ctx)
	if err != nil || len(list) == 0 {
		return nil, err
	}
	ids := make([]string, 0, len(list))
	resumable := 0
	for _, agent := range list {
		ids = append(ids, agent.TaskID)
		if agent.Resumable {
			resumable++
		}
	}
	// A new set of interrupted agents shows again once dismissed.
	sort.Strings(ids)
	agents := "1 agent was"
	if len(list) != 1 {
		agents = fmt.Sprintf("%d agents were", len(list))
	}
	detail := "Their tmux sessions were gone when Legato started: the machine or tmux went down. Resume them from the Agents list, where they are kept as Interrupted."
	if resumable < len(list) {
		detail += fmt.Sprintf(" %d of them cannot be resumed; the list says why.", len(list)-resumable)
	}
	actions := []AttentionAction{{ID: "open", Label: "Show them", Kind: "navigate"}}
	if resumable > 0 {
		actions = append(actions, AttentionAction{ID: "resume_all", Label: "Resume all", Kind: "execute", Intent: "primary", RequiresConfirm: true})
	}
	return []AttentionInput{{
		SignalKind: AttentionSignalAgentsInterrupted,
		SourceKey:  "agents-interrupted", SourceRevision: strings.Join(ids, ","),
		Title:    agents + " interrupted",
		Reason:   "Legato stopped while they were running",
		Detail:   detail,
		Category: "agents", Urgency: "high",
		Navigation: AttentionNavigation{Kind: "agents", Target: "interrupted"},
		Actions:    actions,
	}}, nil
}

// Execute resumes every interrupted agent that can be.
func (a *InterruptedAttentionAdapter) Execute(ctx context.Context, request AttentionActionRequest) error {
	if request.ActionID != "resume_all" {
		return fmt.Errorf("unknown action %q", request.ActionID)
	}
	var errs []error
	for _, result := range a.agents.ResumeAllInterruptedAgents(ctx) {
		if result.Err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", result.TaskID, result.Err))
		}
	}
	return errors.Join(errs...)
}
