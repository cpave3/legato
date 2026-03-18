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
	all := map[string]service.CardDetail{
		"LEG-1": {ID: "LEG-1", Summary: "Set up project scaffolding", Status: "Backlog", Priority: "High", IssueType: "Task", DescriptionMD: "## Overview\n\nScaffold the initial project structure including:\n\n- Go module initialization\n- Directory layout (`cmd/`, `internal/`, `config/`)\n- Basic `Taskfile.yml` with build/test/lint targets\n- CI pipeline skeleton\n\n## Acceptance Criteria\n\n- [ ] `task build` produces a binary\n- [ ] `task test` runs with zero failures\n- [ ] `task lint` passes cleanly"},
		"LEG-2": {ID: "LEG-2", Summary: "Design authentication flow for SSO integration", Status: "Backlog", Priority: "Medium", IssueType: "Story", Labels: "auth,design", EpicKey: "LEG-100", EpicName: "SSO Integration", DescriptionMD: "## Context\n\nWe need to support SAML-based SSO for enterprise customers.\n\n## Requirements\n\n1. Redirect to IdP login page\n2. Handle SAML assertion callback\n3. Map SAML attributes to local user profile\n4. Session management with configurable TTL\n\n## Open Questions\n\n- Should we support OIDC as well?\n- What's the session timeout policy?"},
		"LEG-3": {ID: "LEG-3", Summary: "Fix timezone offset in sync timestamps", Status: "Backlog", Priority: "High", IssueType: "Bug", DescriptionMD: "## Bug Report\n\nSync timestamps are stored in local time instead of UTC, causing issues when the user travels across timezones.\n\n### Steps to Reproduce\n\n1. Sync tickets in timezone UTC+10\n2. Travel to UTC-5\n3. Sync again\n4. Observe duplicate updates due to timestamp mismatch\n\n### Expected Behavior\n\nAll timestamps stored as UTC. Conversion to local time happens only at display.\n\n### Actual Behavior\n\n`sync_log.synced_at` uses `datetime('now', 'localtime')` instead of `datetime('now')`."},
		"LEG-4": {ID: "LEG-4", Summary: "Implement column mapping configuration", Status: "Ready", Priority: "Medium", IssueType: "Story", DescriptionMD: "## Summary\n\nAllow users to configure how remote statuses map to kanban columns.\n\n```yaml\ncolumns:\n  - name: Backlog\n    statuses: [Open, Reopened]\n  - name: Doing\n    statuses: [In Progress]\n```\n\nThe mapping is stored in SQLite and synced from config on startup."},
		"LEG-5": {ID: "LEG-5", Summary: "Build kanban board TUI with vim navigation", Status: "Doing", Priority: "High", IssueType: "Story", URL: "https://jira.example.com/browse/LEG-5", DescriptionMD: "## Summary\n\nBuild the main kanban board view using Bubbletea and Lipgloss.\n\n## Key Features\n\n- **Vim navigation**: `h/l` columns, `j/k` cards, `g/G` jump\n- **Column rendering**: header with count, cards below\n- **Card rendering**: key, summary, priority border color\n- **Selected state**: highlighted with distinct style\n\n## Technical Notes\n\n- Use `lipgloss.JoinHorizontal` for column layout\n- Cards use left-border color for priority indication\n- Done column uses strikethrough + muted text"},
		"LEG-6": {ID: "LEG-6", Summary: "Add keyboard shortcuts documentation", Status: "Doing", Priority: "Low", IssueType: "Task", DescriptionMD: "Document all keyboard shortcuts in a help overlay.\n\n| Key | Action |\n|-----|--------|\n| h/l | Move between columns |\n| j/k | Move between cards |\n| enter | Open detail view |\n| m | Move card |\n| y | Copy description |\n| Y | Copy full context |\n| o | Open in browser |\n| q | Quit |"},
		"LEG-7": {ID: "LEG-7", Summary: "Refactor event bus to support typed events", Status: "Review", Priority: "Medium", IssueType: "Task", DescriptionMD: "## Motivation\n\nThe current event bus uses `interface{}` payloads which requires type assertions everywhere.\n\n## Proposal\n\nSwitch to a typed `Event` struct:\n\n```go\ntype Event struct {\n    Type    EventType\n    Payload map[string]string\n    At      time.Time\n}\n```\n\nThis is safer and easier to test."},
		"LEG-8": {ID: "LEG-8", Summary: "Initialize SQLite store with migrations", Status: "Done", Priority: "High", IssueType: "Story", DescriptionMD: "Set up SQLite database with embedded migrations via `embed.FS`."},
		"LEG-9": {ID: "LEG-9", Summary: "Create YAML config parser", Status: "Done", Priority: "Low", IssueType: "Task", DescriptionMD: "Parse `config.yaml` with env var expansion using `os.ExpandEnv`."},
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
		return fmt.Sprintf("## %s: %s\n\n%s", card.ID, card.Summary, card.DescriptionMD), nil
	case service.ExportFormatFull:
		return fmt.Sprintf("# Ticket: %s\n\n**Summary:** %s\n**Type:** %s\n**Priority:** %s\n**Status:** %s\n\n---\n\n%s",
			card.ID, card.Summary, card.IssueType, card.Priority, card.Status, card.DescriptionMD), nil
	default:
		return "", fmt.Errorf("unsupported format")
	}
}
