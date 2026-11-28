package service

import (
	"context"
	"errors"
	"log"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/cpave3/legato/internal/engine/managedhooks"
	"github.com/cpave3/legato/internal/engine/skills"
	"github.com/cpave3/legato/internal/engine/store"
)

// Agent capabilities are what Legato gives an agent beyond its tool and its
// work, the same whichever tool runs it and wherever: today the operator's
// skills (LEG-216), and the seam for managed hooks (LEG-687). The seams are
// meant for more:
//
//   - A CapabilitySource chooses what an agent gets from a CapabilityRequest.
//     SkillService gives every agent every skill that is on; a profile (a
//     named set of skills and MCP tools, LEG-215) would be another source
//     choosing by the request's repository, role or kind.
//   - AgentCapabilities is what was chosen, a field per kind: Skills, and
//     Hooks, which no source fills yet. MCP broker tools (LEG-212) would be
//     another.
//   - Each execution gets a folder of its own under CapabilityFolders, which
//     the sandbox mounts read-only and which goes when the execution ends; a
//     tool's adapter says how its tool finds what is in it, one interface
//     per kind (SkillsAdapter, HooksAdapter). Each kind is given on its own
//     and in a bundle folder of its own, so one that fails costs only itself.
//     A capability is copied in at the start, so what an agent has does not
//     change while it runs: a change reaches agents started or reloaded
//     afterwards.
//
// See docs/ai/skills.md.

// CapabilityRequest is the agent a CapabilitySource chooses for: what the
// spawn knows of it. A field the spawn does not know is empty.
type CapabilityRequest struct {
	TaskID      string
	ExecutionID string
	AgentKind   string
	Role        string
	Slot        string
	WorkDir     string
	// Repository is the repository the agent works in, host/owner/name as
	// config.NormalizeRepository writes it: the key repository resources
	// and guidance use. "" outside a repository Legato can name.
	Repository string
	// WorkflowRunID, WorkflowAttemptID and WorkflowStepID are the workflow
	// step attempt that started the agent, if one did.
	WorkflowRunID     string
	WorkflowAttemptID string
	WorkflowStepID    string
	// ReadOnly says the agent may look but not change its checkout
	// (AgentSpawnOptions.ReadOnlyCheckout: a reviewer, a find-out).
	ReadOnly bool
	// Reeve says the agent is the Reeve.
	Reeve bool
	// HandoffPurpose is a handed-off task's purpose (HandoffMakeChange,
	// HandoffFindOut); "" for any other agent. A main-slot agent started
	// again on a handed-off card (restart, resume) gets it from the open
	// handoff, since the spawn does not say it again.
	HandoffPurpose string
	// HooksOff are the managed hooks the spawn turns off (hooks_off).
	HooksOff []string
	// ToolTakesHooks says the agent's tool can run managed hooks: its
	// adapter is a HooksAdapter.
	ToolTakesHooks bool
}

// AgentCapabilities is what an agent is given, one field per kind. Each
// kind is given on its own (giveCapabilities): one that fails never takes
// another from the agent.
type AgentCapabilities struct {
	Skills []skills.Skill
	// Hooks are the managed hooks the agent runs (LEG-687): those the
	// HookSource decided on.
	Hooks []ManagedHook
}

// ManagedHook is one managed hook an agent is given: a folder of the
// operator's, copied for the execution as a skill is.
type ManagedHook struct {
	// Name is the hook's folder name, the folder its copy goes in.
	Name string
	// Path is the hook's folder.
	Path string
	// Events are the author events it runs on (stop, session_start, ...),
	// which the tool's bundle registers.
	Events []string
	// Manifest is the hook as scanned: its handlers (commands, time
	// limits, matchers), resources and yields_to. Empty for a hook made
	// by hand.
	Manifest managedhooks.Hook
}

// CapabilitySource chooses an agent's capabilities.
type CapabilitySource interface {
	Capabilities(ctx context.Context, req CapabilityRequest) (AgentCapabilities, error)
}

// SkillsAdapter is an adapter whose tool can load skills from a folder
// Legato makes for one execution.
type SkillsAdapter interface {
	// SkillsBundle writes in root what the tool needs around a folder of
	// skills (a plugin's or a mod's manifest), adds to env what makes the
	// tool load it, and returns the folder each skill's folder goes in.
	SkillsBundle(root string, env map[string]string) (skillsDir string, err error)
}

// HooksAdapter is an adapter whose tool can run managed hooks from a folder
// Legato makes for one execution. A tool is a supported tool for managed
// hooks exactly when its adapter implements it.
type HooksAdapter interface {
	// HooksBundle writes in root what the tool needs to run the hooks (a
	// plugin's or a mod's manifest, an entry calling the runner for each
	// event and for the internal canary and turn reset), adds to env what
	// makes the tool load it, and returns the bundle's folder, in which
	// Legato then lays out what the runner reads (writeHooksContract).
	// events maps every delivered event the hooks use to the longest time
	// limit, in seconds, of the hooks that run on it.
	HooksBundle(root string, events map[string]int, env map[string]string) (bundleDir string, err error)
}

// CapabilityFolders holds each execution's capabilities, one folder per
// execution under Root (<data_dir>/capabilities), and one per tool inside
// it. The sandbox shows an execution its own folder, read-only.
type CapabilityFolders struct {
	Root string
}

// Dir is an execution's folder. Empty without a Root or for an execution
// ID that cannot name a folder.
func (c CapabilityFolders) Dir(executionID string) string {
	if c.Root == "" || !executionFolder.MatchString(executionID) {
		return ""
	}
	return filepath.Join(c.Root, executionID)
}

// Remove removes an execution's folder.
func (c CapabilityFolders) Remove(executionID string) error {
	dir := c.Dir(executionID)
	if dir == "" {
		return nil
	}
	return os.RemoveAll(dir)
}

func (c CapabilityFolders) removeLogged(executionID string) {
	if err := c.Remove(executionID); err != nil {
		log.Printf("capabilities: removing the folder of %s: %v", executionID, err)
	}
}

// Sweep removes the folders of executions that have ended, as
// SandboxScratch.Sweep does: a running execution's, or one this Legato does
// not know, is left alone. It returns how many it removed.
func (c CapabilityFolders) Sweep(ctx context.Context, statusOf ExecutionStatus) (int, error) {
	if c.Root == "" {
		return 0, nil
	}
	entries, err := os.ReadDir(c.Root)
	if os.IsNotExist(err) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	cleared := 0
	var errs []error
	for _, entry := range entries {
		id := entry.Name()
		if !entry.IsDir() || !executionFolder.MatchString(id) {
			continue
		}
		status, found, err := statusOf(ctx, id)
		if err != nil {
			return cleared, err
		}
		if !found || status == store.ExecutionRunning {
			continue
		}
		if err := c.Remove(id); err != nil {
			errs = append(errs, err)
			continue
		}
		cleared++
	}
	return cleared, errors.Join(errs...)
}

// needsRepositoryKey says whether a spawn works out the repository it
// works in for something other than its project's learnings and guidance:
// the repository's own resources, or the capability request.
func (a *agentService) needsRepositoryKey() bool {
	return a.repoResources != nil || a.capabilities != nil || a.managedHooks != nil
}

// handoffPurpose is the purpose of the handoff a spawn is for: the
// options', else, for a main-slot agent, the open handoff's of its card, so
// an agent started again (restart, resume, Reload) keeps it.
func (a *agentService) handoffPurpose(ctx context.Context, taskID string, named bool, opt AgentSpawnOptions) string {
	if opt.HandoffPurpose != "" || named {
		return opt.HandoffPurpose
	}
	if h, err := a.store.OpenHandoffForTask(ctx, taskID); err == nil {
		return h.Purpose
	}
	return ""
}

// repositoryOfKey is the repository (host/owner/name) a project identity
// key names, or "" for a key that names none (a folder's).
func repositoryOfKey(key string) string {
	if repo, ok := strings.CutPrefix(key, "repo:"); ok {
		return repo
	}
	return ""
}

// spawnWorkflowStep is the workflow step attempt a spawn is for: the run and
// attempt from the options, else from the workflow identity the session is
// given (a resumed workflow agent names only its run), and the step from
// that identity.
func spawnWorkflowStep(opt AgentSpawnOptions) (runID, attemptID, stepID string) {
	runID, attemptID = opt.WorkflowRunID, opt.WorkflowAttemptID
	if runID == "" {
		runID = opt.ExtraEnv["LEGATO_WORKFLOW_RUN_ID"]
	}
	if attemptID == "" {
		attemptID = opt.ExtraEnv["LEGATO_WORKFLOW_ATTEMPT_ID"]
	}
	return runID, attemptID, opt.ExtraEnv["LEGATO_WORKFLOW_STEP_ID"]
}

// giveCapabilities copies what the source chooses for an agent into its
// execution's folder, where its tool's adapter says the tool finds it, and
// adds to env what the tool needs. Each kind is given on its own, if the
// adapter can take it: skills (SkillsAdapter), then managed hooks
// (HooksAdapter). A failure leaves the agent without the kind that failed,
// logged, and with every other: a capability is an addition, never a reason
// not to start.
func (a *agentService) giveCapabilities(ctx context.Context, req CapabilityRequest, adapter AIToolAdapter, env map[string]string) {
	if adapter == nil {
		return
	}
	_, req.ToolTakesHooks = adapter.(HooksAdapter)
	// The managed hooks are decided and recorded whether or not there is
	// a folder to give them in: the records say what the agent has.
	hooks, decided := a.decideManagedHooks(ctx, req)
	dir := a.capabilityFolders.Dir(req.ExecutionID)
	if dir == "" {
		return
	}
	given := a.chooseCapabilities(ctx, req)
	if decided {
		given.Hooks = hooks
	}
	if req.ToolTakesHooks {
		a.noteUndeliveredHookEvents(ctx, req.TaskID, given.Hooks)
	}
	kinds := capabilityKinds{dir: dir, tool: filepath.Join(dir, adapter.Name()), req: req, env: env, now: time.Now().UTC()}
	kinds.giveSkills(adapter, given.Skills)
	kinds.giveHooks(adapter, given.Hooks)
}

// chooseCapabilities is what the capability source chooses; nothing
// without one, or when it fails (logged).
func (a *agentService) chooseCapabilities(ctx context.Context, req CapabilityRequest) AgentCapabilities {
	if a.capabilities == nil {
		return AgentCapabilities{}
	}
	given, err := a.capabilities.Capabilities(ctx, req)
	if err != nil {
		log.Printf("capabilities: choosing for %s (%s): %v", req.TaskID, req.ExecutionID, err)
		return AgentCapabilities{}
	}
	return given
}

// giveSkills gives the skills, if the adapter can take them.
func (k capabilityKinds) giveSkills(adapter AIToolAdapter, given []skills.Skill) {
	bundler, ok := adapter.(SkillsAdapter)
	if !ok || len(given) == 0 {
		return
	}
	k.give("skills", func(bundleEnv map[string]string) error {
		skillsDir, err := bundler.SkillsBundle(k.tool, bundleEnv)
		if err != nil {
			return err
		}
		for _, skill := range given {
			k.copyLogged("skill", skill, filepath.Join(skillsDir, skill.Name))
		}
		return nil
	})
}

// giveHooks gives the managed hooks, if the adapter can take them: the
// tool's bundle, then what the runner reads in it (writeHooksContract:
// each hook's copy and hook.env, the seed, the context and the index).
func (k capabilityKinds) giveHooks(adapter AIToolAdapter, given []ManagedHook) {
	bundler, ok := adapter.(HooksAdapter)
	if !ok || len(given) == 0 {
		return
	}
	k.give("managed hooks", func(bundleEnv map[string]string) error {
		bundle, err := bundler.HooksBundle(k.tool, deliveredHookTimeouts(given), bundleEnv)
		if err != nil {
			return err
		}
		return writeHooksContract(bundle, given, k.req, k.now)
	})
}

// copyLogged copies one folder into a bundle; a failure leaves the agent
// without that one, logged, and is reported.
func (k capabilityKinds) copyLogged(what string, folder skills.Skill, dst string) bool {
	return copyFolderLogged(what, folder, dst, k.req)
}

func copyFolderLogged(what string, folder skills.Skill, dst string, req CapabilityRequest) bool {
	if err := skills.Copy(folder, dst); err != nil {
		log.Printf("capabilities: %s %s for %s (%s): %v", what, folder.Name, req.TaskID, req.ExecutionID, err)
		return false
	}
	return true
}

// capabilityKinds gives one execution's capabilities kind by kind, each in
// its own bundle folder inside the tool's folder.
type capabilityKinds struct {
	dir  string // the execution's folder
	tool string // the tool's folder inside it
	req  CapabilityRequest
	env  map[string]string
	now  time.Time // when the agent starts: the seed's time
}

// give writes one kind's bundle with write, which adds to the copy of the
// session's environment it is handed. The environment is changed only once
// write succeeds, so a tool is never pointed at a bundle that failed; on a
// failure, what write left in the tool's folder is removed, and nothing a
// kind before it wrote.
func (k capabilityKinds) give(kind string, write func(bundleEnv map[string]string) error) {
	before := folderEntries(k.tool)
	bundleEnv := maps.Clone(k.env)
	if err := write(bundleEnv); err != nil {
		log.Printf("capabilities: the %s bundle of %s (%s): %v", kind, k.req.TaskID, k.req.ExecutionID, err)
		k.removeAddedSince(before)
		return
	}
	maps.Copy(k.env, bundleEnv)
}

// removeAddedSince removes what was added to the tool's folder since before
// was listed, then the tool's and the execution's folders if that leaves
// them empty: an agent given nothing has no folder.
func (k capabilityKinds) removeAddedSince(before map[string]bool) {
	for name := range folderEntries(k.tool) {
		if before[name] {
			continue
		}
		if err := os.RemoveAll(filepath.Join(k.tool, name)); err != nil {
			log.Printf("capabilities: removing %s of %s: %v", name, k.req.ExecutionID, err)
		}
	}
	// os.Remove removes only an empty folder.
	_ = os.Remove(k.tool)
	_ = os.Remove(k.dir)
}

// folderEntries names what is in a folder; none for one that is not there.
func folderEntries(dir string) map[string]bool {
	entries, _ := os.ReadDir(dir)
	names := make(map[string]bool, len(entries))
	for _, entry := range entries {
		names[entry.Name()] = true
	}
	return names
}
