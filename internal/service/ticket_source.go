package service

import (
	"context"
	"errors"
	"io"
	"net/url"
	"strings"
	"time"

	"github.com/cpave3/legato/internal/engine/store"
)

// TicketSource is one ticket provider as the sync service uses it: Jira,
// Burnlite. Each owns how its tickets are found, fetched and placed on the
// board; the sync service owns what they share (updating tasks, stale marks,
// the conflict window, attachments, events). A task belongs to the source
// whose Name is its provider.
type TicketSource interface {
	// Name is the provider stored on its tasks ("jira").
	Name() string
	// Label is the provider's name for people ("Jira").
	Label() string
	// Enabled says whether the source can be used now: configured and, for
	// one that needs it, connected. A source that is not is skipped quietly.
	Enabled(ctx context.Context) bool
	// Interval is how often its tracked tasks are pulled.
	Interval() time.Duration
	// MovesCards says whether a change of remote status moves the card.
	MovesCards() bool
	// Pull fetches the tracked tasks' tickets. Missing names the keys the
	// provider no longer has; a key in neither list is left as it is.
	Pull(ctx context.Context, tracked []store.Task) (PullBatch, error)
	// Search finds tickets by text, for import.
	Search(ctx context.Context, text string) ([]RemoteTicket, error)
	// Get fetches one ticket by key; nil when there is none.
	Get(ctx context.Context, key string) (*RemoteTicket, error)
	// Column is the board column for the ticket's status, "" for none.
	Column(ctx context.Context, t RemoteTicket) (string, error)
}

// PullBatch is what a pull found.
type PullBatch struct {
	Tickets []RemoteTicket
	Missing []string
}

// ErrNoTicketProvider: no ticket provider is connected, or not the one named.
var ErrNoTicketProvider = errors.New("no ticket provider is connected to Legato")

// ErrUnknownColumn: an import named a column the board does not have.
var ErrUnknownColumn = errors.New("the board has no such column")

// ErrTicketKeyTaken: a task with the ticket's key exists, from another
// provider or made in Legato.
var ErrTicketKeyTaken = errors.New("a task with this key already exists from another source")

// storeBinder is a source that reads Legato's own store (the Jira source's
// column mappings); the sync service gives it its store.
type storeBinder interface {
	bindStore(*store.Store)
}

// jiraSource is Jira: JQL searches, and statuses placed on the board by the
// column mappings. It is the one source that pushes card moves.
type jiraSource struct {
	provider    TicketProvider
	jql         string
	projectKeys []string
	interval    time.Duration
	statusSync  bool
	site        string
	store       *store.Store
}

// NewJiraSource is the Jira source over a provider. jql narrows the tracked
// tickets; projectKeys narrow searches.
func NewJiraSource(provider TicketProvider, jql string, projectKeys []string, interval time.Duration, statusSync bool) TicketSource {
	return NewJiraSourceWith(provider, JiraSourceOptions{JQL: jql, ProjectKeys: projectKeys, Interval: interval, StatusSync: statusSync})
}

// JiraSourceOptions configure the Jira source.
type JiraSourceOptions struct {
	JQL         string
	ProjectKeys []string
	Interval    time.Duration
	StatusSync  bool
	// Site is the Jira site (jira.base_url), where its tickets are seen.
	Site string
}

// NewJiraSourceWith is the Jira source over a provider.
func NewJiraSourceWith(provider TicketProvider, o JiraSourceOptions) TicketSource {
	return &jiraSource{provider: provider, jql: o.JQL, projectKeys: o.ProjectKeys, interval: o.Interval, statusSync: o.StatusSync, site: o.Site}
}

func (j *jiraSource) bindStore(s *store.Store)     { j.store = s }
func (j *jiraSource) Name() string                 { return "jira" }
func (j *jiraSource) Label() string                { return "Jira" }
func (j *jiraSource) Enabled(context.Context) bool { return j.provider != nil }
func (j *jiraSource) Interval() time.Duration      { return j.interval }
func (j *jiraSource) MovesCards() bool             { return j.statusSync }

func (j *jiraSource) Pull(ctx context.Context, tracked []store.Task) (PullBatch, error) {
	var batch PullBatch
	seen := map[string]bool{}
	for start := 0; start < len(tracked); start += trackedTicketBatchSize {
		end := min(start+trackedTicketBatchSize, len(tracked))
		tickets, err := j.provider.Search(ctx, j.trackedTicketsJQL(tracked[start:end]))
		if err != nil {
			return PullBatch{}, err
		}
		for _, t := range tickets {
			seen[t.ID] = true
		}
		batch.Tickets = append(batch.Tickets, tickets...)
	}
	for _, task := range tracked {
		if !seen[task.ID] {
			batch.Missing = append(batch.Missing, task.ID)
		}
	}
	return batch, nil
}

func (j *jiraSource) trackedTicketsJQL(tasks []store.Task) string {
	keys := make([]string, len(tasks))
	for i, task := range tasks {
		key := task.ID
		if task.RemoteID != nil && *task.RemoteID != "" {
			key = *task.RemoteID
		}
		keys[i] = `"` + strings.ReplaceAll(key, `"`, `\"`) + `"`
	}
	keyFilter := "key in (" + strings.Join(keys, ", ") + ")"
	if strings.TrimSpace(j.jql) != "" {
		return "(" + j.jql + ") AND " + keyFilter
	}
	return keyFilter
}

func (j *jiraSource) Search(ctx context.Context, text string) ([]RemoteTicket, error) {
	return j.provider.Search(ctx, buildSearchJQL(j.projectKeys, text))
}

func (j *jiraSource) Get(ctx context.Context, key string) (*RemoteTicket, error) {
	return j.provider.GetTicket(ctx, key)
}

func (j *jiraSource) Column(ctx context.Context, t RemoteTicket) (string, error) {
	mappings, err := j.store.ListColumnMappings(ctx)
	if err != nil {
		return "", err
	}
	return resolveColumn(t.Status, mappings), nil
}

// Key patterns: what a ticket key of each provider looks like, as a regular
// expression a browser extension can use.
const (
	JiraKeyPattern     = `^[A-Z][A-Z0-9_]*-[0-9]+$`
	BurnliteKeyPattern = `^[A-Z]{2,6}[0-9]{0,4}-[1-9][0-9]*$`
)

// TicketProviderInfo is a connected provider as a browser extension needs
// it: where its tickets are seen, and what their keys look like.
type TicketProviderInfo struct {
	Name       string `json:"name"`
	Label      string `json:"label"`
	WebOrigin  string `json:"web_origin"`
	KeyPattern string `json:"key_pattern"`
}

// TicketProviderLister lists the connected providers that have a site.
type TicketProviderLister interface {
	TicketProviders(ctx context.Context) []TicketProviderInfo
}

// webSite is a source whose tickets are seen at an address.
type webSite interface {
	WebSite(ctx context.Context) string
	KeyPattern() string
}

func (j *jiraSource) WebSite(context.Context) string { return j.site }
func (j *jiraSource) KeyPattern() string             { return JiraKeyPattern }

// origin is an address's scheme and host, "" for one that is not.
func origin(address string) string {
	u, err := url.Parse(address)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return ""
	}
	return u.Scheme + "://" + u.Host
}

// downloader is the source's attachment downloader, if it has one.
func downloader(source TicketSource) (attachmentDownloader, bool) {
	if j, ok := source.(*jiraSource); ok {
		d, ok := j.provider.(attachmentDownloader)
		return d, ok
	}
	d, ok := source.(attachmentDownloader)
	return d, ok
}

type attachmentDownloader interface {
	DownloadAttachment(ctx context.Context, id string) (io.ReadCloser, error)
}
