package service

import (
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/cpave3/legato/internal/engine/store"
	"github.com/cpave3/legato/internal/engine/swarm"
)

// TmuxManager abstracts tmux operations for testability.
type TmuxManager interface {
	Spawn(name, workDir string, width, height int, envVars ...string) error
	Kill(name string) error
	Capture(name string) (string, error)
	CaptureWithEscapes(name string) (string, error)
	Attach(name string) *exec.Cmd
	ListSessions() ([]string, error)
	IsAlive(name string) (bool, error)
	SendKeys(name, keys string) error
	SendKey(name, key string) error
	SendKeysLine(name, line string) error
	SendKeysMultiline(name, payload string) error
	SendKeysShellCommand(name, command string) error
	PipeOutput(name string) (io.Reader, func(), error)
	SetEnv(sessionName, key, value string) error
	SetOption(sessionName, key, value string) error
	PaneCommands() (map[string]string, error)
}

// AgentSession represents a running or completed agent session.
type AgentSession struct {
	ID           int
	TaskID       string
	Title        string
	TmuxSession  string
	Command      string
	Status       string
	Activity     string // "working", "waiting", or "" (idle)
	Role         string // "conductor", "" for non-swarm, or worker role label
	ParentTaskID string // when set, this session belongs to a swarm
	SubtaskID    string // when set, this session is a swarm worker
	StartedAt    time.Time
	EndedAt      *time.Time

	// Swarm worker metadata, hydrated from the subtask row.
	Description string
	Prompt      string   // per-worker instructions (initial brief)
	Scope       []string // file globs
}

// DurationData holds aggregated state durations for a task.
type DurationData struct {
	Working time.Duration
	Waiting time.Duration
}

// AgentSpawnOptions controls per-spawn behavior — used by swarm orchestration
// to inject role tags, scope-based conflict checks, a role system prompt, and
// the per-worker initial brief that the launch command delivers.
type AgentSpawnOptions struct {
	Role         string   // free-form role label (e.g. "conductor", "backend"); "" for non-swarm
	ParentTaskID string   // parent task ID when spawning a swarm sub-task
	SubtaskID    string   // sub-task ID being assigned
	Scope        []string // scope globs for conflict checks
	WorkingDir   string   // override for tmux session working directory; falls back to agent service workDir
	AgentKind    string   // adapter name to use; "" → default adapter
	Tier         string   // adapter tier name (selects per-tier launch_args from cfg.Adapters.<kind>.tiers); "" → base launch_args only
	Brief        string   // the per-worker initial brief; conductors leave this empty
	StrictScope  bool     // when true, scope conflicts hard-block the spawn; otherwise advisory
}

// AgentSpawnConflict describes a scope-overlap warning encountered at spawn time.
// It is returned alongside a non-error from SpawnAgent when StrictScope is false
// and the spawn proceeds. Callers (e.g. SwarmService) can use it to deliver an
// advisory `[swarm event] scope warning` to the conductor.
type AgentSpawnConflict struct {
	SiblingSubtaskID string
	SiblingTitle     string
	Files            []string
}

// AgentService manages agent session lifecycle.
type AgentService interface {
	SpawnAgent(ctx context.Context, taskID string, width, height int, opts ...AgentSpawnOptions) error
	KillAgent(ctx context.Context, taskID string) error
	ListAgents(ctx context.Context) ([]AgentSession, error)
	ListAgentsByParent(ctx context.Context, parentTaskID string) ([]AgentSession, error)
	ReconcileSessions(ctx context.Context) error
	CaptureOutput(ctx context.Context, taskID string) (string, error)
	AttachCmd(ctx context.Context, taskID string) (*exec.Cmd, error)
	GetTaskDurations(ctx context.Context, taskIDs []string) (map[string]DurationData, error)
	GetAgentSummary(ctx context.Context, excludeTaskID string) (working, waiting, idle int, err error)
	SpawnEphemeralAgent(ctx context.Context, title string, width, height int, opts ...AgentSpawnOptions) error
	LastSpawnConflicts() []AgentSpawnConflict
	RegisteredAdapters() []string
	DefaultAdapter() string
	// AdapterFor returns the AIToolAdapter for the given kind, or nil if none
	// is registered (e.g. kind "shell"). Empty kind resolves to the default.
	AdapterFor(kind string) AIToolAdapter
	// GetStateTimeline returns bucketed state labels for a task over a window.
	GetStateTimeline(ctx context.Context, taskID string, window time.Duration, buckets int) ([]string, error)
}

type agentService struct {
	store             *store.Store
	tmux              TmuxManager
	workDir           string
	adapter           AIToolAdapter
	adapters          map[string]AIToolAdapter // optional registry keyed by name; falls back to `adapter`
	socketPath        string
	tmuxOptions       map[string]string
	prSvc             PRTrackingService
	binaryPath        string
	bus               EventPublisher
	briefKickoffDelay time.Duration

	conflictsMu   sync.Mutex
	lastConflicts []AgentSpawnConflict
}

// LastSpawnConflicts returns a copy of the scope-overlap warnings collected
// during the most recent SpawnAgent call. Callers (e.g. SwarmService) use this
// to surface advisory warnings to the conductor. Returns a defensive copy so
// concurrent SpawnAgent calls cannot mutate the slice mid-iteration.
func (a *agentService) LastSpawnConflicts() []AgentSpawnConflict {
	a.conflictsMu.Lock()
	defer a.conflictsMu.Unlock()
	if len(a.lastConflicts) == 0 {
		return nil
	}
	out := make([]AgentSpawnConflict, len(a.lastConflicts))
	copy(out, a.lastConflicts)
	return out
}

func (a *agentService) setLastConflicts(c []AgentSpawnConflict) {
	a.conflictsMu.Lock()
	defer a.conflictsMu.Unlock()
	a.lastConflicts = c
}

// Tmux returns the underlying TmuxManager. Used by SwarmService for direct
// send-keys delivery to swarm participants.
func (a *agentService) Tmux() TmuxManager {
	return a.tmux
}

// briefKickoffDelay is the default pause between the launch command and the
// "read your brief" send-keys. Override per-process via
// AgentServiceOptions.BriefKickoffDelay (wired from cfg.Swarm.BriefKickoffDelayMs).
// 250ms covers claude/chimera boot in practice on local hardware.
const briefKickoffDelay = 250 * time.Millisecond

// briefKickoffMessage is the short instruction send-keysed to a swarm agent
// after auto-launch. It is intentionally terse and directive: read the brief
// file, then begin. The role prompt (worker.md / conductor.md) reinforces the
// contract that the brief file is authoritative.
const briefKickoffMessage = `Read $LEGATO_BRIEF_FILE in full — that file is your complete assignment. Then begin work as instructed.`

// writeAgentPromptFiles writes the role system prompt and per-worker brief
// to per-agent files under ~/.legato/agents/<taskID>/. Returns the
// canonical paths (empty strings when content is empty), or a non-nil error
// when the filesystem cannot be written. Files are 0600 to keep prompts off
// other users on shared machines.
func writeAgentPromptFiles(taskID, rolePrompt, brief string) (string, string, error) {
	if taskID == "" {
		return "", "", fmt.Errorf("taskID is required")
	}
	dir, err := swarm.AgentDir(taskID)
	if err != nil {
		return "", "", fmt.Errorf("resolve agent dir: %w", err)
	}
	rolePath := ""
	if rolePrompt != "" {
		rolePath = filepath.Join(dir, "role-prompt.md")
		if err := os.WriteFile(rolePath, []byte(rolePrompt), 0o600); err != nil {
			return "", "", fmt.Errorf("write role prompt: %w", err)
		}
	}
	briefPath := ""
	if brief != "" {
		briefPath = filepath.Join(dir, "brief.md")
		if err := os.WriteFile(briefPath, []byte(brief), 0o600); err != nil {
			return "", "", fmt.Errorf("write brief: %w", err)
		}
	}
	return rolePath, briefPath, nil
}

// EventPublisher publishes events. Wraps *events.Bus to keep the service layer
// loosely coupled.
type EventPublisher interface {
	PublishAgentDied(taskID, parentTaskID, subtaskID, role string)
}

// AgentServiceOptions configures optional AI tool integration for agent sessions.
type AgentServiceOptions struct {
	// Adapter is the default AI tool adapter used when AgentSpawnOptions
	// doesn't specify an AgentKind, or when the requested AgentKind isn't
	// in the Adapters registry.
	Adapter AIToolAdapter
	// Adapters is the per-name registry consulted when AgentSpawnOptions.AgentKind
	// is non-empty. Lets a single agent service support multiple AI tools
	// (e.g. one swarm using Chimera workers + Claude reviewers).
	Adapters          map[string]AIToolAdapter
	SocketPath        string
	TmuxOptions       map[string]string
	PRService         PRTrackingService
	BinaryPath        string // Absolute path to legato binary for tmux status line
	EventBus          EventPublisher
	BriefKickoffDelay time.Duration // override the default brief-kickoff send-keys delay
}

// NewAgentService creates an AgentService.
func NewAgentService(s *store.Store, tmux TmuxManager, workDir string, opts ...AgentServiceOptions) AgentService {
	svc := &agentService{store: s, tmux: tmux, workDir: workDir, briefKickoffDelay: briefKickoffDelay}
	if len(opts) > 0 {
		svc.adapter = opts[0].Adapter
		svc.adapters = opts[0].Adapters
		svc.socketPath = opts[0].SocketPath
		svc.tmuxOptions = opts[0].TmuxOptions
		svc.prSvc = opts[0].PRService
		svc.binaryPath = opts[0].BinaryPath
		svc.bus = opts[0].EventBus
		if opts[0].BriefKickoffDelay > 0 {
			svc.briefKickoffDelay = opts[0].BriefKickoffDelay
		}
	}
	return svc
}

func (a *agentService) SpawnAgent(ctx context.Context, taskID string, width, height int, opts ...AgentSpawnOptions) error {
	var opt AgentSpawnOptions
	if len(opts) > 0 {
		opt = opts[0]
	}

	workDir := opt.WorkingDir
	if workDir == "" {
		workDir = a.workDir
	}

	// Swarm scope-conflict check: collect overlaps with active siblings (status
	// `dispatched` or `in_progress`). When StrictScope is set, hard-refuse;
	// otherwise the conflict is advisory and surfaced to the caller.
	a.setLastConflicts(nil)
	if opt.ParentTaskID != "" && len(opt.Scope) > 0 {
		conflicts, err := a.collectSiblingConflicts(ctx, opt, workDir)
		if err != nil {
			return fmt.Errorf("listing siblings: %w", err)
		}
		if len(conflicts) > 0 && opt.StrictScope {
			return fmt.Errorf("scope conflict with active sibling sub-task %s", conflicts[0].SiblingSubtaskID)
		}
		a.setLastConflicts(conflicts)
	}

	// Check for existing running session
	existing, err := a.store.GetAgentSessionByTaskID(ctx, taskID)
	if err == nil {
		// DB says "running" — verify the tmux session is actually alive
		alive, aliveErr := a.tmux.IsAlive(existing.TmuxSession)
		if aliveErr == nil && alive {
			return fmt.Errorf("agent already running for task %s", taskID)
		}
		// Tmux session is gone — mark it dead so we can re-spawn
		_ = a.store.UpdateAgentSessionStatus(ctx, taskID, "dead")
	}

	// Clean up dead sessions so UNIQUE constraint doesn't block re-spawn
	_ = a.store.DeleteDeadAgentSessions(ctx, taskID)

	sessionName := "legato-" + taskID
	// Kill any orphaned tmux session with the same name
	a.tmux.Kill(sessionName)

	adapter := a.resolveAdapter(opt.AgentKind)

	// Build env vars to inject into the initial shell. Track them as a map
	// for the adapter LaunchCommand call, then flatten for tmux Spawn.
	envMap := map[string]string{}
	if adapter != nil {
		for k, v := range adapter.EnvVars(taskID, a.socketPath) {
			envMap[k] = v
		}
	}
	// Swarm-specific env vars.
	if opt.Role != "" {
		envMap["LEGATO_AGENT_ROLE"] = opt.Role
	}
	if opt.ParentTaskID != "" {
		envMap["LEGATO_PARENT_TASK_ID"] = opt.ParentTaskID
	}
	if opt.SubtaskID != "" {
		envMap["LEGATO_SUBTASK_ID"] = opt.SubtaskID
	}
	// Resolve the role system prompt (string), then write it and any brief
	// to per-agent files under ~/.legato/agents/<taskID>/. The launch
	// command receives paths via env vars so multi-line/quoted content never
	// has to traverse shell escaping. Skipped for non-swarm spawns where no
	// role or brief is supplied.
	rolePrompt := ""
	if rp, ok := adapter.(RolePromptingAdapter); ok && opt.Role != "" {
		rolePrompt = rp.RoleSystemPrompt(opt.Role)
	}
	// Prepend any adapter-specific preamble (e.g. Chimera's host-mode warning
	// for legato CLI / env access from inside a sandbox).
	if pp, ok := adapter.(RolePromptPreambleAdapter); ok {
		if preamble := pp.RolePromptPreamble(); preamble != "" {
			if rolePrompt != "" {
				rolePrompt = preamble + "\n\n---\n\n" + rolePrompt
			} else {
				rolePrompt = preamble
			}
		}
	}
	if rolePrompt != "" || opt.Brief != "" {
		rolePath, briefPath, perr := writeAgentPromptFiles(taskID, rolePrompt, opt.Brief)
		if perr != nil {
			// Non-fatal — fall back to a session without prompt files. The
			// adapter's LaunchCommand will see no LEGATO_*_FILE env and skip
			// auto-launch.
			log.Printf("writing agent prompt files for %s: %v", taskID, perr)
		} else {
			if rolePath != "" {
				envMap["LEGATO_ROLE_PROMPT_FILE"] = rolePath
			}
			if briefPath != "" {
				envMap["LEGATO_BRIEF_FILE"] = briefPath
			}
		}
	}

	envVars := make([]string, 0, len(envMap))
	for k, v := range envMap {
		envVars = append(envVars, k+"="+v)
	}

	if err := a.tmux.Spawn(sessionName, workDir, width, height, envVars...); err != nil {
		return fmt.Errorf("spawning tmux session: %w", err)
	}

	// Apply legato status line defaults before user options (user can override).
	if a.binaryPath != "" {
		var statusRight string
		if opt.ParentTaskID != "" {
			statusRight = fmt.Sprintf("#(%s agent status %s --format tmux)", a.binaryPath, taskID)
		} else {
			statusRight = fmt.Sprintf("#(%s agent summary --exclude %s)", a.binaryPath, taskID)
		}
		statusDefaults := map[string]string{
			"status-right":    statusRight,
			"status-interval": "5",
			"status-style":    "bg=colour235,fg=colour245",
			"status-left":     "",
		}
		for k, v := range statusDefaults {
			if err := a.tmux.SetOption(sessionName, k, v); err != nil {
				a.tmux.Kill(sessionName)
				return fmt.Errorf("setting tmux status option %s: %w", k, err)
			}
		}
	}

	for k, v := range a.tmuxOptions {
		if err := a.tmux.SetOption(sessionName, k, v); err != nil {
			a.tmux.Kill(sessionName)
			return fmt.Errorf("setting tmux option %s: %w", k, err)
		}
	}

	sess := store.AgentSession{
		TaskID:      taskID,
		TmuxSession: sessionName,
		Command:     "shell",
		Status:      "running",
		Role:        opt.Role,
	}
	if opt.ParentTaskID != "" {
		p := opt.ParentTaskID
		sess.ParentTaskID = &p
	}
	if opt.SubtaskID != "" {
		st := opt.SubtaskID
		sess.SubtaskID = &st
	}
	if err := a.store.InsertAgentSession(ctx, sess); err != nil {
		// Roll back tmux session on DB failure
		a.tmux.Kill(sessionName)
		return fmt.Errorf("recording agent session: %w", err)
	}

	// Auto-launch: if the adapter knows how to start its AI tool, send-keys
	// the launch command into the freshly-created session. When a brief file
	// was written, kick the agent off with a short pointer to read it; this
	// is more robust than streaming the brief content through send-keys
	// because the brief is in the agent's filesystem and survives any
	// terminal/shell escaping pitfalls.
	if launcher, ok := adapter.(LaunchCommandAdapter); ok {
		if cmd := launcher.LaunchCommand(envMap, opt.Brief, opt.Tier); cmd != "" {
			// Use SendKeysShellCommand because the receiver here is the
			// freshly-spawned bash shell, not the AI tool. Bash with
			// bracketed-paste mode treats a single text+Enter send-keys as
			// a paste and leaves the command sitting in the prompt buffer
			// instead of executing it.
			if err := a.tmux.SendKeysShellCommand(sessionName, cmd); err != nil {
				// Launch failure is best-effort: the session exists, the user
				// can recover by attaching and running the AI tool manually.
				log.Printf("auto-launch send-keys failed for %s: %v", sessionName, err)
			} else if _, hasBrief := envMap["LEGATO_BRIEF_FILE"]; hasBrief {
				// Skip the kickoff for adapters whose launch command already
				// constitutes the first user turn (e.g. Chimera's `--prompt`).
				// The role prompt content already includes "read your brief"
				// instructions, so a separate kickoff would be a redundant
				// second user turn.
				skip := false
				if k, ok := adapter.(LaunchSelfKickoff); ok && k.LaunchIsSelfKickoff() {
					skip = true
				}
				if !skip {
					// Deliver the brief pointer on a short delay so the AI tool's
					// boot sequence has rendered its prompt. The brief itself is
					// on disk; we only need the agent to notice it.
					delay := a.briefKickoffDelay
					go func(session string) {
						time.Sleep(delay)
						if err := a.tmux.SendKeysLine(session, briefKickoffMessage); err != nil {
							log.Printf("auto-launch brief kickoff failed for %s: %v", session, err)
						}
					}(sessionName)
				}
			}
		}
	}

	// Auto-link git branch to task for PR tracking (best-effort)
	if a.prSvc != nil {
		if svc, ok := a.prSvc.(*prTrackingService); ok {
			go svc.AutoLinkBranch(ctx, taskID)
		}
	}

	return nil
}

// resolveAdapter returns the adapter to use for the given kind, falling back
// to the default adapter when kind is empty or unknown.
// The special kind "shell" returns nil so the tmux session stays at a
// plain shell prompt with no auto-launch.
func (a *agentService) resolveAdapter(kind string) AIToolAdapter {
	if kind == "shell" {
		return nil
	}
	if kind != "" && a.adapters != nil {
		if found, ok := a.adapters[kind]; ok {
			return found
		}
	}
	return a.adapter
}

// AdapterFor returns the AIToolAdapter for the given kind. Empty kind
// resolves to the default adapter. "shell" returns nil. Delegates to
// resolveAdapter so that callers (e.g. SwarmService) can look up adapters
// dynamically without importing the adapter resolution logic.
func (a *agentService) AdapterFor(kind string) AIToolAdapter {
	return a.resolveAdapter(kind)
}

// collectSiblingConflicts walks active swarm siblings and returns scope
// overlaps. "Active" is `dispatched`, `in_progress`, or `reporting` — any
// state where the worker still owns its scope.
func (a *agentService) collectSiblingConflicts(ctx context.Context, opt AgentSpawnOptions, workDir string) ([]AgentSpawnConflict, error) {
	var out []AgentSpawnConflict
	for _, status := range []string{"dispatched", "in_progress", "reporting"} {
		siblings, err := a.store.ListSubtasksByParentAndStatus(ctx, opt.ParentTaskID, status)
		if err != nil {
			return nil, err
		}
		for _, sib := range siblings {
			if sib.ID == opt.SubtaskID {
				continue
			}
			sibScope, _ := store.ParseScopeGlobs(sib.ScopeGlobs)
			hit, files := swarm.ScopeOverlaps(opt.Scope, sibScope, workDir)
			if hit {
				out = append(out, AgentSpawnConflict{
					SiblingSubtaskID: sib.ID,
					SiblingTitle:     sib.Title,
					Files:            files,
				})
			}
		}
	}
	return out, nil
}

func (a *agentService) SpawnEphemeralAgent(ctx context.Context, title string, width, height int, opts ...AgentSpawnOptions) error {
	taskID, err := a.store.CreateEphemeralTask(ctx, title)
	if err != nil {
		return fmt.Errorf("creating ephemeral task: %w", err)
	}
	return a.SpawnAgent(ctx, taskID, width, height, opts...)
}

// RegisteredAdapters returns the names of all registered adapters sorted
// alphabetically.
func (a *agentService) RegisteredAdapters() []string {
	if a.adapters == nil {
		return nil
	}
	names := make([]string, 0, len(a.adapters))
	for name := range a.adapters {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// DefaultAdapter returns the name of the default adapter, or an empty string
// when none is configured.
func (a *agentService) DefaultAdapter() string {
	if a.adapter == nil {
		return ""
	}
	return a.adapter.Name()
}

func (a *agentService) KillAgent(ctx context.Context, taskID string) error {
	session, err := a.store.GetAgentSessionByTaskID(ctx, taskID)
	if err != nil {
		// No running session — delete dead records
		return a.store.DeleteDeadAgentSessions(ctx, taskID)
	}

	a.tmux.Kill(session.TmuxSession)
	if err := a.store.UpdateAgentSessionStatus(ctx, taskID, "dead"); err != nil {
		return err
	}
	// Publish AgentDied so the swarm conductor (and any other subscribers)
	// receive the same notification regardless of whether the kill came from
	// an explicit user/conductor action or external session termination.
	if a.bus != nil {
		parent := ""
		if session.ParentTaskID != nil {
			parent = *session.ParentTaskID
		}
		subtask := ""
		if session.SubtaskID != nil {
			subtask = *session.SubtaskID
		}
		a.bus.PublishAgentDied(taskID, parent, subtask, session.Role)
	}
	return a.store.DeleteDeadAgentSessions(ctx, taskID)
}

func (a *agentService) ListAgents(ctx context.Context) ([]AgentSession, error) {
	sessions, err := a.store.ListAgentSessions(ctx)
	if err != nil {
		return nil, err
	}

	// Query live pane commands; fall back to DB values on error.
	liveCmds, _ := a.tmux.PaneCommands()

	result := make([]AgentSession, len(sessions))
	for i, s := range sessions {
		startedAt, err := time.Parse("2006-01-02 15:04:05", s.StartedAt)
		if err != nil {
			startedAt = time.Now() // fallback to avoid huge elapsed times
		}
		var endedAt *time.Time
		if s.EndedAt != nil {
			t, _ := time.Parse("2006-01-02 15:04:05", *s.EndedAt)
			endedAt = &t
		}

		command := s.Command
		if liveCmd, ok := liveCmds[s.TmuxSession]; ok && liveCmd != "" {
			command = liveCmd
		}

		// Look up task title from store.
		var title string
		if task, err := a.store.GetTask(ctx, s.TaskID); err == nil {
			title = task.Title
		}

		parentID := ""
		if s.ParentTaskID != nil {
			parentID = *s.ParentTaskID
		}
		subtaskID := ""
		if s.SubtaskID != nil {
			subtaskID = *s.SubtaskID
		}
		result[i] = AgentSession{
			ID:           s.ID,
			TaskID:       s.TaskID,
			Title:        title,
			TmuxSession:  s.TmuxSession,
			Command:      command,
			Status:       s.Status,
			Activity:     s.Activity,
			Role:         s.Role,
			ParentTaskID: parentID,
			SubtaskID:    subtaskID,
			StartedAt:    startedAt,
			EndedAt:      endedAt,
		}

		// Hydrate subtask metadata for swarm workers.
		if subtaskID != "" {
			if st, err := a.store.GetSubtask(ctx, subtaskID); err == nil {
				globs, _ := store.ParseScopeGlobs(st.ScopeGlobs)
				result[i].Description = st.Description
				result[i].Prompt = st.Prompt
				result[i].Scope = globs
			}
		}
	}
	return result, nil
}

func (a *agentService) ReconcileSessions(ctx context.Context) error {
	sessions, err := a.store.ListAgentSessions(ctx)
	if err != nil {
		return err
	}

	liveSessions, err := a.tmux.ListSessions()
	if err != nil {
		return err
	}

	live := make(map[string]bool)
	for _, s := range liveSessions {
		live[s] = true
	}

	for _, s := range sessions {
		if s.Status == "running" && !live[s.TmuxSession] {
			if err := a.store.UpdateAgentSessionStatus(ctx, s.TaskID, "dead"); err != nil {
				return err
			}
			// Close any orphaned state intervals for this task
			_ = a.store.RecordStateTransition(ctx, s.TaskID, "", "")
			// Notify swarm orchestrator that an agent died (best-effort).
			if a.bus != nil {
				parent := ""
				if s.ParentTaskID != nil {
					parent = *s.ParentTaskID
				}
				subtask := ""
				if s.SubtaskID != nil {
					subtask = *s.SubtaskID
				}
				a.bus.PublishAgentDied(s.TaskID, parent, subtask, s.Role)
			}
		}
	}

	return nil
}

// ListAgentsByParent returns all agent sessions whose parent_task_id matches.
// Useful for swarm coordination snapshots.
func (a *agentService) ListAgentsByParent(ctx context.Context, parentTaskID string) ([]AgentSession, error) {
	all, err := a.ListAgents(ctx)
	if err != nil {
		return nil, err
	}
	// Hit the DB directly for the filter — agent_sessions doesn't expose parent in ListAgentSessions output.
	rows, err := a.store.ListAgentSessions(ctx)
	if err != nil {
		return nil, err
	}
	parentByTask := make(map[string]string)
	for _, r := range rows {
		if r.ParentTaskID != nil {
			parentByTask[r.TaskID] = *r.ParentTaskID
		}
	}
	var result []AgentSession
	for _, s := range all {
		if parentByTask[s.TaskID] == parentTaskID && parentTaskID != "" {
			result = append(result, s)
		}
	}
	return result, nil
}

func (a *agentService) GetTaskDurations(ctx context.Context, taskIDs []string) (map[string]DurationData, error) {
	batch, err := a.store.GetStateDurationsBatch(ctx, taskIDs)
	if err != nil {
		return nil, err
	}
	result := make(map[string]DurationData, len(batch))
	for taskID, durations := range batch {
		result[taskID] = DurationData{
			Working: durations["working"],
			Waiting: durations["waiting"],
		}
	}
	return result, nil
}

func (a *agentService) GetAgentSummary(ctx context.Context, excludeTaskID string) (working, waiting, idle int, err error) {
	if err := a.ReconcileSessions(ctx); err != nil {
		return 0, 0, 0, err
	}
	return a.store.GetAgentActivityCounts(ctx, excludeTaskID)
}

func (a *agentService) CaptureOutput(ctx context.Context, taskID string) (string, error) {
	session, err := a.store.GetAgentSessionByTaskID(ctx, taskID)
	if err != nil {
		return "", err
	}
	return a.tmux.CaptureWithEscapes(session.TmuxSession)
}

func (a *agentService) AttachCmd(ctx context.Context, taskID string) (*exec.Cmd, error) {
	session, err := a.store.GetAgentSessionByTaskID(ctx, taskID)
	if err != nil {
		return nil, err
	}
	return a.tmux.Attach(session.TmuxSession), nil
}

// GetStateTimeline delegates to the store query for bucketed state labels.
func (a *agentService) GetStateTimeline(ctx context.Context, taskID string, window time.Duration, buckets int) ([]string, error) {
	return a.store.GetStateTimeline(ctx, taskID, window, buckets)
}
