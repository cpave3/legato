package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/cpave3/legato/internal/engine/gitrepo"
	"github.com/cpave3/legato/internal/engine/managedhooks"
	"github.com/cpave3/legato/internal/engine/skills"
	"github.com/cpave3/legato/internal/engine/store"
)

// The managed-hooks bundle (spec LEG-687, "Per-execution delivery" and "The
// runner"): what Legato lays out in the folder a HooksAdapter returns, the
// contract `legato agent managed-hooks run` reads, the same for every tool.
// See docs/ai/managed-hooks.md § The bundle.
//
//	<bundle>/
//	  index.json          the hooks the agent runs: hookIndex, written last
//	  context.json        the runner's legato object, fixed fields: hookContext
//	  seed.json           the checkout at this launch: hookSeed
//	  copies/<name>/      each hook's files, copied as a skill's are
//	  env/<name>.env      each hook's hook.env: KEY=VALUE lines
//
// The tool's own files (Claude Code's .claude-plugin/ and hooks/) sit beside
// them. Every path in the files is absolute: the sandbox shows the
// capability folder at its own path, read-only.

// hookContractVersion is the version of every file of the contract.
const hookContractVersion = 1

// hookStateRoot holds each hook's state folder,
// /tmp/legato-hooks/<execution>/<name>, which the runner makes (G8).
const hookStateRoot = "/tmp/legato-hooks"

// deliveredHookEvents are the author events Phase 1 delivers. pre_tool
// and post_tool are validated and listed but registered for nothing until
// Phase 2.
var deliveredHookEvents = []managedhooks.Event{managedhooks.SessionStart, managedhooks.Stop}

// hookSeed is seed.json: what the agent's checkout was at this launch, so a
// hook can compare against it (CodeEval builds its base from head, CE-2).
// A relaunch is a new execution and is seeded again. head and branch are
// empty outside a git checkout, branch also when HEAD is detached.
type hookSeed struct {
	Version     int    `json:"version"`
	Head        string `json:"head"`
	Branch      string `json:"branch"`
	Repo        string `json:"repo"`
	TaskID      string `json:"task_id"`
	ExecutionID string `json:"execution_id"`
	WorkDir     string `json:"work_dir"`
	// Time is when the agent started, RFC 3339 in UTC.
	Time string `json:"time"`
}

// hookContext is context.json: the fields of the runner's legato object
// that do not change during the execution. The runner adds tool-time ones
// (event, stop_id, turn, stop_blocks_this_turn) and each hook's state_dir.
type hookContext struct {
	Version     int          `json:"version"`
	Tool        string       `json:"tool"`
	TaskID      string       `json:"task_id"`
	ExecutionID string       `json:"execution_id"`
	Slot        string       `json:"slot"`
	Role        string       `json:"role"`
	Repo        string       `json:"repo"`
	WorkDir     string       `json:"work_dir"`
	Workflow    hookWorkflow `json:"workflow"`
	Seed        string       `json:"seed"`
}

// hookWorkflow is the workflow step attempt that started the agent; empty
// for any other agent.
type hookWorkflow struct {
	RunID     string `json:"run_id"`
	AttemptID string `json:"attempt_id"`
	StepID    string `json:"step_id"`
}

// hookIndex is index.json: every hook the agent runs, sorted by name, each
// with the delivered events it runs on. A hook whose copy failed is not in
// it.
type hookIndex struct {
	Version int              `json:"version"`
	Hooks   []hookIndexEntry `json:"hooks"`
}

type hookIndexEntry struct {
	Name string `json:"name"`
	// Dir is the hook's copy, which its commands run from.
	Dir string `json:"dir"`
	// Env is its hook.env.
	Env      string `json:"env"`
	StateDir string `json:"state_dir"`
	// Events are its handlers of the delivered events; none for a hook
	// that has only events not delivered yet.
	Events map[string]hookIndexHandler `json:"events"`
}

type hookIndexHandler struct {
	Command string `json:"command"`
	// Timeout is in seconds.
	Timeout int `json:"timeout"`
}

// hookHandlers are a hook's handlers of the delivered events. A hook made
// by hand, with no manifest, gets each event's default time limit.
func hookHandlers(hook ManagedHook) map[string]hookIndexHandler {
	handlers := map[string]hookIndexHandler{}
	for _, name := range hook.Events {
		event := managedhooks.Event(name)
		if !slices.Contains(deliveredHookEvents, event) {
			continue
		}
		handler, ok := hook.Manifest.Events[event]
		if !ok || handler.Timeout == 0 {
			handler.Timeout, _, _ = managedhooks.TimeLimit(event)
		}
		handlers[name] = hookIndexHandler{Command: handler.Command, Timeout: handler.Timeout}
	}
	return handlers
}

// deliveredHookTimeouts maps each delivered event the hooks use to the
// longest time limit of the hooks that run on it: what the tool's bundle
// registers.
func deliveredHookTimeouts(given []ManagedHook) map[string]int {
	events := map[string]int{}
	for _, hook := range given {
		for event, handler := range hookHandlers(hook) {
			events[event] = max(events[event], handler.Timeout)
		}
	}
	return events
}

// writeHooksContract lays out what the runner reads in a tool's bundle:
// each hook's copy and hook.env, then the seed and the context, then the
// index. A hook whose copy fails is left out, logged; any other failure is
// the bundle's.
func writeHooksContract(bundle string, given []ManagedHook, req CapabilityRequest, now time.Time) error {
	seedPath := filepath.Join(bundle, "seed.json")
	index := hookIndex{Version: hookContractVersion, Hooks: []hookIndexEntry{}}
	sorted := slices.SortedFunc(slices.Values(given), func(a, b ManagedHook) int { return strings.Compare(a.Name, b.Name) })
	for _, hook := range sorted {
		entry := hookIndexEntry{
			Name:     hook.Name,
			Dir:      filepath.Join(bundle, "copies", hook.Name),
			Env:      filepath.Join(bundle, "env", hook.Name+".env"),
			StateDir: filepath.Join(hookStateRoot, req.ExecutionID, hook.Name),
			Events:   hookHandlers(hook),
		}
		if !copyFolderLogged("managed hook", skills.Skill{Name: hook.Name, Path: hook.Path}, entry.Dir, req) {
			continue
		}
		if err := writeHookEnv(entry, seedPath); err != nil {
			return err
		}
		index.Hooks = append(index.Hooks, entry)
	}
	if err := writeContractFile(seedPath, newHookSeed(req, now)); err != nil {
		return err
	}
	slot := req.Slot
	if slot == "" {
		slot = store.SlotMain
	}
	hookCtx := hookContext{
		Version: hookContractVersion, Tool: req.AgentKind, TaskID: req.TaskID, ExecutionID: req.ExecutionID,
		Slot: slot, Role: req.Role, Repo: req.Repository, WorkDir: req.WorkDir,
		Workflow: hookWorkflow{RunID: req.WorkflowRunID, AttemptID: req.WorkflowAttemptID, StepID: req.WorkflowStepID},
		Seed:     seedPath,
	}
	if err := writeContractFile(filepath.Join(bundle, "context.json"), hookCtx); err != nil {
		return err
	}
	return writeContractFile(filepath.Join(bundle, "index.json"), index)
}

// newHookSeed reads the agent's checkout as git's files: no git runs in it.
func newHookSeed(req CapabilityRequest, now time.Time) hookSeed {
	seed := hookSeed{Version: hookContractVersion, Repo: req.Repository, TaskID: req.TaskID, ExecutionID: req.ExecutionID,
		WorkDir: req.WorkDir, Time: now.UTC().Format(time.RFC3339)}
	if req.WorkDir == "" {
		return seed
	}
	branch, commit, err := gitrepo.HeadOf(req.WorkDir)
	if err != nil {
		if !errors.Is(err, gitrepo.ErrNoCheckout) {
			log.Printf("managed hooks: the seed of %s (%s): %v", req.TaskID, req.ExecutionID, err)
		}
		return seed
	}
	seed.Head, seed.Branch = commit, branch
	return seed
}

// writeHookEnv writes a hook's hook.env: one KEY=VALUE line each, unquoted,
// the value everything after the first =. The resource variables
// (LEGATO_HOOK_RESOURCE_<NAME>) are added by the resources ticket.
func writeHookEnv(entry hookIndexEntry, seedPath string) error {
	vars := [][2]string{
		{"LEGATO_HOOK_NAME", entry.Name},
		{"LEGATO_HOOK_DIR", entry.Dir},
		{"LEGATO_HOOK_STATE_DIR", entry.StateDir},
		{"LEGATO_HOOK_SEED", seedPath},
	}
	var b strings.Builder
	for _, v := range vars {
		if strings.ContainsAny(v[1], "\n\r\x00") {
			return fmt.Errorf("%s of %s cannot be written on one line", v[0], entry.Name)
		}
		b.WriteString(v[0] + "=" + v[1] + "\n")
	}
	return writeReadOnly(entry.Env, []byte(b.String()))
}

func writeContractFile(path string, value any) error {
	content, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	return writeReadOnly(path, append(content, '\n'))
}

// writeReadOnly writes a file of the contract, read-only: the runner and
// the hooks read it and never change it.
func writeReadOnly(path string, content []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, content, 0o444)
}

// noteUndeliveredHookEvents notes on the task, once per execution, the
// hooks the agent is given that have events Phase 1 does not deliver
// (pre_tool, post_tool).
func (a *agentService) noteUndeliveredHookEvents(ctx context.Context, taskID string, given []ManagedHook) {
	var named []string
	for _, hook := range given {
		var undelivered []string
		for _, event := range hook.Events {
			if !slices.Contains(deliveredHookEvents, managedhooks.Event(event)) {
				undelivered = append(undelivered, event)
			}
		}
		if len(undelivered) > 0 {
			named = append(named, fmt.Sprintf("%s (%s)", hook.Name, strings.Join(undelivered, ", ")))
		}
	}
	if len(named) == 0 {
		return
	}
	a.noteManagedHooks(ctx, taskID, fmt.Sprintf("Managed hooks' pre_tool and post_tool events are not delivered yet, so these run only at session_start and stop, if at all: %s.",
		strings.Join(named, "; ")))
}
