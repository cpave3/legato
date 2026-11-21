package service

import (
	"context"
	"reflect"
	"strconv"
	"testing"

	"github.com/cpave3/legato/internal/engine/store"
)

// stackRow is a tracked branch with a PR on base.
func stackRow(repo, branch, base string, number int, state string) store.TaskBranch {
	return store.TaskBranch{Repo: repo, Branch: branch, PR: &store.PRMeta{PRNumber: number, State: state, Base: base}}
}

func prNumbers(prs []PRMetaView) []string {
	out := make([]string, len(prs))
	for i, pr := range prs {
		out[i] = pr.Repo + "#" + strconv.Itoa(pr.PRNumber)
	}
	return out
}

func TestTaskPullRequestsAreInStackOrderBottomFirst(t *testing.T) {
	task := &store.Task{ID: "t1"}
	t.Run("the bottom tracked last comes first", func(t *testing.T) {
		rows := []store.TaskBranch{
			stackRow("acme/app", "b195", "b194", 195, "OPEN"),
			stackRow("acme/app", "b196", "b195", 196, "OPEN"),
			stackRow("acme/app", "b194", "main", 194, "OPEN"),
		}
		if got, want := prNumbers(TaskPullRequests(task, rows)), []string{"acme/app#194", "acme/app#195", "acme/app#196"}; !reflect.DeepEqual(got, want) {
			t.Fatalf("order = %v, want %v", got, want)
		}
	})
	t.Run("merged layers retargeted to main stay at the bottom", func(t *testing.T) {
		rows := []store.TaskBranch{
			stackRow("acme/app", "b195", "main", 195, "MERGED"),
			stackRow("acme/app", "b196", "main", 196, "OPEN"),
			stackRow("acme/app", "b197", "b196", 197, "OPEN"),
			stackRow("acme/app", "b194", "main", 194, "MERGED"),
		}
		if got, want := prNumbers(TaskPullRequests(task, rows)), []string{"acme/app#194", "acme/app#195", "acme/app#196", "acme/app#197"}; !reflect.DeepEqual(got, want) {
			t.Fatalf("order = %v, want %v", got, want)
		}
	})
	t.Run("roots come first, then each PR's children by number", func(t *testing.T) {
		rows := []store.TaskBranch{
			stackRow("acme/api", "b13", "b10", 13, "OPEN"),
			stackRow("acme/api", "b10", "main", 10, "MERGED"),
			stackRow("acme/api", "b12", "b10", 12, "OPEN"),
			stackRow("acme/api", "b11", "main", 11, "CLOSED"),
		}
		if got, want := prNumbers(TaskPullRequests(task, rows)), []string{"acme/api#10", "acme/api#11", "acme/api#12", "acme/api#13"}; !reflect.DeepEqual(got, want) {
			t.Fatalf("order = %v, want %v", got, want)
		}
	})
	t.Run("each repo keeps its first position", func(t *testing.T) {
		rows := []store.TaskBranch{
			stackRow("acme/web", "w5", "w4", 5, "OPEN"),
			stackRow("acme/api", "b10", "main", 10, "OPEN"),
			stackRow("acme/web", "w4", "main", 4, "OPEN"),
		}
		if got, want := prNumbers(TaskPullRequests(task, rows)), []string{"acme/web#4", "acme/web#5", "acme/api#10"}; !reflect.DeepEqual(got, want) {
			t.Fatalf("order = %v, want %v", got, want)
		}
	})
	t.Run("PRs with no base yet keep their stored order, after the chained ones", func(t *testing.T) {
		rows := []store.TaskBranch{
			stackRow("acme/app", "x", "", 30, "OPEN"),
			stackRow("acme/app", "b2", "b1", 2, "OPEN"),
			stackRow("acme/app", "y", "", 20, "OPEN"),
			stackRow("acme/app", "b1", "main", 1, "OPEN"),
		}
		if got, want := prNumbers(TaskPullRequests(task, rows)), []string{"acme/app#1", "acme/app#2", "acme/app#30", "acme/app#20"}; !reflect.DeepEqual(got, want) {
			t.Fatalf("order = %v, want %v", got, want)
		}
	})
	t.Run("a PR built on one with no base yet follows it", func(t *testing.T) {
		rows := []store.TaskBranch{
			stackRow("acme/app", "b2", "b1", 2, "OPEN"),
			stackRow("acme/app", "b1", "", 1, "OPEN"),
		}
		if got, want := prNumbers(TaskPullRequests(task, rows)), []string{"acme/app#1", "acme/app#2"}; !reflect.DeepEqual(got, want) {
			t.Fatalf("order = %v, want %v", got, want)
		}
	})
	t.Run("each view names its base", func(t *testing.T) {
		prs := TaskPullRequests(task, []store.TaskBranch{stackRow("acme/app", "b1", "main", 1, "OPEN")})
		if prs[0].Base != "main" {
			t.Fatalf("base = %q", prs[0].Base)
		}
	})
}

func TestAPRsBaseIsKeptAndFilledOnRefresh(t *testing.T) {
	gh := newFakeGitHub(t)
	gh.set("acme/widget", fakePR{Number: 21, Head: "legato/task-1-a", Base: "main"}, fakePR{Number: 22, Head: "legato/task-1-b", Base: "legato/task-1-a"})
	s, svc, _ := newBranchTracking(t, gh)
	ctx := context.Background()
	createPRTask(t, s, "task-1")
	setRepoContext(t, s, "task-1", "acme/widget", "")
	if _, err := svc.AddTaskBranches(ctx, "task-1", "acme/widget", []string{"legato/task-1-a", "legato/task-1-b"}); err != nil {
		t.Fatal(err)
	}
	if err := svc.PollAll(ctx); err != nil {
		t.Fatal(err)
	}
	bases := func() []string {
		rows, err := s.ListTaskBranches(ctx, "task-1")
		if err != nil {
			t.Fatal(err)
		}
		var out []string
		for _, row := range rows {
			if row.PR == nil {
				t.Fatalf("branch %s has no PR", row.Branch)
			}
			out = append(out, row.PR.Base)
		}
		return out
	}
	if got := bases(); !reflect.DeepEqual(got, []string{"main", "legato/task-1-a"}) {
		t.Fatalf("bases = %v", got)
	}

	// A PR tracked before bases were kept gets one on its next refresh.
	rows, _ := s.ListTaskBranches(ctx, "task-1")
	old := *rows[1].PR
	old.Base = ""
	if err := s.SetTaskBranchPR(ctx, "task-1", "acme/widget", "legato/task-1-b", &old); err != nil {
		t.Fatal(err)
	}
	if err := svc.PollAll(ctx); err != nil {
		t.Fatal(err)
	}
	if got := bases(); !reflect.DeepEqual(got, []string{"main", "legato/task-1-a"}) {
		t.Fatalf("bases after refresh = %v", got)
	}
}
