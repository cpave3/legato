package service

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log"
	"maps"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/cpave3/legato/config"
	"github.com/cpave3/legato/internal/engine/chimera"
	gitpkg "github.com/cpave3/legato/internal/engine/git"
	"github.com/cpave3/legato/internal/engine/store"
	"github.com/cpave3/legato/internal/engine/swarm"
	enginetmux "github.com/cpave3/legato/internal/engine/tmux"
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
	ShellReturned(name string) (bool, error)
}

// AgentSession represents a running or completed agent session.
// RunCard is a board card a workflow run is for: its ID, the key a person
// knows it by (its ticket's, else its ID) and its title.
type RunCard struct {
	TaskID string `json:"task_id"`
	Key    string `json:"key"`
	Title  string `json:"title"`
}

type AgentSession struct {
	ID                int
	TaskID            string // backing task used for session controls
	ExecutionID       string
	AttributionTaskID string // user-facing task this execution concerns
	ExecutionKind     string
	WorkflowRunID     string
	// WorkflowAttemptID is the step attempt that started it, with
	// WorkflowRunID.
	WorkflowAttemptID string
	// RunCard is the card a workflow run is for when this agent works on
	// the run's own task (a step's agent, the run's reviewer), nil
	// otherwise: the run's task is named after the run, not the card.
	RunCard *RunCard
	// Engram is the engram the agent runs as, "" for none.
	Engram        string
	Title         string
	Group         string
	TmuxSession   string
	Command       string
	AgentKind     string
	Account       string
	AccountLabel  string
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
	// Sandboxed is true when the agent runs in Legato's sandbox.
	Sandboxed bool
	// DockerHostNetwork is true when its containers may use the host's
	// network through its filtered Docker socket.
	DockerHostNetwork bool
	// Slot is the task's agent slot it runs in: "main", or a named slot
	// such as "reviewer". A named slot's row has the agent ID
	// <task>_<slot> as TaskID, so every action on the row reaches it.
	Slot string
	// CardTaskID is the task the agent's slot belongs to.
	CardTaskID string
	// Reeve is true for the agent of this Legato itself.
	Reeve bool
	// ReeveName is the Reeve's name (reeve.name) on the Reeve's agent, ""
	// when it has none; its Title is then the name.
	ReeveName string
	// BackgroundJobs is what a running agent started that still runs and
	// will wake it (see docs/ai/agent-states.md); none for an ended one.
	BackgroundJobs []BackgroundJob
	// BackgroundShells are the background shells its mod reported, running
	// and ended (Chimera); a Claude Code agent's are served by its bridge.
	BackgroundShells []store.BackgroundShell
	// WorkingDir is the directory the agent was started in: the
	// execution's working directory (a job clone, a worktree or a folder).
	WorkingDir string
	// CloneRepo and CloneBranch are the job clone at WorkingDir, when it is
	// one: its repository and the branch it was made on.
	CloneRepo   string
	CloneBranch string
	// OpenItems counts the agent's open operator items (for_operator), and
	// OpenDoneItems those of them that are done notes. Activity stays what
	// its agent tool reports; see Glance.
	OpenItems     int
	OpenDoneItems int
	// PlanPending is true for a task's main slot when the task has a
	// Legato plan proposed to the operator or a swarm plan pending.
	PlanPending bool
	// Stage is its place's stage ("" when none is recorded), StageReason
	// why, StageSource what set it, StageSince when it got there. See
	// WorkStageService.
	Stage       string
	StageReason string
	StageSource string
	StageSince  *time.Time
	// ReeveWaitSince is when the agent asked the Reeve something it has not
	// answered yet (nor the operator taken a turn), nil for no wait;
	// ReeveWait is that wait's state (ReeveWaitOpen, ReeveWaitUnanswered).
	ReeveWaitSince *time.Time
	ReeveWait      string
}

// NeedsOperator says whether a running agent waits on the operator: its
// glance is Needs you.
func (a AgentSession) NeedsOperator() bool {
	return a.Glance() == GlanceNeedsYou
}

// ShownActivity is the board strip's activity word: waiting for a running
// agent with an open operator item that is not a done note, unless it is in
// error. Grouping uses Glance.
func (a AgentSession) ShownActivity() string {
	if a.Status == "running" && a.OpenItems > a.OpenDoneItems && a.Activity != store.ActivityError {
		return store.ActivityWaiting
	}
	return a.Activity
}

// BackgroundJob is one thing an agent started that still runs and will wake
// it: Kind bash, monitor, subagent or cron, with a Label saying what it is.
type BackgroundJob = store.BackgroundJob

// AgentPeer describes one live, routable agent sharing a Git worktree.
type AgentPeer struct {
	TaskID      string `json:"task_id"`
	ExecutionID string `json:"execution_id"`
	Title       string `json:"title"`
	Kind        string `json:"kind"`
	Role        string `json:"role,omitempty"`
	Activity    string `json:"activity"`
	WorkingDir  string `json:"working_dir"`
	// Slot is set for an agent in a named slot of its task (a reviewer);
	// ReadOnly for one that can only look, which is never messaged.
	Slot     string `json:"slot,omitempty"`
	ReadOnly bool   `json:"read_only,omitempty"`
}

// AgentDeliveryFailure records one peer that could not receive a message.
type AgentDeliveryFailure struct {
	TaskID string `json:"task_id"`
	Error  string `json:"error"`
}

// AgentBroadcastResult preserves partial success across a directory broadcast.
type AgentBroadcastResult struct {
	Delivered []string               `json:"delivered"`
	Failed    []AgentDeliveryFailure `json:"failed"`
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
	// Label names this agent in lists in place of its task's title (an
	// agent.run step's with.name); kept on its execution.
	Label        string
	Role         string   // free-form role label (e.g. "conductor", "backend"); "" for non-swarm
	ParentTaskID string   // parent task ID when spawning a swarm sub-task
	SubtaskID    string   // sub-task ID being assigned
	Scope        []string // scope globs for conflict checks
	WorkingDir   string   // override for tmux session working directory; falls back to agent service workDir
	// Repo, a GitHub repository (owner/name), makes the agent work in a
	// fresh clone of it at Ref (the default branch when empty) made for
	// its execution, instead of WorkingDir.
	Repo string
	Ref  string
	// Clone, a job clone's ID, makes the agent work in that clone, which
	// another task made: the next branch of a stack in the same folder.
	// Without it, a task with a clone of Repo works in that one again.
	Clone int64
	// OwnClone asks for a fresh clone of Repo even when the task has one
	// (a version 2 workflow step: clones are explicit, LEG-485).
	OwnClone bool
	// Branch is the branch the clone ends on. A new clone: GitHub's branch
	// of that name when there is one, else a new one made at Ref, and
	// legato/<task id> when empty, which the GitHub credential lets agents
	// push. A clone that exists: a new branch at the commit it has checked
	// out, and the branch it is on when empty.
	Branch           string
	AgentKind        string                      // adapter name to use; "" → default adapter
	Account          string                      `json:"account,omitempty"` // local Claude account; empty uses the unchanged default
	AccountIdentity  *store.AgentAccountIdentity `json:"-"`                 // internal: frozen identity on resume
	Model            string                      // direct model selector; "" → adapter default
	Tier             string                      // adapter tier name (selects per-tier launch_args from cfg.Adapters.<kind>.tiers); "" → base launch_args only
	Brief            string                      // the per-worker initial brief; conductors leave this empty
	StrictScope      bool                        // when true, scope conflicts hard-block the spawn; otherwise advisory
	SessionPolicy    string                      // new or resume; Chimera also supports its internal error collision policy
	ResumeSessionID  string                      // saved provider conversation ID
	ResumeWorkingDir string                      // saved native conversation directory
	ResumeStorageDir string                      // provider's transcript storage directory
	Ephemeral        bool                        // internal: omit task-linked session identity
	Assignment       string                      // standalone workflow assignment; distinct from swarm role prompts
	ExtraEnv         map[string]string           // trusted Legato-owned workflow identity values
	// Holder is the name the agent claims and closes its ticket under
	// (LEGATO_HOLDER): a handed-off agent's is the main's task ID, which a
	// tracker's holder rule prefers over LEGATO_TASK_ID.
	Holder            string
	ExecutionKind     string // reporting execution kind; defaults from workflow/swarm context
	AttributionTaskID string // user-facing reporting task; defaults to taskID
	WorkflowRunID     string // owning workflow run, when applicable
	// Prompt is the agent's first message, sent once it can take one (after
	// the brief kickoff when there is one). Blank sends nothing.
	Prompt string
	// Unsandboxed starts this agent outside Legato's sandbox when it is on.
	// Swarm and workflow agents ignore it: they are always sandboxed.
	Unsandboxed bool
	// DockerHostNetwork lets this agent's containers and builds use the
	// host's network through its filtered Docker socket. Only for an agent
	// the operator starts in the sandbox with Docker in it: anything else
	// asking for it is refused (ErrDockerHostNetwork), never ignored.
	DockerHostNetwork bool
	// LaunchPrompt replaces the general launch prompt (the Reeve's): no
	// repository guidance, review prompt or learnings are added to it.
	LaunchPrompt string
	// LaunchArgs are added to the agent tool's launch command, after its
	// adapter's own (the Reeve's reeve.launch_args).
	LaunchArgs []string
	// Unattended starts an agent nobody watches (a handed-off task's): in
	// Legato's sandbox whatever Unsandboxed says, with its tool's own
	// permission prompts off (PromptsOffAdapter). It is refused when the
	// sandbox is off or the tool cannot turn its prompts off.
	Unattended bool
	// ReadOnlyCheckout starts an agent that may look but not change (a
	// find-out delegation): sandboxed whatever Unsandboxed says, with its
	// working directory and git folder read-only, and its execution marked so
	// the credential broker refuses its pushes and pull request writes.
	ReadOnlyCheckout bool
	// ThrowawayCheckout, with ReadOnlyCheckout, shows the checkout beneath
	// a throwaway layer, so the agent can build and run tests and nothing
	// it writes reaches the host (a reviewer). Where the sandbox has no
	// overlays the checkout is plain read-only (CheckoutOverlayProber).
	ThrowawayCheckout bool
	// Branches is what a handed-off task's session is told of its
	// branches, in place of the ordinary legato/<task id>-… guidance; zero
	// for any other session.
	Branches SessionBranches
	// PushedBranch says Branch is already on GitHub (a handoff that starts
	// from a pushed branch): the clone checks out GitHub's branch, and the
	// spawn fails with ErrBranchNotOnGitHub rather than make a new one.
	PushedBranch      bool
	WorkflowAttemptID string     // owning workflow attempt, when applicable
	SwarmID           string     // owning swarm parent ID, when applicable
	Resources         []Resource // files and folders the agent is given: granted to Chimera, listed in every launch prompt
	// Slot is the task's agent slot the agent runs in: "" or "main" for the
	// main slot, a name such as "reviewer" for a named one, which only
	// Legato's own services start. Each slot keeps its own tmux session,
	// execution and saved conversation.
	Slot string
	// Engram starts the agent as that engram (docs/ai/engram.md): its boot
	// packet first in the launch prompt, ENGRAM_* in its environment. A
	// boot that fails starts the agent without it, unless EngramRequired.
	Engram string
	// EngramSession is the engram session a resumed conversation keeps
	// (from the execution it resumes); found from the task when empty.
	EngramSession string
	// EngramRequired fails the spawn, before anything starts, when the
	// engram is not one engram lists (a workflow step's).
	EngramRequired bool
	// EngramHome keeps the engram in a store of its own (ENGRAM_HOME): the
	// Reeve's. "" is the person's store.
	EngramHome string
	// EngramSeed issues the engram with this seed when its store does not
	// have it yet.
	EngramSeed string
	// EngramMissing is what stands first in the launch prompt, in place of
	// the packet, when the engram did not boot (the Reeve's line); nil adds
	// nothing.
	EngramMissing func(reason string) string
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
	NewWorkDefaults(ctx context.Context) (NewWorkDefaults, error)
	// AdapterFor returns the AIToolAdapter for the given kind, or nil if none
	// is registered (e.g. kind "shell"). Empty kind resolves to the default.
	AdapterFor(kind string) AIToolAdapter
	// GetStateTimeline returns bucketed state labels for a task over a window.
	GetStateTimeline(ctx context.Context, taskID string, window time.Duration, buckets int) ([]string, error)
	// SetTaskNotifyEnabled toggles push notifications for a task.
	SetTaskNotifyEnabled(ctx context.Context, taskID string, enabled bool) error
	// GetTaskNotifyEnabled returns whether push notifications are enabled for a task.
	GetTaskNotifyEnabled(ctx context.Context, taskID string) (bool, error)
	GetTaskResumeSession(ctx context.Context, taskID, agentKind string) (*TaskResumeSession, error)
	DiscoverChimera(ctx context.Context, taskID string) error
	// ListPeers returns live agents sharing the caller's Git worktree. An
	// explicit directory overrides lookup from taskID/executionID.
	ListPeers(ctx context.Context, taskID, executionID, directory string) ([]AgentPeer, error)
	SendPeerMessage(ctx context.Context, senderTaskID, recipientTaskID, message string) error
	BroadcastToPeers(ctx context.Context, taskID, executionID, directory, message string) (AgentBroadcastResult, error)
	// SendMessage delivers a text message to the agent through the
	// appropriate interaction adapter (tmux send-keys or Chimera HTTP).
	SendMessage(ctx context.Context, taskID, message string) error
	// Interrupt sends an interrupt to the agent's active turn.
	InterruptAgent(ctx context.Context, taskID string) error
	// ResolvePermission resolves a pending permission request for the
	// agent. Only applies to agents with structured permissions (Chimera).
	ResolvePermission(ctx context.Context, taskID, requestID string, action PermissionAction) error
	// AnswerQuestion answers a question the agent put to the person. Only
	// applies to agents driven through a native endpoint.
	AnswerQuestion(ctx context.Context, taskID, requestID string, answer QuestionAnswer) error
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
	newWorkDefaults          func(context.Context) (NewWorkDefaults, error)
	claudeAccounts           []config.ClaudeAccount
	claudeManagedSettingsDir string
	accountDataDir           string
	toolCheck                func(ctx context.Context, tool string) error
	capabilities             CapabilitySource
	capabilityFolders        CapabilityFolders
	interruptedMu            sync.Mutex
	interruptedRoutes        []InterruptedResumeRoute
	handedOff                HandedOffAgents
	resourceResolver         ResourceResolver
	interruptedListeners     []func()
	sessionEnvMu             sync.Mutex
	sessionEnv               SessionEnvProvider       // credential channels; nil without any
	brokerEnv                SessionEnvProvider       // the host broker's socket and a token; nil without one
	repos                    JobCloner                // makes clones for agents spawned with a repository; nil without one
	engrams                  AgentEngrams             // boots engrams for agents started as one; nil without
	primaryDir               func(repo string) string // a repository's main checkout, for a fresh clone's setup; nil without
	setupsMu                 sync.Mutex
	setups                   map[string]string // task -> the status file of its clone's setup, while it runs
	sandbox                  *SandboxSettings  // nil: agents run unsandboxed
	scratch                  SandboxScratch    // the sandboxes' scratch folders, removed when executions end
	store                    *store.Store
	tmux                     TmuxManager
	workDir                  string
	adapter                  AIToolAdapter
	adapters                 map[string]AIToolAdapter // optional registry keyed by name; falls back to `adapter`
	socketPath               string
	tmuxOptions              map[string]string
	prSvc                    PRTrackingService
	checkoutInspector        CheckoutInspector
	guidance                 RepositoryGuidanceSource
	prModes                  PullRequestModeSource
	reeveName                ReeveNameSource
	repoResources            RepositoryResourceSource
	docker                   *SandboxDocker
	reviewsOff               bool
	hasSkill                 func(name string) bool
	binaryPath               string
	bus                      EventPublisher
	workStages               *WorkStageService
	briefKickoffDelay        time.Duration
	notifier                 Notifier
	osNotifier               Notifier
	learning                 LearningPromptProvider
	learningMaxItems         int
	learningMaxBytes         int
	peerAwareness            bool
	peerMessageSent          func()
	reeveWakes               bool
	// nativeBindTimeout bounds how long a launch message waits for the
	// session's native endpoint before it is typed into the pane instead.
	nativeBindTimeout time.Duration
	chimeraModWait    time.Duration
	// launchOwner names this service as the deliverer of the launch
	// messages it keeps in launch_messages (launch_messages.go).
	launchOwner      string
	launchDeliveries sync.Map
	// launchWG counts the deliveries running in the background, so tests
	// can wait for them before closing the store (waitLaunchDeliveries).
	launchWG         sync.WaitGroup
	foregroundAgents sync.Map

	sendKeysAdapter AgentInteraction
	chimeraAdapter  *ChimeraInteractionAdapter

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

// nativeBindTimeout is how long a launch message waits for a session that is
// expected to bind a native endpoint. It covers the tool's boot; a session
// that has not bound by then gets the message typed, as before.
const nativeBindTimeout = 30 * time.Second

// nativeBindPoll is how often a waiting launch message checks for the binding.
const nativeBindPoll = 50 * time.Millisecond

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
	// PublishSlotAgentDied announces that a named slot's agent ended, which
	// is not the task's agent ending.
	PublishSlotAgentDied(taskID, slot string)
	PublishAgentActivityChanged(taskID, activity string)
	PublishAgentChimeraDiscovered(taskID string)
	PublishWorkflowChanged(runID string)
}

// NewWorkDefaults selects the agent and optional tier for fresh, unspecified work.
type NewWorkDefaults struct {
	Agent   string
	Tier    string
	Account string
}

// AgentServiceOptions configures optional AI tool integration for agent sessions.
type AgentServiceOptions struct {
	// ClaudeManagedSettingsDir is the platform's OS-level policy directory.
	// Tests supply only synthetic settings; the command entry point wires it.
	ClaudeManagedSettingsDir string
	ClaudeAccounts           []config.ClaudeAccount
	AccountDataDir           string
	// NewWorkDefaults selects fresh work with no explicit agent. Nil uses
	// Adapter; callback failures refuse the spawn before launch validation.
	NewWorkDefaults func(context.Context) (NewWorkDefaults, error)
	// ToolCheck, when set, is asked before a spawn whether the chosen
	// agent tool (its adapter name) can start; an error refuses the spawn
	// before anything is made. Doctor.SpawnCheck is the runtime's.
	ToolCheck func(ctx context.Context, tool string) error
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
	// DisablePeerAwareness suppresses automatic startup notices. Explicit peer
	// discovery and messaging remain available.
	DisablePeerAwareness bool
	// PeerMessageSent, when set, is called after a peer message between
	// two agents is delivered and recorded, so the web UI's graph of agents
	// draws the link (the runtime tells every Legato process over IPC).
	PeerMessageSent func()
	// ReeveWakes is reeve.watch.wake: an agent's message to the Reeve
	// starts one when none runs, so a wait on the Reeve with no session is
	// still open, not unanswered.
	ReeveWakes bool
	// RepositoryGuidance adds the guidance of the repository an agent works
	// in to its launch prompt. Nil: none.
	RepositoryGuidance RepositoryGuidanceSource
	// PullRequestModes says, in the launch prompt of an agent that works in
	// a repository, whether it opens pull requests as drafts or ready for
	// review. Nil: nothing is said.
	PullRequestModes PullRequestModeSource
	// ReeveName names the Reeve's agent in ListAgents.
	ReeveName ReeveNameSource
	// RepositoryResources gives every agent working in a repository its
	// resources, as if ticked at spawn. Nil: none.
	RepositoryResources RepositoryResourceSource
	// ReviewsOff tells agents to skip Legato's review tours and handoff
	// packets unless the person asks for one.
	ReviewsOff bool
	// HasSkill says whether agents have a skill of that name (managed or
	// the agent tools' own): the reviews prompt names legato-review only
	// then. Nil: assume they do.
	HasSkill func(name string) bool
	// Sandbox runs agents in Legato's sandbox. Nil: no sandbox.
	Sandbox *SandboxSettings
	// SandboxScratch is where sandboxes keep their scratch, removed when
	// their executions end and swept on reconcile, also while the sandbox
	// is off. Nil: the Sandbox's own.
	SandboxScratch *SandboxScratch
	// Capabilities chooses what each agent is given beyond its tool: the
	// skills that are on. Nil: nothing. See agent_capabilities.go.
	Capabilities CapabilitySource
	// CapabilityFolders is where each execution's capabilities are copied,
	// removed when it ends and swept on reconcile.
	CapabilityFolders CapabilityFolders
}

// JobCloner makes the clone an agent spawned with a repository works in.
type JobCloner interface {
	JobClone(ctx context.Context, req JobCloneRequest) (store.JobClone, error)
}

// SetRepositories lets agents spawned from now on work in a clone of a
// repository.
// SetWorkStages gives the service the process's agent stages, which a spawn
// that does not resume resets. Without it the service resets through stages
// of its own over its store, which publish nothing.
func (a *agentService) SetWorkStages(stages *WorkStageService) {
	a.workStages = stages
}

func (a *agentService) SetRepositories(repos JobCloner) {
	a.sessionEnvMu.Lock()
	defer a.sessionEnvMu.Unlock()
	a.repos = repos
}

// AgentEngrams boots an engram for a spawn and ends its session
// (EngramService).
type AgentEngrams interface {
	Boot(ctx context.Context, req EngramBootRequest) (EngramBoot, error)
	EndSession(ctx context.Context, ref EngramSessionRef) error
	// Touch records that an engram was spawned in dir, for the order of
	// its place's bound engrams.
	Touch(ctx context.Context, dir, name string) error
}

// SetEngrams lets agents spawned from now on start as engrams.
func (a *agentService) SetEngrams(engrams AgentEngrams) {
	a.sessionEnvMu.Lock()
	defer a.sessionEnvMu.Unlock()
	a.engrams = engrams
}

func (a *agentService) engramBooter() AgentEngrams {
	a.sessionEnvMu.Lock()
	defer a.sessionEnvMu.Unlock()
	return a.engrams
}

func (a *agentService) repositories() JobCloner {
	a.sessionEnvMu.Lock()
	defer a.sessionEnvMu.Unlock()
	return a.repos
}

// SetSessionEnv gives every session spawned from now on the environment of
// the credential channels.
func (a *agentService) SetSessionEnv(provider SessionEnvProvider) {
	a.sessionEnvMu.Lock()
	defer a.sessionEnvMu.Unlock()
	a.sessionEnv = provider
}

// SetBrokerEnv gives every session spawned from now on the host broker's
// socket and a token of its own.
func (a *agentService) SetBrokerEnv(provider SessionEnvProvider) {
	a.sessionEnvMu.Lock()
	defer a.sessionEnvMu.Unlock()
	a.brokerEnv = provider
}

func (a *agentService) brokerSessionEnv() SessionEnvProvider {
	a.sessionEnvMu.Lock()
	defer a.sessionEnvMu.Unlock()
	return a.brokerEnv
}

func (a *agentService) credentialEnv() SessionEnvProvider {
	a.sessionEnvMu.Lock()
	defer a.sessionEnvMu.Unlock()
	return a.sessionEnv
}

// NewAgentService creates an AgentService.
func NewAgentService(s *store.Store, tmux TmuxManager, workDir string, opts ...AgentServiceOptions) AgentService {
	svc := &agentService{store: s, tmux: tmux, workDir: workDir, briefKickoffDelay: briefKickoffDelay, peerAwareness: true, nativeBindTimeout: nativeBindTimeout, chimeraModWait: 10 * time.Second, launchOwner: newLaunchOwner()}
	// Every spawn resets its place's stage, whichever process spawns; the
	// runtime gives its shared stages, which also announce the change.
	svc.workStages = NewWorkStageService(s, nil)
	if len(opts) > 0 {
		svc.adapter = opts[0].Adapter
		svc.newWorkDefaults = opts[0].NewWorkDefaults
		svc.claudeAccounts = slices.Clone(opts[0].ClaudeAccounts)
		svc.claudeManagedSettingsDir = opts[0].ClaudeManagedSettingsDir
		svc.accountDataDir = opts[0].AccountDataDir
		svc.adapters = opts[0].Adapters
		svc.socketPath = opts[0].SocketPath
		svc.tmuxOptions = opts[0].TmuxOptions
		svc.prSvc = opts[0].PRService
		svc.checkoutInspector = opts[0].CheckoutInspector
		svc.guidance = opts[0].RepositoryGuidance
		svc.prModes = opts[0].PullRequestModes
		svc.reeveName = opts[0].ReeveName
		svc.repoResources = opts[0].RepositoryResources
		if opts[0].Sandbox != nil {
			svc.docker = opts[0].Sandbox.Docker
		}
		svc.reviewsOff = opts[0].ReviewsOff
		svc.hasSkill = opts[0].HasSkill
		svc.binaryPath = opts[0].BinaryPath
		svc.bus = opts[0].EventBus
		if opts[0].BriefKickoffDelay > 0 {
			svc.briefKickoffDelay = opts[0].BriefKickoffDelay
		}
		svc.notifier = opts[0].Notifier
		svc.toolCheck = opts[0].ToolCheck
		svc.osNotifier = opts[0].OSNotifier
		svc.learning = opts[0].LearningPromptProvider
		svc.learningMaxItems = opts[0].LearningPromptMaxItems
		svc.learningMaxBytes = opts[0].LearningPromptMaxBytes
		svc.peerAwareness = !opts[0].DisablePeerAwareness
		svc.peerMessageSent = opts[0].PeerMessageSent
		svc.reeveWakes = opts[0].ReeveWakes
		svc.sandbox = opts[0].Sandbox
		svc.capabilities = opts[0].Capabilities
		svc.capabilityFolders = opts[0].CapabilityFolders
		switch {
		case opts[0].SandboxScratch != nil:
			svc.scratch = *opts[0].SandboxScratch
		case opts[0].Sandbox != nil:
			svc.scratch = scratchFor(opts[0].Sandbox.Base)
		}
	}
	// Wire the interaction adapters. The send-keys adapter is always
	// available (it wraps the tmux manager). The Chimera adapter is only
	// used for agents with a discovered endpoint.
	svc.sendKeysAdapter = NewSendKeysAdapter(tmux, s, svc.verifyPaneDelivery)
	if guarded, ok := tmux.(interface {
		SetDeliveryGuard(func(string, string) error)
	}); ok {
		guarded.SetDeliveryGuard(svc.verifyPaneCommand)
	}
	svc.chimeraAdapter = NewChimeraInteractionAdapter(s, nil)
	return svc
}

func (a *agentService) SpawnAgent(ctx context.Context, taskID string, width, height int, opts ...AgentSpawnOptions) (spawnErr error) {
	var opt AgentSpawnOptions
	if len(opts) > 0 {
		opt = opts[0]
	}

	if err := RefuseAway(ctx, a.store, taskID); err != nil {
		return err
	}
	if err := ValidateAgentSlot(opt.Slot); err != nil {
		return err
	}
	if opt.Account != "" && opt.AgentKind == "" && opt.SessionPolicy != "resume" {
		opt.AgentKind = "claude-code"
	}
	if opt.AgentKind == "" && opt.SessionPolicy != "resume" && opt.Role == "" && opt.ParentTaskID == "" && !IsReeveTask(ctx, a.store, taskID) {
		defaults, err := a.NewWorkDefaults(ctx)
		if err != nil {
			return err
		}
		opt.AgentKind = defaults.Agent
		opt.Account = defaults.Account
		if opt.Tier == "" {
			opt.Tier = defaults.Tier
		}
	}
	if err := a.prepareTaskResume(ctx, taskID, &opt); err != nil {
		return err
	}
	adapter := a.resolveAdapter(opt.AgentKind)
	kind := opt.AgentKind
	if adapter != nil {
		kind = adapter.Name()
	}
	accountIdentity, err := a.resolveClaudeAccount(ctx, opt, kind)
	if err != nil {
		return err
	}
	if a.toolCheck != nil && adapter != nil {
		if err := a.toolCheck(ctx, adapter.Name()); err != nil {
			return err
		}
	}
	if validator, ok := adapter.(LaunchValidator); ok {
		if err := validator.ValidateLaunch(ctx, opt.Tier, opt.LaunchArgs); err != nil {
			return err
		}
	}
	slot := opt.Slot
	if slot == "" {
		slot = store.SlotMain
	}
	named := IsNamedSlot(slot)
	// A Reeve session runs only in Legato's sandbox.
	reeve := IsReeveTask(ctx, a.store, taskID)
	if opt.DockerHostNetwork {
		if err := a.checkDockerHostNetwork(reeve, named, opt); err != nil {
			return err
		}
	}
	if opt.Unattended {
		if a.sandbox == nil {
			return errors.New("Legato's sandbox is off here, and an unattended agent runs only in it")
		}
		if _, ok := a.resolveAdapter(opt.AgentKind).(PromptsOffAdapter); !ok {
			return fmt.Errorf("%s cannot be started with its permission prompts off", orUnknown(opt.AgentKind))
		}
	}
	if reeve {
		if opt.Unsandboxed {
			return ErrReeveSandboxed
		}
		if a.sandbox == nil {
			return fmt.Errorf("%w, which is off here (agents.sandbox.enabled)", ErrReeveSandboxed)
		}
	}
	if (opt.Repo != "" || opt.Clone != 0) && opt.WorkingDir != "" {
		return fmt.Errorf("an agent works in a job clone or in %s, not both", opt.WorkingDir)
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

	// Check for an existing running session in the same slot.
	existing, err := a.store.GetAgentSessionInSlot(ctx, taskID, slot)
	if err == nil {
		// DB says "running" — verify the tmux session is actually alive
		alive, aliveErr := a.tmux.IsAlive(existing.TmuxSession)
		if aliveErr == nil && alive {
			if named {
				return fmt.Errorf("agent already running in the %s slot of task %s", slot, taskID)
			}
			return fmt.Errorf("agent already running for task %s", taskID)
		}
		// Tmux session is gone — terminalize its durable execution before
		// marking the operational session dead so reporting cannot leak.
		if existing.ExecutionID != "" {
			_ = a.EndExecution(ctx, existing.ExecutionID, store.ExecutionLost)
		}
		_ = a.store.UpdateAgentSessionStatusInSlot(ctx, taskID, slot, "dead")
	}

	// Clean up dead sessions so UNIQUE constraint doesn't block re-spawn
	_ = a.store.DeleteDeadAgentSessionsInSlot(ctx, taskID, slot)

	executionID := "ex-" + store.GenerateTaskID()
	var freshClone *cloneSetup
	cloneRepo := ""
	if opt.Repo != "" || opt.Clone != 0 {
		// A clone, made or joined before anything starts. One whose spawn
		// fails below is swept with the other idle clones.
		repos := a.repositories()
		if repos == nil {
			return fmt.Errorf("%s: Legato clones repositories for agents with a github credential, and none is configured", opt.Repo)
		}
		// A named slot's agent (a workflow's triage agent) gets a clone of
		// its own on a branch of its own, so it never pushes the card's.
		fresh := "legato/" + strings.ToLower(taskID)
		if named {
			fresh += "-" + slot
		}
		clone, err := repos.JobClone(ctx, JobCloneRequest{ExecutionID: executionID, TaskID: taskID, Repo: opt.Repo, Ref: opt.Ref,
			Branch: opt.Branch, FreshBranch: fresh, CloneID: opt.Clone, Own: opt.OwnClone, PushedBranch: opt.PushedBranch, Slot: slot})
		if err != nil {
			return err
		}
		workDir = clone.Path
		cloneRepo = clone.Repo
		if opt.SessionPolicy == "resume" && opt.ResumeWorkingDir != "" && !samePath(clone.Path, opt.ResumeWorkingDir) {
			return fmt.Errorf("%w: the saved conversation is in %s, not in the clone at %s", ErrTaskResumeConflict, opt.ResumeWorkingDir, clone.Path)
		}
		freshClone = a.prepareFreshClone(clone, executionID, clone.Repo, opt.Ref)
		if note := freshClone.note(); note != "" {
			// On the card, so the operator (and a main, on Return) sees
			// why the agent lacks what the setup should have given it.
			if _, err := AddTaskNote(ctx, a.store, taskID, note, "legato"); err != nil {
				log.Printf("noting the clone setup of %s: %v", taskID, err)
			}
		}
	}
	var launchPeers []AgentPeer
	// An agent in a read-only named slot (a reviewer) cannot change the
	// tree: it is not announced to the worktree's peers, and is told none.
	quietSlot := named && opt.ReadOnlyCheckout
	if a.peerAwareness && !quietSlot {
		if peers, peerErr := a.ListPeers(ctx, taskID, "", workDir); peerErr == nil {
			launchPeers = peers
		}
	}

	sessionName := AgentTmuxSession(taskID, slot)
	// The agent's own folder of prompt files, one per slot.
	agentKey := AgentSlotKey(taskID, slot)
	// Kill any orphaned tmux session with the same name
	a.tmux.Kill(sessionName)

	agentKind := "shell"
	if adapter != nil {
		agentKind = adapter.Name()
	}
	nativeResume := opt.SessionPolicy == "resume" && (agentKind == "claude-code" || agentKind == "codex")

	// An engram boots on the host, where the agent will work, before its
	// launch prompt is written. A boot that fails leaves the agent to start
	// without it.
	booted, err := a.bootEngram(ctx, taskID, executionID, agentKind, workDir, opt)
	if err != nil {
		return err
	}
	// Until the engram is recorded on the execution (which ends its session
	// with it), a spawn that fails ends the session it booted.
	engramRecorded := false
	defer func() {
		if spawnErr != nil && !engramRecorded {
			booted.endIfFresh(ctx, a.engramBooter(), workDir)
		}
	}()

	// Build env vars to inject into the initial shell. Track them as a map
	// for the adapter LaunchCommand call, then flatten for tmux Spawn.
	envMap := map[string]string{}
	if opt.Ephemeral {
		envMap["LEGATO_TASK_EPHEMERAL"] = "1"
	}
	// For display only: inside the session, the slot comes from its
	// execution.
	if named {
		envMap["LEGATO_SLOT"] = slot
	}
	if opt.Holder != "" {
		envMap["LEGATO_HOLDER"] = opt.Holder
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
	// An ephemeral agent names no session, but one restarted resumes its own.
	if adapter != nil && adapter.Name() == "chimera" && opt.Ephemeral && opt.SessionPolicy == "resume" && opt.ResumeSessionID != "" {
		envMap["LEGATO_CHIMERA_SESSION_ID"] = opt.ResumeSessionID
		envMap["LEGATO_CHIMERA_SESSION_EXISTS"] = "resume"
	}
	if adapter != nil && adapter.Name() == "chimera" && !opt.Ephemeral {
		policy := opt.SessionPolicy
		if policy == "" {
			policy = "new"
		}
		envMap["LEGATO_CHIMERA_SESSION_NAME"] = agentKey
		// For resume, pass the real hook-captured Chimera session ID so
		// LaunchCommand emits --session <realID>. For new sessions with no
		// prior history (first creation), pass the task ID so Chimera creates
		// a deterministic session. For new sessions with prior history, omit
		// the ID entirely so Chimera generates a random one (collision-safe).
		if policy == "resume" && opt.ResumeSessionID != "" {
			envMap["LEGATO_CHIMERA_SESSION_ID"] = opt.ResumeSessionID
		} else if policy == "new" && opt.ResumeSessionID == "" {
			envMap["LEGATO_CHIMERA_SESSION_ID"] = agentKey
		}
		envMap["LEGATO_CHIMERA_SESSION_EXISTS"] = policy
	}
	if (!opt.Ephemeral || opt.SessionPolicy == "resume") && (agentKind == "claude-code" || agentKind == "codex") {
		envMap["LEGATO_SESSION_POLICY"] = opt.SessionPolicy
		if opt.SessionPolicy == "resume" {
			envMap["LEGATO_RESUME_SESSION_ID"] = opt.ResumeSessionID
			if opt.ResumeStorageDir != "" {
				key := "CODEX_HOME"
				if agentKind == "claude-code" {
					key = "CLAUDE_CONFIG_DIR"
				}
				envMap[key] = opt.ResumeStorageDir
			}
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
	// Resolve the prompt content for the launch. Swarm participants (a role
	// or parent task is set) get the role system prompt with the adapter
	// preamble; plain/ephemeral spawns get a general standalone prompt so the
	// agent doesn't mistake itself for a swarm worker. The result and any
	// brief are written to per-agent files under ~/.legato/agents/<taskID>/.
	// The launch command receives paths via env vars so multi-line/quoted
	// content never has to traverse shell escaping.
	isSwarm := opt.Role != "" || opt.ParentTaskID != ""
	rolePrompt := ""
	ownPrompt := opt.LaunchPrompt != ""
	switch {
	case isSwarm:
		rolePrompt = rolePromptForAdapter(adapter, opt.Role)
	case ownPrompt:
		rolePrompt = opt.LaunchPrompt
	default:
		rolePrompt = generalPromptForAdapter(adapter)
	}
	peerContextInjected := false
	if len(launchPeers) > 0 && rolePrompt != "" {
		rolePrompt = joinPromptSections(rolePrompt, peerCoordinationContext(launchPeers))
		peerContextInjected = true
	}
	// After the peer decision above, so that a session whose only launch
	// prompt is this guidance still gets its peers as a message, as before.
	if tp, ok := adapter.(BranchToolsPromptAdapter); ok && opt.Branches.Base != "" {
		rolePrompt = joinPromptSections(rolePrompt, tp.ToolsPromptFor(opt.Branches.Base, opt.Branches.Given, opt.Branches.Granted))
	} else if tp, ok := adapter.(ToolsPromptAdapter); ok {
		rolePrompt = joinPromptSections(rolePrompt, tp.ToolsPrompt())
	}
	// The repository's own resources (agents.repository_resources), for
	// every kind of agent, worked out again at each spawn.
	needsProject := (a.learning != nil || a.guidance != nil || a.prModes != nil) && adapter != nil && !nativeResume && !ownPrompt
	identityKey := ""
	if needsProject || a.repoResources != nil {
		key, identityErr := a.spawnProjectKey(ctx, taskID, workDir, cloneRepo)
		if identityErr != nil && needsProject {
			return identityErr
		}
		identityKey = key
	}
	var repoResources []Resource
	if repo, ok := strings.CutPrefix(identityKey, "repo:"); ok && a.repoResources != nil {
		given, err := a.repoResources.ResourcesForRepository(ctx, repo)
		if err != nil {
			log.Printf("reading the resources of %s: %v", repo, err)
		}
		_, repoResources = mergeResources(opt.Resources, given)
	}
	resources := usableResources(append(append([]Resource(nil), opt.Resources...), repoResources...))
	rolePrompt = joinPromptSections(rolePrompt, resourcesPrompt(resources))
	rolePrompt = joinPromptSections(rolePrompt, freshClone.prompt(workDir))
	if nativeResume {
		rolePrompt = ""
	}
	projectKey := ""
	if needsProject {
		projectKey = identityKey
	}
	// With reviews on, an adapter whose agents learn of them only from here
	// (Claude Code, Codex) is told; swarm workers never review.
	if a.reviewsOff && !nativeResume && !ownPrompt {
		rolePrompt = joinPromptSections(rolePrompt, reviewsOffPrompt)
	} else if rp, ok := adapter.(ReviewsPromptAdapter); ok && rp.NeedsReviewsPrompt() && !nativeResume && !isSwarm && !ownPrompt &&
		(a.hasSkill == nil || a.hasSkill(reviewSkill)) {
		rolePrompt = joinPromptSections(rolePrompt, reviewsOnPrompt)
	}
	// An agent that launches with a prompt is asked to report its stage. A
	// reviewer slot only ever reviews, the Reeve (its own prompt) has no
	// stage, and a swarm's stages come from the swarm's own milestones.
	if rolePrompt != "" && !nativeResume && !ownPrompt && !named && !isSwarm {
		rolePrompt = joinPromptSections(rolePrompt, stagePrompt)
	}
	if repo, ok := strings.CutPrefix(projectKey, "repo:"); ok && a.guidance != nil {
		text, guidanceErr := a.guidance.GuidanceFor(ctx, repo)
		if guidanceErr != nil {
			log.Printf("reading the guidance for %s: %v", repo, guidanceErr)
		}
		rolePrompt = joinPromptSections(rolePrompt, repositoryGuidancePrompt(repo, text))
	}
	if repo, ok := strings.CutPrefix(projectKey, "repo:"); ok && a.prModes != nil {
		rolePrompt = joinPromptSections(rolePrompt, pullRequestModePrompt(repo, a.prModes.PullRequestMode(ctx, repo)))
	}
	var promptManifestID string
	if a.learning != nil && adapter != nil && !nativeResume && !ownPrompt {
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
	if booted.booted() && booted.Packet != "" {
		rolePrompt = joinPromptSections(EngramPromptSection(booted.name, booted.Packet), rolePrompt)
	}
	missing := ""
	if booted != nil && !booted.booted() && opt.EngramMissing != nil {
		missing = opt.EngramMissing(booted.failed)
		rolePrompt = joinPromptSections(missing, rolePrompt)
	}
	if rolePrompt != "" || assignment != "" {
		rolePath, briefPath, perr := writeAgentPromptFiles(agentKey, rolePrompt, assignment)
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

	// A prompt of more than one line reaches a terminal agent as a file it
	// is told to read; the pane takes one line.
	prompt := strings.TrimSpace(opt.Prompt)
	// A resumed conversation takes no launch prompt (Claude Code and Codex
	// drop it, and Chimera's --session launch does not pass --prompt), so
	// it gets a freshly booted engram's packet as its first message.
	resumedLaunch := nativeResume || (opt.SessionPolicy == "resume" && agentKind == "chimera")
	if resumedLaunch && booted.booted() && !booted.resumed && booted.Packet != "" {
		prompt = joinPromptSections(EngramPromptSection(booted.name, booted.Packet), prompt)
	}
	if resumedLaunch && missing != "" {
		prompt = joinPromptSections(missing, prompt)
	}
	if strings.ContainsAny(prompt, "\n\r\"") {
		if dir, err := swarm.AgentDir(agentKey); err != nil {
			log.Printf("writing the prompt of %s: %v", taskID, err)
		} else {
			path := filepath.Join(dir, "prompt.md")
			if err := os.WriteFile(path, []byte(prompt+"\n"), 0o600); err != nil {
				log.Printf("writing the prompt of %s: %v", taskID, err)
			} else {
				envMap["LEGATO_PROMPT_FILE"] = path
			}
		}
	}

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
		SubagentTelemetry: reportsSubagents(adapter), Label: strings.TrimSpace(opt.Label),
	}
	if err := a.store.CreateExecution(ctx, execution); err != nil {
		return fmt.Errorf("creating execution: %w", err)
	}
	if accountIdentity != nil {
		if err := a.store.PutExecutionAccount(ctx, executionID, *accountIdentity); err != nil {
			_ = a.EndExecution(ctx, executionID, store.ExecutionFailed)
			return fmt.Errorf("recording Claude account identity: %w", err)
		}
	}
	if opt.ReadOnlyCheckout {
		if err := a.store.SetExecutionReadOnly(ctx, executionID); err != nil {
			return fmt.Errorf("marking the execution read-only: %w", err)
		}
	}
	envMap["LEGATO_EXECUTION_ID"] = executionID
	// A new conversation starts its place's work again; a resumed one
	// (Resume, Reload, the sandbox switch) keeps where it stood.
	if opt.SessionPolicy != "resume" {
		place := TaskPlace(taskID, slot)
		if executionPlace(executionKind, attributionTaskID, taskID) {
			place.PlaceExecutionID = executionID
		}
		if err := a.workStages.Start(ctx, place, executionID); err != nil {
			log.Printf("resetting the stage of %s: %v", AgentSlotKey(taskID, slot), err)
		}
	}
	// Once recorded on the execution, an engram session is ended with it.
	failExecution := func() { _ = a.EndExecution(ctx, executionID, store.ExecutionFailed) }
	if booted.booted() {
		for k, v := range booted.Env {
			envMap[k] = v
		}
		if err := a.store.SetExecutionEngram(ctx, executionID, booted.name, booted.session); err != nil {
			failExecution()
			return fmt.Errorf("recording the engram: %w", err)
		}
		if booted.home != "" {
			if err := a.store.SetExecutionEngramHome(ctx, executionID, booted.home); err != nil {
				failExecution()
				return fmt.Errorf("recording the engram's store: %w", err)
			}
		}
		engramRecorded = true
	} else if booted != nil {
		if err := a.store.SetExecutionEngramFailure(ctx, executionID, booted.name, booted.failed); err != nil {
			log.Printf("engram: recording that %s did not boot for %s: %v", booted.name, taskID, err)
		}
	}
	// Credential channels: placeholder keys, the proxy's address and the
	// session's own SSH agent, which win over what the session inherits.
	if provider := a.credentialEnv(); provider != nil {
		channelEnv, err := provider.SessionEnv(ctx, taskID, executionID)
		if err != nil {
			log.Printf("credentials: session environment for %s: %v", taskID, err)
		}
		for k, v := range channelEnv {
			envMap[k] = v
		}
	}
	// The host broker, which agent-side processes ask instead of the store.
	if provider := a.brokerSessionEnv(); provider != nil {
		brokerEnv, err := provider.SessionEnv(ctx, taskID, executionID)
		if err != nil {
			log.Printf("broker: session environment for %s: %v", taskID, err)
		}
		for k, v := range brokerEnv {
			envMap[k] = v
		}
	}

	// Skills, copied for this execution where its tool finds them.
	if err := applyAccountEnvironment(envMap, accountIdentity); err != nil {
		failExecution()
		return err
	}
	accountArgs := slices.Clone(opt.LaunchArgs)
	if configured, ok := adapter.(interface{ LaunchArguments(string) []string }); ok {
		accountArgs = append(accountArgs, configured.LaunchArguments(opt.Tier)...)
	}
	if err := validateNamedAccountAuth(accountIdentity, workDir, accountArgs); err != nil {
		failExecution()
		return err
	}
	if accountIdentity != nil && accountIdentity.Account != "" && a.claudeManagedSettingsDir != "" {
		if err := validateNamedAccountManagedSettings(a.claudeManagedSettingsDir); err != nil {
			failExecution()
			return err
		}
	}
	a.giveCapabilities(ctx, CapabilityRequest{TaskID: taskID, ExecutionID: executionID, AgentKind: agentKind, Role: opt.Role, Slot: slot, WorkDir: workDir}, adapter, envMap)

	// The bootstrap secret is read by the launch command, never tmux's environment.
	var chimeraSecret, chimeraSecretPath string
	if agentKind == "chimera" {
		dir, err := swarm.AgentDir(agentKey)
		if err != nil {
			failExecution()
			return fmt.Errorf("locating Chimera secret folder: %w", err)
		}
		privateDir, err := os.MkdirTemp(dir, "chimera-launch-")
		if err != nil {
			failExecution()
			return fmt.Errorf("creating Chimera secret folder: %w", err)
		}
		random := make([]byte, 32)
		if _, err := rand.Read(random); err != nil {
			failExecution()
			return fmt.Errorf("generating Chimera secret: %w", err)
		}
		chimeraSecret = hex.EncodeToString(random)
		chimeraSecretPath = filepath.Join(privateDir, "server-secret")
		if err := os.WriteFile(chimeraSecretPath, []byte(chimeraSecret), 0600); err != nil {
			failExecution()
			return fmt.Errorf("writing Chimera secret: %w", err)
		}
		envMap["LEGATO_CHIMERA_SECRET_FILE"] = chimeraSecretPath
		defer func() {
			if spawnErr != nil {
				_ = os.Remove(chimeraSecretPath)
			}
		}()
	}

	// Legato's sandbox: decided and checked before any tmux session exists,
	// so a sandbox that cannot be built never leaves an unsandboxed agent.
	sandboxPrefix := ""
	sandboxed := a.sandbox != nil && (!opt.Unsandboxed || opt.Unattended || opt.ReadOnlyCheckout || opt.ParentTaskID != "" || opt.SubtaskID != "" || opt.WorkflowRunID != "")
	if sandboxed {
		var engramMounts SandboxEngram
		if booted.booted() {
			engramMounts = SandboxEngram{Binary: booted.Binary, Store: booted.Store}
		}
		prefix, err := a.sandboxPrefix(ctx, taskID, attributionTaskID, executionID, agentKind, workDir, envMap, append(resources, freshClone.sandboxResources()...), opt.ReadOnlyCheckout, opt.ThrowawayCheckout, opt.DockerHostNetwork, engramMounts)
		if err != nil {
			failExecution()
			return fmt.Errorf("Legato's sandbox: %w", err)
		}
		sandboxPrefix = prefix
	}

	envVars := make([]string, 0, len(envMap))
	for k, v := range envMap {
		envVars = append(envVars, k+"="+v)
	}

	// Snapshot ordinary repository HEAD before the agent starts. Review sync can
	// then import only this session's commits even when no task worktree exists.
	// A named slot's agent is not the task's author: its session is not one.
	if !named && gitpkg.IsRepository(ctx, workDir) {
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
		Sandboxed:   sandboxed,
		Slot:        slot,
		// Checked above: only a sandboxed agent of the operator's has it.
		DockerHostNetwork: opt.DockerHostNetwork,
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
	if chimeraSecret != "" {
		if err := a.store.SetChimeraLaunchSecret(ctx, sessionID, chimeraSecret, chimeraSecretPath); err != nil {
			a.tmux.Kill(sessionName)
			_ = a.store.UpdateAgentSessionStatusInSlot(ctx, taskID, slot, "dead")
			failExecution()
			return fmt.Errorf("recording Chimera launch authentication: %w", err)
		}
	}
	if err := a.store.LinkExecutionAgentSession(ctx, executionID, sessionID); err != nil {
		a.tmux.Kill(sessionName)
		_ = a.store.UpdateAgentSessionStatusInSlot(ctx, taskID, slot, "dead")
		failExecution()
		return fmt.Errorf("linking execution to agent session: %w", err)
	}
	if len(opt.Resources) > 0 {
		names := make([]string, 0, len(opt.Resources))
		for _, resource := range opt.Resources {
			names = append(names, resource.Name)
		}
		if err := a.store.AddExecutionResources(ctx, executionID, names); err != nil {
			log.Printf("recording the resources of %s: %v", executionID, err)
		}
	}
	if len(repoResources) > 0 {
		names := make([]string, 0, len(repoResources))
		for _, resource := range repoResources {
			names = append(names, resource.Name)
		}
		if err := a.store.AddExecutionRepositoryResources(ctx, executionID, names); err != nil {
			log.Printf("recording the repository resources of %s: %v", executionID, err)
		}
	}
	if promptManifestID != "" {
		if err := a.store.BindLearningPromptManifestSession(ctx, promptManifestID, sessionID); err != nil {
			a.tmux.Kill(sessionName)
			_ = a.store.UpdateAgentSessionStatusInSlot(ctx, taskID, slot, "dead")
			failExecution()
			return fmt.Errorf("binding learning prompt manifest: %w", err)
		}
	}

	// Capture the lockfile baseline before launching Chimera. Fast agents can
	// write their lockfile synchronously during SendKeysShellCommand; taking this
	// snapshot in the discovery goroutine would incorrectly classify it as old.
	var chimeraLockfilesBeforeLaunch map[string]bool
	// Discovery matches by session name (the task ID), which Chimera always
	// preserves from --session-name regardless of collision-avoidance.
	expectedChimeraSessionName := envMap["LEGATO_CHIMERA_SESSION_NAME"]
	if agentKind == "chimera" {
		chimeraLockfilesBeforeLaunch = snapshotChimeraLockfiles()
	}

	// Auto-launch: if the adapter knows how to start its AI tool, send-keys
	// the launch command into the freshly-created session. When a brief file
	// was written, kick the agent off with a short pointer to read it; this
	// is more robust than streaming the brief content through send-keys
	// because the brief is in the agent's filesystem and survives any
	// terminal/shell escaping pitfalls.
	launchEnv := envMap
	if sandboxed {
		// The adapter is told its tool runs in the sandbox (Chimera then
		// drops its own Docker sandbox); the pane's shell is not.
		launchEnv = maps.Clone(envMap)
		launchEnv["LEGATO_SANDBOXED"] = "1"
	}
	// A clone this spawn made is set up first, where the agent will be: in
	// its sandbox. The agent starts whether or not the setup succeeds.
	setupLine := ""
	if freshClone != nil && freshClone.command != "" {
		setupLine = freshClone.command
		if sandboxed {
			setupLine = sandboxPrefix + " " + setupLine
		}
		// The host shell says how it ended, even when the sandbox failed
		// to start, so nothing waits for a setup that never ran.
		status := cloneSetupStatus(workDir)
		_ = os.Remove(status)
		setupLine += "; echo $? > " + shellWord(status)
		a.waitForCloneSetup(taskID, status)
	}
	thenLaunch := func(cmd string) string {
		if setupLine == "" {
			return cmd
		}
		return setupLine + "; " + cmd
	}
	if _, ok := adapter.(LaunchCommandAdapter); !ok {
		switch {
		case sandboxed:
			// A shell agent: the person's shell, in the sandbox.
			if err := a.tmux.SendKeysShellCommand(sessionName, thenLaunch(sandboxPrefix+" ${SHELL:-/bin/bash}")); err != nil {
				log.Printf("sandboxed shell send-keys failed for %s: %v", sessionName, err)
			}
		case setupLine != "":
			if err := a.tmux.SendKeysShellCommand(sessionName, setupLine); err != nil {
				log.Printf("clone setup send-keys failed for %s: %v", sessionName, err)
			}
		}
	}
	if launcher, ok := adapter.(LaunchCommandAdapter); ok {
		var cmd string
		wrapped, ownsPrefix := adapter.(SandboxLaunchCommandAdapter)
		if sandboxed && ownsPrefix {
			cmd = wrapped.LaunchCommandWithSandbox(launchEnv, opt.Brief, opt.Model, opt.Tier, sandboxPrefix)
		} else {
			cmd = launcher.LaunchCommand(launchEnv, opt.Brief, opt.Model, opt.Tier)
		}
		if cmd != "" {
			if off, ok := adapter.(PromptsOffAdapter); ok && opt.Unattended {
				for _, arg := range off.PromptsOffArgs() {
					cmd += " " + shellWord(arg)
				}
			}
			if granter, ok := adapter.(PathGrantingAdapter); ok && len(resources) > 0 {
				read, write := resourceGrants(resources)
				cmd += granter.PathGrantFlags(read, write)
			}
			for _, arg := range opt.LaunchArgs {
				cmd += " " + shellWord(arg)
			}
			if sandboxed && !ownsPrefix {
				cmd = sandboxPrefix + " " + cmd
			}
			cmd = accountLaunchCommand(cmd, accountIdentity)
			cmd = thenLaunch(cmd)
			if truster, ok := adapter.(WorkspaceTrustAdapter); ok {
				if err := truster.TrustWorkDir(launchEnv, workDir); err != nil {
					log.Printf("trusting %s for %s: %v", workDir, taskID, err)
				}
			}
			// Use SendKeysShellCommand because the receiver here is the
			// freshly-spawned bash shell, not the AI tool. Bash with
			// bracketed-paste mode treats a single text+Enter send-keys as
			// a paste and leaves the command sitting in the prompt buffer
			// instead of executing it.
			if err := a.tmux.SendKeysShellCommand(sessionName, cmd); err != nil {
				// Launch failure is best-effort: the session exists, the user
				// can recover by attaching and running the AI tool manually.
				log.Printf("auto-launch send-keys failed for %s: %v", sessionName, err)
			} else {
				// The brief kickoff, then the prompt, once the AI tool can
				// take a message: over its native endpoint, else typed.
				var messages []string
				if _, hasBrief := envMap["LEGATO_BRIEF_FILE"]; hasBrief && !nativeResume {
					// Skip the kickoff for adapters whose launch command already
					// constitutes the first user turn (e.g. Chimera's `--prompt`).
					if k, ok := adapter.(LaunchSelfKickoff); !ok || !k.LaunchIsSelfKickoff() {
						messages = append(messages, briefKickoffMessage)
					}
				}
				if prompt != "" {
					messages = append(messages, prompt)
				}
				if len(messages) > 0 {
					a.launchWith(ctx, executionID, taskID, slot, sessionName, agentKind, adapter, messages, envMap["LEGATO_PROMPT_FILE"])
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
		go a.discoverChimeraLockfile(WithAgentSlot(context.Background(), slot), taskID, expectedChimeraSessionName, chimeraLockfilesBeforeLaunch)
	}

	if executionKind == store.ExecutionInteractive && !named && !opt.Ephemeral && opt.Role == "" && opt.ParentTaskID == "" && opt.SubtaskID == "" {
		if err := a.rememberAgentWorkingDir(ctx, taskID, workDir); err != nil {
			return fmt.Errorf("remembering agent working directory: %w", err)
		}
	}

	// Capture repository identity from the exact agent working directory after
	// remembering it, so asynchronous enrichment always merges from the latest
	// task context. Agent startup remains successful when local Git inspection or
	// GitHub synchronization is unavailable.
	if a.checkoutInspector != nil && !named {
		go a.captureAgentCodeContext(taskID, workDir)
	}
	if a.bus != nil {
		a.bus.PublishAgentStarted(taskID)
	}
	if len(launchPeers) > 0 {
		go a.notifyPeerJoin(taskID, executionID, workDir, adapter, launchPeers, !peerContextInjected)
	}

	return nil
}

// samePath says whether two paths name the same folder, links resolved.
func samePath(a, b string) bool {
	ra, errA := filepath.EvalSymlinks(a)
	rb, errB := filepath.EvalSymlinks(b)
	if errA != nil || errB != nil {
		return filepath.Clean(a) == filepath.Clean(b)
	}
	return ra == rb
}

func peerCoordinationContext(peers []AgentPeer) string {
	peerIDs := make([]string, 0, len(peers))
	for _, peer := range peers {
		peerIDs = append(peerIDs, fmt.Sprintf("%s (%s)", peer.TaskID, peer.ExecutionID))
	}
	return fmt.Sprintf("## Active worktree peers\n\nAgents already working in this Git worktree: %s. Before reverting unexpected changes, use `legato agent message <task-id> <text>` to coordinate.", strings.Join(peerIDs, ", "))
}

func (a *agentService) notifyPeerJoin(taskID, executionID, workDir string, adapter AIToolAdapter, peers []AgentPeer, notifyNewcomer bool) {
	root, err := gitWorktreeRoot(workDir)
	if err != nil {
		root = workDir
	}
	if notifyNewcomer {
		// The newcomer is told once it can take a message; its peers hear of
		// it after that, as they always did.
		a.awaitNativeEndpoint(context.Background(), taskID, adapter)
		if err := a.QueueMessage(context.Background(), taskID, "[legato coordination] "+strings.TrimPrefix(peerCoordinationContext(peers), "## Active worktree peers\n\n")); err != nil {
			log.Printf("agent coordination notice to %s (%s) failed: %v", taskID, executionID, err)
		}
	}
	// Legato's own notice, not the newcomer's words: it goes out without the
	// peer framing and names the command that reaches the newcomer itself.
	notice := fmt.Sprintf("[legato coordination] Agent %s (%s) joined Git worktree %s. It may create shared-tree changes; use `legato agent message %s <text>` before reverting unexpected edits.", taskID, executionID, root, taskID)
	for _, peer := range peers {
		if err := a.QueueMessage(context.Background(), peer.TaskID, notice); err != nil {
			log.Printf("agent coordination join notice from %s to %s failed: %v", taskID, peer.TaskID, err)
		}
	}
}

// deliverLaunchMessages keeps each message until its transport accepts it.
// Chimera launch tasks require a new native turn; only other terminal agents
// use the prompt-file pointer fallback.
func (a *agentService) deliverLaunchMessages(ctx context.Context, executionID, taskID, session string, adapter AIToolAdapter, messages []store.LaunchMessage, promptFile string) {
	if _, active := a.launchDeliveries.LoadOrStore(executionID, struct{}{}); active {
		log.Printf("launch messages for %s (%s) skipped: delivery already in progress", taskID, executionID)
		return
	}
	defer a.launchDeliveries.Delete(executionID)

	// The agent tool starts once its clone is set up, which takes minutes
	// the first time the dependencies are fetched.
	if err := a.AwaitCloneSetup(ctx, taskID); err != nil {
		log.Printf("launch messages for %s: %v", session, err)
	}
	native := a.awaitNativeEndpoint(ctx, taskID, adapter)
	if !native && a.failUnboundWorkflowSteps(ctx, executionID, taskID, session, adapter) {
		return
	}
	for _, kept := range messages {
		owned, err := a.store.OwnsLaunchMessage(ctx, executionID, kept.Position, a.launchOwner)
		if err != nil {
			log.Printf("launch message %s/%d for %s retained: checking ownership: %v", executionID, kept.Position, taskID, err)
			return
		}
		if !owned {
			log.Printf("launch message %s/%d for %s skipped: no longer pending for this owner", executionID, kept.Position, taskID)
			return
		}
		message := kept.Message
		if adapter != nil && adapter.Name() == "chimera" {
			if !native {
				err = fmt.Errorf("native binding not ready; launch message was not typed into the composer")
			} else {
				deliveryCtx, cancel := context.WithTimeout(ctx, a.nativeBindTimeout)
				err = a.chimeraAdapter.startLaunchMessage(deliveryCtx, taskID, message)
				cancel()
			}
		} else if native {
			err = a.QueueMessage(ctx, taskID, message)
		} else {
			err = a.verifyPaneDelivery(session)
			if err == nil {
				typed := message
				if promptFile != "" && strings.ContainsAny(message, "\n\r\"") {
					typed = "Read " + promptFile + " in full: it is your task from the operator. Then do it."
				}
				err = a.tmux.SendKeysLine(session, typed)
			}
		}
		if err != nil {
			log.Printf("launch message %s/%d for %s retained: delivery failed: %v", executionID, kept.Position, taskID, err)
			return
		}
		completed, err := a.store.CompleteLaunchMessage(ctx, executionID, kept.Position, a.launchOwner)
		if err != nil {
			log.Printf("launch message %s/%d for %s accepted but acknowledgement failed; retained message may replay: %v", executionID, kept.Position, taskID, err)
			return
		}
		if !completed {
			log.Printf("launch message %s/%d for %s accepted but ownership changed before acknowledgement", executionID, kept.Position, taskID)
			return
		}
		log.Printf("launch message %s/%d for %s delivered (native: %t)", executionID, kept.Position, taskID, native)
	}
}

// failUnboundWorkflowSteps fails the workflow steps waiting on an agent whose
// native route never came up, instead of holding its prompt for a delivery
// that will not happen while the step waits forever (LEG-531). The prompt is
// dropped with the step: delivered later, it would start work for a step
// that has already failed. It reports whether it failed any step; an agent
// no step waits on keeps its prompt as before.
func (a *agentService) failUnboundWorkflowSteps(ctx context.Context, executionID, taskID, tmuxSession string, adapter AIToolAdapter) bool {
	binder, ok := adapter.(NativeBindingAdapter)
	if !ok || !binder.BindsNativeEndpoint() {
		return false
	}
	session, err := slotSession(ctx, a.store, taskID)
	if err != nil || session.ExecutionID != executionID {
		return false
	}
	attempts, err := a.store.ListWaitingWorkflowAttemptsByAgentSession(ctx, session.ID)
	if err != nil || len(attempts) == 0 {
		return false
	}
	reason := fmt.Sprintf("the agent never connected to Legato: its %s session did not bind its native route within %s, so its prompt was not delivered (its terminal is tmux session %s)",
		adapter.Name(), a.nativeBindTimeout, tmuxSession)
	failed := false
	for _, attempt := range attempts {
		if err := a.store.FailWorkflowStep(ctx, attempt.RunID, attempt.Path, attempt.ID, reason); err != nil {
			if !errors.Is(err, store.ErrWorkflowStateConflict) {
				log.Printf("failing workflow step run=%s step=%s of unbound agent %s: %v", attempt.RunID, attempt.Path, taskID, err)
			}
			continue
		}
		failed = true
		log.Printf("workflow step failed run=%s step=%s attempt=%s: %s", attempt.RunID, attempt.Path, attempt.ID, reason)
		if a.bus != nil {
			a.bus.PublishWorkflowChanged(attempt.RunID)
		}
	}
	if failed {
		if err := a.store.DeleteLaunchMessages(ctx, executionID); err != nil {
			log.Printf("dropping launch messages of %s (%s): %v", taskID, executionID, err)
		}
	}
	return failed
}

// awaitNativeEndpoint waits for adapters that bind natively; other tools get
// the fixed kickoff delay. False leaves the caller to choose its failure path.
func (a *agentService) awaitNativeEndpoint(ctx context.Context, taskID string, adapter AIToolAdapter) bool {
	binder, ok := adapter.(NativeBindingAdapter)
	if !ok || !binder.BindsNativeEndpoint() {
		time.Sleep(a.briefKickoffDelay)
		return false
	}
	deadline := time.Now().Add(a.nativeBindTimeout)
	for {
		session, err := slotSession(ctx, a.store, taskID)
		if err != nil {
			return false
		}
		if session.ChimeraEndpoint != nil && session.ChimeraSessionID != nil {
			return true
		}
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(nativeBindPoll)
	}
}

func (a *agentService) rememberAgentWorkingDir(ctx context.Context, taskID, workDir string) error {
	task, err := a.store.GetTask(ctx, taskID)
	if err != nil {
		return err
	}
	codeContext, err := store.ParseCodeContext(task.CodeContext)
	if err != nil {
		return fmt.Errorf("parsing task code context: %w", err)
	}
	if codeContext == nil {
		codeContext = &store.CodeContext{}
	}
	codeContext.WorkingDir = workDir
	codeContext.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	return a.store.SetCodeContext(ctx, taskID, codeContext)
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
	// The same checkout on the same branch (a resume, a reload) keeps what
	// was found: only branches with no pull request are looked for. A new
	// branch looks again from nothing.
	if existing != nil && existing.Repo == captured.Repo && existing.Branch == captured.Branch {
		if actions, ok := a.prSvc.(PRBranchActions); ok {
			_ = actions.DiscoverPRs(ctx, taskID)
		}
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
	taskID, err := a.CreateEphemeralTask(ctx, title)
	if err != nil {
		return err
	}
	if report := spawnReportFrom(ctx); report != nil {
		report.TaskID = taskID
	}
	var opt AgentSpawnOptions
	if len(opts) > 0 {
		opt = opts[0]
	}
	opt.Ephemeral = true
	return a.SpawnAgent(ctx, taskID, width, height, opt)
}

// CreateEphemeralTask makes the task of an agent that belongs to no card,
// before the agent is started: a spawn that clones in the background
// answers with it at once.
func (a *agentService) CreateEphemeralTask(ctx context.Context, title string) (string, error) {
	taskID, err := a.store.CreateEphemeralTask(ctx, title)
	if err != nil {
		return "", fmt.Errorf("creating ephemeral task: %w", err)
	}
	return taskID, nil
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

// DefaultAdapter reads the new-work default with a background context. On
// failure it shows the configured default; SpawnAgent returns the error.
func (a *agentService) DefaultAdapter() string {
	defaults, err := a.NewWorkDefaults(context.Background())
	if err == nil {
		return defaults.Agent
	}
	return a.baseDefaultAdapter()
}

func (a *agentService) NewWorkDefaults(ctx context.Context) (NewWorkDefaults, error) {
	if a.newWorkDefaults != nil {
		defaults, err := a.newWorkDefaults(ctx)
		if err != nil {
			return NewWorkDefaults{}, err
		}
		if defaults.Agent != "" {
			return defaults, nil
		}
	}
	return NewWorkDefaults{Agent: a.baseDefaultAdapter()}, nil
}

func (a *agentService) baseDefaultAdapter() string {
	if a.adapter == nil {
		return ""
	}
	return a.adapter.Name()
}

// KillAgent stops the agent of an agent ID: the task's main slot's, or a
// named slot's (<task>_<slot>).
func (a *agentService) KillAgent(ctx context.Context, taskID string) error {
	taskID, slot := SplitAgentID(taskID)
	return a.stopAgentInSlot(ctx, taskID, slot, true)
}

// stopAgent ends the agent of taskID. announce says whether the agent has
// ended for good: a restart stops it without announcing that it died.
func (a *agentService) stopAgent(ctx context.Context, taskID string, announce bool) error {
	return a.stopAgentInSlot(ctx, taskID, store.SlotMain, announce)
}

// stopAgentInSlot ends the agent of one of the task's slots.
func (a *agentService) stopAgentInSlot(ctx context.Context, taskID, slot string, announce bool) error {
	session, err := a.store.GetAgentSessionInSlot(ctx, taskID, slot)
	if err != nil {
		// No running session — delete dead records
		return a.store.DeleteDeadAgentSessionsInSlot(ctx, taskID, slot)
	}

	// Until the pane has stopped, its session stays running: a pane that
	// cannot be stopped keeps its execution and its sandbox's scratch.
	if err := a.tmux.Kill(session.TmuxSession); err != nil {
		return fmt.Errorf("stopping the agent's pane: %w", err)
	}

	if session.ExecutionID != "" {
		if err := a.EndExecution(ctx, session.ExecutionID, store.ExecutionCancelled); err != nil {
			return err
		}
	}

	if err := a.store.UpdateAgentSessionStatusInSlot(ctx, taskID, slot, "dead"); err != nil {
		return err
	}
	if a.bus != nil && announce && IsNamedSlot(slot) {
		a.bus.PublishSlotAgentDied(taskID, slot)
		return a.store.DeleteDeadAgentSessionsInSlot(ctx, taskID, slot)
	}
	// Publish AgentDied so the swarm conductor (and any other subscribers)
	// receive the same notification regardless of whether the kill came from
	// an explicit user/conductor action or external session termination.
	if a.bus != nil && announce {
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
	return a.store.DeleteDeadAgentSessionsInSlot(ctx, taskID, slot)
}

func (a *agentService) ListAgents(ctx context.Context) ([]AgentSession, error) {
	sessions, err := a.store.ListAgentSessions(ctx)
	if err != nil {
		return nil, err
	}

	// Query live pane commands; fall back to DB values on error.
	liveCmds, _ := a.tmux.PaneCommands()

	result := make([]AgentSession, len(sessions))
	var reeveReachable *bool // read once, when an agent waits on the Reeve
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
		if s.ExecutionLabel != "" {
			title = s.ExecutionLabel // an agent.run step's name for its agent
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

		attributionTaskID := s.AttributionTaskID
		if attributionTaskID == "" {
			attributionTaskID = s.TaskID
		}
		result[i] = AgentSession{
			ID:                s.ID,
			TaskID:            s.TaskID,
			ExecutionID:       s.ExecutionID,
			AttributionTaskID: attributionTaskID,
			ExecutionKind:     s.ExecutionKind,
			WorkflowRunID:     s.WorkflowRunID,
			WorkflowAttemptID: s.WorkflowAttemptID,
			Engram:            s.Engram,
			Title:             title,
			Group:             group,
			TmuxSession:       s.TmuxSession,
			Command:           command,
			AgentKind:         s.AgentKind,
			Account:           s.Account, AccountLabel: a.claudeAccountLabel(s.Account),
			Status:            s.Status,
			Activity:          s.Activity,
			Role:              s.Role,
			ParentTaskID:      parentID,
			SubtaskID:         subtaskID,
			DockerHostNetwork: s.DockerHostNetwork,
			StartedAt:         startedAt,
			EndedAt:           endedAt,
			NotifyEnabled:     notifyEnabled,
			Sandboxed:         s.Sandboxed,
			Slot:              s.Slot,
			CardTaskID:        s.TaskID,
			Reeve:             IsReeveTask(ctx, a.store, s.TaskID),
			WorkingDir:        s.WorkingDir,
			CloneRepo:         s.CloneRepo,
			CloneBranch:       s.CloneBranch,
			OpenItems:         s.OpenItems,
			OpenDoneItems:     s.OpenDoneItems,
			PlanPending:       s.PlanPending,
			Stage:             s.WorkStage,
			StageReason:       s.WorkStageReason,
			StageSource:       s.WorkStageSource,
			StageSince:        parseStageTime(s.WorkStageSince),
		}
		if since, err := time.Parse(time.RFC3339, s.ReeveWaitSince); err == nil && s.ReeveWaitSince != "" {
			if reeveReachable == nil {
				current, _ := a.store.CurrentReeveSession(ctx)
				reachable := current != nil || a.reeveWakes
				reeveReachable = &reachable
			}
			var toldAt *time.Time
			if told, err := time.Parse(time.RFC3339, s.ReeveWaitToldAt); err == nil {
				toldAt = &told
			}
			result[i].ReeveWaitSince, result[i].ReeveWait = &since, reeveWaitState(since, toldAt, *reeveReachable, time.Now())
		}
		if s.RunCardTaskID != "" {
			result[i].RunCard = &RunCard{TaskID: s.RunCardTaskID, Key: s.RunCardKey, Title: s.RunCardTitle}
		}
		if s.Status == "running" {
			result[i].BackgroundJobs = s.BackgroundJobs
			result[i].BackgroundShells = s.BackgroundShells
		}
		if result[i].Reeve {
			if name := a.reeveName.Of(ctx); name != "" {
				result[i].ReeveName, result[i].Title = string(name), name.Start()
			}
		}
		if IsNamedSlot(s.Slot) {
			result[i].TaskID = AgentSlotKey(s.TaskID, s.Slot)
			result[i].Title = strings.TrimSpace(title + " · " + s.Slot)
		}

		// Hydrate subtask metadata for swarm workers.
		if subtaskID != "" {
			if st, err := a.store.GetSubtask(ctx, subtaskID); err == nil {
				globs, _ := store.ParseScopeGlobs(st.ScopeGlobs)
				result[i].Description = st.Description
				result[i].Prompt = st.Prompt
				result[i].Scope = globs
				if result[i].Title == "" {
					// A worker's task is its sub-task, which is no task row.
					result[i].Title = st.Title
				}
			}
		}
	}
	return result, nil
}

// EndExecution records that an execution has ended and removes its
// sandbox's scratch folders, which nothing reads after the agent.
func (a *agentService) EndExecution(ctx context.Context, executionID, status string) error {
	end := a.store.TerminalizeExecution
	if reason, ok := ctx.Value(runEndReasonKey{}).(string); ok {
		end = func(ctx context.Context, executionID, status string) error {
			return a.store.TerminalizeExecutionEndingRuns(ctx, executionID, status, reason)
		}
	}
	if err := end(ctx, executionID, status); err != nil {
		return err
	}
	// Asked before the scratch goes: the view is in the runtime folder.
	if a.docker != nil && a.docker.HasProxy(executionID) {
		a.docker.cleanLogged(executionID)
	}
	a.scratch.removeLogged(executionID)
	a.capabilityFolders.removeLogged(executionID)
	a.endEngramSession(ctx, executionID)
	a.endOperatorItems(ctx, executionID)
	return nil
}

type runEndReasonKey struct{}

// withRunEndReason says why the runs of the execution EndExecution ends
// ended, when its status does not: store.RunExited for an agent whose pane
// is gone, store.RunInterrupted for one a crash took.
func withRunEndReason(ctx context.Context, reason string) context.Context {
	return context.WithValue(ctx, runEndReasonKey{}, reason)
}

// RemoveSandboxScratch removes the scratch folders of an execution that has
// ended.
func (a *agentService) RemoveSandboxScratch(executionID string) {
	a.scratch.removeLogged(executionID)
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

	commands, _ := a.tmux.PaneCommands()
	for _, s := range sessions {
		if agentForeground(s.AgentKind, commands[s.TmuxSession]) {
			a.foregroundAgents.Store(s.ID, struct{}{})
		}
		exited := false
		if s.Status == "running" && live[s.TmuxSession] && enginetmux.IsShellCommand(commands[s.TmuxSession]) && a.agentStartupOver(s) {
			exited, err = a.tmux.ShellReturned(s.TmuxSession)
			if err != nil {
				return err
			}
		}
		if s.Status == "running" && (!live[s.TmuxSession] || exited) {
			log.Printf("agent ended task=%s session=%s foreground=%q", s.TaskID, s.TmuxSession, commands[s.TmuxSession])
			if err := a.endGoneSession(ctx, s, "dead"); err != nil {
				return err
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
	if cleared, err := a.scratch.Sweep(ctx, StoreExecutionStatus(a.store)); err != nil {
		log.Printf("sandbox: sweeping the scratch of ended executions: %v", err)
	} else if cleared > 0 {
		log.Printf("sandbox: removed the scratch of %d ended execution(s)", cleared)
	}
	a.docker.sweepLogged(ctx, StoreExecutionStatus(a.store))
	if cleared, err := a.capabilityFolders.Sweep(ctx, StoreExecutionStatus(a.store)); err != nil {
		log.Printf("capabilities: sweeping the folders of ended executions: %v", err)
	} else if cleared > 0 {
		log.Printf("capabilities: removed the folders of %d ended execution(s)", cleared)
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

// endGoneSession ends a running session whose tmux session is gone, as
// status: "dead" for an agent that ended, AgentSessionInterrupted for one a
// crash took. Its execution is lost; an interrupted agent keeps its engram
// session for the agent that resumes it. Whatever owns the agent hears that
// it died.
func (a *agentService) endGoneSession(ctx context.Context, s store.AgentSession, status string) error {
	return a.endGoneSessionFor(ctx, s, status, legacyAgentExited)
}

// endGoneSessionFor is endGoneSession, failing the steps that wait on an
// ended agent with stepReason. An interrupted agent's agent.run steps are
// suspended instead, until it is resumed.
func (a *agentService) endGoneSessionFor(ctx context.Context, s store.AgentSession, status, stepReason string) error {
	if s.ExecutionID != "" {
		endCtx := withRunEndReason(ctx, store.RunExited)
		if status == store.AgentSessionInterrupted {
			endCtx = withRunEndReason(withOperatorItemsKept(withEngramSessionKept(ctx)), store.RunInterrupted)
		}
		if err := a.EndExecution(endCtx, s.ExecutionID, store.ExecutionLost); err != nil {
			return err
		}
	}
	if err := a.store.UpdateAgentSessionStatusInSlot(ctx, s.TaskID, s.Slot, status); err != nil {
		return err
	}
	a.foregroundAgents.Delete(s.ID)
	log.Printf("agent ended task=%s session=%s status=%s", s.TaskID, s.TmuxSession, status)
	attempts, err := a.store.ListWaitingWorkflowAttemptsByAgentSession(ctx, s.ID)
	if err != nil {
		return err
	}
	for _, attempt := range attempts {
		// A step that waits for its agent to end (wait_for: ended) ends on
		// the workflow host's check; one on an agent no workflow owns waits
		// through an interruption, and binds again when it is resumed
		// (LEG-485). A wait that cannot be read is left to the host too,
		// which reads the tables again: failing or suspending it here could
		// end a step that waits for exactly this.
		signals, err := a.store.WorkflowStepWaitSignals(ctx, attempt.RunID, attempt.Path)
		unreadable := err != nil && !errors.Is(err, store.ErrNotFound)
		if unreadable {
			log.Printf("workflow run %s step %s: reading what it waits for as its agent ended: %v; left to the workflow host", attempt.RunID, attempt.Path, err)
		}
		if unreadable || slices.Contains(signals, "ended") ||
			(status == store.AgentSessionInterrupted && len(signals) > 0 && (plainTaskAgent(s) || attempt.ExecutorType != "agent.run")) {
			if a.bus != nil {
				a.bus.PublishWorkflowChanged(attempt.RunID)
			}
			continue
		}
		if status == store.AgentSessionInterrupted && attempt.ExecutorType == "agent.run" {
			if err := a.store.SuspendWorkflowStep(ctx, attempt.RunID, attempt.Path, attempt.ID, WorkflowSuspendedReason); err != nil && !errors.Is(err, store.ErrWorkflowStateConflict) {
				return err
			}
			log.Printf("workflow agent interrupted run=%s step=%s attempt=%s: step suspended", attempt.RunID, attempt.Path, attempt.ID)
			if a.bus != nil {
				a.bus.PublishWorkflowChanged(attempt.RunID)
			}
			continue
		}
		if err := a.store.FailWorkflowStep(ctx, attempt.RunID, attempt.Path, attempt.ID, stepReason); err != nil && !errors.Is(err, store.ErrWorkflowStateConflict) {
			return err
		}
		log.Printf("workflow agent exited run=%s step=%s attempt=%s", attempt.RunID, attempt.Path, attempt.ID)
		if a.bus != nil {
			a.bus.PublishWorkflowChanged(attempt.RunID)
		}
	}
	if a.bus != nil && IsNamedSlot(s.Slot) {
		a.bus.PublishSlotAgentDied(s.TaskID, s.Slot)
		return nil
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
	// Legato's plugin or mod reports this session; its events own the
	// state and its background jobs (docs/ai/agent-states.md).
	if sess.HookSource == store.HookSourcePlugin || sess.HookSource == store.HookSourceMod {
		return nil
	}
	oldActivity := sess.Activity

	if err := a.store.UpdateAgentActivity(ctx, taskID, activity); err != nil {
		if errors.Is(err, store.ErrAgentStateIgnored) {
			return nil
		}
		return fmt.Errorf("updating agent activity: %w", err)
	}
	if err := a.store.RecordExecutionActivity(ctx, sess.ExecutionID, activity, workingDir); err != nil {
		return fmt.Errorf("recording execution activity: %w", err)
	}
	if a.bus != nil {
		a.bus.PublishAgentActivityChanged(taskID, activity)
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
	session, err := slotSession(ctx, a.store, taskID)
	if err != nil {
		return "", err
	}
	return a.tmux.CaptureWithEscapes(session.TmuxSession)
}

func (a *agentService) AttachCmd(ctx context.Context, taskID string) (*exec.Cmd, error) {
	session, err := slotSession(ctx, a.store, taskID)
	if err != nil {
		return nil, err
	}
	return a.tmux.Attach(session.TmuxSession), nil
}

// GetStateTimeline delegates to the store query for bucketed state labels.
func (a *agentService) GetStateTimeline(ctx context.Context, taskID string, window time.Duration, buckets int) ([]string, error) {
	return a.store.GetStateTimeline(ctx, taskID, window, buckets)
}

// ListPeers returns live agents whose launch directories resolve to the same
// Git worktree root as the caller or explicit directory.
func (a *agentService) ListPeers(ctx context.Context, taskID, executionID, directory string) ([]AgentPeer, error) {
	if err := a.ReconcileSessions(ctx); err != nil {
		return nil, fmt.Errorf("reconciling agent sessions: %w", err)
	}
	rows, err := a.store.ListRunningExecutionPeers(ctx)
	if err != nil {
		return nil, err
	}

	callerDir := strings.TrimSpace(directory)
	if callerDir == "" {
		for _, row := range rows {
			if (executionID != "" && row.ExecutionID == executionID) || (executionID == "" && taskID != "" && row.TaskID == taskID && row.Slot == store.SlotMain) {
				callerDir = row.WorkingDir
				break
			}
		}
	}
	if callerDir == "" {
		return nil, errors.New("cannot resolve agent worktree: provide a running task, execution, or directory")
	}
	callerRoot, err := gitWorktreeRoot(callerDir)
	if err != nil {
		return nil, err
	}

	peers := make([]AgentPeer, 0, len(rows))
	for _, row := range rows {
		// The caller itself; the agents of its task's other slots (its
		// reviewer) are its peers too.
		if (executionID != "" && row.ExecutionID == executionID) || (taskID != "" && row.TaskID == taskID && row.Slot == store.SlotMain) {
			continue
		}
		root, rootErr := gitWorktreeRoot(row.WorkingDir)
		if rootErr != nil || root != callerRoot {
			continue
		}
		peer := AgentPeer{
			TaskID: row.TaskID, ExecutionID: row.ExecutionID, Title: row.TaskTitle,
			Kind: row.Kind, Role: row.Role, Activity: row.Activity, WorkingDir: root,
		}
		if IsNamedSlot(row.Slot) {
			peer.Slot, peer.ReadOnly = row.Slot, row.ReadOnly
		}
		peers = append(peers, peer)
	}
	sort.Slice(peers, func(i, j int) bool { return peers[i].TaskID < peers[j].TaskID })
	return peers, nil
}

// SendPeerMessage queues a coordination message so busy structured agents
// receive it at their next supported step boundary.
func (a *agentService) SendPeerMessage(ctx context.Context, senderTaskID, recipientID, message string) error {
	recipientCtx, recipientTaskID, err := a.resolveAgentAddress(ctx, recipientID)
	if err != nil {
		return err
	}
	if err := a.QueueMessage(recipientCtx, recipientTaskID, formatPeerMessage(senderTaskID, message)); err != nil {
		return err
	}
	// Who messaged whom, never what, for the graph of agents. A message
	// with no sender is the operator's, not coordination between agents.
	if senderTaskID == "" {
		return nil
	}
	if err := a.store.RecordPeerMessage(ctx, senderTaskID, recipientTaskID, time.Now()); err != nil {
		log.Printf("recording peer message %s → %s: %v", senderTaskID, recipientTaskID, err)
		return nil
	}
	if a.peerMessageSent != nil {
		a.peerMessageSent()
	}
	return nil
}

// resolveAgentAddress is the task, and the context that addresses its slot,
// of the agent an address names: a task ID (its main slot), else a running
// execution's ID, whose own slot it keeps, so a reviewer's execution is
// told and not its task's main agent.
func (a *agentService) resolveAgentAddress(ctx context.Context, address string) (context.Context, string, error) {
	if _, err := a.store.GetAgentSessionByTaskID(ctx, address); err == nil {
		return ctx, address, nil
	} else if !errors.Is(err, store.ErrNotFound) {
		return ctx, "", err
	}
	execution, err := a.store.GetExecution(ctx, address)
	if err != nil {
		return ctx, "", fmt.Errorf("agent address %s: %w", address, err)
	}
	if execution.Status != store.ExecutionRunning || execution.ExecutionTaskID == "" || execution.AgentSessionID == nil {
		return ctx, "", fmt.Errorf("execution %s is not a live addressable agent", address)
	}
	session, err := a.store.GetAgentSessionByID(ctx, *execution.AgentSessionID)
	if err != nil || session.Status != "running" || session.TaskID != execution.ExecutionTaskID {
		return ctx, "", fmt.Errorf("execution %s is not linked to a live agent session", address)
	}
	if IsNamedSlot(session.Slot) {
		ctx = WithAgentSlot(ctx, session.Slot)
	}
	return ctx, execution.ExecutionTaskID, nil
}

// formatPeerMessage frames another agent's words for delivery. The opening
// `[legato peer <sender>]` tag tells them from Legato's own `[legato ...]`
// messages and from what a person typed; the footer tells the recipient how
// to answer.
func formatPeerMessage(senderTaskID, message string) string {
	message = strings.TrimSpace(message)
	if senderTaskID == "" {
		return "[legato peer] " + message + "\n\n---\nAgent-to-agent message. Reply with `legato agent message <task-id> \"<reply>\"`."
	}
	return fmt.Sprintf("[legato peer %s] %s\n\n---\nAgent-to-agent message from `%s`. Reply with `legato agent message %s \"<reply>\"`.", senderTaskID, message, senderTaskID, senderTaskID)
}

// BroadcastToPeers sends a non-urgent message to every current worktree peer
// and preserves failures for callers without undoing successful deliveries.
func (a *agentService) BroadcastToPeers(ctx context.Context, taskID, executionID, directory, message string) (AgentBroadcastResult, error) {
	peers, err := a.ListPeers(ctx, taskID, executionID, directory)
	if err != nil {
		return AgentBroadcastResult{}, err
	}
	result := AgentBroadcastResult{Delivered: []string{}, Failed: []AgentDeliveryFailure{}}
	for _, peer := range peers {
		// A peer in a named slot is addressed by its task, which is the
		// main slot's agent; a read-only one is told nothing.
		if peer.Slot != "" {
			continue
		}
		if err := a.SendPeerMessage(ctx, taskID, peer.TaskID, message); err != nil {
			result.Failed = append(result.Failed, AgentDeliveryFailure{TaskID: peer.TaskID, Error: err.Error()})
			continue
		}
		result.Delivered = append(result.Delivered, peer.TaskID)
	}
	return result, nil
}

func gitWorktreeRoot(directory string) (string, error) {
	absolute, err := filepath.Abs(strings.TrimSpace(directory))
	if err != nil {
		return "", fmt.Errorf("resolve working directory: %w", err)
	}
	absolute, err = filepath.EvalSymlinks(filepath.Clean(absolute))
	if err != nil {
		return "", fmt.Errorf("resolve working directory %q: %w", directory, err)
	}
	cmd := exec.Command("git", "rev-parse", "--show-toplevel")
	cmd.Dir = absolute
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("resolve Git worktree for %q: %w: %s", absolute, err, strings.TrimSpace(string(output)))
	}
	root := strings.TrimSpace(string(output))
	if root == "" {
		return "", fmt.Errorf("resolve Git worktree for %q: Git returned an empty path", absolute)
	}
	root, err = filepath.EvalSymlinks(filepath.Clean(root))
	if err != nil {
		return "", fmt.Errorf("resolve Git worktree root %q: %w", root, err)
	}
	return root, nil
}

// SendMessage delivers a text message to the agent through the appropriate
// interaction adapter. For Chimera agents with a discovered endpoint, this
// uses the HTTP API; for all others, it uses tmux send-keys.
func (a *agentService) SendMessage(ctx context.Context, taskID, message string) error {
	if err := a.awaitMessageBinding(ctx, taskID); err != nil {
		return err
	}
	adapter := a.resolveInteractionAdapter(ctx, taskID)
	return adapter.SendMessage(ctx, taskID, message)
}

// InterruptAgent sends an interrupt to the agent's active turn.
// QueueMessage delegates step-boundary delivery to the active interaction adapter.
func (a *agentService) QueueMessage(ctx context.Context, taskID, message string) error {
	if err := a.awaitMessageBinding(ctx, taskID); err != nil {
		return err
	}
	return a.resolveInteractionAdapter(ctx, taskID).QueueMessage(ctx, taskID, message)
}

func (a *agentService) awaitMessageBinding(ctx context.Context, taskID string) error {
	session, err := slotSession(ctx, a.store, taskID)
	if err != nil {
		return err
	}
	if session.NativeOff || (session.ChimeraEndpoint != nil && session.ChimeraSessionID != nil) {
		return nil
	}
	adapter := a.resolveAdapter(session.AgentKind)
	binder, ok := adapter.(NativeBindingAdapter)
	if !ok || !binder.BindsNativeEndpoint() {
		return nil
	}
	if a.awaitNativeEndpoint(ctx, taskID, adapter) {
		return nil
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return fmt.Errorf("Agent %s has not bound its native endpoint; message was not typed into the starting terminal", taskID)
}

func (a *agentService) InterruptAgent(ctx context.Context, taskID string) error {
	adapter := a.resolveInteractionAdapter(ctx, taskID)
	return adapter.Interrupt(ctx, taskID)
}

// ResolvePermission resolves a pending permission request for the agent.
func (a *agentService) ResolvePermission(ctx context.Context, taskID, requestID string, action PermissionAction) error {
	adapter := a.resolveInteractionAdapter(ctx, taskID)
	return adapter.ResolvePermission(ctx, taskID, requestID, action)
}

// AnswerQuestion answers a question the agent put to the person.
func (a *agentService) AnswerQuestion(ctx context.Context, taskID, requestID string, answer QuestionAnswer) error {
	return a.resolveInteractionAdapter(ctx, taskID).AnswerQuestion(ctx, taskID, requestID, answer)
}

// HasChimeraEndpoint returns true if the agent has a discovered Chimera
// endpoint, meaning the web UI should render the structured panel.
func (a *agentService) HasChimeraEndpoint(ctx context.Context, taskID string) bool {
	session, err := slotSession(ctx, a.store, taskID)
	if err != nil {
		return false
	}
	return session.ChimeraEndpoint != nil && session.ChimeraSessionID != nil
}

// ChimeraBindingInvalidator conditionally clears a stale native binding.
type ChimeraBindingInvalidator interface {
	InvalidateChimeraBinding(ctx context.Context, agentSessionID int, expectedSessionID, expectedEndpoint string) (bool, error)
}

// NativeDowngrader switches an agent to its terminal on purpose: unlike an
// invalidated binding, a restart of Legato does not bind it again.
type NativeDowngrader interface {
	DowngradeToTmux(ctx context.Context, agentSessionID int, expectedSessionID, expectedEndpoint string) (bool, error)
}

func (a *agentService) DowngradeToTmux(ctx context.Context, agentSessionID int, expectedSessionID, expectedEndpoint string) (bool, error) {
	return a.store.KeepAgentSessionOnTmux(ctx, agentSessionID, expectedSessionID, expectedEndpoint)
}

// InvalidateChimeraBinding clears an unusable native binding only while the
// active agent row still matches the endpoint and session observed by its caller.
func (a *agentService) InvalidateChimeraBinding(ctx context.Context, agentSessionID int, expectedSessionID, expectedEndpoint string) (bool, error) {
	return a.store.ClearChimeraDiscoveryForSession(ctx, agentSessionID, expectedSessionID, expectedEndpoint)
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
	session, err := slotSession(ctx, a.store, taskID)
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
	return slotSession(ctx, a.store, taskID)
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

var (
	ErrChimeraDiscoveryNotFound  = errors.New("no verified Chimera instance found")
	ErrChimeraDiscoveryAmbiguous = errors.New("multiple verified Chimera instances found")
)

const chimeraDiscoveryRequestTimeout = 2 * time.Second

type operatorUpgradeKey struct{}

// WithOperatorUpgrade marks a discovery as the operator's own upgrade of an
// agent to its native panel. Only such a discovery may bind an agent the
// operator switched to its terminal (native_off), and it ends that switch;
// every other discovery leaves such an agent where the operator put it.
func WithOperatorUpgrade(ctx context.Context) context.Context {
	return context.WithValue(ctx, operatorUpgradeKey{}, true)
}

func isOperatorUpgrade(ctx context.Context) bool {
	upgrade, _ := ctx.Value(operatorUpgradeKey{}).(bool)
	return upgrade
}

// bindDiscovered binds an unbound row to a verified Chimera endpoint, with
// the operator's authority when the discovery is theirs.
func (a *agentService) bindDiscovered(ctx context.Context, agentSessionID int, sessionID, endpoint string) (bool, error) {
	if isOperatorUpgrade(ctx) {
		return a.store.UpgradeChimeraDiscoveryIfUnbound(ctx, agentSessionID, sessionID, endpoint)
	}
	return a.store.BindChimeraDiscoveryIfUnbound(ctx, agentSessionID, sessionID, endpoint)
}

// bindBridge binds a verified bridge to the exact row, with the operator's
// authority when the discovery is theirs.
func (a *agentService) bindBridge(ctx context.Context, agentSessionID int, sessionID, endpoint string) error {
	if isOperatorUpgrade(ctx) {
		return a.store.UpdateChimeraDiscoveryForSession(ctx, agentSessionID, sessionID, endpoint)
	}
	bound, err := a.store.BindNativeEndpointUnlessOff(ctx, agentSessionID, sessionID, endpoint)
	if err != nil {
		return err
	}
	if !bound {
		return store.ErrNotFound
	}
	return nil
}

// DiscoverChimera binds the active task agent to its native endpoint. For a
// Chimera agent it scans all current Chimera lockfiles and upgrades only when
// exactly one live endpoint serves the task-bound session; a Claude Code or
// Codex agent is bound to the bridge its session started (see
// discoverClaudeBridge and discoverCodexBridge).
func (a *agentService) DiscoverChimera(ctx context.Context, taskID string) error {
	session, err := slotSession(ctx, a.store, taskID)
	if err != nil {
		return err
	}
	if session.AgentKind == claudeCodeAgentKind {
		return a.discoverClaudeBridge(ctx, taskID, session.ID)
	}
	if session.AgentKind == codexAgentKind {
		return a.discoverCodexBridge(ctx, taskID, session.ID)
	}
	task, err := a.store.GetTask(ctx, session.TaskID)
	if err != nil {
		return err
	}
	expectedName := AgentSlotKey(session.TaskID, session.Slot)
	if task.Ephemeral {
		expectedName = ""
	}
	return a.discoverChimeraFromLockfiles(ctx, taskID, session.ID, expectedName, nil)
}

func (a *agentService) discoverChimeraFromLockfiles(ctx context.Context, taskID string, agentSessionID int, expectedSessionName string, excluded map[string]bool) error {
	secret, _, err := a.store.GetChimeraLaunchSecret(ctx, agentSessionID)
	if err != nil {
		return fmt.Errorf("reading Chimera launch authentication: %w", err)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("locating Chimera instances: %w", err)
	}
	entries, err := os.ReadDir(filepath.Join(home, ".chimera", "instances"))
	if err != nil {
		if os.IsNotExist(err) {
			return ErrChimeraDiscoveryNotFound
		}
		return fmt.Errorf("reading Chimera instances: %w", err)
	}

	type candidate struct {
		sessionID string
		endpoint  string
	}
	valid := make([]candidate, 0, 1)
	seenEndpoints := map[string]bool{}
	for _, entry := range entries {
		if !strings.HasSuffix(entry.Name(), ".json") || excluded[entry.Name()] {
			continue
		}
		data, err := os.ReadFile(filepath.Join(home, ".chimera", "instances", entry.Name()))
		if err != nil {
			continue
		}
		lf, err := chimera.ParseLockfile(data)
		if err != nil || seenEndpoints[lf.URL] || !isLoopbackHTTPURL(lf.URL) {
			continue
		}
		seenEndpoints[lf.URL] = true
		endpoint := lf.URL
		if secret != "" {
			endpoint = strings.TrimRight(lf.URL, "/") + "/" + url.PathEscape(secret)
		}
		if !nativeEndpointServes(ctx, endpoint, lf.SessionID, expectedSessionName) {
			// Older Chimera ignores the secret variable and still serves a bare URL.
			if secret == "" || !nativeEndpointServes(ctx, lf.URL, lf.SessionID, expectedSessionName) {
				continue
			}
			endpoint = lf.URL
		}
		valid = append(valid, candidate{sessionID: lf.SessionID, endpoint: endpoint})
	}

	if len(valid) == 0 {
		return ErrChimeraDiscoveryNotFound
	}
	if len(valid) > 1 {
		return ErrChimeraDiscoveryAmbiguous
	}
	if bound, err := a.bindDiscovered(ctx, agentSessionID, valid[0].sessionID, valid[0].endpoint); err != nil {
		return fmt.Errorf("persisting Chimera discovery: %w", err)
	} else if !bound {
		return store.ErrNotFound
	}
	if err := a.store.RemoveChimeraLaunchSecretFile(ctx, agentSessionID); err != nil {
		return fmt.Errorf("removing Chimera bootstrap secret: %w", err)
	}
	if a.bus != nil {
		a.bus.PublishAgentChimeraDiscovered(taskID)
	}
	return nil
}

// nativeEndpointServes reports whether endpoint is a live native-protocol
// instance serving a session under the expected name. The snapshot is fetched
// by the advertised session ID and verified by name, which Chimera preserves
// from --session-name while the ID may be random on collision.
func nativeEndpointServes(ctx context.Context, endpoint, sessionID, expectedSessionName string) bool {
	requestCtx, cancel := context.WithTimeout(ctx, chimeraDiscoveryRequestTimeout)
	defer cancel()
	httpClient := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	client := chimera.NewClientWithHTTPClient(endpoint, httpClient)
	if _, err := client.GetInstance(requestCtx); err != nil {
		return false
	}
	snapshot, err := client.GetSession(requestCtx, sessionID)
	return err == nil && (expectedSessionName == "" || snapshot.Name == expectedSessionName)
}

func isLoopbackHTTPURL(raw string) bool {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme != "http" || parsed.Host == "" || parsed.User != nil {
		return false
	}
	host := parsed.Hostname()
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func (a *agentService) discoverChimeraLockfile(ctx context.Context, taskID, expectedSessionName string, preExisting map[string]bool) {
	activeSession, err := slotSession(ctx, a.store, taskID)
	if err != nil {
		return
	}
	// Give the mod first refusal. Legacy adapters start discovery immediately.
	if binder, ok := a.resolveAdapter(activeSession.AgentKind).(NativeBindingAdapter); ok && binder.BindsNativeEndpoint() {
		deadline := time.Now().Add(a.chimeraModWait)
		for time.Now().Before(deadline) {
			current, err := slotSession(ctx, a.store, taskID)
			if err != nil || current.ID != activeSession.ID || current.Status != "running" || current.NativeOff || current.ChimeraEndpoint != nil || current.ChimeraSessionID != nil {
				return
			}
			select {
			case <-ctx.Done():
				return
			case <-time.After(nativeBindPoll):
			}
		}
	}
	// Poll for up to 30 seconds — Chimera may take a few seconds to boot.
	for i := 0; i < 60; i++ {
		// Stop once the agent is gone or replaced: its endpoint is no one's
		// to find now, and every poll asks each instance on the machine.
		if current, err := slotSession(ctx, a.store, taskID); err != nil || current.ID != activeSession.ID || current.Status != "running" || current.NativeOff || current.ChimeraEndpoint != nil || current.ChimeraSessionID != nil {
			return
		}
		if err := a.discoverChimeraFromLockfiles(ctx, taskID, activeSession.ID, expectedSessionName, preExisting); err == nil {
			log.Printf("chimera lockfile discovered for %s", taskID)
			return
		} else if !errors.Is(err, ErrChimeraDiscoveryNotFound) {
			log.Printf("chimera lockfile discovery for %s: %v", taskID, err)
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(500 * time.Millisecond):
		}
	}
	log.Printf("chimera lockfile discovery for %s: timed out after 30s", taskID)
}
