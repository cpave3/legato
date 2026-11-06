package service

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/cpave3/legato/internal/engine/burnlite"
	"github.com/cpave3/legato/internal/engine/store"
)

// BurnliteIntegration is Burnlite's name as an integration and as a provider.
const BurnliteIntegration = "burnlite"

// BurnliteColumns are the board columns of a Burnlite task's states.
type BurnliteColumns struct {
	Ready   string
	Blocked string
	Claimed string
	Closed  string
}

// BurnliteSourceOptions configure the Burnlite source.
type BurnliteSourceOptions struct {
	// Projects are the project keys searched for import; none is every one.
	Projects   func(ctx context.Context) []string
	Columns    BurnliteColumns
	Interval   time.Duration
	MovesCards bool
	HTTP       *http.Client
}

// burnliteReader is what the source reads from Burnlite.
type burnliteReader interface {
	ListProjects(ctx context.Context) ([]burnlite.Project, error)
	ListTasks(ctx context.Context, project, state string) ([]burnlite.TaskRow, error)
	GetTask(ctx context.Context, key string) (*burnlite.Task, error)
	GetMilestone(ctx context.Context, key string) (*burnlite.Milestone, error)
}

// burnliteOpener gives a reader of the connected Burnlite and its address,
// or ErrNotConnected.
type burnliteOpener func(ctx context.Context) (burnliteReader, string, error)

// burnliteSource is Burnlite, read-only: tasks are imported and kept up to
// date, and a card moved on the board never changes the task. A task's
// state places its card: closed, else claimed, else blocked, else ready.
type burnliteSource struct {
	opts BurnliteSourceOptions
	open burnliteOpener
}

// NewBurnliteSource is the Burnlite that Legato is connected to (the
// integration "burnlite"), with the tokens of that connection.
func NewBurnliteSource(db *store.Store, tokens burnlite.TokenSource, opts BurnliteSourceOptions) TicketSource {
	return newBurnliteSource(opts, func(ctx context.Context) (burnliteReader, string, error) {
		in, err := db.GetIntegration(ctx, BurnliteIntegration)
		if errors.Is(err, store.ErrNotFound) || (err == nil && in.State != store.IntegrationConnected) {
			return nil, "", fmt.Errorf("Legato is not connected to Burnlite: %w", ErrNotConnected)
		}
		if err != nil {
			return nil, "", err
		}
		return burnlite.New(in.URL, tokens, opts.HTTP), in.URL, nil
	})
}

func newBurnliteSource(opts BurnliteSourceOptions, open burnliteOpener) *burnliteSource {
	return &burnliteSource{opts: opts, open: open}
}

func (b *burnliteSource) Name() string            { return BurnliteIntegration }
func (b *burnliteSource) Label() string           { return "Burnlite" }
func (b *burnliteSource) Interval() time.Duration { return b.opts.Interval }
func (b *burnliteSource) MovesCards() bool        { return b.opts.MovesCards }

// WebSite is the connected Burnlite's address.
func (b *burnliteSource) WebSite(ctx context.Context) string {
	_, base, err := b.open(ctx)
	if err != nil {
		return ""
	}
	return base
}

func (b *burnliteSource) KeyPattern() string { return BurnliteKeyPattern }

func (b *burnliteSource) Enabled(ctx context.Context) bool {
	_, _, err := b.open(ctx)
	return err == nil
}

// Burnlite task states, as Legato shows them.
const (
	burnliteReady      = "Ready"
	burnliteBlocked    = "Blocked"
	burnliteClaimed    = "Claimed"
	burnliteClosed     = "Closed"
	burnliteNotPlanned = "Not planned"
)

func burnliteState(state, closeReason, claimHolder string, blocked bool) string {
	switch {
	case state == "closed" && closeReason == "not_planned":
		return burnliteNotPlanned
	case state == "closed":
		return burnliteClosed
	case claimHolder != "":
		return burnliteClaimed
	case blocked:
		return burnliteBlocked
	}
	return burnliteReady
}

func (b *burnliteSource) Column(_ context.Context, t RemoteTicket) (string, error) {
	switch t.Status {
	case burnliteReady:
		return b.opts.Columns.Ready, nil
	case burnliteBlocked:
		return b.opts.Columns.Blocked, nil
	case burnliteClaimed:
		return b.opts.Columns.Claimed, nil
	case burnliteClosed, burnliteNotPlanned:
		return b.opts.Columns.Closed, nil
	}
	return "", nil
}

// burnliteProjectsKey is where the web UI saves Burnlite's projects.
const burnliteProjectsKey = "burnlite.projects"

var burnliteProjectKey = regexp.MustCompile(`^[A-Z]{2,6}[0-9]{0,4}$`)

// BurnliteProjects are the projects searched for import: configured (from
// burnlite.projects) when there are any, else those the web UI saved.
func BurnliteProjects(ctx context.Context, db *store.Store, configured []string) []string {
	if len(configured) > 0 {
		return configured
	}
	raw, err := db.GetInstanceValue(ctx, burnliteProjectsKey)
	if err != nil || raw == "" {
		return nil
	}
	return strings.Split(raw, ",")
}

// SaveBurnliteProjects keeps the projects the web UI was given, as upper
// case keys without repeats. None searches every project.
func SaveBurnliteProjects(ctx context.Context, db *store.Store, projects []string) error {
	var keys []string
	seen := map[string]bool{}
	for _, p := range projects {
		p = strings.ToUpper(strings.TrimSpace(p))
		if p == "" || seen[p] {
			continue
		}
		if !burnliteProjectKey.MatchString(p) {
			return fmt.Errorf("%q is not a Burnlite project key: two to six letters, then up to four digits, like EST", p)
		}
		seen[p] = true
		keys = append(keys, p)
	}
	return db.SetInstanceValue(ctx, burnliteProjectsKey, strings.Join(keys, ","))
}

// projectOf is a key's project: EST in EST-42.
func projectOf(key string) string {
	if i := strings.LastIndex(key, "-"); i > 0 {
		return key[:i]
	}
	return key
}

var burnliteKey = regexp.MustCompile(`^[A-Za-z]{2,6}[0-9]{0,4}-[1-9][0-9]*$`)

// Pull lists each project of the tracked tasks once and fetches in full only
// the tasks whose updatedAt is newer than the one Legato holds.
func (b *burnliteSource) Pull(ctx context.Context, tracked []store.Task) (PullBatch, error) {
	reader, base, err := b.open(ctx)
	if err != nil {
		return PullBatch{}, err
	}
	byProject := map[string][]store.Task{}
	var projects []string
	for _, task := range tracked {
		p := projectOf(task.ID)
		if _, ok := byProject[p]; !ok {
			projects = append(projects, p)
		}
		byProject[p] = append(byProject[p], task)
	}
	var batch PullBatch
	for _, project := range projects {
		rows, err := reader.ListTasks(ctx, project, "any")
		if errors.Is(err, burnlite.ErrNotFound) {
			// The project is gone, and its tasks with it.
			for _, task := range byProject[project] {
				batch.Missing = append(batch.Missing, task.ID)
			}
			continue
		}
		if err != nil {
			return PullBatch{}, err
		}
		byKey := make(map[string]burnlite.TaskRow, len(rows))
		for _, row := range rows {
			byKey[row.Key] = row
		}
		for _, task := range byProject[project] {
			row, ok := byKey[task.ID]
			if !ok && parseRemoteMeta(task.RemoteMeta).IssueType == burnliteMilestone {
				ticket, found, err := b.pullMilestone(ctx, reader, base, task)
				if err != nil {
					return PullBatch{}, err
				}
				if found {
					batch.Tickets = append(batch.Tickets, ticket)
				} else {
					batch.Missing = append(batch.Missing, task.ID)
				}
				continue
			}
			if !ok {
				batch.Missing = append(batch.Missing, task.ID)
				continue
			}
			if row.UpdatedAt.IsZero() {
				return PullBatch{}, errors.New("this Burnlite gives its tasks no updatedAt, which Legato needs to sync them: update Burnlite")
			}
			held, _ := time.Parse(time.RFC3339Nano, parseRemoteMeta(task.RemoteMeta).RemoteUpdatedAt)
			if !row.UpdatedAt.After(held) {
				// Unchanged: the service leaves it as it is.
				batch.Tickets = append(batch.Tickets, rowTicket(row, base))
				continue
			}
			full, err := reader.GetTask(ctx, row.Key)
			if errors.Is(err, burnlite.ErrNotFound) {
				batch.Missing = append(batch.Missing, task.ID)
				continue
			}
			if err != nil {
				return PullBatch{}, err
			}
			batch.Tickets = append(batch.Tickets, fullTicket(row, full, base))
		}
	}
	return batch, nil
}

// pullMilestone reads a Milestone again. Burnlite gives it no updatedAt,
// and a Task leaving it changes no timestamp it still lists, so the ticket
// is dated now when what Legato shows of it has changed, and keeps the date
// Legato holds when it has not.
func (b *burnliteSource) pullMilestone(ctx context.Context, reader burnliteReader, base string, task store.Task) (RemoteTicket, bool, error) {
	m, err := reader.GetMilestone(ctx, task.ID)
	if errors.Is(err, burnlite.ErrNotFound) {
		return RemoteTicket{}, false, nil
	}
	if err != nil {
		return RemoteTicket{}, false, err
	}
	meta := parseRemoteMeta(task.RemoteMeta)
	held, _ := time.Parse(time.RFC3339Nano, meta.RemoteUpdatedAt)
	t := milestoneTicket(m, base, held)
	if t.Summary != task.Title || t.DescriptionMD != task.RemoteDescriptionMD || t.Status != meta.RemoteStatus ||
		strings.Join(t.Labels, ",") != meta.Labels || t.URL != meta.URL {
		t.UpdatedAt = time.Now()
		if !t.UpdatedAt.After(held) {
			t.UpdatedAt = held.Add(time.Millisecond)
		}
	}
	return t, true, nil
}

// searchLimit is the most results a search gives.
const searchLimit = 50

// Search finds open tasks by name in the projects (every project when none
// is configured), and any task by its key.
func (b *burnliteSource) Search(ctx context.Context, text string) ([]RemoteTicket, error) {
	reader, base, err := b.open(ctx)
	if err != nil {
		return nil, err
	}
	text = strings.TrimSpace(text)
	var results []RemoteTicket
	seen := map[string]bool{}
	if burnliteKey.MatchString(text) {
		key := strings.ToUpper(text)
		if t, err := b.get(ctx, reader, base, key); err == nil && t != nil {
			results = append(results, *t)
			seen[t.ID] = true
		}
	}
	var projects []string
	if b.opts.Projects != nil {
		projects = b.opts.Projects(ctx)
	}
	if len(projects) == 0 {
		all, err := reader.ListProjects(ctx)
		if err != nil {
			return nil, err
		}
		for _, p := range all {
			projects = append(projects, p.Key)
		}
	}
	needle := strings.ToLower(text)
	for _, project := range projects {
		rows, err := reader.ListTasks(ctx, project, "open")
		if err != nil {
			return nil, err
		}
		for _, row := range rows {
			if seen[row.Key] || !(strings.Contains(strings.ToLower(row.Name), needle) || strings.EqualFold(row.Key, text)) {
				continue
			}
			seen[row.Key] = true
			results = append(results, rowTicket(row, base))
			if len(results) == searchLimit {
				return results, nil
			}
		}
	}
	return results, nil
}

// Get fetches a task in full, with its milestone from its project's list.
func (b *burnliteSource) Get(ctx context.Context, key string) (*RemoteTicket, error) {
	reader, base, err := b.open(ctx)
	if err != nil {
		return nil, err
	}
	return b.get(ctx, reader, base, key)
}

func (b *burnliteSource) get(ctx context.Context, reader burnliteReader, base, key string) (*RemoteTicket, error) {
	full, err := reader.GetTask(ctx, key)
	if errors.Is(err, burnlite.ErrNotFound) {
		// Milestones share the series of keys, and get_task refuses them.
		m, err := reader.GetMilestone(ctx, key)
		if errors.Is(err, burnlite.ErrNotFound) {
			return nil, nil
		}
		if err != nil {
			return nil, err
		}
		t := milestoneTicket(m, base, time.Now())
		return &t, nil
	}
	if err != nil {
		return nil, err
	}
	row := burnlite.TaskRow{Key: full.Key, URL: full.URL, Name: full.Name, Kind: full.Kind, State: full.State, CloseReason: full.CloseReason,
		Labels: full.Labels, Blocked: full.Blocked, ClaimHolder: full.ClaimHolder, UpdatedAt: full.UpdatedAt}
	// The milestone is only in the list.
	if rows, err := reader.ListTasks(ctx, projectOf(full.Key), "any"); err == nil {
		for _, r := range rows {
			if r.Key == full.Key {
				row.MilestoneKey, row.MilestoneName = r.MilestoneKey, r.MilestoneName
				break
			}
		}
	}
	t := fullTicket(row, full, base)
	return &t, nil
}

// rowTicket is a ticket from a list row: everything but the text.
func rowTicket(row burnlite.TaskRow, base string) RemoteTicket {
	return RemoteTicket{
		ID:        row.Key,
		Summary:   row.Name,
		Status:    burnliteState(row.State, row.CloseReason, row.ClaimHolder, row.Blocked),
		IssueType: kindLabel(row.Kind),
		Assignee:  row.ClaimHolder,
		Labels:    row.Labels,
		EpicKey:   row.MilestoneKey,
		EpicName:  row.MilestoneName,
		URL:       strings.TrimRight(base, "/") + row.URL,
		UpdatedAt: row.UpdatedAt,
		BlockedBy: row.OpenBlockers,
	}
}

// fullTicket is a ticket from a list row and the task's own text.
func fullTicket(row burnlite.TaskRow, task *burnlite.Task, base string) RemoteTicket {
	t := rowTicket(row, base)
	t.Summary = task.Name
	t.Status = burnliteState(task.State, task.CloseReason, task.ClaimHolder, task.Blocked)
	t.Assignee = task.ClaimHolder
	t.Labels = task.Labels
	t.UpdatedAt = task.UpdatedAt
	t.DescriptionMD = burnliteDescription(task)
	t.Size, t.Uncertainty = task.Size, task.Uncertainty
	t.Resolution = task.ResolutionSummary
	t.BlockedBy = nil
	for _, blocker := range task.Blockers {
		if blocker.State == "open" {
			t.BlockedBy = append(t.BlockedBy, blocker.Key)
		}
	}
	return t
}

// burnliteMilestone is the issue type of a task imported from a Milestone.
const burnliteMilestone = "Milestone"

// milestoneTicket is a Milestone as a ticket: its Description, then a line
// for each of its Tasks. Burnlite gives a Milestone no updatedAt, so the
// caller says when it was read.
func milestoneTicket(m *burnlite.Milestone, base string, updated time.Time) RemoteTicket {
	base = strings.TrimRight(base, "/")
	var b strings.Builder
	if d := strings.TrimSpace(m.Description); d != "" {
		b.WriteString(d + "\n")
	}
	if len(m.Tasks) > 0 {
		if b.Len() > 0 {
			b.WriteString("\n")
		}
		b.WriteString("## Tasks\n\n")
		for _, row := range m.Tasks {
			mark := " "
			if row.State == "closed" {
				mark = "x"
			}
			state := burnliteState(row.State, row.CloseReason, row.ClaimHolder, row.Blocked)
			if state == burnliteClaimed {
				state += " by " + row.ClaimHolder
			}
			fmt.Fprintf(&b, "- [%s] [%s](%s) %s (%s)\n", mark, row.Key, base+row.URL, row.Name, state)
		}
	}
	return RemoteTicket{
		ID:            m.Key,
		Summary:       m.Name,
		DescriptionMD: b.String(),
		Status:        burnliteState(m.State, m.CloseReason, "", false),
		IssueType:     burnliteMilestone,
		Labels:        m.Labels,
		URL:           base + m.URL,
		UpdatedAt:     updated,
	}
}

func kindLabel(kind string) string {
	switch kind {
	case "work":
		return "Work"
	case "meta":
		return "Meta"
	}
	return kind
}

// burnliteDescription is a task's Description, then its Plan and Checklist.
func burnliteDescription(task *burnlite.Task) string {
	var b strings.Builder
	if d := strings.TrimSpace(task.Description); d != "" {
		b.WriteString(d + "\n")
	}
	if p := strings.TrimSpace(task.Plan); p != "" {
		if b.Len() > 0 {
			b.WriteString("\n")
		}
		b.WriteString("## Plan\n\n" + p + "\n")
	}
	if len(task.Checklist) > 0 {
		if b.Len() > 0 {
			b.WriteString("\n")
		}
		b.WriteString("## Checklist\n\n")
		for _, item := range task.Checklist {
			mark := " "
			if item.Done {
				mark = "x"
			}
			b.WriteString("- [" + mark + "] " + item.Name + "\n")
		}
	}
	return b.String()
}
