package service

import (
	"context"
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

// NewStubSyncService creates a SyncService that seeds fake ticket data.
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
		return &SyncResult{TicketsSynced: 0}, nil
	}

	// Seed column mappings
	columns := []store.ColumnMapping{
		{ColumnName: "Backlog", JiraStatuses: `["To Do","Open"]`, SortOrder: 0},
		{ColumnName: "In Progress", JiraStatuses: `["In Progress"]`, SortOrder: 1},
		{ColumnName: "In Review", JiraStatuses: `["In Review"]`, SortOrder: 2},
		{ColumnName: "Done", JiraStatuses: `["Done","Closed"]`, SortOrder: 3},
	}
	for _, col := range columns {
		if err := s.store.CreateColumnMapping(ctx, col); err != nil {
			s.broadcast(SyncEvent{Type: EventSyncFailed, Message: err.Error()})
			s.bus.Publish(events.Event{Type: events.EventSyncFailed, Payload: err.Error(), At: time.Now()})
			return nil, err
		}
	}

	now := time.Now().UTC().Format(time.RFC3339)
	tickets := fakeTickets(now)

	for _, t := range tickets {
		if err := s.store.CreateTicket(ctx, t); err != nil {
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

	return &SyncResult{TicketsSynced: len(tickets)}, nil
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

func (s *stubSyncService) broadcast(e SyncEvent) {
	for _, ch := range s.subs {
		select {
		case ch <- e:
		default:
		}
	}
}

func fakeTickets(now string) []store.Ticket {
	return []store.Ticket{
		{
			ID: "LEG-1", Summary: "Set up project scaffolding and CI pipeline",
			Description: "Initialize the Go module, set up linting, and configure GitHub Actions.",
			DescriptionMD: "Initialize the Go module, set up linting, and configure GitHub Actions.",
			Status: "Done", JiraStatus: "Done", Priority: "High", IssueType: "Task",
			Assignee: "alice", Labels: "infra", EpicKey: "LEG-100", EpicName: "Foundation",
			URL: "https://jira.example.com/browse/LEG-1", CreatedAt: now, UpdatedAt: now,
			JiraUpdatedAt: now, SortOrder: 0,
		},
		{
			ID: "LEG-2", Summary: "Implement SQLite storage layer with migrations",
			Description: "Build the store package with embedded SQL migrations and CRUD operations for tickets, column mappings, and sync log.",
			DescriptionMD: "Build the store package with embedded SQL migrations and CRUD operations for tickets, column mappings, and sync log.\n\n## Acceptance Criteria\n\n- [ ] Store opens SQLite with WAL mode\n- [ ] Migrations run automatically\n- [ ] All CRUD operations tested",
			Status: "Done", JiraStatus: "Done", Priority: "High", IssueType: "Story",
			Assignee: "bob", Labels: "backend,storage", EpicKey: "LEG-100", EpicName: "Foundation",
			URL: "https://jira.example.com/browse/LEG-2", CreatedAt: now, UpdatedAt: now,
			JiraUpdatedAt: now, SortOrder: 1,
		},
		{
			ID: "LEG-3", Summary: "Design service layer interfaces for board and sync operations",
			Description: "Define Go interfaces for BoardService and SyncService that the TUI and future web UI will consume.",
			DescriptionMD: "Define Go interfaces for BoardService and SyncService that the TUI and future web UI will consume.\n\n## Requirements\n\n- All methods accept `context.Context`\n- No presentation-layer imports\n- Support for event-driven updates",
			Status: "In Progress", JiraStatus: "In Progress", Priority: "High", IssueType: "Story",
			Assignee: "alice", Labels: "backend,architecture", EpicKey: "LEG-101", EpicName: "Service Layer",
			URL: "https://jira.example.com/browse/LEG-3", CreatedAt: now, UpdatedAt: now,
			JiraUpdatedAt: now, SortOrder: 0,
		},
		{
			ID: "LEG-4", Summary: "Implement card move and reorder logic with event publishing",
			Description: "Implement MoveCard and ReorderCard on the board service, including sort_order management and event bus integration.",
			DescriptionMD: "Implement MoveCard and ReorderCard on the board service, including sort_order management and event bus integration.\n\n## Details\n\n- MoveCard updates status and places card at end of target column\n- ReorderCard adjusts sort_order for all cards in the column\n- Both operations publish events through the EventBus",
			Status: "In Progress", JiraStatus: "In Progress", Priority: "Medium", IssueType: "Task",
			Assignee: "bob", Labels: "backend", EpicKey: "LEG-101", EpicName: "Service Layer",
			URL: "https://jira.example.com/browse/LEG-4", CreatedAt: now, UpdatedAt: now,
			JiraUpdatedAt: now, SortOrder: 1,
		},
		{
			ID: "LEG-5", Summary: "Build TUI kanban board with column navigation and card rendering using Bubbletea and Lipgloss",
			Description: "",
			DescriptionMD: "",
			Status: "Backlog", JiraStatus: "To Do", Priority: "High", IssueType: "Epic",
			Assignee: "", Labels: "frontend,tui", EpicKey: "LEG-102", EpicName: "TUI Shell",
			URL: "https://jira.example.com/browse/LEG-5", CreatedAt: now, UpdatedAt: now,
			JiraUpdatedAt: now, SortOrder: 0,
		},
		{
			ID: "LEG-6", Summary: "Add Jira REST API client with OAuth and pagination support",
			Description: "Integrate with Jira Cloud REST API v3 for ticket synchronization.",
			DescriptionMD: "Integrate with Jira Cloud REST API v3 for ticket synchronization.\n\n## Scope\n\n- OAuth 2.0 authentication flow\n- Paginated search with JQL\n- Rate limiting and retry logic\n\n## Out of Scope\n\n- Jira Server/Data Center support (future)",
			Status: "Backlog", JiraStatus: "Open", Priority: "Medium", IssueType: "Story",
			Assignee: "", Labels: "backend,integration", EpicKey: "LEG-103", EpicName: "Jira Integration",
			URL: "https://jira.example.com/browse/LEG-6", CreatedAt: now, UpdatedAt: now,
			JiraUpdatedAt: now, SortOrder: 1,
		},
		{
			ID: "LEG-7", Summary: "Implement context export for clipboard with description-only and full structured block formats",
			Description: "Add context export formatting that produces clean markdown for clipboard copy and AI agent consumption.",
			DescriptionMD: "Add context export formatting that produces clean markdown for clipboard copy and AI agent consumption.\n\n## Formats\n\n### Description Only\n- Heading: `## KEY: Summary`\n- Body: markdown description\n\n### Full Block\n- Heading: `# Ticket: KEY`\n- Metadata fields (bold labels)\n- Separator: `---`\n- Full description\n\n## Constraints\n\n- No ANSI escape codes\n- No terminal formatting\n- Pure markdown output",
			Status: "In Review", JiraStatus: "In Review", Priority: "Low", IssueType: "Task",
			Assignee: "alice", Labels: "backend", EpicKey: "LEG-101", EpicName: "Service Layer",
			URL: "https://jira.example.com/browse/LEG-7", CreatedAt: now, UpdatedAt: now,
			JiraUpdatedAt: now, SortOrder: 0,
		},
		{
			ID: "LEG-8", Summary: "Fix sort_order gap after bulk delete causes cards to cluster at top of column",
			Description: "When multiple cards are deleted, the remaining cards keep their original sort_order values, leaving gaps. This causes new cards inserted at sort_order=0 to cluster at the top.",
			DescriptionMD: "When multiple cards are deleted, the remaining cards keep their original sort_order values, leaving gaps. This causes new cards inserted at `sort_order=0` to cluster at the top.\n\n## Steps to Reproduce\n\n1. Create 5 cards in a column\n2. Delete cards at positions 1, 3\n3. Add a new card\n4. Observe new card appears at top instead of bottom\n\n## Expected\n\nNew cards should always appear at the bottom of the column.\n\n## Actual\n\nNew cards appear at the top because `sort_order` defaults to 0.",
			Status: "Backlog", JiraStatus: "To Do", Priority: "Medium", IssueType: "Bug",
			Assignee: "", Labels: "backend,bug", EpicKey: "LEG-101", EpicName: "Service Layer",
			URL: "https://jira.example.com/browse/LEG-8", CreatedAt: now, UpdatedAt: now,
			JiraUpdatedAt: now, SortOrder: 2,
		},
		{
			ID: "LEG-9", Summary: "Add keyboard shortcut overlay showing all available keybindings grouped by context",
			Description: "Display a modal overlay triggered by ? that shows all keyboard shortcuts organized by context (navigation, actions, search).",
			DescriptionMD: "Display a modal overlay triggered by `?` that shows all keyboard shortcuts organized by context (navigation, actions, search).\n\n## Layout\n\n| Key | Action |\n|-----|--------|\n| `j/k` | Move up/down |\n| `h/l` | Move left/right |\n| `Enter` | Open detail |\n| `/` | Search |\n| `?` | Toggle this overlay |",
			Status: "Backlog", JiraStatus: "To Do", Priority: "Low", IssueType: "Story",
			Assignee: "", Labels: "frontend,ux", EpicKey: "LEG-104", EpicName: "Polish",
			URL: "https://jira.example.com/browse/LEG-9", CreatedAt: now, UpdatedAt: now,
			JiraUpdatedAt: now, SortOrder: 3,
		},
		{
			ID: "LEG-10", Summary: "Evaluate whether server-sent events or WebSocket is the right transport for real-time board updates in the future web UI",
			Description: "Research and document the trade-offs between SSE and WebSocket for pushing real-time board state changes to a browser client. This is a spike — no implementation expected.",
			DescriptionMD: "Research and document the trade-offs between SSE and WebSocket for pushing real-time board state changes to a browser client. This is a spike — no implementation expected.\n\n## Considerations\n\n- Unidirectional vs bidirectional needs\n- Browser compatibility\n- Load balancer behavior\n- Connection lifecycle management",
			Status: "Backlog", JiraStatus: "Open", Priority: "Low", IssueType: "Spike",
			Assignee: "", Labels: "research,architecture", EpicKey: "LEG-105", EpicName: "Web UI Planning",
			URL: "https://jira.example.com/browse/LEG-10", CreatedAt: now, UpdatedAt: now,
			JiraUpdatedAt: now, SortOrder: 4,
		},
	}
}
