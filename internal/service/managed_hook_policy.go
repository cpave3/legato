package service

import (
	"context"
	"errors"
	"fmt"
	"log"
	"slices"
	"strings"

	"github.com/cpave3/legato/internal/engine/store"
)

// The managed-hooks policy (spec LEG-687, "Policy: the HookSource"): which
// of the managed hooks an agent is given, and why each other one is off.
// Every layer is evaluated and every off reason recorded, so a hook can be
// off for several; any off wins. See docs/ai/managed-hooks.md § The policy.

// ManagedHookOff is why a managed hook is off for an agent. One vocabulary
// everywhere: records, API, panel and notes.
type ManagedHookOff string

// The off reasons. Phase 1 records global, invalid, unsupported-tool,
// spawn, read-only, reviewer, reeve, find-out and workflow; the others
// come with Phase 3 (repository, step, handoff, yields-to) and the
// resources ticket (missing-resource).
const (
	OffGlobal          ManagedHookOff = "off:global"
	OffInvalid         ManagedHookOff = "off:invalid"
	OffUnsupportedTool ManagedHookOff = "off:unsupported-tool"
	OffRepository      ManagedHookOff = "off:repository"
	OffStep            ManagedHookOff = "off:step"
	OffSpawn           ManagedHookOff = "off:spawn"
	OffHandoff         ManagedHookOff = "off:handoff"
	OffReadOnly        ManagedHookOff = "off:read-only"
	OffReviewer        ManagedHookOff = "off:reviewer"
	OffReeve           ManagedHookOff = "off:reeve"
	OffFindOut         ManagedHookOff = "off:find-out"
	// OffWorkflow is Phase 1 only: an execution with a workflow attempt
	// gets no managed hooks until Phase 3's step-complete skip, when the
	// reason goes.
	OffWorkflow        ManagedHookOff = "off:workflow"
	OffYieldsTo        ManagedHookOff = "off:yields-to"
	OffMissingResource ManagedHookOff = "off:missing-resource"
)

// The sources of a chosen off: what execution_managed_hooks_off records,
// and what a relaunch carries. Phase 1 writes spawn only.
const (
	ChosenOffSpawn   = "spawn"
	ChosenOffStep    = "step"
	ChosenOffHandoff = "handoff"
)

// HookSource decides an agent's managed hooks from the same request the
// capability source gets. It is separate from the CapabilitySource
// (skills): different layers, different records.
type HookSource interface {
	ManagedHooks(ctx context.Context, req CapabilityRequest) (ManagedHookSet, error)
}

// ManagedHookSet is a HookSource's decision for one agent.
type ManagedHookSet struct {
	// Hooks is every hook in the managed-hooks folder, sorted by name,
	// each on or off with its reasons.
	Hooks []ManagedHookDecision
	// UnknownOffs are the names in the request's HooksOff that are no
	// managed hook: ignored, with a note.
	UnknownOffs []string
}

// On is what the agent is given: the hooks with no off reason.
func (s ManagedHookSet) On() []ManagedHook {
	var on []ManagedHook
	for _, d := range s.Hooks {
		if d.On() {
			on = append(on, d.Hook)
		}
	}
	return on
}

// Skipped names the hooks that would have applied but for a tool that
// cannot run managed hooks: what the skip note names.
func (s ManagedHookSet) Skipped() []string {
	var names []string
	for _, d := range s.Hooks {
		if d.ToolSkipped {
			names = append(names, d.Hook.Name)
		}
	}
	return names
}

// ManagedHookDecision is one hook for one agent.
type ManagedHookDecision struct {
	Hook ManagedHook
	// Off is every reason the hook is off, in layer order; empty when on.
	Off []ManagedHookOff
	// ChosenOff are the sources that chose it off (spawn; later step,
	// handoff).
	ChosenOff []string
	// ToolSkipped says the hook is off only because the tool cannot run
	// managed hooks.
	ToolSkipped bool
}

// On reports whether the agent is given the hook.
func (d ManagedHookDecision) On() bool { return len(d.Off) == 0 }

// ManagedHooks decides every hook of the managed-hooks folder for one
// agent, scanned now, so a hook added or changed reaches the next spawn.
// The layers, in order (spec "Policy: the HookSource"):
//
//  1. global: managed_hook_states (off:global);
//  2. manifest: validity (off:invalid) and agent_tools (off:unsupported-tool);
//  3. repository: Phase 3 (off:repository);
//  4. step: Phase 3 (off:step);
//  5. spawn: the request's HooksOff (off:spawn, chosen);
//  6. handoff: Phase 3 (off:handoff);
//  7. auto-off: read-only, reviewer, Reeve, find-out, workflow (Phase 1),
//     and a tool that cannot run hooks (off:unsupported-tool);
//  8. launch checks: yields_to (Phase 3) and a missing resource (the
//     resources ticket).
func (s *ManagedHookService) ManagedHooks(ctx context.Context, req CapabilityRequest) (ManagedHookSet, error) {
	list, err := s.List(ctx)
	if err != nil {
		return ManagedHookSet{}, err
	}
	var set ManagedHookSet
	for _, name := range req.HooksOff {
		known := slices.ContainsFunc(list, func(l ManagedHookListing) bool { return l.Name == name })
		if !known && !slices.Contains(set.UnknownOffs, name) {
			set.UnknownOffs = append(set.UnknownOffs, name)
		}
	}
	for _, listing := range list {
		set.Hooks = append(set.Hooks, decideManagedHook(req, listing))
	}
	return set, nil
}

// decideManagedHook runs every layer on one hook.
func decideManagedHook(req CapabilityRequest, listing ManagedHookListing) ManagedHookDecision {
	d := ManagedHookDecision{Hook: ManagedHook{Name: listing.Name, Path: listing.Path, Events: listing.Events, Manifest: listing.Hook}}
	var offs offList
	// 1. Global.
	offs.addIf(!listing.Enabled, OffGlobal)
	// 2. Manifest.
	offs.addIf(listing.Err != "", OffInvalid)
	offs.addIf(len(listing.AgentTools) > 0 && !slices.Contains(listing.AgentTools, req.AgentKind), OffUnsupportedTool)
	// 3. Repository and 4. step: Phase 3.
	// 5. Spawn.
	if slices.Contains(req.HooksOff, listing.Name) {
		offs.add(OffSpawn)
		d.ChosenOff = append(d.ChosenOff, ChosenOffSpawn)
	}
	// 6. Handoff: Phase 3.
	// 7. Auto-off, from read-only facts about the agent. read_only opts a
	// hook into read-only and reviewer agents, never the Reeve or a
	// find-out.
	offs.addIf(req.ReadOnly && !listing.ReadOnly, OffReadOnly)
	offs.addIf(req.Slot == SlotReviewer && !listing.ReadOnly, OffReviewer)
	offs.addIf(req.Reeve, OffReeve)
	offs.addIf(req.HandoffPurpose == HandoffFindOut, OffFindOut)
	offs.addIf(req.WorkflowAttemptID != "" || req.WorkflowRunID != "", OffWorkflow)
	// 8. Launch checks (yields_to, a missing resource): Phase 3 and the
	// resources ticket.
	if !req.ToolTakesHooks {
		d.ToolSkipped = len(offs) == 0
		offs.add(OffUnsupportedTool)
	}
	d.Off = offs
	return d
}

// offList is a hook's off reasons, each once, in the order found.
type offList []ManagedHookOff

func (o *offList) add(reason ManagedHookOff) {
	if !slices.Contains(*o, reason) {
		*o = append(*o, reason)
	}
}

func (o *offList) addIf(cond bool, reason ManagedHookOff) {
	if cond {
		o.add(reason)
	}
}

// decideManagedHooks asks the HookSource for an agent's managed hooks,
// records them on the execution, and notes on the task what the operator
// should know: names in hooks_off that are no hook, and hooks skipped for a
// tool that cannot run them. It returns the hooks the agent is given;
// decided is false without a source, or when it failed (logged): the agent
// then gets none and nothing is recorded.
func (a *agentService) decideManagedHooks(ctx context.Context, req CapabilityRequest) (on []ManagedHook, decided bool) {
	if a.managedHooks == nil {
		return nil, false
	}
	set, err := a.managedHooks.ManagedHooks(ctx, req)
	if err != nil {
		log.Printf("managed hooks: deciding for %s (%s): %v", req.TaskID, req.ExecutionID, err)
		return nil, false
	}
	hooks := make([]store.ExecutionManagedHook, 0, len(set.Hooks))
	var chosen []store.ExecutionManagedHookOff
	for _, d := range set.Hooks {
		off := make([]string, 0, len(d.Off))
		for _, reason := range d.Off {
			off = append(off, string(reason))
		}
		hooks = append(hooks, store.ExecutionManagedHook{Name: d.Hook.Name, Events: d.Hook.Events, Off: off})
		for _, source := range d.ChosenOff {
			chosen = append(chosen, store.ExecutionManagedHookOff{Name: d.Hook.Name, Source: source})
		}
	}
	if err := a.store.RecordExecutionManagedHooks(ctx, store.ManagedHooksOwner{ExecutionID: req.ExecutionID, TaskID: req.TaskID, Slot: req.Slot}, hooks, chosen); err != nil {
		log.Printf("managed hooks: recording for %s (%s): %v", req.TaskID, req.ExecutionID, err)
	}
	if len(set.UnknownOffs) > 0 {
		a.noteManagedHooks(ctx, req.TaskID, fmt.Sprintf("hooks_off named no managed hook, so it was ignored: %s.", strings.Join(set.UnknownOffs, ", ")))
	}
	if skipped := set.Skipped(); len(skipped) > 0 {
		a.noteManagedHooks(ctx, req.TaskID, fmt.Sprintf("Managed hooks skipped for %s, which cannot run them yet: %s. The agent started without them.",
			req.AgentKind, strings.Join(skipped, ", ")))
	}
	return set.On(), true
}

func (a *agentService) noteManagedHooks(ctx context.Context, taskID, body string) {
	if _, err := AddTaskNote(ctx, a.store, taskID, body, "legato"); err != nil {
		log.Printf("managed hooks: noting on %s: %v", taskID, err)
	}
}

// ManagedHooksReader reads what an agent's spawn recorded of its managed
// hooks: GET /api/agents/{task_id}/managed-hooks.
type ManagedHooksReader interface {
	// AgentManagedHooks is the latest execution of a task's agent (its
	// slot from WithAgentSlot, or a <task>_<slot> address).
	AgentManagedHooks(ctx context.Context, taskID string) (AgentManagedHooks, error)
}

// AgentManagedHooks is an execution's managed hooks as its spawn decided
// them.
type AgentManagedHooks struct {
	ExecutionID string `json:"execution_id"`
	// Recorded is false for an execution with no records: one from before
	// managed hooks, or one started while the folder held none.
	Recorded bool               `json:"recorded"`
	Hooks    []AgentManagedHook `json:"hooks"`
}

// AgentManagedHook is one hook of an execution's records.
type AgentManagedHook struct {
	Name   string   `json:"name"`
	Events []string `json:"events"`
	On     bool     `json:"on"`
	// Off is every reason it is off (off:global, ...); empty when on.
	Off []string `json:"off"`
	// ChosenOff are the sources that chose it off: spawn (later step,
	// handoff).
	ChosenOff []string `json:"chosen_off"`
}

func (a *agentService) AgentManagedHooks(ctx context.Context, taskID string) (AgentManagedHooks, error) {
	executionID, err := a.managedHooksExecution(ctx, taskID)
	if err != nil {
		return AgentManagedHooks{}, err
	}
	out := AgentManagedHooks{ExecutionID: executionID, Hooks: []AgentManagedHook{}}
	if executionID == "" {
		return out, nil
	}
	hooks, offs, err := a.store.ExecutionManagedHooks(ctx, executionID)
	if err != nil {
		return AgentManagedHooks{}, err
	}
	out.Recorded = len(hooks) > 0
	for _, hook := range hooks {
		row := AgentManagedHook{Name: hook.Name, Events: nonNil(hook.Events), On: len(hook.Off) == 0, Off: nonNil(hook.Off), ChosenOff: []string{}}
		for _, off := range offs {
			if off.Name == hook.Name {
				row.ChosenOff = append(row.ChosenOff, off.Source)
			}
		}
		out.Hooks = append(out.Hooks, row)
	}
	return out, nil
}

// managedHooksExecution is the execution whose managed hooks a slot's
// agent shows: its newest session's, running or not, else, once a stop has
// removed the session, the newest execution recorded for the slot.
func (a *agentService) managedHooksExecution(ctx context.Context, address string) (string, error) {
	ctx, taskID := agentAddress(ctx, address)
	slot := AgentSlotFrom(ctx)
	session, err := a.store.LatestAgentSessionInSlot(ctx, taskID, slot)
	if err == nil {
		return session.ExecutionID, nil
	}
	if !errors.Is(err, store.ErrNotFound) {
		return "", err
	}
	if executionID, recErr := a.store.LatestManagedHooksExecution(ctx, taskID, slot); recErr == nil {
		return executionID, nil
	} else if !errors.Is(recErr, store.ErrNotFound) {
		return "", recErr
	}
	return "", err
}

func nonNil(list []string) []string {
	if list == nil {
		return []string{}
	}
	return list
}
