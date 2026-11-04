package service

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/cpave3/legato/internal/engine/events"
	"github.com/cpave3/legato/internal/engine/store"
)

// fakeSource is a ticket source with tickets in memory.
type fakeSource struct {
	name     string
	disabled bool
	tickets  map[string]RemoteTicket
	columns  map[string]string // remote status → column
	pullErr  error
	pulled   [][]string // the keys each pull asked for
	searched []string
}

func (f *fakeSource) Name() string                 { return f.name }
func (f *fakeSource) Label() string                { return "Fake " + f.name }
func (f *fakeSource) Enabled(context.Context) bool { return !f.disabled }
func (f *fakeSource) Interval() time.Duration      { return time.Minute }
func (f *fakeSource) MovesCards() bool             { return true }
func (f *fakeSource) Column(_ context.Context, t RemoteTicket) (string, error) {
	return f.columns[t.Status], nil
}

func (f *fakeSource) Pull(_ context.Context, tracked []store.Task) (PullBatch, error) {
	if f.pullErr != nil {
		return PullBatch{}, f.pullErr
	}
	var batch PullBatch
	keys := make([]string, 0, len(tracked))
	for _, task := range tracked {
		keys = append(keys, task.ID)
		if t, ok := f.tickets[task.ID]; ok {
			batch.Tickets = append(batch.Tickets, t)
		} else {
			batch.Missing = append(batch.Missing, task.ID)
		}
	}
	f.pulled = append(f.pulled, keys)
	return batch, nil
}

func (f *fakeSource) Search(_ context.Context, text string) ([]RemoteTicket, error) {
	f.searched = append(f.searched, text)
	var out []RemoteTicket
	for _, t := range f.tickets {
		out = append(out, t)
	}
	return out, nil
}

func (f *fakeSource) Get(_ context.Context, key string) (*RemoteTicket, error) {
	if t, ok := f.tickets[key]; ok {
		return &t, nil
	}
	return nil, nil
}

func newSourcesTest(t *testing.T, sources ...TicketSource) (SyncService, *store.Store) {
	t.Helper()
	s, err := store.New(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	ctx := context.Background()
	for i, m := range []store.ColumnMapping{
		{ColumnName: "Backlog", RemoteStatuses: `["To Do"]`},
		{ColumnName: "Doing", RemoteStatuses: `["In Progress"]`},
		{ColumnName: "Review", RemoteStatuses: `[]`},
		{ColumnName: "Done", RemoteStatuses: `["Done"]`, RemoteTransition: "31"},
	} {
		m.SortOrder = i
		if err := s.CreateColumnMapping(ctx, m); err != nil {
			t.Fatalf("mapping: %v", err)
		}
	}
	return NewTicketSync(s, events.New(), nil, sources...), s
}

func createProviderTask(t *testing.T, s *store.Store, provider, id, status string, updatedAt time.Time) {
	t.Helper()
	meta, _ := json.Marshal(map[string]string{"remote_updated_at": updatedAt.UTC().Format(time.RFC3339Nano)})
	metaStr := string(meta)
	now := time.Now().UTC().Format(time.RFC3339)
	if err := s.CreateTask(context.Background(), store.Task{ID: id, Title: "old " + id, Status: status,
		Provider: &provider, RemoteID: &id, RemoteMeta: &metaStr, CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
}

func TestEachProviderPullsOnlyItsOwnTasks(t *testing.T) {
	old := time.Now().Add(-time.Hour)
	jira := &fakeSource{name: "jira", tickets: map[string]RemoteTicket{
		"REX-1": {ID: "REX-1", Summary: "jira one", Status: "In Progress", UpdatedAt: time.Now()},
	}, columns: map[string]string{"In Progress": "Doing"}}
	burnlite := &fakeSource{name: "burnlite", tickets: map[string]RemoteTicket{
		"EST-7": {ID: "EST-7", Summary: "burnlite seven", Status: "closed", UpdatedAt: time.Now()},
	}, columns: map[string]string{"closed": "Done"}}
	svc, s := newSourcesTest(t, jira, burnlite)
	createProviderTask(t, s, "jira", "REX-1", "Backlog", old)
	createProviderTask(t, s, "burnlite", "EST-7", "Backlog", old)

	result, err := svc.Sync(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(jira.pulled) != 1 || len(jira.pulled[0]) != 1 || jira.pulled[0][0] != "REX-1" {
		t.Errorf("jira pulled %v", jira.pulled)
	}
	if len(burnlite.pulled) != 1 || len(burnlite.pulled[0]) != 1 || burnlite.pulled[0][0] != "EST-7" {
		t.Errorf("burnlite pulled %v", burnlite.pulled)
	}
	for id, want := range map[string][2]string{"REX-1": {"jira one", "Doing"}, "EST-7": {"burnlite seven", "Done"}} {
		task, _ := s.GetTask(context.Background(), id)
		if task.Title != want[0] || task.Status != want[1] {
			t.Errorf("%s = %q in %s, want %q in %s", id, task.Title, task.Status, want[0], want[1])
		}
	}
	if result.TasksSynced != 2 || result.Sources["jira"].TasksSynced != 1 || result.Sources["burnlite"].TasksSynced != 1 {
		t.Errorf("result = %+v", result)
	}
}

func TestATicketFromOneProviderNeverChangesAnotherProvidersTask(t *testing.T) {
	// Both providers have the key EST-7; the task is Jira's.
	jira := &fakeSource{name: "jira", tickets: map[string]RemoteTicket{}}
	burnlite := &fakeSource{name: "burnlite", tickets: map[string]RemoteTicket{
		"EST-7": {ID: "EST-7", Summary: "burnlite's", Status: "closed", UpdatedAt: time.Now()},
	}}
	svc, s := newSourcesTest(t, jira, burnlite)
	createProviderTask(t, s, "jira", "EST-7", "Backlog", time.Now().Add(-time.Hour))

	if _, err := svc.Sync(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(burnlite.pulled) != 0 {
		t.Errorf("burnlite was asked for %v", burnlite.pulled)
	}
	task, _ := s.GetTask(context.Background(), "EST-7")
	if task.Title != "old EST-7" {
		t.Errorf("title = %q", task.Title)
	}
	if meta := parseRemoteMeta(task.RemoteMeta); meta.StaleAt == "" {
		t.Error("the Jira task missing from Jira is not marked stale")
	}
}

func TestAProviderThatFailsOrIsNotConnectedLeavesTheOthersSyncing(t *testing.T) {
	jira := &fakeSource{name: "jira", pullErr: errors.New("401 authentication failed")}
	burnlite := &fakeSource{name: "burnlite", tickets: map[string]RemoteTicket{
		"EST-7": {ID: "EST-7", Summary: "new", UpdatedAt: time.Now()},
	}}
	off := &fakeSource{name: "other", disabled: true}
	svc, s := newSourcesTest(t, jira, burnlite, off)
	createProviderTask(t, s, "jira", "REX-1", "Backlog", time.Now().Add(-time.Hour))
	createProviderTask(t, s, "burnlite", "EST-7", "Backlog", time.Now().Add(-time.Hour))
	createProviderTask(t, s, "other", "OTH-1", "Backlog", time.Now().Add(-time.Hour))

	result, err := svc.Sync(context.Background())
	if err == nil {
		t.Fatal("no error for the failing provider")
	}
	if result == nil || result.Sources["jira"].Err == nil || result.Sources["burnlite"].TasksSynced != 1 || !result.Sources["other"].Disabled {
		t.Fatalf("result = %+v", result)
	}
	if len(off.pulled) != 0 {
		t.Error("a provider that is not connected was pulled")
	}
	if task, _ := s.GetTask(context.Background(), "EST-7"); task.Title != "new" {
		t.Errorf("burnlite task title = %q", task.Title)
	}
}

func TestAChangeWithinASecondIsNotAppliedAgainEachCycle(t *testing.T) {
	at := time.Date(2026, 11, 5, 9, 0, 0, 500_000_000, time.UTC)
	burnlite := &fakeSource{name: "burnlite", tickets: map[string]RemoteTicket{
		"EST-7": {ID: "EST-7", Summary: "new", UpdatedAt: at},
	}}
	svc, s := newSourcesTest(t, burnlite)
	createProviderTask(t, s, "burnlite", "EST-7", "Backlog", at.Add(-time.Hour))
	first, _ := svc.Sync(context.Background())
	second, _ := svc.Sync(context.Background())
	if first.TasksSynced != 1 || second.TasksSynced != 0 {
		t.Fatalf("synced %d then %d", first.TasksSynced, second.TasksSynced)
	}
}

func TestImportGoesToTheNamedProvider(t *testing.T) {
	jira := &fakeSource{name: "jira", tickets: map[string]RemoteTicket{"REX-1": {ID: "REX-1", Summary: "jira one", Status: "To Do"}}}
	burnlite := &fakeSource{name: "burnlite", tickets: map[string]RemoteTicket{
		"EST-7": {ID: "EST-7", Summary: "burnlite seven", Status: "ready", URL: "https://b.example/EST-7"},
	}, columns: map[string]string{"ready": "Doing"}}
	svc, s := newSourcesTest(t, jira, burnlite)
	ctx := context.Background()

	card, err := svc.ImportRemoteTask(ctx, "burnlite", "EST-7", nil)
	if err != nil {
		t.Fatal(err)
	}
	if card.ID != "EST-7" || card.Status != "Doing" {
		t.Errorf("card = %+v", card)
	}
	task, _ := s.GetTask(ctx, "EST-7")
	if task.Provider == nil || *task.Provider != "burnlite" || parseRemoteMeta(task.RemoteMeta).URL != "https://b.example/EST-7" {
		t.Errorf("task = %+v", task)
	}

	if _, err := svc.ImportRemoteTask(ctx, "", "REX-1", nil); err == nil {
		t.Error("an import without a provider was accepted while two are configured")
	}
	if _, err := svc.ImportRemoteTask(ctx, "linear", "REX-1", nil); err == nil {
		t.Error("an import from a provider that is not configured was accepted")
	}
	// The same key, from the other provider.
	jira.tickets["EST-7"] = RemoteTicket{ID: "EST-7", Summary: "jira's"}
	if _, err := svc.ImportRemoteTask(ctx, "jira", "EST-7", nil); !errors.Is(err, ErrTicketKeyTaken) {
		t.Errorf("err = %v, want ErrTicketKeyTaken", err)
	}
	// The same key again, from its own provider: the task there.
	if again, err := svc.ImportRemoteTask(ctx, "burnlite", "EST-7", nil); err != nil || again.ID != "EST-7" {
		t.Errorf("again = %+v, %v", again, err)
	}
}

func TestImportWithoutAProviderUsesTheOnlyOne(t *testing.T) {
	jira := &fakeSource{name: "jira", tickets: map[string]RemoteTicket{"REX-1": {ID: "REX-1", Summary: "jira one", Status: "To Do"}}}
	off := &fakeSource{name: "burnlite", disabled: true}
	svc, s := newSourcesTest(t, jira, off)
	if _, err := svc.ImportRemoteTask(context.Background(), "", "REX-1", nil); err != nil {
		t.Fatal(err)
	}
	if task, _ := s.GetTask(context.Background(), "REX-1"); *task.Provider != "jira" {
		t.Errorf("provider = %q", *task.Provider)
	}
}

func TestSearchAsksEveryConnectedProviderAndSaysWhichEachResultIsFrom(t *testing.T) {
	jira := &fakeSource{name: "jira", tickets: map[string]RemoteTicket{"REX-1": {ID: "REX-1", Summary: "login page"}}}
	burnlite := &fakeSource{name: "burnlite", tickets: map[string]RemoteTicket{"EST-7": {ID: "EST-7", Summary: "login api"}}}
	off := &fakeSource{name: "other", disabled: true, tickets: map[string]RemoteTicket{"OTH-1": {ID: "OTH-1"}}}
	svc, _ := newSourcesTest(t, jira, burnlite, off)

	results, err := svc.SearchRemote(context.Background(), "login")
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, r := range results {
		got[r.ID] = r.Provider
	}
	if len(got) != 2 || got["REX-1"] != "jira" || got["EST-7"] != "burnlite" {
		t.Errorf("results = %+v", results)
	}
	if len(off.searched) != 0 {
		t.Error("a provider that is not connected was searched")
	}
}

func TestMovingAReadOnlyProvidersCardStaysLocal(t *testing.T) {
	provider := &mockProvider{doTransEvents: make(chan doTransCall, 4)}
	jira := NewJiraSource(provider, "", nil, time.Minute, true)
	burnlite := &fakeSource{name: "burnlite", tickets: map[string]RemoteTicket{}}
	svc, s := newSourcesTest(t, jira, burnlite)
	ctx := context.Background()
	createProviderTask(t, s, "burnlite", "EST-7", "Backlog", time.Now())

	if err := svc.(CardMoveSync).PushMove(ctx, "EST-7", "Done"); err != nil {
		t.Fatal(err)
	}
	if task, _ := s.GetTask(ctx, "EST-7"); task.Status != "Done" {
		t.Errorf("status = %s", task.Status)
	}
	// The next sync's retry of failed pushes must leave it alone too.
	_, _ = svc.Sync(ctx)
	select {
	case call := <-provider.doTransEvents:
		t.Fatalf("Jira was asked to transition %+v", call)
	case <-time.After(100 * time.Millisecond):
	}
}

func TestWithNoProviderConnectedSearchAndImportSaySo(t *testing.T) {
	svc, _ := newSourcesTest(t, &fakeSource{name: "burnlite", disabled: true})
	ctx := context.Background()
	if _, err := svc.SearchRemote(ctx, "login"); !errors.Is(err, ErrNoTicketProvider) {
		t.Errorf("search err = %v", err)
	}
	if _, err := svc.ImportRemoteTask(ctx, "", "EST-1", nil); !errors.Is(err, ErrNoTicketProvider) {
		t.Errorf("import err = %v", err)
	}
	if _, err := svc.ImportRemoteTask(ctx, "burnlite", "EST-1", nil); !errors.Is(err, ErrNoTicketProvider) {
		t.Errorf("import from a named provider err = %v", err)
	}
}

func TestConnectedProvidersSayWhereTheirSitesAre(t *testing.T) {
	jira := NewJiraSourceWith(&mockProvider{}, JiraSourceOptions{Site: "https://example.atlassian.net/", Interval: time.Minute})
	burnlite := newBurnliteSource(BurnliteSourceOptions{Interval: time.Minute}, func(context.Context) (burnliteReader, string, error) {
		return &fakeBurnliteReader{}, "https://burnlite.example.com/base/", nil
	})
	off := newBurnliteSource(BurnliteSourceOptions{}, func(context.Context) (burnliteReader, string, error) { return nil, "", ErrNotConnected })
	svc, _ := newSourcesTest(t, jira, burnlite, &fakeSource{name: "nosite"})
	got := svc.(TicketProviderLister).TicketProviders(context.Background())
	want := []TicketProviderInfo{
		{Name: "jira", Label: "Jira", WebOrigin: "https://example.atlassian.net", KeyPattern: JiraKeyPattern},
		{Name: "burnlite", Label: "Burnlite", WebOrigin: "https://burnlite.example.com", KeyPattern: BurnliteKeyPattern},
	}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("providers = %+v", got)
	}
	svc, _ = newSourcesTest(t, off)
	if got := svc.(TicketProviderLister).TicketProviders(context.Background()); len(got) != 0 {
		t.Errorf("a provider that is not connected was listed: %+v", got)
	}
}
