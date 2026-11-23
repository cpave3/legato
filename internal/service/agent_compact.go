package service

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/cpave3/legato/internal/engine/chimera"
)

// AgentCompactor compacts a running agent's context with its tool's own
// compaction: Claude Code's /compact, Codex's thread/compact/start, Chimera's
// compact route. Every one is reached through the agent's native endpoint,
// at POST /v1/sessions/{id}/compact. The call returns once the tool has
// taken the request; the endpoint's events report compaction_started, then
// compaction_finished (tokensBefore, tokensAfter) or compaction_failed.
type AgentCompactor interface {
	CompactAgent(ctx context.Context, taskID, instructions string) error
}

// Why a compaction was refused.
var (
	// ErrCompactUnsupported: Legato has no way to compact this agent.
	ErrCompactUnsupported = errors.New("this agent cannot be compacted from Legato")
	// ErrCompactFocusUnsupported: the agent's tool compacts with its own
	// prompt only, so focus instructions would be dropped.
	ErrCompactFocusUnsupported = errors.New("this agent's tool takes no focus instructions for a compaction")
	// ErrAgentWorking: a turn is running. Every tool compacts between turns.
	ErrAgentWorking = errors.New("the agent is working")
)

// compactTools are the agent tools Legato can compact through their native
// endpoint, and whether each takes focus instructions.
var compactTools = map[string]struct {
	name       string
	takesFocus bool
}{
	"claude-code": {name: "Claude Code", takesFocus: true},
	"codex":       {name: "Codex", takesFocus: false},
	"chimera":     {name: "Chimera", takesFocus: false},
}

// CanCompact says whether Legato can compact agents of a tool, once they
// have connected to it.
func CanCompact(agentKind string) bool {
	_, ok := compactTools[agentKind]
	return ok
}

// compactError is a refusal in words the operator reads, which errors.Is
// still matches to its kind.
type compactError struct {
	kind error
	text string
}

func (e *compactError) Error() string { return e.text }
func (e *compactError) Unwrap() error { return e.kind }

func refuseCompact(kind error, format string, args ...any) error {
	return &compactError{kind: kind, text: fmt.Sprintf(format, args...)}
}

// CompactAgent compacts the agent's context; see AgentCompactor.
func (a *agentService) CompactAgent(ctx context.Context, taskID, instructions string) error {
	session, err := slotSession(ctx, a.store, taskID)
	if err != nil {
		return err
	}
	tool, known := compactTools[session.AgentKind]
	switch {
	case !known:
		kind := session.AgentKind
		if kind == "" {
			kind = "this"
		}
		return refuseCompact(ErrCompactUnsupported, "Legato cannot compact %s agents: it has no way to ask their tool to.", kind)
	case session.NativeOff:
		return refuseCompact(ErrCompactUnsupported, "This agent runs in its terminal only. Type /compact there, or switch it back to Legato's panel.")
	case session.ChimeraEndpoint == nil || session.ChimeraSessionID == nil:
		return refuseCompact(ErrCompactUnsupported, "This %s agent has not connected to Legato yet, so it cannot be compacted from here.", tool.name)
	}
	instructions = strings.TrimSpace(instructions)
	if instructions != "" && !tool.takesFocus {
		return refuseCompact(ErrCompactFocusUnsupported, "%s compacts with its own prompt only and takes no focus instructions. Compact without them.", tool.name)
	}
	client := chimera.NewClient(*session.ChimeraEndpoint)
	if a.chimeraAdapter != nil {
		client = a.chimeraAdapter.clientFactory(*session.ChimeraEndpoint)
	}
	if err := client.Compact(ctx, *session.ChimeraSessionID, instructions); err != nil {
		if errors.Is(err, chimera.ErrBusy) {
			return refuseCompact(ErrAgentWorking, "The agent is working. Compact it when its turn ends.")
		}
		return err
	}
	return nil
}
