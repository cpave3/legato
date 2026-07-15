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
	EventSyncError
	EventTransitionFailed
	EventAuthFailed
	EventRateLimited
	EventPRStatusUpdated
	EventAgentDied
	EventSwarmChanged
	EventPlanProposed
	EventReviewChanged
)

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

type Event struct {
	Type    EventType
	Payload interface{}
	At      time.Time
}

type Bus struct {
	mu   sync.RWMutex
	subs map[EventType][]chan Event
}

func New() *Bus {
	return &Bus{
		subs: make(map[EventType][]chan Event),
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
