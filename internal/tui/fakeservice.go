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
			{ID: "LEG-1", Title: "Set up project scaffolding", Priority: "High", IssueType: "Task", Status: "Backlog", SortOrder: 0},
			{ID: "LEG-2", Title: "Design authentication flow for SSO integration", Priority: "Medium", IssueType: "Story", Status: "Backlog", SortOrder: 1},
			{ID: "LEG-3", Title: "Fix timezone offset in sync timestamps", Priority: "High", IssueType: "Bug", Status: "Backlog", SortOrder: 2},
		},
		"Ready": {
			{ID: "LEG-4", Title: "Implement column mapping configuration", Priority: "Medium", IssueType: "Story", Status: "Ready", SortOrder: 0},
		},
		"Doing": {
			{ID: "LEG-5", Title: "Build kanban board TUI with vim navigation", Priority: "High", IssueType: "Story", Status: "Doing", SortOrder: 0},
			{ID: "LEG-6", Title: "Add keyboard shortcuts documentation", Priority: "Low", IssueType: "Task", Status: "Doing", SortOrder: 1},
		},
		"Review": {
			{ID: "LEG-7", Title: "Refactor event bus to support typed events", Priority: "Medium", IssueType: "Task", Status: "Review", SortOrder: 0},
		},
		"Done": {
			{ID: "LEG-8", Title: "Initialize SQLite store with migrations", Priority: "High", IssueType: "Story", Status: "Done", SortOrder: 0},
			{ID: "LEG-9", Title: "Create YAML config parser", Priority: "Low", IssueType: "Task", Status: "Done", SortOrder: 1},
		},
	}
	return cards[column], nil
}

func (f *FakeBoardService) GetCard(_ context.Context, id string) (*service.CardDetail, error) {
	all := map[string]service.CardDetail{
		"LEG-1": {ID: "LEG-1", Title: "Set up project scaffolding", Status: "Backlog", Priority: "High", DescriptionMD: "## Overview\n\nScaffold the initial project structure."},
		"LEG-2": {ID: "LEG-2", Title: "Design authentication flow for SSO integration", Status: "Backlog", Priority: "Medium", DescriptionMD: "## Context\n\nWe need to support SAML-based SSO."},
		"LEG-3": {ID: "LEG-3", Title: "Fix timezone offset in sync timestamps", Status: "Backlog", Priority: "High", DescriptionMD: "## Bug Report\n\nSync timestamps are stored in local time instead of UTC."},
		"LEG-4": {ID: "LEG-4", Title: "Implement column mapping configuration", Status: "Ready", Priority: "Medium", DescriptionMD: "## Summary\n\nAllow users to configure how remote statuses map to kanban columns."},
		"LEG-5": {ID: "LEG-5", Title: "Build kanban board TUI with vim navigation", Status: "Doing", Priority: "High", DescriptionMD: "## Summary\n\nBuild the main kanban board view using Bubbletea and Lipgloss."},
		"LEG-6": {ID: "LEG-6", Title: "Add keyboard shortcuts documentation", Status: "Doing", Priority: "Low", DescriptionMD: "Document all keyboard shortcuts in a help overlay."},
		"LEG-7": {ID: "LEG-7", Title: "Refactor event bus to support typed events", Status: "Review", Priority: "Medium", DescriptionMD: "## Motivation\n\nThe current event bus uses `interface{}` payloads."},
		"LEG-8": {ID: "LEG-8", Title: "Initialize SQLite store with migrations", Status: "Done", Priority: "High", DescriptionMD: "Set up SQLite database with embedded migrations via `embed.FS`."},
		"LEG-9": {ID: "LEG-9", Title: "Create YAML config parser", Status: "Done", Priority: "Low", DescriptionMD: "Parse `config.yaml` with env var expansion using `os.ExpandEnv`."},
	}
	if card, ok := all[id]; ok {
		return &card, nil
	}
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

func (f *FakeBoardService) ExportCardContext(_ context.Context, id string, format service.ExportFormat) (string, error) {
	card, err := f.GetCard(context.Background(), id)
	if err != nil {
		return "", err
	}
	switch format {
	case service.ExportFormatDescription:
		return fmt.Sprintf("## %s: %s\n\n%s", card.ID, card.Title, card.DescriptionMD), nil
	case service.ExportFormatFull:
		return fmt.Sprintf("# Task: %s\n\n**Title:** %s\n**Priority:** %s\n**Status:** %s\n\n---\n\n%s",
			card.ID, card.Title, card.Priority, card.Status, card.DescriptionMD), nil
	default:
		return "", fmt.Errorf("unsupported format")
	}
}

func (f *FakeBoardService) DeleteTask(_ context.Context, _ string) error {
	return nil
}

func (f *FakeBoardService) CreateTask(_ context.Context, title, _, column, priority string) (*service.Card, error) {
	return &service.Card{
		ID:       "NEW-1",
		Title:    title,
		Priority: priority,
		Status:   column,
	}, nil
}

func (f *FakeBoardService) UpdateTaskDescription(_ context.Context, _, _ string) error {
	return nil
}

func (f *FakeBoardService) UpdateTaskTitle(_ context.Context, _, _ string) error {
	return nil
}
