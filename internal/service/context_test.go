package service

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode"

	"github.com/cpave3/legato/internal/engine/events"
	"github.com/cpave3/legato/internal/engine/store"
)

func seedTaskForExport(t *testing.T, s *store.Store) {
	t.Helper()
	ctx := context.Background()
	now := time.Now().UTC().Format(time.RFC3339)

	if err := s.CreateColumnMapping(ctx, store.ColumnMapping{
		ColumnName: "Backlog", RemoteStatuses: `["To Do"]`, SortOrder: 0,
	}); err != nil {
		t.Fatal(err)
	}

	provider := "jira"
	remoteID := "REX-1238"
	meta, _ := json.Marshal(map[string]string{
		"remote_status":     "To Do",
		"remote_updated_at": now,
		"issue_type":        "Story",
		"assignee":          "alice",
		"labels":            "backend",
		"epic_key":          "REX-100",
		"epic_name":         "Platform Modernisation",
		"url":               "https://jira.example.com/browse/REX-1238",
	})
	metaStr := string(meta)

	if err := s.CreateTask(ctx, store.Task{
		ID: "REX-1238", Title: "Refactor user service",
		Description:   "Refactor the user service to use the new repository pattern.\n\nThis includes updating all endpoints.",
		DescriptionMD: "Refactor the user service to use the new repository pattern.\n\nThis includes updating all endpoints.",
		Status: "Backlog", Priority: "High",
		Provider: &provider, RemoteID: &remoteID, RemoteMeta: &metaStr,
		CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
}

func TestFormatDescription_Standard(t *testing.T) {
	s, _, svc := setupExportBoard(t)
	seedTaskForExport(t, s)

	out, err := svc.ExportCardContext(context.Background(), "REX-1238", ExportFormatDescription)
	if err != nil {
		t.Fatalf("ExportCardContext: %v", err)
	}
	if !strings.HasPrefix(out, "## REX-1238: Refactor user service\n") {
		t.Errorf("unexpected heading, got:\n%s", out)
	}
	if !strings.Contains(out, "new repository pattern") {
		t.Error("description body not found")
	}
}

func TestFormatDescription_EmptyDescription(t *testing.T) {
	s, _, svc := setupExportBoard(t)
	ctx := context.Background()
	now := time.Now().UTC().Format(time.RFC3339)
	s.CreateColumnMapping(ctx, store.ColumnMapping{ColumnName: "Backlog", RemoteStatuses: `["To Do"]`, SortOrder: 0})
	s.CreateTask(ctx, store.Task{
		ID: "X-1", Title: "No desc", Status: "Backlog",
		CreatedAt: now, UpdatedAt: now,
	})

	out, err := svc.ExportCardContext(ctx, "X-1", ExportFormatDescription)
	if err != nil {
		t.Fatalf("ExportCardContext: %v", err)
	}
	if !strings.HasPrefix(out, "## X-1: No desc\n") {
		t.Errorf("unexpected output: %q", out)
	}
	// Should not have trailing content beyond the heading
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) != 1 {
		t.Errorf("expected 1 line for empty description, got %d", len(lines))
	}
}

func TestFormatFull_Standard(t *testing.T) {
	s, _, svc := setupExportBoard(t)
	seedTaskForExport(t, s)

	out, err := svc.ExportCardContext(context.Background(), "REX-1238", ExportFormatFull)
	if err != nil {
		t.Fatalf("ExportCardContext: %v", err)
	}
	if !strings.Contains(out, "# Task: REX-1238") {
		t.Error("missing task heading")
	}
	if !strings.Contains(out, "**Title:** Refactor user service") {
		t.Error("missing title")
	}
	if !strings.Contains(out, "**Type:** Story") {
		t.Error("missing type")
	}
	if !strings.Contains(out, "**Priority:** High") {
		t.Error("missing priority")
	}
	if !strings.Contains(out, "**Epic:** Platform Modernisation") {
		t.Error("missing epic")
	}
	if !strings.Contains(out, "**Labels:** backend") {
		t.Error("missing labels")
	}
	if !strings.Contains(out, "**URL:**") {
		t.Error("missing URL")
	}
	if !strings.Contains(out, "---") {
		t.Error("missing separator")
	}
	if !strings.Contains(out, "new repository pattern") {
		t.Error("missing description")
	}
}

func TestFormatFull_MissingOptionalFields(t *testing.T) {
	s, _, svc := setupExportBoard(t)
	ctx := context.Background()
	now := time.Now().UTC().Format(time.RFC3339)
	s.CreateColumnMapping(ctx, store.ColumnMapping{ColumnName: "Backlog", RemoteStatuses: `["To Do"]`, SortOrder: 0})
	s.CreateTask(ctx, store.Task{
		ID: "X-2", Title: "Minimal card", DescriptionMD: "Some desc.",
		Status: "Backlog",
		CreatedAt: now, UpdatedAt: now,
	})

	out, err := svc.ExportCardContext(ctx, "X-2", ExportFormatFull)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "**Epic:**") {
		t.Error("should omit missing epic")
	}
	if strings.Contains(out, "**Labels:**") {
		t.Error("should omit missing labels")
	}
	if strings.Contains(out, "**URL:**") {
		t.Error("should omit missing URL")
	}
	if !strings.Contains(out, "Some desc.") {
		t.Error("description should still appear")
	}
}

func TestFormatFull_EmptyDescription(t *testing.T) {
	s, _, svc := setupExportBoard(t)
	ctx := context.Background()
	now := time.Now().UTC().Format(time.RFC3339)
	s.CreateColumnMapping(ctx, store.ColumnMapping{ColumnName: "Backlog", RemoteStatuses: `["To Do"]`, SortOrder: 0})
	s.CreateTask(ctx, store.Task{
		ID: "X-3", Title: "No desc full", Status: "Backlog",
		Priority: "Low",
		CreatedAt: now, UpdatedAt: now,
	})

	out, err := svc.ExportCardContext(ctx, "X-3", ExportFormatFull)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "---") {
		t.Error("separator should still appear")
	}
}

func TestExportCardContext_UnknownFormat(t *testing.T) {
	s, _, svc := setupExportBoard(t)
	seedTaskForExport(t, s)

	_, err := svc.ExportCardContext(context.Background(), "REX-1238", ExportFormat(99))
	if err == nil {
		t.Fatal("expected error for unknown format")
	}
}

func TestExportCardContext_CardNotFound(t *testing.T) {
	_, _, svc := setupExportBoard(t)

	out, err := svc.ExportCardContext(context.Background(), "NOPE-1", ExportFormatDescription)
	if err == nil {
		t.Fatal("expected error for missing card")
	}
	if out != "" {
		t.Errorf("expected empty string, got %q", out)
	}
}

func TestExport_NoANSIEscapeSequences(t *testing.T) {
	s, _, svc := setupExportBoard(t)
	seedTaskForExport(t, s)

	for _, format := range []ExportFormat{ExportFormatDescription, ExportFormatFull} {
		out, err := svc.ExportCardContext(context.Background(), "REX-1238", format)
		if err != nil {
			t.Fatal(err)
		}
		for i, r := range out {
			if r == '\x1b' {
				t.Errorf("ANSI escape at byte %d in format %d", i, format)
			}
			if !unicode.IsPrint(r) && r != '\n' && r != '\t' && r != ' ' && r != '\r' {
				t.Errorf("non-printable character %q at byte %d in format %d", r, i, format)
			}
		}
	}
}

func setupExportBoard(t *testing.T) (*store.Store, *events.Bus, BoardService) {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "test.db")
	s, err := store.New(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	bus := events.New()
	return s, bus, NewBoardService(s, bus)
}
