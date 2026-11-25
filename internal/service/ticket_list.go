package service

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/cpave3/legato/internal/engine/burnlite"
)

// TicketQuery is what tickets.list asks a provider for (LEG-488).
type TicketQuery struct {
	// Project is the project's key; the milestone's project, else the
	// source's own projects, when empty.
	Project string
	// Milestone keeps the tickets of one milestone (Jira: an epic's).
	Milestone string
	// Kind is work, meta, milestone, or "" for any task.
	Kind   string
	Labels []string
	// State is open (the default), closed or any.
	State string
	// Frontier keeps the open tasks that can be taken now.
	Frontier bool
	// Ready keeps, with Kind milestone, the milestones ready to build: open,
	// with an open work task, and none of whose open tasks waits on an open
	// ticket outside it.
	Ready bool
	// Limit is the most items given; 0 is no limit.
	Limit int
}

// Ticket kinds and states as tickets.list gives them.
const (
	TicketKindWork      = "work"
	TicketKindMeta      = "meta"
	TicketKindMilestone = "milestone"
	TicketOpen          = "open"
	TicketClosed        = "closed"
	ticketAnyState      = "any"
)

// TicketItem is one ticket as tickets.list gives it.
type TicketItem struct {
	Key          string   `json:"key"`
	Title        string   `json:"title"`
	URL          string   `json:"url"`
	Kind         string   `json:"kind"`
	State        string   `json:"state"`
	Labels       []string `json:"labels"`
	Blocked      bool     `json:"blocked"`
	OpenBlockers []string `json:"open_blockers"`
	Frontier     bool     `json:"frontier"`
	// Milestone is the key of the milestone it is in, "" for none.
	Milestone string `json:"milestone"`
}

// TicketLister lists a provider's tickets: provider "" is the only one
// connected.
type TicketLister interface {
	ListTickets(ctx context.Context, provider string, q TicketQuery) ([]TicketItem, error)
}

// check refuses a query no source can answer.
func (q TicketQuery) check() error {
	switch q.Kind {
	case "", TicketKindWork, TicketKindMeta, TicketKindMilestone:
	default:
		return fmt.Errorf("kind must be work, meta or milestone, not %q", q.Kind)
	}
	switch q.State {
	case "", TicketOpen, TicketClosed, ticketAnyState:
	default:
		return fmt.Errorf("state must be open, closed or any, not %q", q.State)
	}
	switch {
	case q.Ready && q.Kind != TicketKindMilestone:
		return errors.New("ready is for milestones: give kind milestone with it")
	case q.Frontier && q.Kind == TicketKindMilestone:
		return errors.New("frontier is for tasks, not milestones; ready picks milestones")
	case q.Milestone != "" && q.Kind == TicketKindMilestone:
		return errors.New("milestone lists a milestone's tasks: leave kind milestone out, or give project")
	case q.Limit < 0:
		return errors.New("limit must not be negative")
	}
	return nil
}

func (s *syncService) ListTickets(ctx context.Context, provider string, q TicketQuery) ([]TicketItem, error) {
	if err := q.check(); err != nil {
		return nil, err
	}
	source, err := s.importSource(ctx, provider)
	if err != nil {
		return nil, err
	}
	items, err := source.List(ctx, q)
	if err != nil {
		return nil, err
	}
	if q.Limit > 0 && len(items) > q.Limit {
		items = items[:q.Limit]
	}
	return items, nil
}

// full says whether a limited list has all it may hold.
func full(items []TicketItem, limit int) bool { return limit > 0 && len(items) >= limit }

// readyMilestone is the ready rule: open, with an open work task, and no
// open task waiting on an open ticket outside the milestone.
func readyMilestone(open bool, tasks []TicketItem) bool {
	if !open {
		return false
	}
	inside := map[string]bool{}
	for _, task := range tasks {
		inside[task.Key] = true
	}
	work := false
	for _, task := range tasks {
		if task.State != TicketOpen {
			continue
		}
		work = work || task.Kind == TicketKindWork
		for _, blocker := range task.OpenBlockers {
			if !inside[blocker] {
				return false
			}
		}
	}
	return work
}

// List answers tickets.list from Burnlite: list_tasks with the filters, or
// list_milestones, reading each open milestone in full only when ready asks.
func (b *burnliteSource) List(ctx context.Context, q TicketQuery) ([]TicketItem, error) {
	reader, base, err := b.open(ctx)
	if err != nil {
		return nil, err
	}
	defer closeBurnliteReader(reader)
	projects, err := b.listProjects(ctx, reader, q)
	if err != nil {
		return nil, err
	}
	items := []TicketItem{}
	for _, project := range projects {
		var found []TicketItem
		if q.Kind == TicketKindMilestone {
			found, err = b.listMilestones(ctx, reader, base, project, q, q.Limit-len(items))
		} else {
			var rows []burnlite.TaskRow
			rows, err = reader.FindTasks(ctx, burnlite.TaskFilter{Project: project, Milestone: q.Milestone, Kind: q.Kind, Labels: q.Labels, State: q.State, Frontier: q.Frontier})
			for _, row := range rows {
				found = append(found, burnliteItem(row, base))
			}
		}
		if err != nil {
			return nil, err
		}
		items = append(items, found...)
		if full(items, q.Limit) {
			break
		}
	}
	return items, nil
}

// listProjects are the projects a query reads: its own, its milestone's,
// else those searched for import, else every one.
func (b *burnliteSource) listProjects(ctx context.Context, reader burnliteReader, q TicketQuery) ([]string, error) {
	switch {
	case q.Project != "":
		return []string{strings.ToUpper(q.Project)}, nil
	case q.Milestone != "":
		return []string{projectOf(strings.ToUpper(q.Milestone))}, nil
	}
	if b.opts.Projects != nil {
		if projects := b.opts.Projects(ctx); len(projects) > 0 {
			return projects, nil
		}
	}
	all, err := reader.ListProjects(ctx)
	if err != nil {
		return nil, err
	}
	projects := make([]string, len(all))
	for i, p := range all {
		projects[i] = p.Key
	}
	return projects, nil
}

func (b *burnliteSource) listMilestones(ctx context.Context, reader burnliteReader, base, project string, q TicketQuery, room int) ([]TicketItem, error) {
	state := q.State
	if q.Ready {
		state = TicketOpen
	}
	rows, err := reader.FindMilestones(ctx, burnlite.MilestoneFilter{Project: project, State: state, Labels: q.Labels})
	if err != nil {
		return nil, err
	}
	var items []TicketItem
	for _, row := range rows {
		if q.Limit > 0 && len(items) >= room {
			break
		}
		if q.Ready {
			m, err := reader.GetMilestone(ctx, row.Key)
			if err != nil {
				return nil, fmt.Errorf("reading milestone %s: %w", row.Key, err)
			}
			tasks := make([]TicketItem, len(m.Tasks))
			for i, task := range m.Tasks {
				tasks[i] = burnliteItem(task, base)
			}
			if !readyMilestone(m.State == TicketOpen, tasks) {
				continue
			}
		}
		labels := row.Labels
		if labels == nil {
			labels = []string{}
		}
		items = append(items, TicketItem{Key: row.Key, Title: row.Name, URL: strings.TrimRight(base, "/") + row.URL, Kind: TicketKindMilestone,
			State: row.State, Labels: labels, OpenBlockers: []string{}})
	}
	return items, nil
}

func burnliteItem(row burnlite.TaskRow, base string) TicketItem {
	labels, blockers := row.Labels, row.OpenBlockers
	if labels == nil {
		labels = []string{}
	}
	if blockers == nil {
		blockers = []string{}
	}
	state := TicketOpen
	if row.State == "closed" {
		state = TicketClosed
	}
	return TicketItem{Key: row.Key, Title: row.Name, URL: strings.TrimRight(base, "/") + row.URL, Kind: row.Kind, State: state, Labels: labels,
		Blocked: len(blockers) > 0, OpenBlockers: blockers, Frontier: row.Frontier, Milestone: row.MilestoneKey}
}

// jiraEpic is the issue type tickets.list reads as a milestone.
const jiraEpic = "Epic"

// List answers tickets.list from Jira with JQL. Jira has no meta tasks; a
// milestone is an epic, and its tickets are the issues whose parent it is.
// An issue is closed when its status is in the Done category, and on the
// frontier when it is not started and no open issue blocks it.
func (j *jiraSource) List(ctx context.Context, q TicketQuery) ([]TicketItem, error) {
	if q.Kind == TicketKindMeta {
		return nil, errors.New("Jira has no meta tasks: kind takes work or milestone there")
	}
	tickets, err := j.provider.Search(ctx, j.listJQL(q))
	if err != nil {
		return nil, err
	}
	items := []TicketItem{}
	for _, t := range tickets {
		item := jiraItem(t)
		if q.Frontier && !item.Frontier {
			continue
		}
		if q.Ready {
			children, err := j.provider.Search(ctx, fmt.Sprintf("parent = %s ORDER BY key ASC", jqlString(t.ID)))
			if err != nil {
				return nil, fmt.Errorf("reading epic %s: %w", t.ID, err)
			}
			tasks := make([]TicketItem, 0, len(children))
			for _, child := range children {
				if child.IssueType != jiraEpic {
					tasks = append(tasks, jiraItem(child))
				}
			}
			if !readyMilestone(item.State == TicketOpen, tasks) {
				continue
			}
		}
		items = append(items, item)
		if full(items, q.Limit) {
			break
		}
	}
	return items, nil
}

func (j *jiraSource) listJQL(q TicketQuery) string {
	var clauses []string
	projects := j.projectKeys
	if q.Project != "" {
		projects = []string{q.Project}
	}
	if len(projects) > 0 {
		quoted := make([]string, len(projects))
		for i, p := range projects {
			quoted[i] = jqlString(p)
		}
		clauses = append(clauses, "project in ("+strings.Join(quoted, ", ")+")")
	}
	if q.Milestone != "" {
		clauses = append(clauses, "parent = "+jqlString(q.Milestone))
	}
	if q.Kind == TicketKindMilestone {
		clauses = append(clauses, "issuetype = "+jiraEpic)
	} else {
		clauses = append(clauses, "issuetype != "+jiraEpic)
	}
	for _, label := range q.Labels {
		clauses = append(clauses, "labels = "+jqlString(label))
	}
	switch {
	case q.Frontier:
		clauses = append(clauses, `statusCategory = "To Do"`)
	case q.Ready, q.State == "", q.State == TicketOpen:
		clauses = append(clauses, "statusCategory != Done")
	case q.State == TicketClosed:
		clauses = append(clauses, "statusCategory = Done")
	}
	return strings.Join(clauses, " AND ") + " ORDER BY key ASC"
}

func jiraItem(t RemoteTicket) TicketItem {
	labels, blockers := t.Labels, t.BlockedBy
	if labels == nil {
		labels = []string{}
	}
	if blockers == nil {
		blockers = []string{}
	}
	state, kind := TicketOpen, TicketKindWork
	if t.StatusCategory == "done" {
		state = TicketClosed
	}
	if t.IssueType == jiraEpic {
		kind = TicketKindMilestone
	}
	item := TicketItem{Key: t.ID, Title: t.Summary, URL: t.URL, Kind: kind, State: state, Labels: labels, Blocked: len(blockers) > 0,
		OpenBlockers: blockers, Milestone: t.EpicKey}
	item.Frontier = kind == TicketKindWork && t.StatusCategory == "new" && !item.Blocked
	if kind == TicketKindMilestone {
		item.Milestone = ""
	}
	return item
}
