package jira

import "encoding/json"

// Issue represents a Jira issue from the REST API v3.
type Issue struct {
	ID     string      `json:"id"`
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
	Attachments []Attachment    `json:"attachment"`
	// Parent is the issue's parent, such as its epic in a team-managed
	// project, where there is no Epic Link.
	Parent *LinkedIssue `json:"parent"`
	// IssueLinks are its links to other issues, blockers among them.
	IssueLinks []IssueLink `json:"issuelinks"`
}

// IssueLink is a link between two issues. On the issue that holds it,
// InwardIssue is the other end read with Type.Inward ("is blocked by"),
// OutwardIssue with Type.Outward ("blocks").
type IssueLink struct {
	Type         IssueLinkType `json:"type"`
	InwardIssue  *LinkedIssue  `json:"inwardIssue"`
	OutwardIssue *LinkedIssue  `json:"outwardIssue"`
}

// IssueLinkType names a kind of link and how each end reads it.
type IssueLinkType struct {
	Name    string `json:"name"`
	Inward  string `json:"inward"`
	Outward string `json:"outward"`
}

// LinkedIssue is another issue as a link or a parent gives it.
type LinkedIssue struct {
	Key    string `json:"key"`
	Fields struct {
		Status    StatusField    `json:"status"`
		IssueType IssueTypeField `json:"issuetype"`
	} `json:"fields"`
}

// Attachment describes a file attached to a Jira issue.
type Attachment struct {
	ID       string `json:"id"`
	Filename string `json:"filename"`
	MimeType string `json:"mimeType"`
	Size     int64  `json:"size"`
}

// StatusField represents the status of a Jira issue.
type StatusField struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	// StatusCategory is new, indeterminate or done, whatever the status
	// is called.
	StatusCategory struct {
		Key string `json:"key"`
	} `json:"statusCategory"`
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
	StartAt       int     `json:"startAt"`
	MaxResults    int     `json:"maxResults"`
	Total         int     `json:"total"`
	Issues        []Issue `json:"issues"`
	IsLast        bool    `json:"isLast"`
	NextPageToken string  `json:"nextPageToken"`
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
