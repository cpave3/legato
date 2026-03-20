package setup

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/cpave3/legato/config"
)

// fakeColumnStore implements ColumnSeeder for testing.
type fakeColumnStore struct {
	mappings []ColumnMappingRow
	nextID   int
}

func (f *fakeColumnStore) ListColumnMappings(ctx context.Context) ([]ColumnMappingRow, error) {
	cp := make([]ColumnMappingRow, len(f.mappings))
	copy(cp, f.mappings)
	return cp, nil
}

func (f *fakeColumnStore) CreateColumnMapping(ctx context.Context, m ColumnMappingRow) error {
	f.nextID++
	m.ID = f.nextID
	f.mappings = append(f.mappings, m)
	return nil
}

func (f *fakeColumnStore) DeleteColumnMapping(ctx context.Context, id int) error {
	for i, m := range f.mappings {
		if m.ID == id {
			f.mappings = append(f.mappings[:i], f.mappings[i+1:]...)
			return nil
		}
	}
	return nil
}

func TestWizard_SeedsDefaultColumns(t *testing.T) {
	store := &fakeColumnStore{}
	var out bytes.Buffer
	in := strings.NewReader("n\n") // decline Jira

	columns := config.DefaultColumns()

	err := RunWizard(context.Background(), &out, in, store, columns, nil)
	if err != nil {
		t.Fatalf("RunWizard() error: %v", err)
	}

	if len(store.mappings) != 5 {
		t.Fatalf("expected 5 column mappings, got %d", len(store.mappings))
	}

	expected := []string{"Backlog", "Ready", "Doing", "Review", "Done"}
	for i, name := range expected {
		if store.mappings[i].ColumnName != name {
			t.Errorf("column %d: expected %q, got %q", i, name, store.mappings[i].ColumnName)
		}
		if store.mappings[i].SortOrder != i {
			t.Errorf("column %d: expected sort_order %d, got %d", i, i, store.mappings[i].SortOrder)
		}
	}

	output := out.String()
	if !strings.Contains(output, "Backlog") {
		t.Error("output should mention column names")
	}
}

type fakeJiraSetup struct {
	validateErr error
	projects    []Project
	statuses    []string
}

func (f *fakeJiraSetup) ValidateCredentials(ctx context.Context, baseURL, email, token string) error {
	return f.validateErr
}

func (f *fakeJiraSetup) FetchProjects(ctx context.Context, baseURL, email, token string) ([]Project, error) {
	return f.projects, nil
}

func (f *fakeJiraSetup) FetchStatuses(ctx context.Context, baseURL, email, token, projectKey string) ([]string, error) {
	return f.statuses, nil
}

func TestWizard_AcceptJira_FullFlow(t *testing.T) {
	store := &fakeColumnStore{}
	var out bytes.Buffer

	// Simulate: yes to Jira, URL, email, token, select project 1
	input := "y\nhttps://team.atlassian.net\nuser@example.com\nmy-token\n1\n"
	in := strings.NewReader(input)

	jira := &fakeJiraSetup{
		projects: []Project{
			{Key: "REX", Name: "Rex App"},
			{Key: "INF", Name: "Infrastructure"},
		},
		statuses: []string{"To Do", "In Progress", "In Review", "Done"},
	}

	configPath := t.TempDir() + "/config.yaml"

	err := RunWizard(context.Background(), &out, in, store, config.DefaultColumns(), jira, WithConfigPath(configPath))
	if err != nil {
		t.Fatalf("RunWizard() error: %v", err)
	}

	output := out.String()

	// Should show validation success
	if !strings.Contains(output, "Validating") {
		t.Error("should show validation step")
	}

	// Should list projects
	if !strings.Contains(output, "REX") {
		t.Error("should list available projects")
	}

	// Should show auto-mapping
	if !strings.Contains(output, "Auto-mapped") {
		t.Error("should mention auto-mapping")
	}

	// Columns should be replaced with Jira-mapped ones
	// Original 5 defaults should be replaced by auto-mapped columns
	var columnNames []string
	for _, m := range store.mappings {
		columnNames = append(columnNames, m.ColumnName)
	}
	// Auto-map of [To Do, In Progress, In Review, Done] produces Backlog, Doing, Review, Done
	if len(store.mappings) != 4 {
		t.Fatalf("expected 4 auto-mapped columns, got %d: %v", len(store.mappings), columnNames)
	}

	// Config file should be written
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		t.Error("config file should be written")
	}

	// Should mention env var
	if !strings.Contains(output, "LEGATO_JIRA_TOKEN") {
		t.Error("should mention LEGATO_JIRA_TOKEN env var")
	}
}

func TestWizard_JiraValidationFails(t *testing.T) {
	store := &fakeColumnStore{}
	var out bytes.Buffer

	input := "y\nhttps://team.atlassian.net\nuser@example.com\nbad-token\n"
	in := strings.NewReader(input)

	jira := &fakeJiraSetup{
		validateErr: fmt.Errorf("authentication failed: invalid email or API token"),
	}

	err := RunWizard(context.Background(), &out, in, store, config.DefaultColumns(), jira)
	if err == nil {
		t.Fatal("expected error when validation fails")
	}
	if !strings.Contains(err.Error(), "credential validation failed") {
		t.Errorf("expected credential validation error, got: %v", err)
	}

	// Default columns should still be seeded (happened before Jira step)
	if len(store.mappings) != 5 {
		t.Errorf("expected 5 default columns still seeded, got %d", len(store.mappings))
	}
}

func TestWizard_DeclineJira_LocalOnlyMessage(t *testing.T) {
	store := &fakeColumnStore{}
	var out bytes.Buffer
	in := strings.NewReader("n\n")

	err := RunWizard(context.Background(), &out, in, store, config.DefaultColumns(), nil)
	if err != nil {
		t.Fatalf("RunWizard() error: %v", err)
	}

	output := out.String()
	if !strings.Contains(output, "local-only mode") {
		t.Error("should mention local-only mode when Jira declined")
	}
	if !strings.Contains(output, "Launching Legato") {
		t.Error("should say launching at the end")
	}
}
