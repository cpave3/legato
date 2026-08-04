package service

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/cpave3/legato/internal/engine/chimera"
	gitpkg "github.com/cpave3/legato/internal/engine/git"
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
	ID            int
	TaskID        string
	Title         string
	Group         string
	TmuxSession   string
	Command       string
	AgentKind     string
	Status        string
	Activity      string // "working", "waiting", "error", or "" (idle)
	Role          string // "conductor", "" for non-swarm, or worker role label
	ParentTaskID  string // when set, this session belongs to a swarm
	SubtaskID     string // when set, this session is a swarm worker
	StartedAt     time.Time
	EndedAt       *time.Time
	NotifyEnabled bool

	// Swarm worker metadata, hydrated from the subtask row.
	Description string
	Prompt      string   // per-worker instructions (initial brief)
	Scope       []string // file globs
}

// DurationData holds aggregated state durations for a task.
type DurationData struct {
	Working time.Duration
	Waiting time.Duration
	Error   time.Duration
}

// AgentSpawnOptions controls per-spawn behavior — used by swarm orchestration
// to inject role tags, scope-based conflict checks, a role system prompt, and
// the per-worker initial brief that the launch command delivers.
type AgentSpawnOptions struct {
	Role                 string            // free-form role label (e.g. "conductor", "backend"); "" for non-swarm
	ParentTaskID         string            // parent task ID when spawning a swarm sub-task
	SubtaskID            string            // sub-task ID being assigned
	Scope                []string          // scope globs for conflict checks
	WorkingDir           string            // override for tmux session working directory; falls back to agent service workDir
	AgentKind            string            // adapter name to use; "" → default adapter
	Tier                 string            // adapter tier name (selects per-tier launch_args from cfg.Adapters.<kind>.tiers); "" → base launch_args only
	Brief                string            // the per-worker initial brief; conductors leave this empty
	StrictScope          bool              // when true, scope conflicts hard-block the spawn; otherwise advisory
	ChimeraSessionID     string            // requested or previously stored Chimera session ID
	ChimeraSessionExists string            // Chimera collision policy: new, resume, or error
	Ephemeral            bool              // internal: omit task-linked session identity
	Assignment           string            // standalone workflow assignment; distinct from swarm role prompts
	ExtraEnv             map[string]string // trusted Legato-owned workflow identity values
	ExecutionKind        string            // reporting execution kind; defaults from workflow/swarm context
	AttributionTaskID    string            // user-facing reporting task; defaults to taskID
	WorkflowRunID        string            // owning workflow run, when applicable
	WorkflowAttemptID    string            // owning workflow attempt, when applicable
	SwarmID              string            // owning swarm parent ID, when applicable
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
	SetTaskGroup(ctx context.Context, taskID string, group *string) error
	ListAgents(ctx context.Context) ([]AgentSession, error)
	ListAgentsByParent(ctx context.Context, parentTaskID string) ([]AgentSession, error)
	ReconcileSessions(ctx context.Context) error
	CaptureOutput(ctx context.Context, taskID string) (string, error)
	AttachCmd(ctx context.Context, taskID string) (*exec.Cmd, error)
	GetTaskDurations(ctx context.Context, taskIDs []string) (map[string]DurationData, error)
	GetAgentSummary(ctx context.Context, excludeTaskID string) (working, waiting, errors, idle int, err error)
	SetAgentActivity(ctx context.Context, taskID, activity, workingDir string) error
	SpawnEphemeralAgent(ctx context.Context, title string, width, height int, opts ...AgentSpawnOptions) error
	LastSpawnConflicts() []AgentSpawnConflict
	RegisteredAdapters() []string
	DefaultAdapter() string
	// AdapterFor returns the AIToolAdapter for the given kind, or nil if none
	// is registered (e.g. kind "shell"). Empty kind resolves to the default.
	AdapterFor(kind string) AIToolAdapter
	// GetStateTimeline returns bucketed state labels for a task over a window.
	GetStateTimeline(ctx context.Context, taskID string, window time.Duration, buckets int) ([]string, error)
	// SetTaskNotifyEnabled toggles push notifications for a task.
	SetTaskNotifyEnabled(ctx context.Context, taskID string, enabled bool) error
	// GetTaskNotifyEnabled returns whether push notifications are enabled for a task.
	GetTaskNotifyEnabled(ctx context.Context, taskID string) (bool, error)
	GetTaskChimeraSessionID(ctx context.Context, taskID string) (string, error)
	// SendMessage delivers a text message to the agent through the
	// appropriate interaction adapter (tmux send-keys or Chimera HTTP).
	SendMessage(ctx context.Context, taskID, message string) error
	// Interrupt sends an interrupt to the agent's active turn.
	InterruptAgent(ctx context.Context, taskID string) error
	// ResolvePermission resolves a pending permission request for the
	// agent. Only applies to agents with structured permissions (Chimera).
	ResolvePermission(ctx context.Context, taskID, requestID, decision, rememberScope string) error
	// HasChimeraEndpoint returns true if the agent has a discovered Chimera
	// endpoint, meaning the web UI should render the structured panel
	// instead of the xterm terminal.
	HasChimeraEndpoint(ctx context.Context, taskID string) bool
	// GetAgentSession returns the raw agent session for the given task.
	// Used by the web server to proxy Chimera snapshot/SSE endpoints.
	// The returned session's ChimeraEndpoint is private and must not be
	// exposed in API DTOs.
	GetAgentSession(ctx context.Context, taskID string) (*store.AgentSession, error)
}

// AgentMessageQueuer is implemented by agent services that can defer a
// message until the agent reaches a supported step boundary.
type AgentMessageQueuer interface {
	QueueMessage(ctx context.Context, taskID, message string) error
}

// LearningPromptProvider is the narrow launch-time dependency used to resolve
// durable standing context without coupling AgentService to LearningService.
type LearningPromptProvider interface {
	CompileLearningPrompt(context.Context, LearningPromptCompileInput) (*CompiledLearningPrompt, error)
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
	checkoutInspector CheckoutInspector
	binaryPath        string
	bus               EventPublisher
	briefKickoffDelay time.Duration
	notifier          Notifier
	osNotifier        Notifier
	learning          LearningPromptProvider
	learningMaxItems  int
	learningMaxBytes  int

	sendKeysAdapter AgentInteraction
	chimeraAdapter  AgentInteraction

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

// rolePromptForAdapter resolves the role system prompt for a swarm
// participant, prepending any adapter preamble. Returns empty when there is
// no role — plain (non-swarm) spawns use generalPromptForAdapter instead, so
// the swarm-specific preamble never reaches an ephemeral agent.
func rolePromptForAdapter(adapter AIToolAdapter, role string) string {
	if adapter == nil || role == "" {
		return ""
	}
	rolePrompt := ""
	if rp, ok := adapter.(RolePromptingAdapter); ok {
		rolePrompt = rp.RoleSystemPrompt(role)
	}
	if pp, ok := adapter.(RolePromptPreambleAdapter); ok {
		if preamble := pp.RolePromptPreamble(); preamble != "" {
			if rolePrompt != "" {
				rolePrompt = preamble + "\n\n---\n\n" + rolePrompt
			} else {
				rolePrompt = preamble
			}
		}
	}
	return rolePrompt
}

// generalPromptForAdapter returns a standalone prompt for a plain (non-swarm)
// agent spawn. Adapters opt in via GeneralPromptAdapter; the prompt is used as
// the initial user turn for tools whose launch is self-kickoff (e.g. Chimera's
// --prompt). Returns empty when the adapter doesn't supply one, in which case
// the session launches with no injected first message.
func generalPromptForAdapter(adapter AIToolAdapter) string {
	if adapter == nil {
		return ""
	}
	if gp, ok := adapter.(GeneralPromptAdapter); ok {
		return gp.GeneralPrompt()
	}
	return ""
}

func joinPromptSections(parts ...string) string {
	nonempty := make([]string, 0, len(parts))
	for _, part := range parts {
		if strings.TrimSpace(part) != "" {
			nonempty = append(nonempty, part)
		}
	}
	return strings.Join(nonempty, "\n\n---\n\n")
}

// EventPublisher publishes events. Wraps *events.Bus to keep the service layer
// loosely coupled.
type EventPublisher interface {
	PublishAgentStarted(taskID string)
	PublishAgentDied(taskID, parentTaskID, subtaskID, role string)
	PublishAgentChimeraDiscovered(taskID string)
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
	Adapters               map[string]AIToolAdapter
	SocketPath             string
	TmuxOptions            map[string]string
	PRService              PRTrackingService
	CheckoutInspector      CheckoutInspector
	BinaryPath             string // Absolute path to legato binary for tmux status line
	EventBus               EventPublisher
	BriefKickoffDelay      time.Duration // override the default brief-kickoff send-keys delay
	Notifier               Notifier
	OSNotifier             Notifier // OS-native desktop notifications (Linux/macOS)
	LearningPromptProvider LearningPromptProvider
	LearningPromptMaxItems int
	LearningPromptMaxBytes int
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
		svc.checkoutInspector = opts[0].CheckoutInspector
		svc.binaryPath = opts[0].BinaryPath
		svc.bus = opts[0].EventBus
		if opts[0].BriefKickoffDelay > 0 {
			svc.briefKickoffDelay = opts[0].BriefKickoffDelay
		}
		svc.notifier = opts[0].Notifier
		svc.osNotifier = opts[0].OSNotifier
		svc.learning = opts[0].LearningPromptProvider
		svc.learningMaxItems = opts[0].LearningPromptMaxItems
		svc.learningMaxBytes = opts[0].LearningPromptMaxBytes
	}
	// Wire the interaction adapters. The send-keys adapter is always
	// available (it wraps the tmux manager). The Chimera adapter is only
	// used for agents with a discovered endpoint.
	svc.sendKeysAdapter = NewSendKeysAdapter(tmux, s)
	svc.chimeraAdapter = NewChimeraInteractionAdapter(s, nil)
	return svc
}

func (a *agentService) GetTaskChimeraSessionID(ctx context.Context, taskID string) (string, error) {
	task, err := a.store.GetTask(ctx, taskID)
	if err != nil {
		return "", err
	}
	if task.ChimeraSessionID == nil {
		return "", nil
	}
	return *task.ChimeraSessionID, nil
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
		// Tmux session is gone — terminalize its durable execution before
		// marking the operational session dead so reporting cannot leak.
		if existing.ExecutionID != "" {
			_ = a.store.TerminalizeExecution(ctx, existing.ExecutionID, store.ExecutionLost)
		}
		_ = a.store.UpdateAgentSessionStatus(ctx, taskID, "dead")
	}

	// Clean up dead sessions so UNIQUE constraint doesn't block re-spawn
	_ = a.store.DeleteDeadAgentSessions(ctx, taskID)

	sessionName := "legato-" + taskID
	// Kill any orphaned tmux session with the same name
	a.tmux.Kill(sessionName)

	adapter := a.resolveAdapter(opt.AgentKind)
	agentKind := "shell"
	if adapter != nil {
		agentKind = adapter.Name()
	}

	// Build env vars to inject into the initial shell. Track them as a map
	// for the adapter LaunchCommand call, then flatten for tmux Spawn.
	envMap := map[string]string{}
	if opt.Ephemeral {
		envMap["LEGATO_TASK_EPHEMERAL"] = "1"
	}
	for key, value := range opt.ExtraEnv {
		if strings.HasPrefix(key, "LEGATO_WORKFLOW_") {
			envMap[key] = value
		}
	}
	if adapter != nil {
		for k, v := range adapter.EnvVars(taskID, a.socketPath) {
			envMap[k] = v
		}
	}
	if adapter != nil && adapter.Name() == "chimera" && !opt.Ephemeral {
		sessionID := opt.ChimeraSessionID
		if sessionID == "" {
			sessionID = taskID
		}
		policy := opt.ChimeraSessionExists
		if policy == "" {
			policy = "new"
		}
		envMap["LEGATO_CHIMERA_SESSION_NAME"] = taskID
		envMap["LEGATO_CHIMERA_SESSION_ID"] = sessionID
		envMap["LEGATO_CHIMERA_SESSION_EXISTS"] = policy
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
	// Resolve the prompt content for the launch. Swarm participants (a role
	// or parent task is set) get the role system prompt with the adapter
	// preamble; plain/ephemeral spawns get a general standalone prompt so the
	// agent doesn't mistake itself for a swarm worker. The result and any
	// brief are written to per-agent files under ~/.legato/agents/<taskID>/.
	// The launch command receives paths via env vars so multi-line/quoted
	// content never has to traverse shell escaping.
	isSwarm := opt.Role != "" || opt.ParentTaskID != ""
	rolePrompt := ""
	if isSwarm {
		rolePrompt = rolePromptForAdapter(adapter, opt.Role)
	} else {
		rolePrompt = generalPromptForAdapter(adapter)
	}
	var promptManifestID string
	if a.learning != nil && adapter != nil {
		projectKey := ""
		if task, taskErr := a.store.GetTask(ctx, taskID); taskErr == nil {
			codeContext, parseErr := store.ParseCodeContext(task.CodeContext)
			if parseErr != nil {
				return fmt.Errorf("parsing task code context: %w", parseErr)
			}
			if identity, identityErr := ResolveProjectIdentity(codeContext, workDir, a.checkoutInspector); identityErr == nil {
				projectKey = identity.Key
			}
		}
		compiled, compileErr := a.learning.CompileLearningPrompt(ctx, LearningPromptCompileInput{
			TaskID: taskID, Adapter: agentKind, Role: opt.Role, ProjectKey: projectKey,
			MaxItems: a.learningMaxItems, MaxBytes: a.learningMaxBytes,
		})
		if compileErr != nil {
			return fmt.Errorf("compiling learning prompt: %w", compileErr)
		}
		promptManifestID = compiled.Manifest.ID
		rolePrompt = joinPromptSections(rolePrompt, compiled.Rendered)
	}

	assignment := opt.Brief
	if opt.Assignment != "" {
		assignment = opt.Assignment
		// Keep assignment distinct in the brief while ensuring self-kickoff
		// launches receive it in their first turn alongside standing context.
		if kickoff, ok := adapter.(LaunchSelfKickoff); ok && kickoff.LaunchIsSelfKickoff() {
			rolePrompt = joinPromptSections(rolePrompt, "## Assignment\n"+opt.Assignment)
		} else if rolePrompt == "" {
			rolePrompt = opt.Assignment
		}
	}
	if rolePrompt != "" || assignment != "" {
		rolePath, briefPath, perr := writeAgentPromptFiles(taskID, rolePrompt, assignment)
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

	executionID := "ex-" + store.GenerateTaskID()
	executionKind := opt.ExecutionKind
	if executionKind == "" {
		switch {
		case opt.SubtaskID != "":
			executionKind = store.ExecutionSwarmWorker
		case opt.Role == "conductor" || opt.ParentTaskID != "":
			executionKind = store.ExecutionSwarmConductor
		default:
			executionKind = store.ExecutionInteractive
		}
	}
	attributionTaskID := opt.AttributionTaskID
	if attributionTaskID == "" && executionKind != store.ExecutionWorkflowAgent {
		attributionTaskID = taskID
	}
	taskTitle := ""
	if task, taskErr := a.store.GetTask(ctx, attributionTaskID); taskErr == nil {
		taskTitle = task.Title
	}
	execution := store.Execution{
		ID: executionID, Kind: executionKind, Status: store.ExecutionRunning,
		TaskID: attributionTaskID, ExecutionTaskID: taskID, TaskTitle: taskTitle,
		WorkflowRunID: opt.WorkflowRunID, WorkflowAttemptID: opt.WorkflowAttemptID,
		SwarmID: opt.SwarmID, SwarmSubtaskID: opt.SubtaskID,
		AgentKind: agentKind, Role: opt.Role, WorkingDir: workDir, StartedAt: time.Now().UTC(),
	}
	if err := a.store.CreateExecution(ctx, execution); err != nil {
		return fmt.Errorf("creating execution: %w", err)
	}
	envMap["LEGATO_EXECUTION_ID"] = executionID
	failExecution := func() { _ = a.store.TerminalizeExecution(ctx, executionID, store.ExecutionFailed) }

	envVars := make([]string, 0, len(envMap))
	for k, v := range envMap {
		envVars = append(envVars, k+"="+v)
	}

	// Snapshot ordinary repository HEAD before the agent starts. Review sync can
	// then import only this session's commits even when no task worktree exists.
	if gitpkg.IsRepository(ctx, workDir) {
		if err := NewReviewService(a.store, nil, nil).BeginCapture(ctx, taskID, workDir); err != nil && opt.ParentTaskID == "" {
			log.Printf("starting review capture for %s: %v", taskID, err)
		}
	}

	if err := a.tmux.Spawn(sessionName, workDir, width, height, envVars...); err != nil {
		failExecution()
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
				failExecution()
				return fmt.Errorf("setting tmux status option %s: %w", k, err)
			}
		}
	}

	for k, v := range a.tmuxOptions {
		if err := a.tmux.SetOption(sessionName, k, v); err != nil {
			a.tmux.Kill(sessionName)
			failExecution()
			return fmt.Errorf("setting tmux option %s: %w", k, err)
		}
	}

	sess := store.AgentSession{
		TaskID:      taskID,
		ExecutionID: executionID,
		TmuxSession: sessionName,
		Command:     "shell",
		AgentKind:   agentKind,
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
	sessionID, err := a.store.InsertAgentSessionReturningID(ctx, sess)
	if err != nil {
		a.tmux.Kill(sessionName)
		failExecution()
		return fmt.Errorf("recording agent session: %w", err)
	}
	if err := a.store.LinkExecutionAgentSession(ctx, executionID, sessionID); err != nil {
		a.tmux.Kill(sessionName)
		_ = a.store.UpdateAgentSessionStatus(ctx, taskID, "dead")
		failExecution()
		return fmt.Errorf("linking execution to agent session: %w", err)
	}
	if promptManifestID != "" {
		if err := a.store.BindLearningPromptManifestSession(ctx, promptManifestID, sessionID); err != nil {
			a.tmux.Kill(sessionName)
			_ = a.store.UpdateAgentSessionStatus(ctx, taskID, "dead")
			failExecution()
			return fmt.Errorf("binding learning prompt manifest: %w", err)
		}
	}

	// Capture the lockfile baseline before launching Chimera. Fast agents can
	// write their lockfile synchronously during SendKeysShellCommand; taking this
	// snapshot in the discovery goroutine would incorrectly classify it as old.
	var chimeraLockfilesBeforeLaunch map[string]bool
	expectedChimeraSessionID := envMap["LEGATO_CHIMERA_SESSION_ID"]
	if agentKind == "chimera" {
		chimeraLockfilesBeforeLaunch = snapshotChimeraLockfiles()
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

	// Discover the Chimera lockfile for Chimera agents. The lockfile is
	// written by the interactive command after the server starts; we poll
	// for it asynchronously so spawn doesn't block on the agent's boot.
	// The endpoint and session ID are persisted on the agent session row
	// so the web UI can proxy the SSE stream and send follow-ups via HTTP.
	if agentKind == "chimera" {
		go a.discoverChimeraLockfile(context.Background(), taskID, expectedChimeraSessionID, chimeraLockfilesBeforeLaunch)
	}

	// Capture repository identity from the exact agent working directory and
	// immediately try discovery. Agent startup remains successful when local Git
	// inspection or GitHub synchronization is unavailable.
	if a.checkoutInspector != nil {
		go a.captureAgentCodeContext(taskID, workDir)
	}
	if a.bus != nil {
		a.bus.PublishAgentStarted(taskID)
	}

	return nil
}

func (a *agentService) captureAgentCodeContext(taskID, workDir string) {
	captured, err := a.checkoutInspector.InspectCodeContext(workDir)
	if err != nil || captured == nil || isDefaultBranch(captured.Branch, captured.BaseBranch) {
		return
	}
	ctx := context.Background()
	task, err := a.store.GetTask(ctx, taskID)
	if err != nil {
		return
	}
	existing, _ := store.ParseCodeContext(task.CodeContext)
	if existing != nil && (existing.RepoSource == "manual" || existing.BranchSource == "manual" || existing.RepoSource == "worktree") {
		return
	}
	captured.WorkingDir = workDir
	captured.RepoSource = "agent"
	captured.BranchSource = "agent"
	captured.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	if err := a.store.SetCodeContext(ctx, taskID, captured); err != nil {
		return
	}
	if actions, ok := a.prSvc.(PRTaskActions); ok {
		_ = actions.RediscoverPR(ctx, taskID)
	}
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

func (a *agentService) SpawnWorkflowAgent(ctx context.Context, taskID string, opt AgentSpawnOptions) (*store.AgentSession, error) {
	if err := a.SpawnAgent(ctx, taskID, 0, 0, opt); err != nil {
		return nil, err
	}
	return a.store.GetAgentSessionByTaskID(ctx, taskID)
}

func (a *agentService) SpawnEphemeralAgent(ctx context.Context, title string, width, height int, opts ...AgentSpawnOptions) error {
	taskID, err := a.store.CreateEphemeralTask(ctx, title)
	if err != nil {
		return fmt.Errorf("creating ephemeral task: %w", err)
	}
	var opt AgentSpawnOptions
	if len(opts) > 0 {
		opt = opts[0]
	}
	opt.Ephemeral = true
	return a.SpawnAgent(ctx, taskID, width, height, opt)
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

	if session.ExecutionID != "" {
		if err := a.store.TerminalizeExecution(ctx, session.ExecutionID, store.ExecutionCancelled); err != nil {
			return err
		}
	}

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

func (a *agentService) SetTaskGroup(ctx context.Context, taskID string, group *string) error {
	task, err := a.store.GetTask(ctx, taskID)
	if err != nil {
		return err
	}
	task.Group = group
	return a.store.UpdateTask(ctx, *task)
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

		// Look up task presentation fields from store.
		var title, group string
		if task, err := a.store.GetTask(ctx, s.TaskID); err == nil {
			title = task.Title
			if task.Group != nil {
				group = *task.Group
			}
		}

		parentID := ""
		if s.ParentTaskID != nil {
			parentID = *s.ParentTaskID
		}
		subtaskID := ""
		if s.SubtaskID != nil {
			subtaskID = *s.SubtaskID
		}
		notifyEnabled, _ := a.store.GetTaskNotifyEnabled(ctx, s.TaskID)

		result[i] = AgentSession{
			ID:            s.ID,
			TaskID:        s.TaskID,
			Title:         title,
			Group:         group,
			TmuxSession:   s.TmuxSession,
			Command:       command,
			AgentKind:     s.AgentKind,
			Status:        s.Status,
			Activity:      s.Activity,
			Role:          s.Role,
			ParentTaskID:  parentID,
			SubtaskID:     subtaskID,
			StartedAt:     startedAt,
			EndedAt:       endedAt,
			NotifyEnabled: notifyEnabled,
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
			if s.ExecutionID != "" {
				if err := a.store.TerminalizeExecution(ctx, s.ExecutionID, store.ExecutionLost); err != nil {
					return err
				}
			}
			if err := a.store.UpdateAgentSessionStatus(ctx, s.TaskID, "dead"); err != nil {
				return err
			}
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

	reconciled, err := a.store.ReconcileLostExecutions(ctx)
	if err != nil {
		return err
	}
	if reconciled > 0 {
		log.Printf("executions: terminalized %d orphaned running execution(s) as lost", reconciled)
	}
	count, oldest, err := a.store.ExecutionHealth(ctx)
	if err != nil {
		return err
	}
	if count > 0 && oldest != nil && time.Since(*oldest) > 24*time.Hour {
		log.Printf("executions: %d running; oldest started %s", count, oldest.Format(time.RFC3339))
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
			Error:   durations["error"],
		}
	}
	return result, nil
}

func (a *agentService) GetAgentSummary(ctx context.Context, excludeTaskID string) (working, waiting, errors, idle int, err error) {
	if err := a.ReconcileSessions(ctx); err != nil {
		return 0, 0, 0, 0, err
	}
	return a.store.GetAgentActivityCounts(ctx, excludeTaskID)
}

func (a *agentService) SetAgentActivity(ctx context.Context, taskID, activity, workingDir string) error {
	sess, err := a.store.GetAgentSessionByTaskID(ctx, taskID)
	if err != nil {
		return err
	}
	if sess.ExecutionID == "" {
		return errors.New("running agent session has no execution identity")
	}
	oldActivity := sess.Activity

	if err := a.store.UpdateAgentActivity(ctx, taskID, activity); err != nil {
		return fmt.Errorf("updating agent activity: %w", err)
	}
	if err := a.store.RecordExecutionActivity(ctx, sess.ExecutionID, activity, workingDir); err != nil {
		return fmt.Errorf("recording execution activity: %w", err)
	}

	if (oldActivity == store.ActivityWorking && (activity == store.ActivityWaiting || activity == "")) ||
		(activity == store.ActivityError && oldActivity != store.ActivityError) {
		a.maybeNotify(taskID, activity)
	}
	return nil
}

// NotificationContext identifies the parent task and, for child agents, the
// human-readable actor that became ready.
type NotificationContext struct {
	TaskID string
	Actor  string
}

// MaybeNotify sends notifications when an agent becomes ready or enters error.
// pushNotifier is gated by per-task notify preference and rate limits;
// osNotifier fires unconditionally when configured.
func MaybeNotify(store *store.Store, pushNotifier, osNotifier Notifier, taskID, activity string) {
	MaybeNotifyAgent(store, pushNotifier, osNotifier, NotificationContext{TaskID: taskID}, activity)
}

// MaybeNotifyAgent sends a readiness or error notification with optional child-agent context.
func MaybeNotifyAgent(db *store.Store, pushNotifier, osNotifier Notifier, notification NotificationContext, activity string) {
	label := notification.TaskID
	if task, err := db.GetTask(context.Background(), notification.TaskID); err == nil && task.Title != "" {
		label = task.Title
	}

	actor := label
	if notification.Actor != "" {
		actor = notification.Actor + " for " + label
	}
	title := "Agent ready"
	msg := fmt.Sprintf("Agent %s is now waiting", actor)
	switch activity {
	case "":
		msg = fmt.Sprintf("Agent %s is now idle", actor)
	case store.ActivityError:
		title = "Agent error"
		msg = fmt.Sprintf("Agent %s had an error", actor)
	}

	if pushNotifier != nil && pushNotifier.Configured() {
		enabled, err := db.GetTaskNotifyEnabled(context.Background(), notification.TaskID)
		if err == nil && enabled && pushNotifier.CanNotify(notification.TaskID) {
			_ = pushNotifier.Notify(title, msg)
		}
	}

	if osNotifier != nil && osNotifier.Configured() {
		_ = osNotifier.Notify(title, msg)
	}
}

func (a *agentService) maybeNotify(taskID, activity string) {
	MaybeNotify(a.store, a.notifier, a.osNotifier, taskID, activity)
}

func (a *agentService) SetTaskNotifyEnabled(ctx context.Context, taskID string, enabled bool) error {
	return a.store.UpdateTaskNotifyEnabled(ctx, taskID, enabled)
}

func (a *agentService) GetTaskNotifyEnabled(ctx context.Context, taskID string) (bool, error) {
	return a.store.GetTaskNotifyEnabled(ctx, taskID)
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

// SendMessage delivers a text message to the agent through the appropriate
// interaction adapter. For Chimera agents with a discovered endpoint, this
// uses the HTTP API; for all others, it uses tmux send-keys.
func (a *agentService) SendMessage(ctx context.Context, taskID, message string) error {
	adapter := a.resolveInteractionAdapter(ctx, taskID)
	return adapter.SendMessage(ctx, taskID, message)
}

// InterruptAgent sends an interrupt to the agent's active turn.
// QueueMessage delegates step-boundary delivery to the active interaction adapter.
func (a *agentService) QueueMessage(ctx context.Context, taskID, message string) error {
	return a.resolveInteractionAdapter(ctx, taskID).QueueMessage(ctx, taskID, message)
}

func (a *agentService) InterruptAgent(ctx context.Context, taskID string) error {
	adapter := a.resolveInteractionAdapter(ctx, taskID)
	return adapter.Interrupt(ctx, taskID)
}

// ResolvePermission resolves a pending permission request for the agent.
func (a *agentService) ResolvePermission(ctx context.Context, taskID, requestID, decision, rememberScope string) error {
	adapter := a.resolveInteractionAdapter(ctx, taskID)
	return adapter.ResolvePermission(ctx, taskID, requestID, decision, rememberScope)
}

// HasChimeraEndpoint returns true if the agent has a discovered Chimera
// endpoint, meaning the web UI should render the structured panel.
func (a *agentService) HasChimeraEndpoint(ctx context.Context, taskID string) bool {
	session, err := a.store.GetAgentSessionByTaskID(ctx, taskID)
	if err != nil {
		return false
	}
	return session.ChimeraEndpoint != nil && session.ChimeraSessionID != nil
}

// AgentEnvironmentProvider exposes the exact launch environment of an active
// agent to out-of-process interactions such as web bang commands.
type AgentEnvironmentProvider interface {
	GetAgentEnvironment(ctx context.Context, taskID string) ([]string, error)
}

type tmuxEnvironmentReader interface {
	SessionEnvironment(sessionName string) ([]string, error)
}

// GetAgentEnvironment returns the exact environment stored on the active tmux
// session. SpawnAgent writes all injected variables there, so later consumers
// automatically inherit additions without maintaining a second variable list.
func (a *agentService) GetAgentEnvironment(ctx context.Context, taskID string) ([]string, error) {
	session, err := a.store.GetAgentSessionByTaskID(ctx, taskID)
	if err != nil {
		return nil, err
	}
	reader, ok := a.tmux.(tmuxEnvironmentReader)
	if !ok {
		return nil, fmt.Errorf("tmux session environment is unavailable")
	}
	return reader.SessionEnvironment(session.TmuxSession)
}

// GetAgentSession returns the raw agent session row for the given task.
func (a *agentService) GetAgentSession(ctx context.Context, taskID string) (*store.AgentSession, error) {
	return a.store.GetAgentSessionByTaskID(ctx, taskID)
}

// resolveInteractionAdapter returns the appropriate interaction adapter
// for the given task. Chimera agents with a discovered endpoint use the
// Chimera adapter; all others use the send-keys adapter.
func (a *agentService) resolveInteractionAdapter(ctx context.Context, taskID string) AgentInteraction {
	if a.HasChimeraEndpoint(ctx, taskID) && a.chimeraAdapter != nil {
		return a.chimeraAdapter
	}
	return a.sendKeysAdapter
}

// discoverChimeraLockfile polls ~/.chimera/instances/ for a lockfile written
// by the Chimera interactive command running in the agent's tmux session.
// When found, it persists the endpoint and session ID on the agent session
// row so the web UI can proxy the SSE stream and send follow-ups via HTTP.
//
// The lockfile is named <pid>.json and contains the server URL and session
// ID. We don't know the PID ahead of time, so we scan the instances directory
// for the exact session ID assigned before launch.
func snapshotChimeraLockfiles() map[string]bool {
	preExisting := map[string]bool{}
	home, err := os.UserHomeDir()
	if err != nil {
		return preExisting
	}
	entries, err := os.ReadDir(filepath.Join(home, ".chimera", "instances"))
	if err != nil {
		return preExisting
	}
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), ".json") {
			preExisting[entry.Name()] = true
		}
	}
	return preExisting
}

func (a *agentService) discoverChimeraLockfile(ctx context.Context, taskID, expectedSessionID string, preExisting map[string]bool) {
	home, err := os.UserHomeDir()
	if err != nil {
		log.Printf("chimera lockfile discovery for %s: %v", taskID, err)
		return
	}
	instancesDir := filepath.Join(home, ".chimera", "instances")

	// Poll for up to 30 seconds — Chimera may take a few seconds to boot.
	for i := 0; i < 60; i++ {
		select {
		case <-ctx.Done():
			return
		default:
		}

		entries, err := os.ReadDir(instancesDir)
		if err != nil {
			time.Sleep(500 * time.Millisecond)
			continue
		}

		for _, entry := range entries {
			if !strings.HasSuffix(entry.Name(), ".json") {
				continue
			}
			if preExisting[entry.Name()] {
				continue
			}
			data, err := os.ReadFile(filepath.Join(instancesDir, entry.Name()))
			if err != nil {
				continue
			}
			lf, err := chimera.ParseLockfile(data)
			if err != nil || (expectedSessionID != "" && lf.SessionID != expectedSessionID) {
				continue
			}
			// Prefer the session assigned before launch. Ephemeral agents have no
			// assigned ID and retain the new-lockfile fallback.
			if err := a.store.UpdateChimeraDiscovery(ctx, taskID, lf.SessionID, lf.URL); err != nil {
				log.Printf("chimera lockfile discovery for %s: persisting: %v", taskID, err)
			} else {
				log.Printf("chimera lockfile discovered for %s: endpoint=%s session=%s",
					taskID, lf.URL, lf.SessionID)
				// Notify connected web clients that the agent list changed
				// so they re-fetch and switch to the NativeAgentPanel.
				if a.bus != nil {
					a.bus.PublishAgentChimeraDiscovered(taskID)
				}
			}
			return
		}

		time.Sleep(500 * time.Millisecond)
	}
	log.Printf("chimera lockfile discovery for %s: timed out after 30s", taskID)
}
