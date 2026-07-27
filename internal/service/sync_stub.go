package service

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	"github.com/cpave3/legato/internal/engine/events"
	"github.com/cpave3/legato/internal/engine/store"
)

type stubSyncService struct {
	store  *store.Store
	bus    *events.Bus
	mu     sync.Mutex
	synced bool
	last   time.Time
	subs   []chan SyncEvent
}

// NewStubSyncService creates a SyncService that seeds fake task data.
func NewStubSyncService(s *store.Store, bus *events.Bus) SyncService {
	return &stubSyncService{store: s, bus: bus}
}

func (s *stubSyncService) Sync(ctx context.Context) (*SyncResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.broadcast(SyncEvent{Type: EventSyncStarted, Message: "sync started"})
	s.bus.Publish(events.Event{Type: events.EventSyncStarted, At: time.Now()})

	if s.synced {
		s.broadcast(SyncEvent{Type: EventSyncCompleted, Message: "sync completed (no-op)"})
		s.bus.Publish(events.Event{Type: events.EventSyncCompleted, At: time.Now()})
		return &SyncResult{TasksSynced: 0}, nil
	}

	// Seed column mappings
	columns := []store.ColumnMapping{
		{ColumnName: "Backlog", RemoteStatuses: `["To Do","Open"]`, SortOrder: 0},
		{ColumnName: "In Progress", RemoteStatuses: `["In Progress"]`, SortOrder: 1},
		{ColumnName: "In Review", RemoteStatuses: `["In Review"]`, SortOrder: 2},
		{ColumnName: "Done", RemoteStatuses: `["Done","Closed"]`, SortOrder: 3},
	}
	for _, col := range columns {
		if err := s.store.CreateColumnMapping(ctx, col); err != nil {
			s.broadcast(SyncEvent{Type: EventSyncFailed, Message: err.Error()})
			s.bus.Publish(events.Event{Type: events.EventSyncFailed, Payload: err.Error(), At: time.Now()})
			return nil, err
		}
	}

	now := time.Now().UTC().Format(time.RFC3339)
	tasks := fakeTasks(now)

	for _, t := range tasks {
		if err := s.store.CreateTask(ctx, t); err != nil {
			s.broadcast(SyncEvent{Type: EventSyncFailed, Message: err.Error()})
			s.bus.Publish(events.Event{Type: events.EventSyncFailed, Payload: err.Error(), At: time.Now()})
			return nil, err
		}
	}

	s.synced = true
	s.last = time.Now()

	s.broadcast(SyncEvent{Type: EventCardsRefreshed, Message: "cards refreshed"})
	s.bus.Publish(events.Event{Type: events.EventCardsRefreshed, At: time.Now()})

	s.broadcast(SyncEvent{Type: EventSyncCompleted, Message: "sync completed"})
	s.bus.Publish(events.Event{Type: events.EventSyncCompleted, At: time.Now()})

	return &SyncResult{TasksSynced: len(tasks)}, nil
}

func (s *stubSyncService) Status() SyncStatus {
	s.mu.Lock()
	defer s.mu.Unlock()
	return SyncStatus{
		InProgress: false,
		LastSync:   s.last,
	}
}

func (s *stubSyncService) Subscribe() <-chan SyncEvent {
	s.mu.Lock()
	defer s.mu.Unlock()
	ch := make(chan SyncEvent, 64)
	s.subs = append(s.subs, ch)
	return ch
}

func (s *stubSyncService) StartScheduler(ctx context.Context) func() {
	return func() {}
}

func (s *stubSyncService) SearchRemote(ctx context.Context, query string) ([]RemoteSearchResult, error) {
	return nil, nil
}

func (s *stubSyncService) ImportRemoteTask(ctx context.Context, ticketID string, workspaceID *int) (*Card, error) {
	return nil, nil
}

func (s *stubSyncService) broadcast(e SyncEvent) {
	for _, ch := range s.subs {
		select {
		case ch <- e:
		default:
		}
	}
}

func makeRemoteMeta(remoteStatus, issueType, assignee, labels, epicKey, epicName, url string) *string {
	m := map[string]string{
		"remote_status":    remoteStatus,
		"remote_updated_at": time.Now().UTC().Format(time.RFC3339),
		"issue_type":       issueType,
		"assignee":         assignee,
		"labels":           labels,
		"epic_key":         epicKey,
		"epic_name":        epicName,
		"url":              url,
	}
	b, _ := json.Marshal(m)
	s := string(b)
	return &s
}

func strPtr(s string) *string { return &s }

func fakeTasks(now string) []store.Task {
	return []store.Task{
		{
			ID: "LEG-1", Title: "Set up project scaffolding and CI pipeline",
			Description: "Initialize the Go module, set up linting, and configure GitHub Actions.",
			DescriptionMD: "Initialize the Go module, set up linting, and configure GitHub Actions.",
			Status: "Done", Priority: "High", SortOrder: 0,
			Provider: strPtr("jira"), RemoteID: strPtr("LEG-1"),
			RemoteMeta: makeRemoteMeta("Done", "Task", "alice", "infra", "LEG-100", "Foundation", "https://jira.example.com/browse/LEG-1"),
			CreatedAt: now, UpdatedAt: now,
		},
		{
			ID: "LEG-2", Title: "Implement SQLite storage layer with migrations",
			Description: "Build the store package with embedded SQL migrations and CRUD operations for tasks, column mappings, and sync log.",
			DescriptionMD: "Build the store package with embedded SQL migrations and CRUD operations for tasks, column mappings, and sync log.\n\n## Acceptance Criteria\n\n- [ ] Store opens SQLite with WAL mode\n- [ ] Migrations run automatically\n- [ ] All CRUD operations tested",
			Status: "Done", Priority: "High", SortOrder: 1,
			Provider: strPtr("jira"), RemoteID: strPtr("LEG-2"),
			RemoteMeta: makeRemoteMeta("Done", "Story", "bob", "backend,storage", "LEG-100", "Foundation", "https://jira.example.com/browse/LEG-2"),
			CreatedAt: now, UpdatedAt: now,
		},
		{
			ID: "LEG-3", Title: "Design service layer interfaces for board and sync operations",
			Description: "Define Go interfaces for BoardService and SyncService that the TUI and future web UI will consume.",
			DescriptionMD: "Define Go interfaces for BoardService and SyncService that the TUI and future web UI will consume.\n\n## Requirements\n\n- All methods accept `context.Context`\n- No presentation-layer imports\n- Support for event-driven updates",
			Status: "In Progress", Priority: "High", SortOrder: 0,
			Provider: strPtr("jira"), RemoteID: strPtr("LEG-3"),
			RemoteMeta: makeRemoteMeta("In Progress", "Story", "alice", "backend,architecture", "LEG-101", "Service Layer", "https://jira.example.com/browse/LEG-3"),
			CreatedAt: now, UpdatedAt: now,
		},
		{
			ID: "LEG-4", Title: "Implement card move and reorder logic with event publishing",
			Description: "Implement MoveCard and ReorderCard on the board service, including sort_order management and event bus integration.",
			DescriptionMD: "Implement MoveCard and ReorderCard on the board service, including sort_order management and event bus integration.\n\n## Details\n\n- MoveCard updates status and places card at end of target column\n- ReorderCard adjusts sort_order for all cards in the column\n- Both operations publish events through the EventBus",
			Status: "In Progress", Priority: "Medium", SortOrder: 1,
			Provider: strPtr("jira"), RemoteID: strPtr("LEG-4"),
			RemoteMeta: makeRemoteMeta("In Progress", "Task", "bob", "backend", "LEG-101", "Service Layer", "https://jira.example.com/browse/LEG-4"),
			CreatedAt: now, UpdatedAt: now,
		},
		{
			ID: "LEG-5", Title: "Build TUI kanban board with column navigation and card rendering using Bubbletea and Lipgloss",
			Status: "Backlog", Priority: "High", SortOrder: 0,
			Provider: strPtr("jira"), RemoteID: strPtr("LEG-5"),
			RemoteMeta: makeRemoteMeta("To Do", "Epic", "", "frontend,tui", "LEG-102", "TUI Shell", "https://jira.example.com/browse/LEG-5"),
			CreatedAt: now, UpdatedAt: now,
		},
		{
			ID: "LEG-6", Title: "Add Jira REST API client with OAuth and pagination support",
			Description: "Integrate with Jira Cloud REST API v3 for ticket synchronization.",
			DescriptionMD: "Integrate with Jira Cloud REST API v3 for ticket synchronization.\n\n## Scope\n\n- OAuth 2.0 authentication flow\n- Paginated search with JQL\n- Rate limiting and retry logic\n\n## Out of Scope\n\n- Jira Server/Data Center support (future)",
			Status: "Backlog", Priority: "Medium", SortOrder: 1,
			Provider: strPtr("jira"), RemoteID: strPtr("LEG-6"),
			RemoteMeta: makeRemoteMeta("Open", "Story", "", "backend,integration", "LEG-103", "Jira Integration", "https://jira.example.com/browse/LEG-6"),
			CreatedAt: now, UpdatedAt: now,
		},
		{
			ID: "LEG-7", Title: "Implement context export for clipboard with description-only and full structured block formats",
			Description: "Add context export formatting that produces clean markdown for clipboard copy and AI agent consumption.",
			DescriptionMD: "Add context export formatting that produces clean markdown for clipboard copy and AI agent consumption.\n\n## Formats\n\n### Description Only\n- Heading: `## KEY: Summary`\n- Body: markdown description\n\n### Full Block\n- Heading: `# Ticket: KEY`\n- Metadata fields (bold labels)\n- Separator: `---`\n- Full description\n\n## Constraints\n\n- No ANSI escape codes\n- No terminal formatting\n- Pure markdown output",
			Status: "In Review", Priority: "Low", SortOrder: 0,
			Provider: strPtr("jira"), RemoteID: strPtr("LEG-7"),
			RemoteMeta: makeRemoteMeta("In Review", "Task", "alice", "backend", "LEG-101", "Service Layer", "https://jira.example.com/browse/LEG-7"),
			CreatedAt: now, UpdatedAt: now,
		},
		{
			ID: "LEG-8", Title: "Fix sort_order gap after bulk delete causes cards to cluster at top of column",
			Description: "When multiple cards are deleted, the remaining cards keep their original sort_order values, leaving gaps.",
			DescriptionMD: "When multiple cards are deleted, the remaining cards keep their original sort_order values, leaving gaps. This causes new cards inserted at `sort_order=0` to cluster at the top.\n\n## Steps to Reproduce\n\n1. Create 5 cards in a column\n2. Delete cards at positions 1, 3\n3. Add a new card\n4. Observe new card appears at top instead of bottom\n\n## Expected\n\nNew cards should always appear at the bottom of the column.\n\n## Actual\n\nNew cards appear at the top because `sort_order` defaults to 0.",
			Status: "Backlog", Priority: "Medium", SortOrder: 2,
			Provider: strPtr("jira"), RemoteID: strPtr("LEG-8"),
			RemoteMeta: makeRemoteMeta("To Do", "Bug", "", "backend,bug", "LEG-101", "Service Layer", "https://jira.example.com/browse/LEG-8"),
			CreatedAt: now, UpdatedAt: now,
		},
		{
			ID: "LEG-9", Title: "Add keyboard shortcut overlay showing all available keybindings grouped by context",
			Description: "Display a modal overlay triggered by ? that shows all keyboard shortcuts organized by context.",
			DescriptionMD: "Display a modal overlay triggered by `?` that shows all keyboard shortcuts organized by context (navigation, actions, search).\n\n## Layout\n\n| Key | Action |\n|-----|--------|\n| `j/k` | Move up/down |\n| `h/l` | Move left/right |\n| `Enter` | Open detail |\n| `/` | Search |\n| `?` | Toggle this overlay |",
			Status: "Backlog", Priority: "Low", SortOrder: 3,
			Provider: strPtr("jira"), RemoteID: strPtr("LEG-9"),
			RemoteMeta: makeRemoteMeta("To Do", "Story", "", "frontend,ux", "LEG-104", "Polish", "https://jira.example.com/browse/LEG-9"),
			CreatedAt: now, UpdatedAt: now,
		},
		{
			ID: "LEG-10", Title: "Evaluate whether server-sent events or WebSocket is the right transport for real-time board updates",
			Description: "Research and document the trade-offs between SSE and WebSocket for pushing real-time board state changes to a browser client.",
			DescriptionMD: "Research and document the trade-offs between SSE and WebSocket for pushing real-time board state changes to a browser client. This is a spike — no implementation expected.\n\n## Considerations\n\n- Unidirectional vs bidirectional needs\n- Browser compatibility\n- Load balancer behavior\n- Connection lifecycle management",
			Status: "Backlog", Priority: "Low", SortOrder: 4,
			Provider: strPtr("jira"), RemoteID: strPtr("LEG-10"),
			RemoteMeta: makeRemoteMeta("Open", "Spike", "", "research,architecture", "LEG-105", "Web UI Planning", "https://jira.example.com/browse/LEG-10"),
			CreatedAt: now, UpdatedAt: now,
		},
	}
}
