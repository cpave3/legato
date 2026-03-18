package jira

import (
	"encoding/json"
	"testing"
)

func TestIssueUnmarshal(t *testing.T) {
	raw := `{
		"key": "PROJ-123",
		"fields": {
			"summary": "Fix login bug",
			"status": {"name": "In Progress", "id": "3"},
			"priority": {"name": "High"},
			"issuetype": {"name": "Bug"},
			"assignee": {"displayName": "Alice", "emailAddress": "alice@example.com"},
			"labels": ["backend", "urgent"],
			"description": {"version": 1, "type": "doc", "content": []},
			"updated": "2025-01-15T10:30:00.000+0000",
			"project": {"key": "PROJ"},
			"customfield_10014": "PROJ-100"
		}
	}`

	var issue Issue
	if err := json.Unmarshal([]byte(raw), &issue); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if issue.Key != "PROJ-123" {
		t.Errorf("key = %q, want PROJ-123", issue.Key)
	}
	if issue.Fields.Summary != "Fix login bug" {
		t.Errorf("summary = %q, want 'Fix login bug'", issue.Fields.Summary)
	}
	if issue.Fields.Status.Name != "In Progress" {
		t.Errorf("status = %q, want 'In Progress'", issue.Fields.Status.Name)
	}
	if issue.Fields.Priority.Name != "High" {
		t.Errorf("priority = %q, want 'High'", issue.Fields.Priority.Name)
	}
	if issue.Fields.IssueType.Name != "Bug" {
		t.Errorf("issuetype = %q, want 'Bug'", issue.Fields.IssueType.Name)
	}
	if issue.Fields.Assignee.DisplayName != "Alice" {
		t.Errorf("assignee = %q, want 'Alice'", issue.Fields.Assignee.DisplayName)
	}
	if len(issue.Fields.Labels) != 2 || issue.Fields.Labels[0] != "backend" {
		t.Errorf("labels = %v, want [backend urgent]", issue.Fields.Labels)
	}
	if issue.Fields.Updated != "2025-01-15T10:30:00.000+0000" {
		t.Errorf("updated = %q, want '2025-01-15T10:30:00.000+0000'", issue.Fields.Updated)
	}
	if issue.Fields.Project.Key != "PROJ" {
		t.Errorf("project = %q, want 'PROJ'", issue.Fields.Project.Key)
	}
	if issue.Fields.EpicKey != "PROJ-100" {
		t.Errorf("epic key = %q, want 'PROJ-100'", issue.Fields.EpicKey)
	}
}

func TestTransitionUnmarshal(t *testing.T) {
	raw := `{
		"id": "31",
		"name": "Done",
		"to": {"name": "Done", "id": "10001"}
	}`

	var tr Transition
	if err := json.Unmarshal([]byte(raw), &tr); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if tr.ID != "31" {
		t.Errorf("id = %q, want '31'", tr.ID)
	}
	if tr.Name != "Done" {
		t.Errorf("name = %q, want 'Done'", tr.Name)
	}
	if tr.To.Name != "Done" {
		t.Errorf("to.name = %q, want 'Done'", tr.To.Name)
	}
}

func TestSearchResultUnmarshal(t *testing.T) {
	raw := `{
		"startAt": 0,
		"maxResults": 50,
		"total": 2,
		"issues": [
			{"key": "PROJ-1", "fields": {"summary": "First", "status": {"name": "Open"}, "updated": "2025-01-01T00:00:00.000+0000", "project": {"key": "PROJ"}}},
			{"key": "PROJ-2", "fields": {"summary": "Second", "status": {"name": "Done"}, "updated": "2025-01-02T00:00:00.000+0000", "project": {"key": "PROJ"}}}
		]
	}`

	var sr SearchResult
	if err := json.Unmarshal([]byte(raw), &sr); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if sr.Total != 2 {
		t.Errorf("total = %d, want 2", sr.Total)
	}
	if len(sr.Issues) != 2 {
		t.Errorf("issues len = %d, want 2", len(sr.Issues))
	}
	if sr.Issues[0].Key != "PROJ-1" {
		t.Errorf("issues[0].key = %q, want 'PROJ-1'", sr.Issues[0].Key)
	}
}
