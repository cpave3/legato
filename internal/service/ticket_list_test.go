package service

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cpave3/legato/internal/engine/jira"
)

func keysOf(items []TicketItem) string {
	keys := make([]string, len(items))
	for i, item := range items {
		keys[i] = item.Key
	}
	return strings.Join(keys, ",")
}

// tickets.list on a Burnlite milestone gives its frontier, in Burnlite's
// order, as data a later step reads (LEG-488).
func TestTicketsListGivesABurnliteMilestonesFrontier(t *testing.T) {
	fake := buildMilestoneBurnlite()
	svc, _ := newSourcesTest(t, fake.source(t))
	lister := svc.(TicketLister)
	ctx := context.Background()

	items, err := lister.ListTickets(ctx, "burnlite", TicketQuery{Milestone: "EST-9", Kind: "work", Frontier: true})
	if err != nil {
		t.Fatal(err)
	}
	if got := keysOf(items); got != "EST-10,EST-12" {
		t.Fatalf("frontier = %s", got)
	}
	first := items[0]
	if first.Title != "Wire the client" || first.URL != fake.srv.URL+"/EST-10" || first.Kind != "work" || first.State != "open" ||
		first.Milestone != "EST-9" || !first.Frontier || first.Blocked || strings.Join(first.Labels, ",") != "api" || first.OpenBlockers == nil {
		t.Fatalf("item = %+v", first)
	}

	all, err := lister.ListTickets(ctx, "burnlite", TicketQuery{Milestone: "EST-9"})
	if err != nil {
		t.Fatal(err)
	}
	if got := keysOf(all); got != "EST-10,EST-11,EST-12,EST-14" {
		t.Fatalf("open tasks = %s", got)
	}
	if blocked := all[1]; !blocked.Blocked || strings.Join(blocked.OpenBlockers, ",") != "EST-10" || blocked.Frontier {
		t.Fatalf("blocked = %+v", blocked)
	}

	labelled, err := lister.ListTickets(ctx, "burnlite", TicketQuery{Project: "EST", Labels: []string{"docs"}, State: "any"})
	if err != nil || keysOf(labelled) != "EST-12" {
		t.Fatalf("labelled = %s, %v", keysOf(labelled), err)
	}
	limited, err := lister.ListTickets(ctx, "burnlite", TicketQuery{Project: "EST", Limit: 2})
	if err != nil || keysOf(limited) != "EST-10,EST-11" {
		t.Fatalf("limited = %s, %v", keysOf(limited), err)
	}
}

// A milestone is ready when it is open, has an open work task, and none of
// its open tasks waits on an open task outside it.
func TestTicketsListGivesTheReadyBurnliteMilestones(t *testing.T) {
	fake := buildMilestoneBurnlite()
	svc, _ := newSourcesTest(t, fake.source(t))
	lister := svc.(TicketLister)
	ctx := context.Background()

	milestones, err := lister.ListTickets(ctx, "burnlite", TicketQuery{Project: "EST", Kind: "milestone", State: "any"})
	if err != nil {
		t.Fatal(err)
	}
	if got := keysOf(milestones); got != "EST-9,EST-20,EST-30,EST-40" {
		t.Fatalf("milestones = %s", got)
	}
	if m := milestones[0]; m.Title != "Sync" || m.Kind != "milestone" || m.State != "open" || m.URL != fake.srv.URL+"/EST-9" || strings.Join(m.Labels, ",") != "ready-for-agent" {
		t.Fatalf("milestone = %+v", m)
	}
	if fake.toolCalls("get_milestone") != 0 {
		t.Fatalf("listing milestones read them: %v", fake.calls)
	}

	ready, err := lister.ListTickets(ctx, "burnlite", TicketQuery{Project: "EST", Kind: "milestone", Ready: true})
	if err != nil {
		t.Fatal(err)
	}
	if got := keysOf(ready); got != "EST-9" {
		t.Fatalf("ready = %s", got)
	}

	// EST-11 closes: EST-20's one task waits on nothing now.
	fake.mu.Lock()
	fake.task("EST-11").State = "closed"
	fake.mu.Unlock()
	ready, err = lister.ListTickets(ctx, "burnlite", TicketQuery{Project: "EST", Kind: "milestone", Ready: true})
	if err != nil || keysOf(ready) != "EST-9,EST-20" {
		t.Fatalf("ready = %s, %v", keysOf(ready), err)
	}
	// The limit is reached before the last milestone is read.
	before := fake.toolCalls("get_milestone")
	ready, err = lister.ListTickets(ctx, "burnlite", TicketQuery{Project: "EST", Kind: "milestone", Ready: true, Limit: 1})
	if err != nil || keysOf(ready) != "EST-9" || fake.toolCalls("get_milestone")-before != 1 {
		t.Fatalf("ready = %s, %v, read %d", keysOf(ready), err, fake.toolCalls("get_milestone")-before)
	}
}

func TestTicketsListRefusesAQueryItCannotAnswer(t *testing.T) {
	fake := buildMilestoneBurnlite()
	svc, _ := newSourcesTest(t, fake.source(t))
	lister := svc.(TicketLister)
	ctx := context.Background()
	for name, q := range map[string]TicketQuery{
		"ready without milestones":  {Project: "EST", Ready: true},
		"frontier of milestones":    {Project: "EST", Kind: "milestone", Frontier: true},
		"milestones of a milestone": {Milestone: "EST-9", Kind: "milestone"},
		"unknown kind":              {Project: "EST", Kind: "epic"},
		"unknown state":             {Project: "EST", State: "done"},
	} {
		if _, err := lister.ListTickets(ctx, "burnlite", q); err == nil {
			t.Errorf("%s: answered", name)
		}
	}
	if _, err := lister.ListTickets(ctx, "jira", TicketQuery{Project: "EST"}); !errors.Is(err, ErrNoTicketProvider) {
		t.Errorf("jira, not connected: %v", err)
	}
}

// listingJira answers JQL searches from issues in memory: the project, parent,
// issue type, labels and status category clauses tickets.list writes.
type listingJira struct {
	mu     sync.Mutex
	issues []string // JSON issues, in order
	byKey  map[string]fakeJiraIssue
	jql    []string
}

type fakeJiraIssue struct {
	key, summary, issueType, category, parent string
	labels, blockedBy                         []string
}

func (f *listingJira) add(issue fakeJiraIssue) {
	if f.byKey == nil {
		f.byKey = map[string]fakeJiraIssue{}
	}
	f.byKey[issue.key] = issue
	f.issues = append(f.issues, issue.key)
}

func (f *listingJira) json(issue fakeJiraIssue) string {
	var links []string
	for _, key := range issue.blockedBy {
		links = append(links, fmt.Sprintf(`{"type":{"name":"Blocks","inward":"is blocked by","outward":"blocks"},"inwardIssue":{"key":%q,"fields":{"status":{"name":"x","statusCategory":{"key":%q}}}}}`, key, f.byKey[key].category))
	}
	labels := `[]`
	if len(issue.labels) > 0 {
		labels = `["` + strings.Join(issue.labels, `","`) + `"]`
	}
	parent := `null`
	if issue.parent != "" {
		parent = fmt.Sprintf(`{"key":%q,"fields":{"issuetype":{"name":"Epic"}}}`, issue.parent)
	}
	return fmt.Sprintf(`{"key":%q,"fields":{"summary":%q,"status":{"name":"S","statusCategory":{"key":%q}},"issuetype":{"name":%q},"labels":%s,"parent":%s,"issuelinks":[%s],"updated":"2025-01-01T00:00:00.000+0000"}}`,
		issue.key, issue.summary, issue.category, issue.issueType, labels, parent, strings.Join(links, ","))
}

func (f *listingJira) matches(issue fakeJiraIssue, jql string) bool {
	for _, clause := range strings.Split(strings.Split(jql, " ORDER BY ")[0], " AND ") {
		clause = strings.Trim(clause, "() ")
		switch {
		case strings.HasPrefix(clause, "project in "):
			if !strings.Contains(clause, `"`+strings.Split(issue.key, "-")[0]+`"`) {
				return false
			}
		case strings.HasPrefix(clause, "parent = "):
			if strings.Trim(strings.TrimPrefix(clause, "parent = "), `"`) != issue.parent {
				return false
			}
		case clause == "issuetype = Epic":
			if issue.issueType != "Epic" {
				return false
			}
		case clause == "issuetype != Epic":
			if issue.issueType == "Epic" {
				return false
			}
		case strings.HasPrefix(clause, "labels = "):
			if !strings.Contains(strings.Join(issue.labels, ","), strings.Trim(strings.TrimPrefix(clause, "labels = "), `"`)) {
				return false
			}
		case clause == "statusCategory != Done":
			if issue.category == "done" {
				return false
			}
		case clause == "statusCategory = Done":
			if issue.category != "done" {
				return false
			}
		case clause == `statusCategory = "To Do"`:
			if issue.category != "new" {
				return false
			}
		default:
			panic("fake Jira cannot read " + clause)
		}
	}
	return true
}

func (f *listingJira) source(t *testing.T) TicketSource {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		jql := r.URL.Query().Get("jql")
		f.mu.Lock()
		defer f.mu.Unlock()
		f.jql = append(f.jql, jql)
		var found []string
		for _, key := range f.issues {
			if issue := f.byKey[key]; f.matches(issue, jql) {
				found = append(found, f.json(issue))
			}
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"issues":[%s],"isLast":true}`, strings.Join(found, ","))
	}))
	t.Cleanup(srv.Close)
	provider := NewJiraProvider(jira.NewProvider(srv.URL, "a@b.com", "tok", 5*time.Second))
	return NewJiraSourceWith(provider, JiraSourceOptions{ProjectKeys: []string{"APP"}, Site: srv.URL})
}

func jiraBoard() *listingJira {
	f := &listingJira{}
	for _, issue := range []fakeJiraIssue{
		{key: "APP-1", summary: "Login", issueType: "Epic", category: "indeterminate"},
		{key: "APP-2", summary: "Form", issueType: "Story", category: "new", parent: "APP-1", labels: []string{"ui"}},
		{key: "APP-3", summary: "Session", issueType: "Story", category: "new", parent: "APP-1", blockedBy: []string{"APP-2"}},
		{key: "APP-4", summary: "Old", issueType: "Story", category: "done", parent: "APP-1"},
		{key: "APP-5", summary: "Started", issueType: "Task", category: "indeterminate", parent: "APP-1"},
		{key: "APP-6", summary: "Billing", issueType: "Epic", category: "new"},
		{key: "APP-7", summary: "Invoices", issueType: "Story", category: "new", parent: "APP-6", blockedBy: []string{"APP-3"}},
		{key: "APP-8", summary: "Empty", issueType: "Epic", category: "new"},
	} {
		f.add(issue)
	}
	return f
}

// tickets.list reads Jira through JQL: an epic's issues, its frontier (not
// started, blocked by nothing open), and its ready epics.
func TestTicketsListReadsJira(t *testing.T) {
	fake := jiraBoard()
	svc, _ := newSourcesTest(t, fake.source(t))
	lister := svc.(TicketLister)
	ctx := context.Background()

	items, err := lister.ListTickets(ctx, "jira", TicketQuery{Milestone: "APP-1"})
	if err != nil {
		t.Fatal(err)
	}
	if got := keysOf(items); got != "APP-2,APP-3,APP-5" {
		t.Fatalf("open issues = %s (jql %q)", got, fake.jql)
	}
	if blocked := items[1]; !blocked.Blocked || strings.Join(blocked.OpenBlockers, ",") != "APP-2" || blocked.Frontier || blocked.Milestone != "APP-1" || blocked.Kind != "work" {
		t.Fatalf("APP-3 = %+v", blocked)
	}

	frontier, err := lister.ListTickets(ctx, "jira", TicketQuery{Milestone: "APP-1", Frontier: true})
	if err != nil || keysOf(frontier) != "APP-2" {
		t.Fatalf("frontier = %s, %v", keysOf(frontier), err)
	}
	if !frontier[0].Frontier || frontier[0].URL == "" || strings.Join(frontier[0].Labels, ",") != "ui" {
		t.Fatalf("APP-2 = %+v", frontier[0])
	}

	closed, err := lister.ListTickets(ctx, "jira", TicketQuery{Milestone: "APP-1", State: "closed"})
	if err != nil || keysOf(closed) != "APP-4" || closed[0].State != "closed" {
		t.Fatalf("closed = %+v, %v", closed, err)
	}

	epics, err := lister.ListTickets(ctx, "jira", TicketQuery{Kind: "milestone"})
	if err != nil || keysOf(epics) != "APP-1,APP-6,APP-8" || epics[0].Kind != "milestone" {
		t.Fatalf("epics = %s, %v", keysOf(epics), err)
	}
	ready, err := lister.ListTickets(ctx, "jira", TicketQuery{Kind: "milestone", Ready: true})
	if err != nil || keysOf(ready) != "APP-1" {
		t.Fatalf("ready = %s, %v", keysOf(ready), err)
	}

	if _, err := lister.ListTickets(ctx, "jira", TicketQuery{Kind: "meta"}); err == nil {
		t.Fatal("Jira listed meta tasks")
	}
}
