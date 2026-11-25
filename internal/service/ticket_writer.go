package service

import (
	"context"
	"errors"
	"fmt"
	"math"
	"net/http"
	"strings"
	"time"

	"github.com/cpave3/legato/internal/engine/burnlite"
	"github.com/cpave3/legato/internal/engine/jira"
	"github.com/cpave3/legato/internal/engine/store"
)

// TicketWriter writes to one ticket provider for the ticket.create and
// ticket.comment workflow steps (LEG-640), with Legato's own settings or
// connection: no agent sees its token. It is apart from TicketSource, which
// reads and syncs.
type TicketWriter interface {
	// Provider is jira or burnlite.
	Provider() string
	// Configured is nil when the writer can write, else why it cannot.
	Configured(ctx context.Context) error
	Create(ctx context.Context, draft TicketDraft) (CreatedTicket, error)
	Comment(ctx context.Context, key, markdown string) (TicketComment, error)
	// FindCreated lists the tickets of a project named summary that may
	// have been created since a time: what an attempt that lost its answer
	// may have made.
	FindCreated(ctx context.Context, project, summary string, since time.Time) ([]CreatedTicket, error)
}

// TicketDraft is a ticket to create. Description is Markdown.
type TicketDraft struct {
	Project     string
	Summary     string
	Description string
	Labels      []string
	// Jira only.
	Type     string
	Priority string
	Fields   map[string]any
	// Burnlite only.
	Kind        string
	Size        string
	Uncertainty string
	Milestone   string
}

// CreatedTicket is a ticket a provider created, with its absolute address.
type CreatedTicket struct {
	Provider string
	Key      string
	ID       string
	URL      string
}

// TicketComment is a comment a provider added to a ticket.
type TicketComment struct {
	Provider string
	Key      string
	ID       string
	URL      string
}

// ticketRefused says a provider refused a write, so it made nothing: Jira's
// answer of HTTP 400 to 499, or a Burnlite tool's error. Anything else, a
// timeout or Jira's 5xx, leaves the outcome unknown.
func ticketRefused(err error) bool {
	var jiraErr *jira.APIError
	if errors.As(err, &jiraErr) {
		return jiraErr.Status >= 400 && jiraErr.Status < 500
	}
	var toolErr *burnlite.Error
	return errors.As(err, &toolErr) || errors.Is(err, burnlite.ErrUnauthorized)
}

// JiraTicketWriter writes to the Jira of Legato's settings (jira.base_url,
// jira.email, jira.api_token).
type JiraTicketWriter struct {
	BaseURL  string
	Email    string
	APIToken string
	// Timeout of each request; 30 seconds when zero.
	Timeout time.Duration
}

func (w JiraTicketWriter) Provider() string { return "jira" }

func (w JiraTicketWriter) Configured(context.Context) error {
	var missing []string
	for _, setting := range []struct{ name, value string }{
		{"jira.base_url", w.BaseURL}, {"jira.email", w.Email}, {"jira.api_token", w.APIToken},
	} {
		if strings.TrimSpace(setting.value) == "" {
			missing = append(missing, setting.name)
		}
	}
	if len(missing) == 0 {
		return nil
	}
	verb := "is"
	if len(missing) > 1 {
		verb = "are"
	}
	return fmt.Errorf("Jira is not set up: %s %s empty (Settings › Integrations, or config.yaml)", strings.Join(missing, ", "), verb)
}

func (w JiraTicketWriter) client() *jira.Client {
	timeout := w.Timeout
	if timeout == 0 {
		timeout = 30 * time.Second
	}
	return jira.NewClient(w.BaseURL, w.Email, w.APIToken, timeout)
}

func (w JiraTicketWriter) Create(ctx context.Context, d TicketDraft) (CreatedTicket, error) {
	issue, err := w.client().CreateIssue(ctx, jira.IssueDraft{Project: d.Project, Type: d.Type, Summary: d.Summary,
		Description: d.Description, Labels: d.Labels, Priority: d.Priority, Fields: d.Fields})
	if err != nil {
		return CreatedTicket{}, err
	}
	return CreatedTicket{Provider: "jira", Key: issue.Key, ID: issue.ID, URL: issue.URL}, nil
}

func (w JiraTicketWriter) Comment(ctx context.Context, key, markdown string) (TicketComment, error) {
	comment, err := w.client().AddComment(ctx, key, markdown)
	if err != nil {
		return TicketComment{}, err
	}
	return TicketComment{Provider: "jira", Key: key, ID: comment.ID, URL: comment.URL}, nil
}

// FindCreated searches the issues Legato's Jira user created in the project
// since then (a minute's slack for clocks), and keeps those of that exact
// summary. Minutes back, not a date, since Jira reads a date in the user's
// time zone.
func (w JiraTicketWriter) FindCreated(ctx context.Context, project, summary string, since time.Time) ([]CreatedTicket, error) {
	minutes := int(math.Ceil(time.Since(since).Minutes())) + 1
	jql := fmt.Sprintf(`project = %s AND creator = currentUser() AND created >= -%dm ORDER BY created ASC`, jqlString(project), minutes)
	client := w.client()
	issues, err := client.Search(ctx, jql)
	if err != nil {
		return nil, err
	}
	var found []CreatedTicket
	for _, issue := range issues {
		if issue.Fields.Summary == summary {
			found = append(found, CreatedTicket{Provider: "jira", Key: issue.Key, ID: issue.ID, URL: client.IssueURL(issue.Key)})
		}
	}
	return found, nil
}

func jqlString(s string) string {
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(s) + `"`
}

// BurnliteTicketWriter writes to the Burnlite Legato is connected to, as
// the holder legato.
type BurnliteTicketWriter struct {
	Connection BurnliteConnection
	// HTTP, when set, replaces the connection's client (tests).
	HTTP *http.Client
}

const burnliteHolder = "legato"

func (w BurnliteTicketWriter) Provider() string { return BurnliteIntegration }

func (w BurnliteTicketWriter) Configured(ctx context.Context) error {
	if w.Connection == nil {
		return errBurnliteNotConnected
	}
	link, err := w.Connection.Current(ctx)
	if err != nil {
		return err
	}
	if link.State != store.IntegrationConnected {
		return errBurnliteNotConnected
	}
	return nil
}

var errBurnliteNotConnected = fmt.Errorf("Legato is not connected to Burnlite (Settings › Integrations, or legato integration connect burnlite): %w", ErrNotConnected)

func (w BurnliteTicketWriter) open(ctx context.Context) (*burnlite.Client, string, error) {
	if w.Connection == nil {
		return nil, "", errBurnliteNotConnected
	}
	client, base, err := openBurnlite(ctx, w.Connection, w.HTTP)
	return client, strings.TrimRight(base, "/"), err
}

// absolute is Burnlite's relative address (/LEG-9) on its site.
func absolute(base, url string) string {
	if strings.HasPrefix(url, "/") {
		return base + url
	}
	return url
}

func (w BurnliteTicketWriter) Create(ctx context.Context, d TicketDraft) (CreatedTicket, error) {
	client, base, err := w.open(ctx)
	if err != nil {
		return CreatedTicket{}, err
	}
	defer client.Close()
	task, err := client.CreateTask(ctx, burnlite.TaskDraft{Project: d.Project, Name: d.Summary, Description: d.Description,
		Labels: d.Labels, Kind: d.Kind, Size: d.Size, Uncertainty: d.Uncertainty, Milestone: d.Milestone, Holder: burnliteHolder})
	if err != nil {
		return CreatedTicket{}, err
	}
	return CreatedTicket{Provider: BurnliteIntegration, Key: task.Key, ID: task.ID, URL: absolute(base, task.URL)}, nil
}

// Comment adds a Note. A Note has no address of its own, so URL is the
// Task's.
func (w BurnliteTicketWriter) Comment(ctx context.Context, key, markdown string) (TicketComment, error) {
	client, base, err := w.open(ctx)
	if err != nil {
		return TicketComment{}, err
	}
	defer client.Close()
	id, err := client.AddNote(ctx, key, markdown, burnliteHolder)
	if err != nil {
		return TicketComment{}, err
	}
	return TicketComment{Provider: BurnliteIntegration, Key: key, ID: id, URL: base + "/" + key}, nil
}

func (w BurnliteTicketWriter) FindCreated(ctx context.Context, project, summary string, since time.Time) ([]CreatedTicket, error) {
	client, base, err := w.open(ctx)
	if err != nil {
		return nil, err
	}
	defer client.Close()
	rows, err := client.ListTasksSince(ctx, project, "any", since.Add(-time.Minute))
	if err != nil {
		return nil, err
	}
	var found []CreatedTicket
	for _, row := range rows {
		if row.Name == summary {
			found = append(found, CreatedTicket{Provider: BurnliteIntegration, Key: row.Key, ID: row.ID, URL: absolute(base, row.URL)})
		}
	}
	return found, nil
}
