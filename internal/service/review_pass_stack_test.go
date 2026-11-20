package service

import (
	"context"
	"fmt"
	"slices"
	"testing"
)

// fakePulls is GitHub's open pull requests of acme/widget.
type fakePulls struct {
	prs   []ReviewGitHubPR
	calls int
}

func (f *fakePulls) open(number int, branch, base string) *fakePulls {
	f.prs = append(f.prs, ReviewGitHubPR{Number: number, Branch: branch, Base: base, HeadSHA: fmt.Sprintf("sha%d", number), Author: "legato-author[bot]", HeadRepo: "acme/widget"})
	return f
}

func (f *fakePulls) OpenPulls(_ context.Context, _ string, q PullQuery) ([]ReviewGitHubPR, error) {
	f.calls++
	var out []ReviewGitHubPR
	for _, pr := range f.prs {
		if (q.Head != "" && pr.Branch == q.Head) || (q.Base != "" && pr.Base == q.Base) {
			out = append(out, pr)
		}
	}
	return out, nil
}

func numbers(prs []ReviewGitHubPR) []int {
	var out []int
	for _, pr := range prs {
		out = append(out, pr.Number)
	}
	return out
}

// linear is cpave3/legato #137 to #141, each on the one before.
func linear() *fakePulls {
	f := &fakePulls{}
	f.open(137, "s1", "main").open(138, "s2", "s1").open(139, "s3", "s2").open(140, "s4", "s3").open(141, "s5", "s4")
	return f
}

func TestALinearStackIsFoundFromAnyOfItsBranchesInOrder(t *testing.T) {
	for _, seed := range []string{"s1", "s3", "s5"} {
		stack, err := FindReviewStack(context.Background(), linear(), "acme/widget", []string{seed}, nil)
		if err != nil || !slices.Equal(numbers(stack.PRs), []int{137, 138, 139, 140, 141}) || stack.CutShort {
			t.Fatalf("from %s: %v, cut short %v, %v", seed, numbers(stack.PRs), stack.CutShort, err)
		}
	}
}

func TestAForkIsKeptAsATreeDepthFirst(t *testing.T) {
	f := &fakePulls{}
	f.open(10, "a", "main").open(11, "b1", "a").open(12, "b2", "a").open(13, "c1", "b1")

	stack, err := FindReviewStack(context.Background(), f, "acme/widget", []string{"b2"}, nil)
	if err != nil || !slices.Equal(numbers(stack.PRs), []int{10, 11, 13, 12}) {
		t.Fatalf("stack %v, %v", numbers(stack.PRs), err)
	}
	// A Staccato graph orders the siblings.
	stack, _ = FindReviewStack(context.Background(), f, "acme/widget", []string{"b2"}, []string{"a", "b2", "b1", "c1"})
	if !slices.Equal(numbers(stack.PRs), []int{10, 12, 11, 13}) {
		t.Fatalf("ordered by the graph: %v", numbers(stack.PRs))
	}
}

func TestAMergedBottomRetargetedToMainIsLeftOut(t *testing.T) {
	// #137 merged; GitHub moved #138's base to main.
	f := &fakePulls{}
	f.open(138, "s2", "main").open(139, "s3", "s2")

	stack, err := FindReviewStack(context.Background(), f, "acme/widget", []string{"s1", "s3"}, nil)
	if err != nil || !slices.Equal(numbers(stack.PRs), []int{138, 139}) {
		t.Fatalf("stack %v, %v", numbers(stack.PRs), err)
	}
}

func TestAStrangersPullRequestOnOurHeadIsLeftOut(t *testing.T) {
	f := linear()
	f.prs = append(f.prs,
		ReviewGitHubPR{Number: 200, Branch: "theirs", Base: "s5", Author: "stranger", HeadRepo: "acme/widget"},
		ReviewGitHubPR{Number: 201, Branch: "fork-branch", Base: "s5", Author: "legato-author[bot]", HeadRepo: "someone/widget"})

	stack, err := FindReviewStack(context.Background(), f, "acme/widget", []string{"s5"}, nil)
	if err != nil || !slices.Equal(numbers(stack.PRs), []int{137, 138, 139, 140, 141}) {
		t.Fatalf("stack %v, %v", numbers(stack.PRs), err)
	}
}

func TestAStackIsCutShortAtTheCap(t *testing.T) {
	f := &fakePulls{}
	base := "main"
	for i := 1; i <= MaxReviewStack+5; i++ {
		branch := fmt.Sprintf("s%d", i)
		f.open(i, branch, base)
		base = branch
	}

	stack, err := FindReviewStack(context.Background(), f, "acme/widget", []string{"s1"}, nil)
	if err != nil || len(stack.PRs) != MaxReviewStack || !stack.CutShort || stack.PRs[0].Number != 1 {
		t.Fatalf("%d pull requests, cut short %v, %v", len(stack.PRs), stack.CutShort, err)
	}
}

func TestASeedWithNoOpenPullRequestIsAnEmptyStack(t *testing.T) {
	stack, err := FindReviewStack(context.Background(), linear(), "acme/widget", []string{"nothing"}, nil)
	if err != nil || len(stack.PRs) != 0 {
		t.Fatalf("stack %v, %v", numbers(stack.PRs), err)
	}
}
