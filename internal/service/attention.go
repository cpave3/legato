package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/cpave3/legato/internal/engine/events"
	"github.com/cpave3/legato/internal/engine/store"
)

type AttentionNavigation struct {
	Kind   string `json:"kind"`
	Target string `json:"target"`
}

type AttentionAction struct {
	ID              string `json:"id"`
	Label           string `json:"label"`
	Kind            string `json:"kind"`
	Intent          string `json:"intent,omitempty"`
	RequiresText    bool   `json:"requires_text,omitempty"`
	RequiresConfirm bool   `json:"requires_confirmation,omitempty"`
}

type AttentionInput struct {
	Producer       string
	SourceKey      string
	SourceRevision string
	TaskID         string
	WorkspaceID    *int
	Title          string
	Reason         string
	Detail         string
	Category       string
	Urgency        string
	Navigation     AttentionNavigation
	Actions        []AttentionAction
}

type AttentionItem struct {
	ID                string              `json:"id"`
	Producer          string              `json:"producer"`
	SourceKey         string              `json:"source_key"`
	SourceRevision    string              `json:"source_revision"`
	TaskID            string              `json:"task_id,omitempty"`
	WorkspaceID       *int                `json:"workspace_id,omitempty"`
	Title             string              `json:"title"`
	Reason            string              `json:"reason"`
	Detail            string              `json:"detail,omitempty"`
	Category          string              `json:"category"`
	Urgency           string              `json:"urgency"`
	Navigation        AttentionNavigation `json:"navigation"`
	Actions           []AttentionAction   `json:"actions"`
	Status            string              `json:"status"`
	DismissedRevision string              `json:"dismissed_revision,omitempty"`
	CreatedAt         string              `json:"created_at"`
	UpdatedAt         string              `json:"updated_at"`
}

type AttentionFilter struct {
	Producer    string
	Category    string
	Urgency     string
	TaskID      string
	WorkspaceID *int
}

type AttentionActionRequest struct {
	SourceKey        string `json:"-"`
	ActionID         string `json:"action_id,omitempty"`
	ExpectedRevision string `json:"expected_revision"`
	Text             string `json:"text,omitempty"`
	Confirmed        bool   `json:"confirmed,omitempty"`
}

type AttentionActionHandler interface {
	Execute(context.Context, AttentionActionRequest) error
}

type AttentionProducer interface {
	Producer() string
	Snapshot(context.Context) ([]AttentionInput, error)
}

var ErrAttentionConflict = errors.New("attention item changed; refresh and try again")

type AttentionService struct {
	store           *store.Store
	bus             *events.Bus
	handlerMu       sync.RWMutex
	handlers        map[string]AttentionActionHandler
	producerMu      sync.RWMutex
	producers       map[string]AttentionProducer
	eventToProducer map[events.EventType]string
}

func NewAttentionService(s *store.Store, bus *events.Bus) *AttentionService {
	return &AttentionService{
		store: s, bus: bus,
		handlers:        map[string]AttentionActionHandler{},
		producers:       map[string]AttentionProducer{},
		eventToProducer: map[events.EventType]string{},
	}
}

// StartEventReconciliation subscribes to source events and triggers
// re-reconciliation of the corresponding producer. Call once after all
// producers are registered.
func (s *AttentionService) StartEventReconciliation(ctx context.Context) func() {
	if s.bus == nil {
		return func() {}
	}
	var channels []<-chan events.Event
	for eventType, producerName := range s.eventToProducer {
		ch := s.bus.Subscribe(eventType)
		producerName := producerName
		channels = append(channels, ch)
		go func() {
			for range ch {
				_ = s.Reconcile(ctx, producerName)
			}
		}()
	}
	return func() {
		for _, ch := range channels {
			s.bus.Unsubscribe(ch)
		}
	}
}

func (s *AttentionService) RegisterProducer(producer AttentionProducer) error {
	if producer == nil || strings.TrimSpace(producer.Producer()) == "" {
		return fmt.Errorf("attention producer requires a name")
	}
	s.producerMu.Lock()
	defer s.producerMu.Unlock()
	name := producer.Producer()
	if _, exists := s.producers[name]; exists {
		return fmt.Errorf("attention producer %q already registered", name)
	}
	s.producers[name] = producer
	return nil
}

// RegisterProducerWithEvents registers a producer and maps source events
// to trigger reconciliation for that producer.
func (s *AttentionService) RegisterProducerWithEvents(producer AttentionProducer, eventTypes ...events.EventType) error {
	if err := s.RegisterProducer(producer); err != nil {
		return err
	}
	for _, eventType := range eventTypes {
		s.eventToProducer[eventType] = producer.Producer()
	}
	return nil
}

func (s *AttentionService) RegisterHandler(producer string, handler AttentionActionHandler) error {
	if strings.TrimSpace(producer) == "" || handler == nil {
		return fmt.Errorf("attention handler requires producer and handler")
	}
	s.handlerMu.Lock()
	defer s.handlerMu.Unlock()
	if _, exists := s.handlers[producer]; exists {
		return fmt.Errorf("attention handler already registered for %q", producer)
	}
	s.handlers[producer] = handler
	return nil
}

func (s *AttentionService) Reconcile(ctx context.Context, producerName string) error {
	s.producerMu.RLock()
	producer := s.producers[producerName]
	s.producerMu.RUnlock()
	if producer == nil {
		return fmt.Errorf("attention producer %q is not registered", producerName)
	}
	inputs, err := producer.Snapshot(ctx)
	if err != nil {
		return fmt.Errorf("reconciling attention producer %q: %w", producerName, err)
	}
	keys := make([]string, 0, len(inputs))
	for _, input := range inputs {
		if input.Producer != "" && input.Producer != producerName {
			return fmt.Errorf("attention producer %q returned item for %q", producerName, input.Producer)
		}
		input.Producer = producerName
		if _, err := s.Upsert(ctx, input); err != nil {
			return fmt.Errorf("reconciling attention producer %q: %w", producerName, err)
		}
		keys = append(keys, input.SourceKey)
	}
	if err := s.store.ResolveAttentionItemsMissing(ctx, producerName, keys); err != nil {
		return fmt.Errorf("resolving stale attention for %q: %w", producerName, err)
	}
	return nil
}

func (s *AttentionService) ReconcileAll(ctx context.Context) error {
	s.producerMu.RLock()
	names := make([]string, 0, len(s.producers))
	for name := range s.producers {
		names = append(names, name)
	}
	s.producerMu.RUnlock()
	for _, name := range names {
		if err := s.Reconcile(ctx, name); err != nil {
			return err
		}
	}
	return nil
}

func (s *AttentionService) Upsert(ctx context.Context, input AttentionInput) (*AttentionItem, error) {
	if err := validateAttentionInput(input); err != nil {
		return nil, err
	}
	navigation, _ := json.Marshal(input.Navigation)
	actions, _ := json.Marshal(input.Actions)
	stored, err := s.store.UpsertAttentionItem(ctx, store.AttentionItem{
		ID: randomPlanID("att-"), Producer: input.Producer, SourceKey: input.SourceKey,
		SourceRevision: input.SourceRevision, TaskID: input.TaskID, WorkspaceID: input.WorkspaceID,
		Title: input.Title, Reason: input.Reason, Detail: input.Detail, Category: input.Category,
		Urgency: input.Urgency, NavigationJSON: string(navigation), ActionsJSON: string(actions),
	})
	if err != nil {
		return nil, err
	}
	s.publish(stored, "upserted")
	return decodeAttentionItem(*stored)
}

func (s *AttentionService) List(ctx context.Context, filter AttentionFilter) ([]AttentionItem, error) {
	stored, err := s.store.ListAttentionItems(ctx, "open")
	if err != nil {
		return nil, err
	}
	items := make([]AttentionItem, 0, len(stored))
	for _, candidate := range stored {
		if filter.Producer != "" && candidate.Producer != filter.Producer ||
			filter.Category != "" && candidate.Category != filter.Category ||
			filter.Urgency != "" && candidate.Urgency != filter.Urgency ||
			filter.TaskID != "" && candidate.TaskID != filter.TaskID ||
			filter.WorkspaceID != nil && (candidate.WorkspaceID == nil || *candidate.WorkspaceID != *filter.WorkspaceID) {
			continue
		}
		item, decodeErr := decodeAttentionItem(candidate)
		if decodeErr != nil {
			return nil, decodeErr
		}
		items = append(items, *item)
	}
	return items, nil
}

func (s *AttentionService) Execute(ctx context.Context, id string, request AttentionActionRequest) error {
	stored, err := s.store.GetAttentionItem(ctx, id)
	if err != nil {
		return err
	}
	if stored.Status != "open" || stored.SourceRevision != request.ExpectedRevision {
		return ErrAttentionConflict
	}
	item, err := decodeAttentionItem(*stored)
	if err != nil {
		return err
	}
	var declared *AttentionAction
	for i := range item.Actions {
		if item.Actions[i].ID == request.ActionID {
			declared = &item.Actions[i]
			break
		}
	}
	if declared == nil || declared.Kind != "execute" {
		return fmt.Errorf("attention action %q is not executable", request.ActionID)
	}
	if declared.RequiresText && strings.TrimSpace(request.Text) == "" {
		return fmt.Errorf("attention action %q requires text", request.ActionID)
	}
	if declared.RequiresConfirm && !request.Confirmed {
		return fmt.Errorf("attention action %q requires confirmation", request.ActionID)
	}
	s.handlerMu.RLock()
	handler := s.handlers[item.Producer]
	s.handlerMu.RUnlock()
	if handler == nil {
		return fmt.Errorf("attention producer %q has no action handler", item.Producer)
	}
	request.SourceKey = item.SourceKey
	if err := handler.Execute(ctx, request); err != nil {
		return err
	}
	s.producerMu.RLock()
	_, hasProducer := s.producers[item.Producer]
	s.producerMu.RUnlock()
	if hasProducer {
		if err := s.Reconcile(ctx, item.Producer); err != nil {
			return err
		}
	}
	current, err := s.store.GetAttentionItem(ctx, id)
	if err == nil {
		s.publish(current, "acted")
	}
	return err
}

func (s *AttentionService) Dismiss(ctx context.Context, id, sourceRevision string) error {
	if err := s.store.DismissAttentionItem(ctx, id, sourceRevision); err != nil {
		if errors.Is(err, store.ErrConflict) {
			return ErrAttentionConflict
		}
		return err
	}
	item, err := s.store.GetAttentionItem(ctx, id)
	if err == nil {
		s.publish(item, "dismissed")
	}
	return err
}

func (s *AttentionService) Resolve(ctx context.Context, producer, sourceKey string) error {
	if err := s.store.ResolveAttentionItem(ctx, producer, sourceKey); err != nil {
		return err
	}
	item, err := s.store.GetAttentionItemBySource(ctx, producer, sourceKey)
	if err == nil {
		s.publish(item, "resolved")
	}
	return err
}

func validateAttentionInput(input AttentionInput) error {
	for name, value := range map[string]string{
		"producer": input.Producer, "source key": input.SourceKey, "source revision": input.SourceRevision,
		"title": input.Title, "reason": input.Reason, "category": input.Category,
		"urgency": input.Urgency, "navigation kind": input.Navigation.Kind, "navigation target": input.Navigation.Target,
	} {
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("attention %s is required", name)
		}
	}
	seen := map[string]bool{}
	for _, action := range input.Actions {
		if action.ID == "" || action.Label == "" || action.Kind == "" {
			return fmt.Errorf("attention actions require id, label, and kind")
		}
		if seen[action.ID] {
			return fmt.Errorf("duplicate attention action %q", action.ID)
		}
		seen[action.ID] = true
	}
	return nil
}

func decodeAttentionItem(item store.AttentionItem) (*AttentionItem, error) {
	out := &AttentionItem{
		ID: item.ID, Producer: item.Producer, SourceKey: item.SourceKey, SourceRevision: item.SourceRevision,
		TaskID: item.TaskID, WorkspaceID: item.WorkspaceID, Title: item.Title, Reason: item.Reason,
		Detail: item.Detail, Category: item.Category, Urgency: item.Urgency, Status: item.Status,
		DismissedRevision: item.DismissedRevision, CreatedAt: item.CreatedAt, UpdatedAt: item.UpdatedAt,
	}
	if err := json.Unmarshal([]byte(item.NavigationJSON), &out.Navigation); err != nil {
		return nil, fmt.Errorf("decoding attention navigation: %w", err)
	}
	if err := json.Unmarshal([]byte(item.ActionsJSON), &out.Actions); err != nil {
		return nil, fmt.Errorf("decoding attention actions: %w", err)
	}
	return out, nil
}

func (s *AttentionService) publish(item *store.AttentionItem, kind string) {
	if s.bus == nil {
		return
	}
	s.bus.Publish(events.Event{Type: events.EventAttentionChanged, Payload: events.AttentionChangedPayload{
		ItemID: item.ID, Producer: item.Producer, SourceKey: item.SourceKey, Kind: kind,
	}})
}
