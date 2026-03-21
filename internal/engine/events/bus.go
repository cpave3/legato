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
)

// ErrorPayload carries structured error information for error events.
type ErrorPayload struct {
	ErrorType string // "offline", "auth_failed", "transition_failed", "rate_limited"
	Message   string
	TicketKey string // affected ticket, empty for general errors
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
