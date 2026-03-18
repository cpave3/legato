package jira

import "encoding/json"

// Issue represents a Jira issue from the REST API v3.
type Issue struct {
	Key    string      `json:"key"`
	Fields IssueFields `json:"fields"`
}

// IssueFields contains the fields of a Jira issue.
type IssueFields struct {
	Summary     string          `json:"summary"`
	Description json.RawMessage `json:"description"` // ADF document
	Status      StatusField     `json:"status"`
	Priority    PriorityField   `json:"priority"`
	IssueType   IssueTypeField  `json:"issuetype"`
	Assignee    *UserField      `json:"assignee"`
	Labels      []string        `json:"labels"`
	Updated     string          `json:"updated"`
	Project     ProjectField    `json:"project"`
	EpicKey     string          `json:"customfield_10014"` // Epic Link
}

// StatusField represents the status of a Jira issue.
type StatusField struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// PriorityField represents the priority of a Jira issue.
type PriorityField struct {
	Name string `json:"name"`
}

// IssueTypeField represents the issue type.
type IssueTypeField struct {
	Name string `json:"name"`
}

// UserField represents a Jira user.
type UserField struct {
	DisplayName  string `json:"displayName"`
	EmailAddress string `json:"emailAddress"`
}

// ProjectField represents a Jira project.
type ProjectField struct {
	Key  string `json:"key"`
	Name string `json:"name"`
}

// Transition represents an available workflow transition.
type Transition struct {
	ID   string      `json:"id"`
	Name string      `json:"name"`
	To   StatusField `json:"to"`
}

// TransitionsResponse wraps the transitions list from the API.
type TransitionsResponse struct {
	Transitions []Transition `json:"transitions"`
}

// SearchResult represents the response from the Jira search API.
type SearchResult struct {
	StartAt    int     `json:"startAt"`
	MaxResults int     `json:"maxResults"`
	Total      int     `json:"total"`
	Issues     []Issue `json:"issues"`
}

// JiraProject represents a project from the Jira API.
type JiraProject struct {
	Key  string `json:"key"`
	Name string `json:"name"`
}

// IssueTypeStatuses represents statuses for an issue type in a project.
type IssueTypeStatuses struct {
	Name     string        `json:"name"`
	Statuses []StatusField `json:"statuses"`
}
