package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"sync"
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
	ListTasksSince(ctx context.Context, project, state string, since time.Time) ([]burnlite.TaskRow, error)
	ListMilestones(ctx context.Context, project string) ([]burnlite.MilestoneRow, error)
	GetTask(ctx context.Context, key string) (*burnlite.Task, error)
	GetMilestone(ctx context.Context, key string) (*burnlite.Milestone, error)
}

// burnliteOpener gives a reader of the connected Burnlite and its address,
// or ErrNotConnected. The reader is for one run of calls: closeBurnliteReader
// ends it.
type burnliteOpener func(ctx context.Context) (burnliteReader, string, error)

// closeBurnliteReader ends a reader that keeps a session open.
func closeBurnliteReader(reader burnliteReader) {
	if c, ok := reader.(interface{ Close() }); ok {
		c.Close()
	}
}

// burnliteFullListEvery is how often a pull lists a project whole. Between
// those it asks only for the tasks that changed, which costs Burnlite almost
// nothing when none did; a whole list is what shows that a task was deleted.
const burnliteFullListEvery = 15 * time.Minute

// burnliteSinceOverlap is how far before the newest change Legato holds a
// pull asks from. Burnlite dates a change when it is written, a moment
// before it can be read, so a change dated just before the newest one may
// appear after it.
const burnliteSinceOverlap = 5 * time.Second

// burnliteSource is Burnlite, read-only: tasks are imported and kept up to
// date, and a card moved on the board never changes the task. A task's
// state places its card: closed, else claimed, else blocked, else ready.
type burnliteSource struct {
	opts BurnliteSourceOptions
	open burnliteOpener
	now  func() time.Time

	mu sync.Mutex
	// fullListAt is when each project was last listed whole, by project key.
	// It starts empty, so the first pull after a start lists every project.
	fullListAt map[string]time.Time
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
	return &burnliteSource{opts: opts, open: open, now: time.Now, fullListAt: map[string]time.Time{}}
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
	raw, ok, err := db.GetSetting(ctx, burnliteProjectsKey)
	if err != nil || !ok {
		return nil
	}
	var projects []string
	if json.Unmarshal(raw, &projects) != nil || len(projects) == 0 {
		return nil
	}
	return projects
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
	if keys == nil {
		keys = []string{}
	}
	raw, _ := json.Marshal(keys)
	return db.SetSetting(ctx, burnliteProjectsKey, raw, "web")
}

// projectOf is a key's project: EST in EST-42.
func projectOf(key string) string {
	if i := strings.LastIndex(key, "-"); i > 0 {
		return key[:i]
	}
	return key
}

var burnliteKey = regexp.MustCompile(`^[A-Za-z]{2,6}[0-9]{0,4}-[1-9][0-9]*$`)

// Pull brings the tracked tasks up to date at the least cost to Burnlite.
// For each project it asks for the tasks that changed since the newest
// change Legato holds, and fetches in full only those; every
// burnliteFullListEvery it lists the project whole instead, which is what
// shows that a task is gone. A tracked Milestone is read again only when
// Burnlite's list of the project's Milestones says it changed. A task or
// Milestone left out of the batch is unchanged.
func (b *burnliteSource) Pull(ctx context.Context, tracked []store.Task) (PullBatch, error) {
	reader, base, err := b.open(ctx)
	if err != nil {
		return PullBatch{}, err
	}
	defer closeBurnliteReader(reader)

	type cards struct{ tasks, milestones []store.Task }
	byProject := map[string]*cards{}
	var projects []string
	for _, task := range tracked {
		p := projectOf(task.ID)
		if _, ok := byProject[p]; !ok {
			byProject[p] = &cards{}
			projects = append(projects, p)
		}
		if parseRemoteMeta(task.RemoteMeta).IssueType == burnliteMilestone {
			byProject[p].milestones = append(byProject[p].milestones, task)
		} else {
			byProject[p].tasks = append(byProject[p].tasks, task)
		}
	}
	var batch PullBatch
	for _, project := range projects {
		gone, err := b.pullTasks(ctx, reader, base, project, byProject[project].tasks, &batch)
		if err == nil && !gone {
			gone, err = b.pullMilestones(ctx, reader, base, project, byProject[project].milestones, &batch)
		}
		if err != nil {
			return PullBatch{}, err
		}
		if gone {
			// The project is gone, and its tasks with it.
			for _, task := range append(byProject[project].tasks, byProject[project].milestones...) {
				batch.Missing = append(batch.Missing, task.ID)
			}
		}
	}
	return batch, nil
}

// heldUpdatedAt is the remote updatedAt Legato holds for a task.
func heldUpdatedAt(task store.Task) time.Time {
	held, _ := time.Parse(time.RFC3339Nano, parseRemoteMeta(task.RemoteMeta).RemoteUpdatedAt)
	return held
}

// fullListDue says whether the project's next list is a whole one, and when
// it is, counts it as made.
func (b *burnliteSource) fullListDue(project string, tasks []store.Task) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	now := b.now()
	due := now.Sub(b.fullListAt[project]) >= burnliteFullListEvery
	for _, task := range tasks {
		// Nothing held to ask from.
		due = due || heldUpdatedAt(task).IsZero()
	}
	if due {
		b.fullListAt[project] = now
	}
	return due
}

// pullTasks adds a project's tracked tasks that changed to the batch, and
// on a whole list the ones Burnlite no longer has. gone says the project is
// not there.
func (b *burnliteSource) pullTasks(ctx context.Context, reader burnliteReader, base, project string, tasks []store.Task, batch *PullBatch) (gone bool, err error) {
	if len(tasks) == 0 {
		return false, nil
	}
	full := b.fullListDue(project, tasks)
	var rows []burnlite.TaskRow
	if full {
		rows, err = reader.ListTasks(ctx, project, "any")
	} else {
		var newest time.Time
		for _, task := range tasks {
			if held := heldUpdatedAt(task); held.After(newest) {
				newest = held
			}
		}
		rows, err = reader.ListTasksSince(ctx, project, "any", newest.Add(-burnliteSinceOverlap))
	}
	if errors.Is(err, burnlite.ErrNotFound) {
		return true, nil
	}
	if err != nil {
		if full {
			// Not made after all: the next pull lists the project whole.
			b.mu.Lock()
			delete(b.fullListAt, project)
			b.mu.Unlock()
		}
		return false, err
	}
	byKey := make(map[string]burnlite.TaskRow, len(rows))
	for _, row := range rows {
		byKey[row.Key] = row
	}
	for _, task := range tasks {
		row, ok := byKey[task.ID]
		if !ok {
			// Only a whole list says a task is gone; otherwise it is unchanged.
			if full {
				batch.Missing = append(batch.Missing, task.ID)
			}
			continue
		}
		if row.UpdatedAt.IsZero() {
			return false, errors.New("this Burnlite gives its tasks no updatedAt, which Legato needs to sync them: update Burnlite")
		}
		if !row.UpdatedAt.After(heldUpdatedAt(task)) {
			// Unchanged: the service leaves it as it is.
			batch.Tickets = append(batch.Tickets, rowTicket(row, base))
			continue
		}
		fetched, err := reader.GetTask(ctx, row.Key)
		if errors.Is(err, burnlite.ErrNotFound) {
			batch.Missing = append(batch.Missing, task.ID)
			continue
		}
		if err != nil {
			return false, err
		}
		batch.Tickets = append(batch.Tickets, fullTicket(row, fetched, base))
	}
	return false, nil
}

// pullMilestones adds a project's tracked Milestones that changed to the
// batch, and the ones Burnlite no longer has. One list of the project's
// Milestones says which changed: its updatedAt moves when the Milestone or
// any of its Tasks does. gone says the project is not there.
func (b *burnliteSource) pullMilestones(ctx context.Context, reader burnliteReader, base, project string, milestones []store.Task, batch *PullBatch) (gone bool, err error) {
	if len(milestones) == 0 {
		return false, nil
	}
	rows, err := reader.ListMilestones(ctx, project)
	if errors.Is(err, burnlite.ErrNotFound) {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	updatedAt := make(map[string]time.Time, len(rows))
	for _, row := range rows {
		updatedAt[row.Key] = row.UpdatedAt
	}
	for _, task := range milestones {
		updated, listed := updatedAt[task.ID]
		if !listed {
			batch.Missing = append(batch.Missing, task.ID)
			continue
		}
		if updated.IsZero() {
			// A Burnlite too old to date its Milestones: read it and compare.
			ticket, found, err := b.pullMilestone(ctx, reader, base, task)
			if err != nil {
				return false, err
			}
			if found {
				batch.Tickets = append(batch.Tickets, ticket)
			} else {
				batch.Missing = append(batch.Missing, task.ID)
			}
			continue
		}
		if !updated.After(heldUpdatedAt(task)) {
			// Unchanged: the service leaves it as it is.
			batch.Tickets = append(batch.Tickets, RemoteTicket{ID: task.ID, IssueType: burnliteMilestone, UpdatedAt: updated})
			continue
		}
		m, err := reader.GetMilestone(ctx, task.ID)
		if errors.Is(err, burnlite.ErrNotFound) {
			batch.Missing = append(batch.Missing, task.ID)
			continue
		}
		if err != nil {
			return false, err
		}
		batch.Tickets = append(batch.Tickets, milestoneTicket(m, base, updated))
	}
	return false, nil
}

// pullMilestone reads a Milestone of a Burnlite that gives it no updatedAt.
// A Task leaving it changes no timestamp it still lists, so the ticket is
// dated now when what Legato shows of it has changed, and keeps the date
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
	defer closeBurnliteReader(reader)
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
	defer closeBurnliteReader(reader)
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
		// An old Burnlite gives a Milestone no updatedAt: date it now.
		updated := m.UpdatedAt
		if updated.IsZero() {
			updated = time.Now()
		}
		t := milestoneTicket(m, base, updated)
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
// for each of its Tasks, dated updated.
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
