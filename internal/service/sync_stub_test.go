package service

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/cpave3/legato/internal/engine/events"
	"github.com/cpave3/legato/internal/engine/store"
)

func setupTestSync(t *testing.T) (*store.Store, *events.Bus, SyncService, BoardService) {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "test.db")
	s, err := store.New(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })

	bus := events.New()
	sync := NewStubSyncService(s, bus)
	board := NewBoardService(s, bus)
	return s, bus, sync, board
}

func TestSync_FirstCall_SeedsData(t *testing.T) {
	_, _, sync, board := setupTestSync(t)
	ctx := context.Background()

	result, err := sync.Sync(ctx)
	if err != nil {
		t.Fatalf("Sync: %v", err)
	}
	if result.TicketsSynced < 8 {
		t.Errorf("expected at least 8 tickets synced, got %d", result.TicketsSynced)
	}

	// Verify tickets queryable through board service
	cols, err := board.ListColumns(ctx)
	if err != nil {
		t.Fatalf("ListColumns: %v", err)
	}
	if len(cols) < 3 {
		t.Errorf("expected at least 3 columns, got %d", len(cols))
	}

	// Count total cards across all columns
	total := 0
	for _, col := range cols {
		cards, err := board.ListCards(ctx, col.Name)
		if err != nil {
			t.Fatalf("ListCards(%q): %v", col.Name, err)
		}
		total += len(cards)
	}
	if total < 8 {
		t.Errorf("expected at least 8 total cards, got %d", total)
	}
}

func TestSync_SubsequentCall_NoOp(t *testing.T) {
	_, _, sync, _ := setupTestSync(t)
	ctx := context.Background()

	_, err := sync.Sync(ctx)
	if err != nil {
		t.Fatal(err)
	}

	result, err := sync.Sync(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if result.TicketsSynced != 0 {
		t.Errorf("expected 0 tickets on second sync, got %d", result.TicketsSynced)
	}
}

func TestSync_PublishesEvents(t *testing.T) {
	_, bus, sync, _ := setupTestSync(t)

	startCh := bus.Subscribe(events.EventSyncStarted)
	completeCh := bus.Subscribe(events.EventSyncCompleted)
	refreshCh := bus.Subscribe(events.EventCardsRefreshed)

	_, err := sync.Sync(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	select {
	case <-startCh:
	default:
		t.Error("expected EventSyncStarted")
	}
	select {
	case <-refreshCh:
	default:
		t.Error("expected EventCardsRefreshed")
	}
	select {
	case <-completeCh:
	default:
		t.Error("expected EventSyncCompleted")
	}
}

func TestStatus_BeforeSync(t *testing.T) {
	_, _, sync, _ := setupTestSync(t)

	status := sync.Status()
	if status.InProgress {
		t.Error("expected not in progress before sync")
	}
	if !status.LastSync.IsZero() {
		t.Error("expected zero last sync time before sync")
	}
}

func TestStatus_AfterSync(t *testing.T) {
	_, _, sync, _ := setupTestSync(t)

	before := time.Now()
	sync.Sync(context.Background())
	after := time.Now()

	status := sync.Status()
	if status.InProgress {
		t.Error("expected not in progress after sync")
	}
	if status.LastSync.Before(before) || status.LastSync.After(after) {
		t.Errorf("last sync time %v not between %v and %v", status.LastSync, before, after)
	}
}

func TestSubscribe_ReceivesEvents(t *testing.T) {
	_, _, sync, _ := setupTestSync(t)

	ch := sync.Subscribe()

	sync.Sync(context.Background())

	// Should receive at least: started, refreshed, completed
	var received []SyncEvent
	for i := 0; i < 3; i++ {
		select {
		case evt := <-ch:
			received = append(received, evt)
		default:
			t.Fatalf("expected 3 events, only got %d", len(received))
		}
	}

	if received[0].Type != EventSyncStarted {
		t.Errorf("first event should be sync.started, got %q", received[0].Type)
	}
	if received[1].Type != EventCardsRefreshed {
		t.Errorf("second event should be cards.refreshed, got %q", received[1].Type)
	}
	if received[2].Type != EventSyncCompleted {
		t.Errorf("third event should be sync.completed, got %q", received[2].Type)
	}
}

func TestSubscribe_MultipleSubscribers(t *testing.T) {
	_, _, sync, _ := setupTestSync(t)

	ch1 := sync.Subscribe()
	ch2 := sync.Subscribe()

	sync.Sync(context.Background())

	// Both channels should receive events
	for _, ch := range []<-chan SyncEvent{ch1, ch2} {
		count := 0
		for {
			select {
			case <-ch:
				count++
			default:
				goto done
			}
		}
	done:
		if count < 3 {
			t.Errorf("subscriber got %d events, expected at least 3", count)
		}
	}
}

func TestFakeData_Variety(t *testing.T) {
	now := time.Now().UTC().Format(time.RFC3339)
	tickets := fakeTickets(now)

	if len(tickets) < 8 {
		t.Errorf("expected at least 8 fake tickets, got %d", len(tickets))
	}

	// Check at least 3 distinct statuses
	statuses := map[string]bool{}
	for _, t := range tickets {
		statuses[t.Status] = true
	}
	if len(statuses) < 3 {
		t.Errorf("expected at least 3 distinct statuses, got %d", len(statuses))
	}

	// Check at least 1 empty description
	hasEmpty := false
	for _, t := range tickets {
		if t.DescriptionMD == "" {
			hasEmpty = true
			break
		}
	}
	if !hasEmpty {
		t.Error("expected at least one ticket with empty description")
	}

	// Check at least 1 long summary (>60 chars)
	hasLong := false
	for _, t := range tickets {
		if len(t.Summary) > 60 {
			hasLong = true
			break
		}
	}
	if !hasLong {
		t.Error("expected at least one ticket with summary > 60 chars")
	}
}
