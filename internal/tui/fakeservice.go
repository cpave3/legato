package tui

import (
	"context"
	"fmt"

	"github.com/cpave3/legato/internal/service"
)

// FakeBoardService returns hardcoded columns and cards for development and testing.
type FakeBoardService struct{}

func (f *FakeBoardService) ListColumns(_ context.Context) ([]service.Column, error) {
	return []service.Column{
		{Name: "Backlog", SortOrder: 0},
		{Name: "Ready", SortOrder: 1},
		{Name: "Doing", SortOrder: 2},
		{Name: "Review", SortOrder: 3},
		{Name: "Done", SortOrder: 4},
	}, nil
}

func (f *FakeBoardService) ListCards(_ context.Context, column string) ([]service.Card, error) {
	cards := map[string][]service.Card{
		"Backlog": {
			{ID: "LEG-1", Summary: "Set up project scaffolding", Priority: "High", IssueType: "Task", Status: "Backlog", SortOrder: 0},
			{ID: "LEG-2", Summary: "Design authentication flow for SSO integration", Priority: "Medium", IssueType: "Story", Status: "Backlog", SortOrder: 1},
			{ID: "LEG-3", Summary: "Fix timezone offset in sync timestamps", Priority: "High", IssueType: "Bug", Status: "Backlog", SortOrder: 2},
		},
		"Ready": {
			{ID: "LEG-4", Summary: "Implement column mapping configuration", Priority: "Medium", IssueType: "Story", Status: "Ready", SortOrder: 0},
		},
		"Doing": {
			{ID: "LEG-5", Summary: "Build kanban board TUI with vim navigation", Priority: "High", IssueType: "Story", Status: "Doing", SortOrder: 0},
			{ID: "LEG-6", Summary: "Add keyboard shortcuts documentation", Priority: "Low", IssueType: "Task", Status: "Doing", SortOrder: 1},
		},
		"Review": {
			{ID: "LEG-7", Summary: "Refactor event bus to support typed events", Priority: "Medium", IssueType: "Task", Status: "Review", SortOrder: 0},
		},
		"Done": {
			{ID: "LEG-8", Summary: "Initialize SQLite store with migrations", Priority: "High", IssueType: "Story", Status: "Done", SortOrder: 0},
			{ID: "LEG-9", Summary: "Create YAML config parser", Priority: "Low", IssueType: "Task", Status: "Done", SortOrder: 1},
		},
	}
	return cards[column], nil
}

func (f *FakeBoardService) GetCard(_ context.Context, id string) (*service.CardDetail, error) {
	return nil, fmt.Errorf("card %s not found", id)
}

func (f *FakeBoardService) MoveCard(_ context.Context, _ string, _ string) error {
	return nil
}

func (f *FakeBoardService) ReorderCard(_ context.Context, _ string, _ int) error {
	return nil
}

func (f *FakeBoardService) SearchCards(_ context.Context, _ string) ([]service.Card, error) {
	return nil, nil
}

func (f *FakeBoardService) ExportCardContext(_ context.Context, _ string, _ service.ExportFormat) (string, error) {
	return "", nil
}
