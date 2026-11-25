package jira

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"sort"
	"strings"
)

// IssueDraft is an issue to create. Description is Markdown, sent as ADF.
// Fields are more fields as Jira names them (customfield_10020), sent as
// they are; the named ones win over them.
type IssueDraft struct {
	Project     string
	Type        string
	Summary     string
	Description string
	Labels      []string
	Priority    string
	Fields      map[string]any
}

// CreatedIssue is an issue Jira created.
type CreatedIssue struct {
	ID  string
	Key string
	URL string
}

// CreatedComment is a comment Jira added, with its address on the issue.
type CreatedComment struct {
	ID  string
	URL string
}

// APIError is an answer of HTTP 400 or more, with what Jira said was wrong.
type APIError struct {
	Status int
	// Messages are Jira's errorMessages, then its errors as "field: message".
	Messages []string
	// Body is the answer when it was not Jira's error JSON.
	Body string
	Path string
}

func (e *APIError) Error() string {
	detail := strings.Join(e.Messages, "; ")
	if detail == "" {
		detail = strings.TrimSpace(e.Body)
	}
	switch e.Status {
	case 401:
		return "authentication failed: invalid credentials"
	case 404:
		if detail == "" {
			return "not found: " + e.Path
		}
		return "not found: " + detail
	}
	return fmt.Sprintf("API error (HTTP %d): %s", e.Status, detail)
}

func newAPIError(status int, path string, body []byte) *APIError {
	e := &APIError{Status: status, Path: path}
	var wire struct {
		ErrorMessages []string          `json:"errorMessages"`
		Errors        map[string]string `json:"errors"`
	}
	if json.Unmarshal(body, &wire) == nil && (len(wire.ErrorMessages) > 0 || len(wire.Errors) > 0) {
		e.Messages = append(e.Messages, wire.ErrorMessages...)
		fields := make([]string, 0, len(wire.Errors))
		for field := range wire.Errors {
			fields = append(fields, field)
		}
		sort.Strings(fields)
		for _, field := range fields {
			e.Messages = append(e.Messages, field+": "+wire.Errors[field])
		}
		return e
	}
	e.Body = string(body)
	return e
}

// CreateIssue creates an issue.
func (c *Client) CreateIssue(ctx context.Context, draft IssueDraft) (CreatedIssue, error) {
	fields := make(map[string]any, len(draft.Fields)+6)
	for name, value := range draft.Fields {
		fields[name] = value
	}
	fields["project"] = map[string]string{"key": draft.Project}
	fields["issuetype"] = map[string]string{"name": draft.Type}
	fields["summary"] = draft.Summary
	if strings.TrimSpace(draft.Description) != "" {
		fields["description"] = MarkdownToADF(draft.Description)
	}
	if len(draft.Labels) > 0 {
		fields["labels"] = draft.Labels
	}
	if draft.Priority != "" {
		fields["priority"] = map[string]string{"name": draft.Priority}
	}
	payload, err := json.Marshal(map[string]any{"fields": fields})
	if err != nil {
		return CreatedIssue{}, fmt.Errorf("create issue: %w", err)
	}
	body, err := c.doPost(ctx, "/rest/api/3/issue", string(payload))
	if err != nil {
		return CreatedIssue{}, fmt.Errorf("create issue: %w", err)
	}
	var created struct {
		ID  string `json:"id"`
		Key string `json:"key"`
	}
	if err := json.Unmarshal(body, &created); err != nil || created.Key == "" {
		return CreatedIssue{}, fmt.Errorf("create issue: reading Jira's answer: %q", body)
	}
	return CreatedIssue{ID: created.ID, Key: created.Key, URL: c.IssueURL(created.Key)}, nil
}

// AddComment adds a Markdown comment, sent as ADF, to an issue.
func (c *Client) AddComment(ctx context.Context, key, markdown string) (CreatedComment, error) {
	payload, err := json.Marshal(map[string]any{"body": MarkdownToADF(markdown)})
	if err != nil {
		return CreatedComment{}, fmt.Errorf("add comment: %w", err)
	}
	body, err := c.doPost(ctx, "/rest/api/3/issue/"+url.PathEscape(key)+"/comment", string(payload))
	if err != nil {
		return CreatedComment{}, fmt.Errorf("add comment: %w", err)
	}
	var created struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(body, &created); err != nil || created.ID == "" {
		return CreatedComment{}, fmt.Errorf("add comment: reading Jira's answer: %q", body)
	}
	return CreatedComment{ID: created.ID, URL: c.IssueURL(key) + "?focusedCommentId=" + url.QueryEscape(created.ID)}, nil
}

// IssueURL is an issue's page.
func (c *Client) IssueURL(key string) string {
	return c.baseURL + "/browse/" + url.PathEscape(key)
}
