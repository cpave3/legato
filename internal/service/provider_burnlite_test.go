package service

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/cpave3/legato/internal/engine/burnlite"
	"github.com/cpave3/legato/internal/engine/events"
	"github.com/cpave3/legato/internal/engine/store"
)

// fakeBurnliteReader is Burnlite's projects and tasks in memory.
type fakeBurnliteReader struct {
	projects []burnlite.Project
	rows     map[string][]burnlite.TaskRow // by project key
	tasks    map[string]*burnlite.Task     // by key
	listed   []string                      // project:state of each list
	got      []string                      // keys fetched in full
	listErr  error
}

func (f *fakeBurnliteReader) ListProjects(context.Context) ([]burnlite.Project, error) {
	return f.projects, nil
}

func (f *fakeBurnliteReader) ListTasks(_ context.Context, project, state string) ([]burnlite.TaskRow, error) {
	f.listed = append(f.listed, project+":"+state)
	if f.listErr != nil {
		return nil, f.listErr
	}
	rows, ok := f.rows[project]
	if !ok {
		return nil, &burnlite.Error{Code: "not_found", Message: "Project " + project + " not found"}
	}
	var out []burnlite.TaskRow
	for _, r := range rows {
		if state == "any" || r.State == state {
			out = append(out, r)
		}
	}
	return out, nil
}

func (f *fakeBurnliteReader) GetTask(_ context.Context, key string) (*burnlite.Task, error) {
	f.got = append(f.got, key)
	if t, ok := f.tasks[key]; ok {
		return t, nil
	}
	return nil, &burnlite.Error{Code: "not_found", Message: "Task " + key + " not found"}
}

var t0 = time.Date(2026, 11, 5, 9, 0, 0, 123_000_000, time.UTC)

func burnliteFixture() *fakeBurnliteReader {
	return &fakeBurnliteReader{
		projects: []burnlite.Project{{Key: "EST", Name: "Estimator"}, {Key: "OPS", Name: "Ops"}},
		rows: map[string][]burnlite.TaskRow{
			"EST": {
				{Key: "EST-1", URL: "/EST-1", Name: "Ready one", Kind: "work", State: "open", Frontier: true, UpdatedAt: t0},
				{Key: "EST-2", URL: "/EST-2", Name: "Blocked one", Kind: "work", State: "open", Blocked: true, OpenBlockers: []string{"EST-1"}, UpdatedAt: t0},
				{Key: "EST-3", URL: "/EST-3", Name: "Claimed and blocked", Kind: "meta", State: "open", Blocked: true, ClaimHolder: "agent-1",
					MilestoneKey: "EST-9", MilestoneName: "Sync", Labels: []string{"api", "sync"}, UpdatedAt: t0},
				{Key: "EST-4", URL: "/EST-4", Name: "Dropped", Kind: "work", State: "closed", CloseReason: "not_planned", UpdatedAt: t0},
			},
			"OPS": {{Key: "OPS-1", URL: "/OPS-1", Name: "Login alerts", Kind: "work", State: "open", Frontier: true, UpdatedAt: t0}},
		},
		tasks: map[string]*burnlite.Task{
			"EST-3": {Key: "EST-3", URL: "/EST-3", Name: "Claimed and blocked", Kind: "meta", State: "open", Blocked: true, ClaimHolder: "agent-1",
				Description: "What and why.", Plan: "1. Do it.", Labels: []string{"api", "sync"},
				Blockers:  []burnlite.Link{{Key: "EST-1", State: "open"}, {Key: "EST-0", State: "closed"}},
				Checklist: []burnlite.ChecklistItem{{Name: "first", Done: true}, {Name: "second"}}, UpdatedAt: t0},
			"EST-1": {Key: "EST-1", URL: "/EST-1", Name: "Ready one", Kind: "work", Size: "Large", Uncertainty: "High", State: "open", UpdatedAt: t0},
			"EST-4": {Key: "EST-4", URL: "/EST-4", Name: "Dropped", Kind: "work", State: "closed", CloseReason: "not_planned", ResolutionSummary: "Not needed", UpdatedAt: t0},
		},
	}
}

func newBurnliteTest(t *testing.T, reader *fakeBurnliteReader, projects []string) (SyncService, *store.Store) {
	t.Helper()
	svc, s := newSourcesTest(t)
	source := newBurnliteSource(BurnliteSourceOptions{
		Projects: func(context.Context) []string { return projects },
		Columns:  BurnliteColumns{Ready: "Ready", Blocked: "Backlog", Claimed: "Doing", Closed: "Done"},
		Interval: time.Minute, MovesCards: true,
	}, func(context.Context) (burnliteReader, string, error) {
		return reader, "https://burnlite.example.com", nil
	})
	svc = NewTicketSync(s, events.New(), nil, source)
	return svc, s
}

func TestABurnliteTaskIsImportedWithItsTextAndState(t *testing.T) {
	reader := burnliteFixture()
	svc, s := newBurnliteTest(t, reader, nil)
	ctx := context.Background()

	card, err := svc.ImportRemoteTask(ctx, "burnlite", "EST-3", nil)
	if err != nil {
		t.Fatal(err)
	}
	if card.Status != "Doing" {
		t.Errorf("a claimed task is in %s, want Doing (claimed comes before blocked)", card.Status)
	}
	task, _ := s.GetTask(ctx, "EST-3")
	if task.Title != "Claimed and blocked" || *task.Provider != "burnlite" {
		t.Errorf("task = %+v", task)
	}
	want := "What and why.\n\n## Plan\n\n1. Do it.\n\n## Checklist\n\n- [x] first\n- [ ] second\n"
	if task.RemoteDescriptionMD != want {
		t.Errorf("description = %q\nwant %q", task.RemoteDescriptionMD, want)
	}
	meta := parseRemoteMeta(task.RemoteMeta)
	if meta.RemoteStatus != "Claimed" || meta.Assignee != "agent-1" || meta.IssueType != "Meta" || meta.EpicKey != "EST-9" ||
		meta.EpicName != "Sync" || meta.Labels != "api,sync" || meta.URL != "https://burnlite.example.com/EST-3" || meta.BlockedBy != "EST-1" {
		t.Errorf("meta = %+v", meta)
	}
}

func TestEachBurnliteStateHasItsColumn(t *testing.T) {
	reader := burnliteFixture()
	reader.tasks["EST-2"] = &burnlite.Task{Key: "EST-2", Name: "Blocked one", State: "open", Blocked: true, UpdatedAt: t0}
	svc, s := newBurnliteTest(t, reader, nil)
	ctx := context.Background()
	for key, want := range map[string][2]string{"EST-1": {"Ready", "Ready"}, "EST-2": {"Blocked", "Backlog"}, "EST-4": {"Not planned", "Done"}} {
		card, err := svc.ImportRemoteTask(ctx, "burnlite", key, nil)
		if err != nil {
			t.Fatal(err)
		}
		task, _ := s.GetTask(ctx, key)
		if got := parseRemoteMeta(task.RemoteMeta).RemoteStatus; got != want[0] || card.Status != want[1] {
			t.Errorf("%s is %q in %s, want %q in %s", key, got, card.Status, want[0], want[1])
		}
	}
	task, _ := s.GetTask(ctx, "EST-1")
	if meta := parseRemoteMeta(task.RemoteMeta); meta.Size != "Large" || meta.Uncertainty != "High" {
		t.Errorf("estimate = %q, %q", meta.Size, meta.Uncertainty)
	}
	task, _ = s.GetTask(ctx, "EST-4")
	if meta := parseRemoteMeta(task.RemoteMeta); meta.Resolution != "Not needed" {
		t.Errorf("resolution = %q", meta.Resolution)
	}
}

func TestPullFetchesOnlyTheTasksThatChanged(t *testing.T) {
	reader := burnliteFixture()
	svc, s := newBurnliteTest(t, reader, nil)
	ctx := context.Background()
	for _, key := range []string{"EST-1", "EST-3"} {
		if _, err := svc.ImportRemoteTask(ctx, "burnlite", key, nil); err != nil {
			t.Fatal(err)
		}
	}
	reader.got, reader.listed = nil, nil

	// Nothing changed: one list for the project, nothing fetched.
	result, err := svc.Sync(ctx)
	if err != nil || result.TasksSynced != 0 || len(reader.got) != 0 || strings.Join(reader.listed, ",") != "EST:any" {
		t.Fatalf("synced %+v, %v; fetched %v, listed %v", result, err, reader.got, reader.listed)
	}

	// EST-3 is closed in Burnlite.
	later := t0.Add(time.Minute)
	reader.rows["EST"][2].State, reader.rows["EST"][2].UpdatedAt = "closed", later
	reader.tasks["EST-3"].State, reader.tasks["EST-3"].CloseReason, reader.tasks["EST-3"].UpdatedAt = "closed", "completed", later
	result, err = svc.Sync(ctx)
	if err != nil || result.TasksSynced != 1 || strings.Join(reader.got, ",") != "EST-3" {
		t.Fatalf("synced %+v, %v; fetched %v", result, err, reader.got)
	}
	if task, _ := s.GetTask(ctx, "EST-3"); task.Status != "Done" {
		t.Errorf("EST-3 is in %s", task.Status)
	}
}

func TestATaskBurnliteNoLongerListsIsStale(t *testing.T) {
	reader := burnliteFixture()
	svc, s := newBurnliteTest(t, reader, nil)
	ctx := context.Background()
	if _, err := svc.ImportRemoteTask(ctx, "burnlite", "EST-1", nil); err != nil {
		t.Fatal(err)
	}
	reader.rows["EST"] = reader.rows["EST"][1:]
	if _, err := svc.Sync(ctx); err != nil {
		t.Fatal(err)
	}
	if task, _ := s.GetTask(ctx, "EST-1"); parseRemoteMeta(task.RemoteMeta).StaleAt == "" {
		t.Error("not marked stale")
	}
	// A project that is gone takes its tasks with it.
	delete(reader.rows, "EST")
	if _, err := svc.Sync(ctx); err != nil {
		t.Fatalf("a missing project failed the sync: %v", err)
	}
}

func TestABurnliteWithoutUpdatedAtIsReportedAsTooOld(t *testing.T) {
	reader := burnliteFixture()
	svc, _ := newBurnliteTest(t, reader, nil)
	ctx := context.Background()
	if _, err := svc.ImportRemoteTask(ctx, "burnlite", "EST-1", nil); err != nil {
		t.Fatal(err)
	}
	reader.rows["EST"][0].UpdatedAt = time.Time{}
	_, err := svc.Sync(ctx)
	if err == nil || !strings.Contains(err.Error(), "updatedAt") {
		t.Fatalf("err = %v", err)
	}
}

func TestSearchLooksInTheProjectsAndMatchesNamesAndKeys(t *testing.T) {
	reader := burnliteFixture()
	svc, _ := newBurnliteTest(t, reader, nil)
	ctx := context.Background()

	results, err := svc.SearchRemote(ctx, "LOGIN")
	if err != nil || len(results) != 1 || results[0].ID != "OPS-1" || results[0].Provider != "burnlite" || results[0].Status != "Ready" {
		t.Fatalf("results = %+v, %v", results, err)
	}
	// Every project when none is configured, open tasks only.
	if strings.Join(reader.listed, ",") != "EST:open,OPS:open" {
		t.Errorf("listed %v", reader.listed)
	}
	// A key finds a closed task too.
	results, err = svc.SearchRemote(ctx, "est-4")
	if err != nil || len(results) != 1 || results[0].ID != "EST-4" {
		t.Fatalf("results = %+v, %v", results, err)
	}

	// Configured projects narrow the search.
	reader2 := burnliteFixture()
	svc2, _ := newBurnliteTest(t, reader2, []string{"EST"})
	if results, _ := svc2.SearchRemote(ctx, "login"); len(results) != 0 || strings.Join(reader2.listed, ",") != "EST:open" {
		t.Errorf("results = %+v, listed %v", results, reader2.listed)
	}
}

func TestBurnliteIsSkippedUntilItIsConnected(t *testing.T) {
	_, s := newSourcesTest(t)
	source := newBurnliteSource(BurnliteSourceOptions{Interval: time.Minute}, func(context.Context) (burnliteReader, string, error) {
		return nil, "", ErrNotConnected
	})
	svc := NewTicketSync(s, events.New(), nil, source)
	if _, err := svc.ImportRemoteTask(context.Background(), "burnlite", "EST-1", nil); err == nil {
		t.Fatal("an import from Burnlite before it is connected was accepted")
	}
	result, err := svc.Sync(context.Background())
	if err != nil || !result.Sources["burnlite"].Disabled {
		t.Fatalf("result = %+v, %v", result, err)
	}
}

func TestBurnliteProjectsComeFromConfigElseWhatTheWebUISaved(t *testing.T) {
	db := newIntegrationStore(t)
	ctx := context.Background()
	if got := BurnliteProjects(ctx, db, nil); len(got) != 0 {
		t.Errorf("nothing saved = %v", got)
	}
	if err := SaveBurnliteProjects(ctx, db, []string{" est ", "OPS", "", "est"}); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(BurnliteProjects(ctx, db, nil), ","); got != "EST,OPS" {
		t.Errorf("saved = %s", got)
	}
	if got := strings.Join(BurnliteProjects(ctx, db, []string{"CFG"}), ","); got != "CFG" {
		t.Errorf("configured = %s", got)
	}
	if err := SaveBurnliteProjects(ctx, db, []string{"not a key!"}); err == nil {
		t.Error("a project key Burnlite would never give was saved")
	}
}
