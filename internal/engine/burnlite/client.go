// Package burnlite reads Burnlite (an issue tracker and estimator) over its
// MCP endpoint, the one API a token holder can use: Projects, their Tasks,
// and one Task with its text. It knows nothing of Legato, and gets its
// bearer token from whoever made it.
package burnlite

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync/atomic"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// TokenSource gives the access token to send. force asks for a new one, after
// Burnlite refused the one in hand.
type TokenSource interface {
	Token(ctx context.Context, force bool) (string, error)
}

var (
	// ErrNotFound: Burnlite has no such Project or Task.
	ErrNotFound = errors.New("burnlite: not found")
	// ErrUnauthorized: Burnlite refused Legato's token, a fresh one too.
	ErrUnauthorized = errors.New("burnlite: authentication failed")
)

// Error is an error a Burnlite tool returned, with its code (not_found,
// validation, insufficient_scope, ...).
type Error struct {
	Code    string
	Message string
}

func (e *Error) Error() string {
	if e.Code == "insufficient_scope" {
		return "burnlite: authentication failed: " + e.Message
	}
	return "burnlite: " + e.Message
}

func (e *Error) Is(target error) bool {
	switch target {
	case ErrNotFound:
		return e.Code == "not_found"
	case ErrUnauthorized:
		return e.Code == "insufficient_scope"
	}
	return false
}

// Client calls one Burnlite's tools.
type Client struct {
	endpoint string
	tokens   TokenSource
	http     *http.Client
}

// New is a client of the Burnlite at baseURL (https://burnlite.example.com).
// httpClient may be nil.
func New(baseURL string, tokens TokenSource, httpClient *http.Client) *Client {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 30 * time.Second}
	}
	return &Client{endpoint: strings.TrimRight(baseURL, "/") + "/api/llm/mcp", tokens: tokens, http: httpClient}
}

// Project is a Burnlite Project.
type Project struct {
	ID   string `json:"id"`
	Key  string `json:"key"`
	Name string `json:"name"`
}

// TaskRow is a Task as list_tasks gives it: no text.
type TaskRow struct {
	Key               string
	URL               string
	Name              string
	Kind              string
	MilestoneKey      string
	MilestoneName     string
	State             string
	CloseReason       string
	ResolutionSummary string
	Labels            []string
	Blocked           bool
	OpenBlockers      []string
	ClaimHolder       string
	Frontier          bool
	UpdatedAt         time.Time
}

// Task is one Task with its text.
type Task struct {
	Key               string
	URL               string
	Name              string
	Kind              string
	Size              string
	Uncertainty       string
	State             string
	CloseReason       string
	ResolutionSummary string
	Description       string
	Plan              string
	Labels            []string
	Blocked           bool
	Blockers          []Link
	ClaimHolder       string
	Checklist         []ChecklistItem
	UpdatedAt         time.Time
}

// Link is another Task, as a Blocker.
type Link struct {
	Key   string
	Name  string
	State string
}

// ChecklistItem is a Task's Checklist item.
type ChecklistItem struct {
	Name string
	Done bool
}

// ListProjects lists the active Projects.
func (c *Client) ListProjects(ctx context.Context) ([]Project, error) {
	var out struct {
		Projects []Project `json:"projects"`
	}
	if err := c.call(ctx, "list_projects", map[string]any{}, &out); err != nil {
		return nil, err
	}
	return out.Projects, nil
}

type wireClaim struct {
	Holder string `json:"holder"`
}

type wireRow struct {
	Key               string     `json:"key"`
	URL               string     `json:"url"`
	TaskName          string     `json:"taskName"`
	Kind              string     `json:"kind"`
	MilestoneKey      *string    `json:"milestoneKey"`
	MilestoneName     *string    `json:"milestoneName"`
	State             string     `json:"state"`
	CloseReason       *string    `json:"closeReason"`
	ResolutionSummary *string    `json:"resolutionSummary"`
	Labels            []string   `json:"labels"`
	Blocked           bool       `json:"blocked"`
	OpenBlockers      []string   `json:"openBlockers"`
	Claim             *wireClaim `json:"claim"`
	Frontier          bool       `json:"frontier"`
	UpdatedAt         string     `json:"updatedAt"`
}

// ListTasks lists a Project's Tasks (project is its key or id) in a state:
// open, closed or any.
func (c *Client) ListTasks(ctx context.Context, project, state string) ([]TaskRow, error) {
	var out struct {
		Tasks []wireRow `json:"tasks"`
	}
	if err := c.call(ctx, "list_tasks", map[string]any{"projectId": project, "state": state}, &out); err != nil {
		return nil, err
	}
	return readRows(out.Tasks), nil
}

func readRows(wire []wireRow) []TaskRow {
	rows := make([]TaskRow, len(wire))
	for i, w := range wire {
		rows[i] = TaskRow{Key: w.Key, URL: w.URL, Name: w.TaskName, Kind: w.Kind, MilestoneKey: deref(w.MilestoneKey),
			MilestoneName: deref(w.MilestoneName), State: w.State, CloseReason: deref(w.CloseReason),
			ResolutionSummary: deref(w.ResolutionSummary), Labels: w.Labels, Blocked: w.Blocked, OpenBlockers: w.OpenBlockers,
			Frontier: w.Frontier, UpdatedAt: parseTime(w.UpdatedAt)}
		if w.Claim != nil {
			rows[i].ClaimHolder = w.Claim.Holder
		}
	}
	return rows
}

// Milestone is one Milestone with its Description and its Tasks, in
// Burnlite's order. A Milestone's key is from the same series as its
// project's Task keys, and get_task refuses it as not_found.
type Milestone struct {
	Key         string
	URL         string
	Name        string
	State       string
	CloseReason string
	Description string
	Labels      []string
	Tasks       []TaskRow
}

// GetMilestone reads a Milestone by its key.
func (c *Client) GetMilestone(ctx context.Context, key string) (*Milestone, error) {
	var out struct {
		Milestone struct {
			Key         string    `json:"key"`
			URL         string    `json:"url"`
			Name        string    `json:"name"`
			State       string    `json:"state"`
			CloseReason *string   `json:"closeReason"`
			Description *string   `json:"description"`
			Labels      []string  `json:"labels"`
			Tasks       []wireRow `json:"tasks"`
		} `json:"milestone"`
	}
	if err := c.call(ctx, "get_milestone", map[string]any{"milestoneId": key}, &out); err != nil {
		return nil, err
	}
	w := out.Milestone
	return &Milestone{Key: w.Key, URL: w.URL, Name: w.Name, State: w.State, CloseReason: deref(w.CloseReason),
		Description: deref(w.Description), Labels: w.Labels, Tasks: readRows(w.Tasks)}, nil
}

// GetTask gets one Task by key (EST-42) or id.
func (c *Client) GetTask(ctx context.Context, key string) (*Task, error) {
	var out struct {
		Task struct {
			Key               string     `json:"key"`
			URL               string     `json:"url"`
			TaskName          string     `json:"taskName"`
			Kind              string     `json:"kind"`
			Size              *string    `json:"size"`
			UncertaintyLevel  *string    `json:"uncertaintyLevel"`
			State             string     `json:"state"`
			CloseReason       *string    `json:"closeReason"`
			ResolutionSummary *string    `json:"resolutionSummary"`
			Description       *string    `json:"description"`
			Plan              *string    `json:"plan"`
			Labels            []string   `json:"labels"`
			Blocked           bool       `json:"blocked"`
			Claim             *wireClaim `json:"claim"`
			UpdatedAt         string     `json:"updatedAt"`
			Blockers          []struct {
				Key      string `json:"key"`
				TaskName string `json:"taskName"`
				State    string `json:"state"`
			} `json:"blockers"`
			ChecklistItems []struct {
				Name         string  `json:"name"`
				Done         bool    `json:"done"`
				DisplayOrder float64 `json:"displayOrder"`
			} `json:"checklistItems"`
		} `json:"task"`
	}
	if err := c.call(ctx, "get_task", map[string]any{"taskId": key}, &out); err != nil {
		return nil, err
	}
	w := out.Task
	task := &Task{Key: w.Key, URL: w.URL, Name: w.TaskName, Kind: w.Kind, Size: deref(w.Size), Uncertainty: deref(w.UncertaintyLevel),
		State: w.State, CloseReason: deref(w.CloseReason), ResolutionSummary: deref(w.ResolutionSummary),
		Description: deref(w.Description), Plan: deref(w.Plan), Labels: w.Labels, Blocked: w.Blocked, UpdatedAt: parseTime(w.UpdatedAt)}
	if w.Claim != nil {
		task.ClaimHolder = w.Claim.Holder
	}
	for _, b := range w.Blockers {
		task.Blockers = append(task.Blockers, Link{Key: b.Key, Name: b.TaskName, State: b.State})
	}
	items := w.ChecklistItems
	sort.SliceStable(items, func(i, j int) bool { return items[i].DisplayOrder < items[j].DisplayOrder })
	for _, item := range items {
		task.Checklist = append(task.Checklist, ChecklistItem{Name: item.Name, Done: item.Done})
	}
	return task, nil
}

// call runs a tool and decodes its structured result into out. A token
// Burnlite refuses is replaced once.
func (c *Client) call(ctx context.Context, tool string, args map[string]any, out any) error {
	token, err := c.tokens.Token(ctx, false)
	if err != nil {
		return fmt.Errorf("burnlite: %w", err)
	}
	result, refused, err := c.callWith(ctx, token, tool, args)
	if refused {
		if token, err = c.tokens.Token(ctx, true); err != nil {
			return fmt.Errorf("burnlite: %w", err)
		}
		result, refused, err = c.callWith(ctx, token, tool, args)
		if refused {
			return ErrUnauthorized
		}
	}
	if err != nil {
		return fmt.Errorf("burnlite: %s: %w", tool, err)
	}
	if result.IsError {
		return toolError(result)
	}
	raw, err := json.Marshal(result.StructuredContent)
	if err != nil {
		return fmt.Errorf("burnlite: %s: %w", tool, err)
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("burnlite: %s: reading its result: %w", tool, err)
	}
	return nil
}

// callWith runs a tool with a token. refused says Burnlite answered 401.
// Burnlite's endpoint keeps no sessions, so each call is a session of its own.
func (c *Client) callWith(ctx context.Context, token, tool string, args map[string]any) (*mcp.CallToolResult, bool, error) {
	var status atomic.Int32
	client := *c.http
	base := client.Transport
	if base == nil {
		base = http.DefaultTransport
	}
	client.Transport = roundTripper(func(r *http.Request) (*http.Response, error) {
		r = r.Clone(r.Context())
		r.Header.Set("Authorization", "Bearer "+token)
		resp, err := base.RoundTrip(r)
		if resp != nil && resp.StatusCode == http.StatusUnauthorized {
			status.Store(http.StatusUnauthorized)
		}
		return resp, err
	})
	mc := mcp.NewClient(&mcp.Implementation{Name: "legato", Version: "1"}, nil)
	session, err := mc.Connect(ctx, &mcp.StreamableClientTransport{Endpoint: c.endpoint, HTTPClient: &client, MaxRetries: -1, DisableStandaloneSSE: true}, nil)
	if err != nil {
		return nil, status.Load() == http.StatusUnauthorized, err
	}
	defer session.Close()
	result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: tool, Arguments: args})
	if err != nil {
		return nil, status.Load() == http.StatusUnauthorized, err
	}
	return result, false, nil
}

type roundTripper func(*http.Request) (*http.Response, error)

func (f roundTripper) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// toolError reads Burnlite's {code, message} from a failed call.
func toolError(result *mcp.CallToolResult) error {
	var text string
	for _, content := range result.Content {
		if t, ok := content.(*mcp.TextContent); ok {
			text += t.Text
		}
	}
	var e Error
	if json.Unmarshal([]byte(text), &e) == nil && e.Code != "" {
		return &e
	}
	if text == "" {
		text = "the tool failed"
	}
	return &Error{Code: "unknown", Message: text}
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func parseTime(s string) time.Time {
	t, _ := time.Parse(time.RFC3339Nano, s)
	return t
}
