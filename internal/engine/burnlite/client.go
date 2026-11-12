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
	"sync"
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

// Client calls one Burnlite's tools. It keeps one MCP session for its calls,
// so a run of them costs one handshake: Close it when the run is over.
type Client struct {
	endpoint string
	tokens   TokenSource
	http     *http.Client

	// mu guards the session, and makes the calls of one client take turns.
	mu      sync.Mutex
	session *mcp.ClientSession
	// refused is set when Burnlite answers 401 to a request of the session.
	refused atomic.Bool
}

// Close ends the client's MCP session, if it has one. The client can be used
// again: its next call opens a new session.
func (c *Client) Close() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.drop()
}

// drop ends the session. The caller holds mu.
func (c *Client) drop() {
	if c.session != nil {
		_ = c.session.Close()
		c.session = nil
	}
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
	return c.listTasks(ctx, map[string]any{"projectId": project, "state": state})
}

// ListTasksSince lists the Tasks of a Project, in a state, whose updatedAt
// is after since: what changed, and nothing when nothing did. A Task that
// was deleted is not among them, so only ListTasks says a Task is gone.
func (c *Client) ListTasksSince(ctx context.Context, project, state string, since time.Time) ([]TaskRow, error) {
	return c.listTasks(ctx, map[string]any{"projectId": project, "state": state,
		"updated_since": since.UTC().Format("2006-01-02T15:04:05.000Z")})
}

func (c *Client) listTasks(ctx context.Context, args map[string]any) ([]TaskRow, error) {
	var out struct {
		Tasks []wireRow `json:"tasks"`
	}
	if err := c.call(ctx, "list_tasks", args, &out); err != nil {
		return nil, err
	}
	return readRows(out.Tasks), nil
}

// MilestoneRow is a Milestone as list_milestones gives it: no text and no
// Tasks. UpdatedAt is when the Milestone or any of its Tasks last changed;
// it is zero from a Burnlite too old to say.
type MilestoneRow struct {
	Key       string
	UpdatedAt time.Time
}

// ListMilestones lists a Project's Milestones, open and closed.
func (c *Client) ListMilestones(ctx context.Context, project string) ([]MilestoneRow, error) {
	var out struct {
		Milestones []struct {
			Key       string `json:"key"`
			UpdatedAt string `json:"updatedAt"`
		} `json:"milestones"`
	}
	if err := c.call(ctx, "list_milestones", map[string]any{"projectId": project, "state": "any"}, &out); err != nil {
		return nil, err
	}
	rows := make([]MilestoneRow, len(out.Milestones))
	for i, m := range out.Milestones {
		rows[i] = MilestoneRow{Key: m.Key, UpdatedAt: parseTime(m.UpdatedAt)}
	}
	return rows, nil
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
	// UpdatedAt is when the Milestone or any of its Tasks last changed;
	// zero from a Burnlite too old to say.
	UpdatedAt time.Time
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
			UpdatedAt   string    `json:"updatedAt"`
		} `json:"milestone"`
	}
	if err := c.call(ctx, "get_milestone", map[string]any{"milestoneId": key}, &out); err != nil {
		return nil, err
	}
	w := out.Milestone
	return &Milestone{Key: w.Key, URL: w.URL, Name: w.Name, State: w.State, CloseReason: deref(w.CloseReason),
		Description: deref(w.Description), Labels: w.Labels, Tasks: readRows(w.Tasks), UpdatedAt: parseTime(w.UpdatedAt)}, nil
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
	c.mu.Lock()
	defer c.mu.Unlock()
	result, refused, err := c.callWith(ctx, false, tool, args)
	if refused {
		result, refused, err = c.callWith(ctx, true, tool, args)
		if refused {
			return ErrUnauthorized
		}
	}
	if err != nil {
		return err
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

// callWith runs a tool in the client's session, which it opens when there is
// none; newToken opens a new one with a new token. refused says Burnlite
// answered 401. A call that fails ends the session, so the next one starts
// clean. The caller holds mu.
func (c *Client) callWith(ctx context.Context, newToken bool, tool string, args map[string]any) (*mcp.CallToolResult, bool, error) {
	if newToken {
		c.drop()
	}
	c.refused.Store(false)
	if c.session == nil {
		token, err := c.tokens.Token(ctx, newToken)
		if err != nil {
			return nil, false, fmt.Errorf("burnlite: %w", err)
		}
		session, err := c.connect(ctx, token)
		if err != nil {
			return nil, c.refused.Load(), fmt.Errorf("burnlite: %s: %w", tool, err)
		}
		c.session = session
	}
	result, err := c.session.CallTool(ctx, &mcp.CallToolParams{Name: tool, Arguments: args})
	if err != nil {
		c.drop()
		return nil, c.refused.Load(), fmt.Errorf("burnlite: %s: %w", tool, err)
	}
	return result, false, nil
}

// connect opens an MCP session that sends token. Burnlite's endpoint keeps
// no sessions of its own, so the session is the handshake and nothing more:
// it holds no stream open, and its calls are plain requests.
func (c *Client) connect(ctx context.Context, token string) (*mcp.ClientSession, error) {
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
			c.refused.Store(true)
		}
		return resp, err
	})
	mc := mcp.NewClient(&mcp.Implementation{Name: "legato", Version: "1"}, nil)
	return mc.Connect(ctx, &mcp.StreamableClientTransport{Endpoint: c.endpoint, HTTPClient: &client, MaxRetries: -1, DisableStandaloneSSE: true}, nil)
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
