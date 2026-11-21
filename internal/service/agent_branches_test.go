package service

import (
	"context"
	"io"
	"net/http"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cpave3/legato/internal/engine/store"
)

// recordedBranches records what the credential channels tell their branch
// listener.
type recordedBranches struct {
	mu   sync.Mutex
	seen []AgentBranch
}

func (r *recordedBranches) listen(b AgentBranch) {
	r.mu.Lock()
	r.seen = append(r.seen, b)
	r.mu.Unlock()
}

func (r *recordedBranches) all() []AgentBranch {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]AgentBranch(nil), r.seen...)
}

func allowSession(t *testing.T, f githubFixture) {
	t.Helper()
	approval := f.pending(t)
	if _, err := f.approvals.Decide(context.Background(), approval.ID, ApprovalDecision{Decision: ApprovalAllow, Option: "session", By: "web"}); err != nil {
		t.Fatal(err)
	}
}

func (f githubFixture) post(t *testing.T, path, body string) (int, string) {
	t.Helper()
	req, _ := http.NewRequest("POST", f.proxy.URL+path, strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+f.env["LEGATO_GITHUB_TOKEN"])
	resp, err := http.DefaultTransport.RoundTrip(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(data)
}

func TestAPullRequestAnAgentOpensThroughTheProxyNamesItsBranch(t *testing.T) {
	f := newGitHubFixture(t)
	heard := &recordedBranches{}
	f.channels.SetBranchListener(heard.listen)
	f.broker.wait = 50 * time.Millisecond
	open := `{"head":"legato/task-1-rail","base":"main","title":"Rail"}`
	if status, _ := f.post(t, "/c/gh/api/repos/cpave3/legato/pulls", open); status != 403 {
		t.Fatalf("status %d", status)
	}
	if got := heard.all(); len(got) != 0 {
		t.Fatalf("a refused pull request was heard: %+v", got)
	}
	allowSession(t, f)
	if status, body := f.post(t, "/c/gh/api/repos/cpave3/legato/pulls", open); status != 201 {
		t.Fatalf("status %d %s", status, body)
	}
	want := AgentBranch{TaskID: "task-1", ExecutionID: "ex-1", Repo: "cpave3/legato", Branch: "legato/task-1-rail", Opened: true}
	if got := heard.all(); len(got) != 1 || got[0] != want {
		t.Fatalf("heard %+v, want %+v", got, want)
	}
}

func TestGHTellsLegatoOfThePullRequestItOpened(t *testing.T) {
	f := newGitHubFixture(t)
	heard := &recordedBranches{}
	f.channels.SetBranchListener(heard.listen)
	if status, body := f.post(t, "/c/gh/pr-opened", `{"repo":"cpave3/legato","branch":"legato/task-1-rail"}`); status != 204 {
		t.Fatalf("status %d %s", status, body)
	}
	want := AgentBranch{TaskID: "task-1", ExecutionID: "ex-1", Repo: "cpave3/legato", Branch: "legato/task-1-rail", Opened: true}
	if got := heard.all(); len(got) != 1 || got[0] != want {
		t.Fatalf("heard %+v, want %+v", got, want)
	}
	for _, bad := range []string{`{"repo":"cpave3/legato","branch":"../x"}`, `{"repo":"nope","branch":"legato/x"}`} {
		if status, _ := f.post(t, "/c/gh/pr-opened", bad); status != 400 {
			t.Fatalf("%s: status %d", bad, status)
		}
	}
	req, _ := http.NewRequest("POST", f.proxy.URL+"/c/gh/pr-opened", strings.NewReader(`{"repo":"cpave3/legato","branch":"legato/x"}`))
	req.Header.Set("Authorization", "Bearer not-a-placeholder")
	resp, err := http.DefaultTransport.RoundTrip(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 401 || len(heard.all()) != 1 {
		t.Fatalf("a foreign key: status %d, heard %+v", resp.StatusCode, heard.all())
	}
}

func TestAnAgentsPushOfALegatoBranchNamesIt(t *testing.T) {
	f := newGitHubFixture(t)
	heard := &recordedBranches{}
	f.channels.SetBranchListener(heard.listen)
	_, _ = f.push(t, "HEAD:refs/heads/legato/task-1-rail")
	allowSession(t, f)
	if out, err := f.push(t, "HEAD:refs/heads/legato/task-1-rail"); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	want := AgentBranch{TaskID: "task-1", ExecutionID: "ex-1", Repo: "cpave3/legato", Branch: "legato/task-1-rail"}
	// The proxy hears GitHub's answer as git does, so it may tell a moment
	// after git ends.
	for deadline := time.Now().Add(2 * time.Second); len(heard.all()) == 0 && time.Now().Before(deadline); {
		time.Sleep(5 * time.Millisecond)
	}
	if got := heard.all(); len(got) != 1 || got[0] != want {
		t.Fatalf("heard %+v, want %+v", got, want)
	}
}

func TestAnAgentsBranchIsTrackedOnItsTaskAndItsPullRequestFoundAtOnce(t *testing.T) {
	gh := newFakeGitHub(t)
	s, svc, _ := newBranchTracking(t, gh)
	ctx := context.Background()
	createPRTask(t, s, "task-1")
	// The agent started on its clone's branch, and builds another.
	if err := s.SetCodeContext(ctx, "task-1", &store.CodeContext{Repo: "acme/widget", Branch: "legato/task-1", BaseBranch: "main", RepoSource: "job_clone", BranchSource: "agent"}); err != nil {
		t.Fatal(err)
	}
	branches := NewAgentBranches(s, svc)

	// A push: the branch is tracked before any pull request exists.
	branches.Track(AgentBranch{TaskID: "task-1", ExecutionID: "ex-1", Repo: "acme/widget", Branch: "legato/task-1-rail"})
	rows, _ := s.ListTaskBranches(ctx, "task-1")
	if !hasBranch(rows, "legato/task-1-rail", BranchSourceAgent) {
		t.Fatalf("tracked = %+v", rows)
	}
	branches.Wait()

	// The pull request is opened from it: found with no poll.
	gh.set("acme/widget", fakePR{Number: 210, Head: "legato/task-1-rail"})
	branches.Track(AgentBranch{TaskID: "task-1", ExecutionID: "ex-1", Repo: "acme/widget", Branch: "legato/task-1-rail", Opened: true})
	branches.Wait()
	if got := prList(t, svc, "task-1"); !reflect.DeepEqual(got, []string{"legato/task-1", "legato/task-1-rail:#210"}) {
		t.Fatalf("tracked = %v", got)
	}

	// A branch outside legato/** that was only pushed, and a session with
	// no task, are left alone.
	branches.Track(AgentBranch{TaskID: "task-1", Repo: "acme/widget", Branch: "feature"})
	branches.Track(AgentBranch{TaskID: "nope", Repo: "acme/widget", Branch: "legato/x", Opened: true})
	branches.Wait()
	if got := prList(t, svc, "task-1"); len(got) != 2 {
		t.Fatalf("tracked = %v", got)
	}
}

func hasBranch(rows []store.TaskBranch, branch, source string) bool {
	for _, row := range rows {
		if row.Branch == branch && row.Source == source {
			return true
		}
	}
	return false
}

func TestTheAgentsContextFollowsItsCheckoutToAnotherBranch(t *testing.T) {
	gh := newFakeGitHub(t)
	gh.set("acme/widget", fakePR{Number: 210, Head: "legato/task-1-rail"})
	s, svc, _ := newBranchTracking(t, gh)
	ctx := context.Background()
	createPRTask(t, s, "task-1")
	createPRTask(t, s, "task-2")
	dir1, dir2 := t.TempDir(), t.TempDir()
	heads := map[string]string{dir1: "legato/task-1-rail", dir2: "legato/task-2-other"}
	svc.SetCheckoutHead(func(dir string) (string, string, error) { return heads[dir], "c0ffee", nil })
	if err := s.SetCodeContext(ctx, "task-1", &store.CodeContext{Repo: "acme/widget", Branch: "legato/task-1", BaseBranch: "main", WorkingDir: dir1, RepoSource: "job_clone", BranchSource: "agent"}); err != nil {
		t.Fatal(err)
	}
	// A context set by hand never moves.
	if err := s.SetCodeContext(ctx, "task-2", &store.CodeContext{Repo: "acme/widget", Branch: "legato/task-2", WorkingDir: dir2, RepoSource: "manual", BranchSource: "manual"}); err != nil {
		t.Fatal(err)
	}

	if err := svc.PollAll(ctx); err != nil {
		t.Fatal(err)
	}
	task, _ := s.GetTask(ctx, "task-1")
	cc, _ := store.ParseCodeContext(task.CodeContext)
	if cc.Branch != "legato/task-1-rail" || cc.BranchSource != "agent" || cc.HeadSHA != "c0ffee" || cc.WorkingDir != dir1 || cc.BaseBranch != "main" {
		t.Fatalf("context = %+v", cc)
	}
	// The branch it started on stays tracked, below the one it moved to.
	if got := prList(t, svc, "task-1"); !reflect.DeepEqual(got, []string{"legato/task-1", "legato/task-1-rail:#210"}) {
		t.Fatalf("tracked = %v", got)
	}
	task, _ = s.GetTask(ctx, "task-2")
	if cc, _ := store.ParseCodeContext(task.CodeContext); cc.Branch != "legato/task-2" {
		t.Fatalf("a manual context moved: %+v", cc)
	}

	// A checkout on the default branch, or detached, is not followed.
	heads[dir1] = "main"
	_ = svc.PollAll(ctx)
	heads[dir1] = ""
	_ = svc.PollAll(ctx)
	task, _ = s.GetTask(ctx, "task-1")
	if cc, _ := store.ParseCodeContext(task.CodeContext); cc.Branch != "legato/task-1-rail" {
		t.Fatalf("context = %+v", cc)
	}
}

func TestLookingAgainInARepositoryLinksItsTasksAndNoOthers(t *testing.T) {
	gh := newFakeGitHub(t)
	gh.set("acme/widget", fakePR{Number: 210, Head: "legato/leg-423-rail-sheet"})
	gh.set("acme/other", fakePR{Number: 5, Head: "legato/leg-9"})
	s, svc, _ := newBranchTracking(t, gh)
	ctx := context.Background()
	createPRTask(t, s, "LEG-423")
	createPRTask(t, s, "LEG-9")
	if err := s.SetCodeContext(ctx, "LEG-423", &store.CodeContext{Repo: "acme/widget", Branch: "legato/leg-423", RepoSource: "job_clone", BranchSource: "agent"}); err != nil {
		t.Fatal(err)
	}
	if err := s.SetCodeContext(ctx, "LEG-9", &store.CodeContext{Repo: "acme/other", Branch: "legato/leg-9", RepoSource: "job_clone", BranchSource: "agent"}); err != nil {
		t.Fatal(err)
	}
	if err := svc.DiscoverRepository(ctx, "Acme/Widget"); err != nil {
		t.Fatal(err)
	}
	if got := prList(t, svc, "LEG-423"); !holdsPRNumber(got, 210) {
		t.Fatalf("LEG-423 tracked = %v", got)
	}
	if n := gh.count("/repos/acme/other/pulls"); n != 0 {
		t.Fatalf("listed another repository %d times", n)
	}
}

func holdsPRNumber(list []string, number int) bool {
	for _, item := range list {
		if strings.Contains(item, ":#"+strconv.Itoa(number)) {
			return true
		}
	}
	return false
}

// The incident of LEG-437: the agent started on its clone's branch, opened
// a pull request from another through the broker, and a review loop asked
// at once. The task holds the pull request as soon as the lookup the
// broker started ends, with no poll.
func TestAPullRequestOpenedThroughTheBrokerIsLinkedToTheTaskWithNoPoll(t *testing.T) {
	f := newGitHubFixture(t)
	ctx := context.Background()
	if err := f.store.SetCodeContext(ctx, "task-1", &store.CodeContext{Repo: "cpave3/legato", Branch: "legato/task-1", BaseBranch: "main", RepoSource: "job_clone", BranchSource: "agent"}); err != nil {
		t.Fatal(err)
	}
	listings := newFakeGitHub(t)
	prs := NewPRTrackingService(f.store, nil, listings.client(), time.Hour, time.Hour).(*prTrackingService)
	branches := NewAgentBranches(f.store, prs)
	f.channels.SetBranchListener(branches.Track)
	f.broker.wait = 50 * time.Millisecond

	open := `{"head":"legato/task-1-rail-sheet","base":"main","title":"Rail sheet"}`
	_, _ = f.post(t, "/c/gh/api/repos/cpave3/legato/pulls", open)
	allowSession(t, f)
	listings.set("cpave3/legato", fakePR{Number: 210, Head: "legato/task-1-rail-sheet"})
	if status, body := f.post(t, "/c/gh/api/repos/cpave3/legato/pulls", open); status != 201 {
		t.Fatalf("status %d %s", status, body)
	}
	branches.Wait()
	if got := prList(t, prs, "task-1"); !reflect.DeepEqual(got, []string{"legato/task-1", "legato/task-1-rail-sheet:#210"}) {
		t.Fatalf("tracked = %v", got)
	}
}
