package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/cpave3/legato/internal/engine/store"
	"github.com/cpave3/legato/internal/service"
)

type mockBoardService struct {
	columns []service.Column
	cards   map[string][]service.Card
}

func (m *mockBoardService) ListColumns(_ context.Context) ([]service.Column, error) {
	return m.columns, nil
}

func (m *mockBoardService) ListCards(_ context.Context, column string) ([]service.Card, error) {
	return m.cards[column], nil
}

func (m *mockBoardService) GetCard(_ context.Context, _ string) (*service.CardDetail, error) {
	return nil, nil
}
func (m *mockBoardService) MoveCard(_ context.Context, _ string, _ string) error { return nil }
func (m *mockBoardService) ReorderCard(_ context.Context, _ string, _ int) error  { return nil }
func (m *mockBoardService) SearchCards(_ context.Context, _ string) ([]service.Card, error) {
	return nil, nil
}
func (m *mockBoardService) ExportCardContext(_ context.Context, _ string, _ service.ExportFormat) (string, error) {
	return "", nil
}
func (m *mockBoardService) DeleteTask(_ context.Context, _ string) error { return nil }
func (m *mockBoardService) CreateTask(_ context.Context, _, _, _, _ string, _ *int) (*service.Card, error) {
	return nil, nil
}
func (m *mockBoardService) UpdateTaskDescription(_ context.Context, _, _ string) error {
	return nil
}
func (m *mockBoardService) UpdateTaskTitle(_ context.Context, _, _ string) error {
	return nil
}
func (m *mockBoardService) ListCardsByWorkspace(_ context.Context, column string, _ store.WorkspaceView) ([]service.Card, error) {
	return m.ListCards(context.Background(), column)
}
func (m *mockBoardService) UpdateTaskWorkspace(_ context.Context, _ string, _ *int) error {
	return nil
}
func (m *mockBoardService) ListWorkspaces(_ context.Context) ([]service.Workspace, error) {
	return nil, nil
}
func (m *mockBoardService) ArchiveDoneCards(_ context.Context) (int, error) { return 0, nil }
func (m *mockBoardService) ArchiveTask(_ context.Context, _ string) error   { return nil }
func (m *mockBoardService) CountDoneCards(_ context.Context) (int, error)   { return 0, nil }

func TestHealthEndpointReturnsOK(t *testing.T) {
	svc := &mockBoardService{
		columns: []service.Column{
			{Name: "Backlog", SortOrder: 0},
			{Name: "Doing", SortOrder: 1},
		},
		cards: map[string][]service.Card{
			"Backlog": {
				{ID: "REX-1", Title: "First", Status: "Backlog"},
				{ID: "REX-2", Title: "Second", Status: "Backlog"},
			},
			"Doing": {
				{ID: "REX-3", Title: "In progress", Status: "Doing"},
			},
		},
	}

	srv := New(svc, ":0")
	handler := srv.Handler()

	req := httptest.NewRequest("GET", "/health", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}

	var resp HealthResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if resp.Status != "ok" {
		t.Errorf("status = %q, want ok", resp.Status)
	}
	if len(resp.Columns) != 2 {
		t.Fatalf("columns = %d, want 2", len(resp.Columns))
	}
	if resp.Columns[0].Name != "Backlog" {
		t.Errorf("column[0] = %q, want Backlog", resp.Columns[0].Name)
	}
	if len(resp.Columns[0].Cards) != 2 {
		t.Errorf("backlog cards = %d, want 2", len(resp.Columns[0].Cards))
	}
	if resp.Columns[1].Name != "Doing" {
		t.Errorf("column[1] = %q, want Doing", resp.Columns[1].Name)
	}
}

func TestHealthEndpointEmptyBoard(t *testing.T) {
	svc := &mockBoardService{
		columns: []service.Column{
			{Name: "Backlog", SortOrder: 0},
		},
		cards: map[string][]service.Card{
			"Backlog": {},
		},
	}

	srv := New(svc, ":0")
	handler := srv.Handler()

	req := httptest.NewRequest("GET", "/health", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}

	var resp HealthResponse
	json.Unmarshal(w.Body.Bytes(), &resp)

	if resp.Status != "ok" {
		t.Errorf("status = %q, want ok", resp.Status)
	}
	if len(resp.Columns[0].Cards) != 0 {
		t.Errorf("should have 0 cards, got %d", len(resp.Columns[0].Cards))
	}
}

func TestServerStartAndStop(t *testing.T) {
	svc := &mockBoardService{
		columns: []service.Column{},
		cards:   map[string][]service.Card{},
	}

	srv := New(svc, "127.0.0.1:0")
	go func() { _ = srv.Start() }()
	time.Sleep(50 * time.Millisecond)
	if err := srv.Stop(context.Background()); err != nil {
		t.Errorf("stop: %v", err)
	}
}
