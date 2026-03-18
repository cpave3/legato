package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/cpave3/legato/config"
	"github.com/cpave3/legato/internal/engine/events"
	"github.com/cpave3/legato/internal/engine/store"
)

func main() {
	// Load config
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "config: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("Config loaded (theme=%q, columns=%d)\n", cfg.Theme, len(cfg.Board.Columns))

	// Open store
	dbPath := config.ResolveDBPath(cfg)
	s, err := store.New(dbPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "store: %v\n", err)
		os.Exit(1)
	}
	defer s.Close()
	fmt.Printf("Store opened at %s\n", dbPath)

	// Set up event bus
	bus := events.New()
	ch := bus.Subscribe(events.EventCardUpdated)
	fmt.Println("Subscribed to EventCardUpdated")

	// Insert a test ticket
	ctx := context.Background()
	now := time.Now().UTC().Format(time.RFC3339)
	ticket := store.Ticket{
		ID:            "TEST-1",
		Summary:       "Legato engine layer smoke test",
		Status:        "Backlog",
		JiraStatus:    "To Do",
		CreatedAt:     now,
		UpdatedAt:     now,
		JiraUpdatedAt: now,
	}
	if err := s.CreateTicket(ctx, ticket); err != nil {
		fmt.Fprintf(os.Stderr, "create ticket: %v\n", err)
		os.Exit(1)
	}

	// Publish an event
	bus.Publish(events.Event{
		Type:    events.EventCardUpdated,
		Payload: ticket.ID,
		At:      time.Now(),
	})

	// Verify event received
	select {
	case evt := <-ch:
		fmt.Printf("Event received: type=%d payload=%v\n", evt.Type, evt.Payload)
	case <-time.After(time.Second):
		fmt.Fprintln(os.Stderr, "timeout waiting for event")
		os.Exit(1)
	}

	// Read ticket back
	got, err := s.GetTicket(ctx, "TEST-1")
	if err != nil {
		fmt.Fprintf(os.Stderr, "get ticket: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("Ticket: %s — %s (status=%s)\n", got.ID, got.Summary, got.Status)

	// Cleanup
	if err := s.DeleteTicket(ctx, "TEST-1"); err != nil {
		fmt.Fprintf(os.Stderr, "delete ticket: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("\n✓ Engine layer smoke test passed")
}
