package store

import (
	"context"
	"testing"
)

func TestAgentSessionsTableExists(t *testing.T) {
	s := newTestStore(t)

	var tables []string
	err := s.db.Select(&tables, "SELECT name FROM sqlite_master WHERE type='table' AND name='agent_sessions'")
	if err != nil {
		t.Fatal(err)
	}
	if len(tables) != 1 {
		t.Fatal("agent_sessions table does not exist after migration")
	}
}

func TestInsertAndListAgentSessions(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	createTestTicket(t, s, "REX-1238")

	err := s.InsertAgentSession(ctx, AgentSession{
		TicketID:    "REX-1238",
		TmuxSession: "legato-REX-1238",
		Command:     "shell",
		Status:      "running",
	})
	if err != nil {
		t.Fatal(err)
	}

	sessions, err := s.ListAgentSessions(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 1 {
		t.Fatalf("got %d sessions, want 1", len(sessions))
	}
	if sessions[0].TicketID != "REX-1238" {
		t.Errorf("TicketID = %q, want %q", sessions[0].TicketID, "REX-1238")
	}
	if sessions[0].TmuxSession != "legato-REX-1238" {
		t.Errorf("TmuxSession = %q, want %q", sessions[0].TmuxSession, "legato-REX-1238")
	}
	if sessions[0].Status != "running" {
		t.Errorf("Status = %q, want %q", sessions[0].Status, "running")
	}
}

func TestGetAgentSessionByTicketID(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	createTestTicket(t, s, "REX-1238")

	err := s.InsertAgentSession(ctx, AgentSession{
		TicketID:    "REX-1238",
		TmuxSession: "legato-REX-1238",
		Command:     "shell",
		Status:      "running",
	})
	if err != nil {
		t.Fatal(err)
	}

	got, err := s.GetAgentSessionByTicketID(ctx, "REX-1238")
	if err != nil {
		t.Fatal(err)
	}
	if got.TmuxSession != "legato-REX-1238" {
		t.Errorf("TmuxSession = %q, want %q", got.TmuxSession, "legato-REX-1238")
	}

	_, err = s.GetAgentSessionByTicketID(ctx, "NOPE-999")
	if err == nil {
		t.Error("expected error for non-existent ticket, got nil")
	}
}

func TestGetAgentSessionByTmuxName(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	createTestTicket(t, s, "REX-1238")

	err := s.InsertAgentSession(ctx, AgentSession{
		TicketID:    "REX-1238",
		TmuxSession: "legato-REX-1238",
		Command:     "shell",
		Status:      "running",
	})
	if err != nil {
		t.Fatal(err)
	}

	got, err := s.GetAgentSessionByTmuxName(ctx, "legato-REX-1238")
	if err != nil {
		t.Fatal(err)
	}
	if got.TicketID != "REX-1238" {
		t.Errorf("TicketID = %q, want %q", got.TicketID, "REX-1238")
	}

	_, err = s.GetAgentSessionByTmuxName(ctx, "legato-NOPE")
	if err == nil {
		t.Error("expected error for non-existent tmux session, got nil")
	}
}

func TestUpdateAgentSessionStatus(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	createTestTicket(t, s, "REX-1238")

	err := s.InsertAgentSession(ctx, AgentSession{
		TicketID:    "REX-1238",
		TmuxSession: "legato-REX-1238",
		Command:     "shell",
		Status:      "running",
	})
	if err != nil {
		t.Fatal(err)
	}

	err = s.UpdateAgentSessionStatus(ctx, "REX-1238", "dead")
	if err != nil {
		t.Fatal(err)
	}

	got, err := s.GetAgentSessionByTmuxName(ctx, "legato-REX-1238")
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != "dead" {
		t.Errorf("Status = %q, want %q", got.Status, "dead")
	}
	if got.EndedAt == nil {
		t.Error("EndedAt should be set when status changes to dead")
	}
}

func TestInsertDuplicateTmuxSessionFails(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	createTestTicket(t, s, "REX-1238")

	session := AgentSession{
		TicketID:    "REX-1238",
		TmuxSession: "legato-REX-1238",
		Command:     "shell",
		Status:      "running",
	}
	if err := s.InsertAgentSession(ctx, session); err != nil {
		t.Fatal(err)
	}
	if err := s.InsertAgentSession(ctx, session); err == nil {
		t.Error("expected error for duplicate tmux_session, got nil")
	}
}

func createTestTicket(t *testing.T, s *Store, id string) {
	t.Helper()
	ctx := context.Background()
	err := s.CreateTicket(ctx, Ticket{
		ID:              id,
		Summary:         "Test ticket " + id,
		Status:          "Doing",
		RemoteStatus:    "In Progress",
		CreatedAt:       "2024-01-01T00:00:00Z",
		UpdatedAt:       "2024-01-01T00:00:00Z",
		RemoteUpdatedAt: "2024-01-01T00:00:00Z",
	})
	if err != nil {
		t.Fatal(err)
	}
}
