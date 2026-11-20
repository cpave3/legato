package events

import (
	"sync"
	"time"
)

type EventType int

const (
	EventCardMoved EventType = iota
	EventCardUpdated
	EventCardsRefreshed
	EventSyncStarted
	EventSyncCompleted
	EventSyncFailed
	EventAgentStarted
	EventAgentOutput
	EventAgentCompleted
	EventAgentActivityChanged
	EventSyncError
	EventTransitionFailed
	EventAuthFailed
	EventRateLimited
	EventPRStatusUpdated
	EventAgentDied
	EventAgentChimeraDiscovered
	EventSwarmChanged
	EventPlanProposed
	EventReviewChanged
	EventPlanChanged
	EventWorkflowChanged
	EventWorkflowTriggerChanged
	EventAttentionChanged
	EventLearningChanged
	EventAgentUIChanged
	EventApprovalChanged
	// EventDelegationChanged: an integration's delegation to a task was
	// made, delivered, failed or answered. Payload: the task ID.
	EventDelegationChanged
	// EventUpdateChanged: where this Legato's updates stand changed (a
	// check, a new mode, an install). No payload.
	EventUpdateChanged
	// EventHandoffChanged: a task handed to this worker by a main instance
	// was accepted or changed state (pending, in progress, done, failed).
	// Payload: the worker's task ID.
	EventHandoffChanged
	// EventSettingsChanged: a setting kept in the database changed. No
	// payload.
	EventSettingsChanged
	// EventReeveSpoke: a line the Reeve said was made, sent to the
	// listening device, playing or played, failed or dropped. Payload:
	// ReeveSpokePayload.
	EventReeveSpoke
	// EventMQTTInvalidated asks the publisher to reread shared state after
	// another host process changed it. It carries no source content.
	EventMQTTInvalidated
	// EventServerThemeChanged follows a committed server preference change.
	EventServerThemeChanged
	// EventServerThemeInvalidated is a content-free cross-process refresh,
	// kept distinct so IPC never echoes the same change indefinitely.
	EventServerThemeInvalidated
	// EventScopeChanged: an origin's Scope pins or subscription changed (a
	// batch sent, a pin delivered, resolved, reopened or withdrawn).
	// Payload: ScopeChangedPayload.
	EventScopeChanged
	// EventPRChangesRequested: a new review requesting changes on a pull
	// request a task owns, by someone other than Legato's own Apps;
	// payload PRChangesRequestedPayload.
	EventPRChangesRequested
	// EventOperatorItemsChanged: an agent posted, updated or cleared an
	// operator item (for_operator). Payload: OperatorItemsChangedPayload.
	EventOperatorItemsChanged
)

// OperatorItemsChangedPayload names the task whose agent's operator items
// changed.
type OperatorItemsChangedPayload struct {
	TaskID string
	ItemID string
}

// ScopeChangedPayload names the origin whose Scope pins changed.
type ScopeChangedPayload struct {
	Origin string
}

// PRChangesRequestedPayload is a new changes-requested review on a task's
// pull request.
type PRChangesRequestedPayload struct {
	TaskID   string
	Repo     string
	Number   int
	URL      string
	ReviewID int64
	Reviewer string
}

// ReeveSpokePayload is what became of one line the Reeve said.
type ReeveSpokePayload struct {
	TaskID string
	Text   string
	// State is made, sent, playing, played, failed or dropped.
	State string
	// Line numbers the line, the same in every event about it.
	Line int64
}

// ErrorPayload carries structured error information for error events.
type ErrorPayload struct {
	ErrorType string // "offline", "auth_failed", "transition_failed", "rate_limited"
	Message   string
	TicketKey string // affected ticket, empty for general errors
}

// AgentDiedPayload is published when an agent's tmux session is detected as dead.
type AgentDiedPayload struct {
	TaskID       string
	ParentTaskID string // empty for non-swarm agents
	SubtaskID    string // empty for non-swarm agents
	Role         string
	// Slot is the agent slot that ended: empty for the task's main slot,
	// which is the task's agent; a named slot (a reviewer) otherwise.
	Slot string
}

type AgentActivityChangedPayload struct {
	TaskID   string
	Activity string
}

// AgentChimeraDiscoveredPayload is published when a Chimera lockfile is
// discovered for an agent session, meaning the web UI can now render the
// structured NativeAgentPanel instead of the xterm terminal.
type AgentChimeraDiscoveredPayload struct {
	TaskID string
}

// SwarmChangedPayload is published when a swarm sub-task changes state.
type SwarmChangedPayload struct {
	ParentTaskID string
	SubtaskID    string
	NewStatus    string
}

// PlanProposedPayload is published when a conductor submits a plan for approval.
// Carries enough context for the TUI to open the plan-approval overlay and
// reply to the conductor's CLI when the user verdicts.
type PlanProposedPayload struct {
	ParentTaskID string
	PlanPath     string
	ReplySocket  string
	Mode         string // "extension" for append-to-existing-swarm plans, empty for normal proposals
}

// ReviewChangedPayload is published when a review tour mutates: steps synced,
// annotation added, transcript entry appended, or status changed.
type ReviewChangedPayload struct {
	TourID string
	TaskID string
	StepID string // empty when the change is tour-wide
	Kind   string // "synced", "annotated", "question", "answer", "ready", "reviewed"
}

// PlanChangedPayload identifies a durable plan mutation for live UI refreshes.
type PlanChangedPayload struct {
	PlanID     string
	TaskID     string
	RevisionID string
	Kind       string
}

type LearningChangedPayload struct {
	LearningID string
	Kind       string
}

type AttentionChangedPayload struct {
	ItemID    string
	Producer  string
	SourceKey string
	Kind      string
}

// AgentUIChangedPayload is published when an agent shows, replaces or loses
// an item in Legato's UI: an artifact in the side panel or a card in the
// conversation.
type AgentUIChangedPayload struct {
	TaskID    string
	ItemID    string
	Placement string
	Revision  int
	Kind      string // "shown", "deleted"
}

// ApprovalChangedPayload is published when an approval request is created or
// closed: decided, dismissed, withdrawn, expired or failed.
type ApprovalChangedPayload struct {
	ApprovalID string
	TaskID     string
	Source     string
	Kind       string // "created", or the approval's new status
}

type WorkflowChangedPayload struct {
	RunID  string
	StepID string
	Kind   string
}

type Event struct {
	Type    EventType
	Payload interface{}
	At      time.Time
}

type Bus struct {
	mu            sync.RWMutex
	subs          map[EventType][]chan Event
	invalidations map[chan struct{}]map[EventType]bool
}

func New() *Bus {
	return &Bus{
		subs:          make(map[EventType][]chan Event),
		invalidations: make(map[chan struct{}]map[EventType]bool),
	}
}

// SubscribeInvalidations coalesces changes into a pending snapshot request.
// Consume before reading authoritative state: a mutation during the read then
// leaves another request pending, even if ordinary event buffers are saturated.
func (b *Bus) SubscribeInvalidations(types ...EventType) (<-chan struct{}, func()) {
	b.mu.Lock()
	ch := make(chan struct{}, 1)
	selected := make(map[EventType]bool, len(types))
	for _, typ := range types {
		selected[typ] = true
	}
	b.invalidations[ch] = selected
	b.mu.Unlock()
	var once sync.Once
	return ch, func() {
		once.Do(func() {
			b.mu.Lock()
			defer b.mu.Unlock()
			delete(b.invalidations, ch)
			close(ch)
		})
	}
}

func (b *Bus) Subscribe(eventType EventType) <-chan Event {
	b.mu.Lock()
	defer b.mu.Unlock()
	ch := make(chan Event, 64)
	b.subs[eventType] = append(b.subs[eventType], ch)
	return ch
}

func (b *Bus) Publish(event Event) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	for ch, types := range b.invalidations {
		if types[event.Type] {
			select {
			case ch <- struct{}{}:
			default:
			}
		}
	}
	for _, ch := range b.subs[event.Type] {
		select {
		case ch <- event:
		default:
		}
	}
}

func (b *Bus) Unsubscribe(ch <-chan Event) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for eventType, subs := range b.subs {
		for i, sub := range subs {
			if sub == ch {
				close(sub)
				b.subs[eventType] = append(subs[:i], subs[i+1:]...)
				return
			}
		}
	}
}
