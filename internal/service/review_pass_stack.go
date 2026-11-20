package service

import (
	"context"
	"log"
	"slices"
	"sort"
	"strings"
)

// MaxReviewStack is the most pull requests a discovered stack holds; a
// stack with more is cut short (ReviewStack.CutShort).
const MaxReviewStack = 20

// PullQuery asks for a repository's open pull requests by head branch or by
// base branch (one of the two).
type PullQuery struct {
	Head string
	Base string
}

// OpenPullLister lists a repository's open pull requests (ReviewPublisher's
// OpenPulls, as the reviewer App).
type OpenPullLister interface {
	OpenPulls(ctx context.Context, repo string, q PullQuery) ([]ReviewGitHubPR, error)
}

// ReviewStack is a stack of open pull requests found on GitHub, bottom
// first, a fork kept as a tree walked depth first.
type ReviewStack struct {
	PRs []ReviewGitHubPR
	// CutShort says the stack had more than MaxReviewStack pull requests:
	// the rest were left out.
	CutShort bool
}

// Branches are the stack's head branches, in its order.
func (s ReviewStack) Branches() []string {
	out := make([]string, 0, len(s.PRs))
	for _, pr := range s.PRs {
		out = append(out, pr.Branch)
	}
	return out
}

// FindReviewStack follows GitHub's base→head chain from seeds, branches of
// the work: down from each seed's open pull request base by base until a
// base has no open pull request (the default branch, say), and up through
// open pull requests whose base is a head already in the stack. Only pull
// requests from the repository itself, by an author of the seeds' pull
// requests, are kept, so a stranger's pull request on a stack branch is not
// pulled in. Closed and merged layers are not open, so they are skipped:
// GitHub retargets the next one's base. order, a Staccato graph's branches
// when there is one, orders siblings; otherwise they go by number. No seed
// with an open pull request is an empty stack.
func FindReviewStack(ctx context.Context, gh OpenPullLister, repo string, seeds, order []string) (ReviewStack, error) {
	byNumber := map[int]ReviewGitHubPR{}
	heads := map[string]bool{}
	authors := map[string]bool{}
	var stack ReviewStack
	add := func(pr ReviewGitHubPR) bool {
		if _, ok := byNumber[pr.Number]; ok {
			return false
		}
		if len(byNumber) >= MaxReviewStack {
			stack.CutShort = true
			return false
		}
		byNumber[pr.Number] = pr
		heads[pr.Branch] = true
		return true
	}
	ours := func(pr ReviewGitHubPR) bool {
		return (pr.HeadRepo == "" || strings.EqualFold(pr.HeadRepo, repo)) && authors[strings.ToLower(pr.Author)]
	}

	var queue []ReviewGitHubPR
	for _, seed := range seeds {
		if seed == "" || heads[seed] {
			continue
		}
		prs, err := gh.OpenPulls(ctx, repo, PullQuery{Head: seed})
		if err != nil {
			return ReviewStack{}, err
		}
		for _, pr := range prs {
			if pr.HeadRepo != "" && !strings.EqualFold(pr.HeadRepo, repo) {
				continue
			}
			authors[strings.ToLower(pr.Author)] = true
			if add(pr) {
				queue = append(queue, pr)
			}
		}
	}
	// Down: each pull request's base, while it is the head of an open
	// pull request of ours.
	seenBase := map[string]bool{}
	for i := 0; i < len(queue); i++ {
		base := queue[i].Base
		if base == "" || seenBase[base] {
			continue
		}
		seenBase[base] = true
		prs, err := gh.OpenPulls(ctx, repo, PullQuery{Head: base})
		if err != nil {
			return ReviewStack{}, err
		}
		for _, pr := range prs {
			if ours(pr) && add(pr) {
				queue = append(queue, pr)
			}
		}
	}
	// Up: open pull requests of ours on any head in the stack, forks
	// included.
	up := make([]string, 0, len(heads))
	for _, pr := range queue {
		up = append(up, pr.Branch)
	}
	seenHead := map[string]bool{}
	for i := 0; i < len(up); i++ {
		if seenHead[up[i]] {
			continue
		}
		seenHead[up[i]] = true
		prs, err := gh.OpenPulls(ctx, repo, PullQuery{Base: up[i]})
		if err != nil {
			return ReviewStack{}, err
		}
		for _, pr := range prs {
			if ours(pr) && add(pr) {
				up = append(up, pr.Branch)
			}
		}
	}
	stack.PRs = orderStack(byNumber, order)
	if stack.CutShort {
		log.Printf("review passes: the stack of %s from %s has more than %d pull requests; reviewing the first %d", repo, strings.Join(seeds, ", "), MaxReviewStack, MaxReviewStack)
	}
	return stack, nil
}

// orderStack walks the stack's tree depth first from its roots (pull
// requests whose base is no other's head), siblings by their place in order,
// then by number.
func orderStack(byNumber map[int]ReviewGitHubPR, order []string) []ReviewGitHubPR {
	heads := map[string]bool{}
	children := map[string][]ReviewGitHubPR{}
	for _, pr := range byNumber {
		heads[pr.Branch] = true
	}
	var roots []ReviewGitHubPR
	for _, pr := range byNumber {
		if heads[pr.Base] && pr.Base != pr.Branch {
			children[pr.Base] = append(children[pr.Base], pr)
		} else {
			roots = append(roots, pr)
		}
	}
	rank := func(branch string) int {
		if i := slices.Index(order, branch); i >= 0 {
			return i
		}
		return len(order)
	}
	sortPRs := func(prs []ReviewGitHubPR) {
		sort.Slice(prs, func(i, j int) bool {
			if ri, rj := rank(prs[i].Branch), rank(prs[j].Branch); ri != rj {
				return ri < rj
			}
			return prs[i].Number < prs[j].Number
		})
	}
	var out []ReviewGitHubPR
	visited := map[int]bool{}
	var walk func(pr ReviewGitHubPR)
	walk = func(pr ReviewGitHubPR) {
		if visited[pr.Number] {
			return
		}
		visited[pr.Number] = true
		out = append(out, pr)
		kids := children[pr.Branch]
		sortPRs(kids)
		for _, kid := range kids {
			walk(kid)
		}
	}
	sortPRs(roots)
	for _, root := range roots {
		walk(root)
	}
	// A cycle (two pull requests on each other's heads) has no root:
	// whatever is left goes last, by number.
	var rest []ReviewGitHubPR
	for _, pr := range byNumber {
		if !visited[pr.Number] {
			rest = append(rest, pr)
		}
	}
	sortPRs(rest)
	return append(out, rest...)
}
