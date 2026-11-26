package service

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// PRWatch is one layer of a loop as the steward's watch read it: one
// GraphQL read of the loop's pull requests (spec LEG-559 C7).
type PRWatch struct {
	Number int
	// State is open, closed or merged.
	State      string
	Draft      bool
	HeadSHA    string
	HeadBranch string
	BaseBranch string
	// Mergeable is GitHub's word: MERGEABLE, CONFLICTING or UNKNOWN.
	Mergeable string
	// MergeState is GitHub's mergeStateStatus: CLEAN, BLOCKED, BEHIND,
	// DIRTY, DRAFT, HAS_HOOKS, UNSTABLE or UNKNOWN.
	MergeState string
	// ReviewDecision is APPROVED, CHANGES_REQUESTED, REVIEW_REQUIRED, or
	// "" when the base requires no review.
	ReviewDecision string
	// DismissedReviews are the IDs of the pull request's reviews that were
	// dismissed (the newest 50): legato-reviews' approval among them with
	// the head unmoved was dismissed by a person, not by a push.
	DismissedReviews []int64
	// Rollup is the head's check rollup: CheckPassing, CheckFailing,
	// CheckPending, or "" when the head has no checks.
	Rollup string
	// Checks are the rollup's checks, check runs and commit statuses;
	// ChecksTruncated says the head has more than the first 100.
	Checks          []WatchCheck
	ChecksTruncated bool
	UpdatedAt       time.Time
}

// WatchCheck is one check of a head's rollup.
type WatchCheck struct {
	Name string
	// State is CheckPending, CheckPassing or CheckFailing.
	State string
	// Conclusion is GitHub's: a check run's conclusion, a status's state;
	// "" while a check run has not completed.
	Conclusion string
	// Required says branch protection requires it for this pull request.
	Required bool
	URL      string
	// RunID is a check run's database ID (its annotations), 0 for a status.
	RunID int64
}

// ReviewWatchGitHub opens the steward's reads of a loop: ReviewPublisher.
type ReviewWatchGitHub interface {
	WatchSession(repo string) ReviewWatchSession
}

// ReviewWatchSession is one loop's reads in one tick, as the reviewer App,
// with one token: made on the first read, used for every read after it,
// and let go by Close. A read refused for a rate limit is a
// *GitHubRateLimit, and every later read of the installation waits it out
// without asking GitHub.
type ReviewWatchSession interface {
	// Watch reads the pull requests numbers in one GraphQL query, in the
	// order given.
	Watch(ctx context.Context, numbers []int) ([]PRWatch, error)
	// PollComments reads a pull request's comments by ETag, as
	// ReviewPublisher.PollComments does.
	PollComments(ctx context.Context, number int, pages PollPages) (CommentPoll, error)
	// Stacked lists the open pull requests onto base: a layer stacked on
	// the loop's top since it opened.
	Stacked(ctx context.Context, base string) ([]ReviewGitHubPR, error)
	Close()
}

// WatchSession opens one loop's reads for one tick on repo.
func (p *ReviewPublisher) WatchSession(repo string) ReviewWatchSession {
	return &reviewWatchSession{p: p, repo: repo}
}

type reviewWatchSession struct {
	p       *ReviewPublisher
	repo    string
	call    *githubCall
	release func()
}

// open makes the session's token once.
func (s *reviewWatchSession) open(ctx context.Context) (*githubCall, error) {
	if s.call != nil {
		return s.call, nil
	}
	call, release, err := s.p.open(ctx, AsReviewer, s.repo, false)
	if err != nil {
		return nil, err
	}
	s.call, s.release = call, release
	return call, nil
}

func (s *reviewWatchSession) Close() {
	if s.release != nil {
		s.release()
	}
	s.call, s.release = nil, nil
}

func (s *reviewWatchSession) PollComments(ctx context.Context, number int, pages PollPages) (CommentPoll, error) {
	call, err := s.open(ctx)
	if err != nil {
		return CommentPoll{}, err
	}
	return s.p.pollComments(ctx, call, s.repo, number, pages)
}

func (s *reviewWatchSession) Stacked(ctx context.Context, base string) ([]ReviewGitHubPR, error) {
	call, err := s.open(ctx)
	if err != nil {
		return nil, err
	}
	return openPulls(ctx, call, s.repo, PullQuery{Base: base})
}

// watchFields are what the watch reads of each pull request.
const watchFields = `number state isDraft headRefOid headRefName baseRefName mergeable mergeStateStatus reviewDecision updatedAt
reviews(last: 50, states: [DISMISSED]) { nodes { fullDatabaseId } }
commits(last: 1) { nodes { commit { oid statusCheckRollup { state contexts(first: 100) { pageInfo { hasNextPage }
nodes { __typename
... on CheckRun { name status conclusion detailsUrl databaseId isRequired(pullRequestNumber: $n%[1]d) }
... on StatusContext { context state targetUrl isRequired(pullRequestNumber: $n%[1]d) } } } } } } }`

type watchedPull struct {
	Number           int     `json:"number"`
	State            string  `json:"state"`
	IsDraft          bool    `json:"isDraft"`
	HeadRefOid       string  `json:"headRefOid"`
	HeadRefName      string  `json:"headRefName"`
	BaseRefName      string  `json:"baseRefName"`
	Mergeable        string  `json:"mergeable"`
	MergeStateStatus string  `json:"mergeStateStatus"`
	ReviewDecision   *string `json:"reviewDecision"`
	UpdatedAt        string  `json:"updatedAt"`
	Reviews          struct {
		Nodes []struct {
			FullDatabaseID string `json:"fullDatabaseId"`
		} `json:"nodes"`
	} `json:"reviews"`
	Commits struct {
		Nodes []struct {
			Commit struct {
				Rollup *struct {
					State    string `json:"state"`
					Contexts struct {
						PageInfo struct {
							HasNextPage bool `json:"hasNextPage"`
						} `json:"pageInfo"`
						Nodes []struct {
							Typename   string  `json:"__typename"`
							Name       string  `json:"name"`
							Status     string  `json:"status"`
							Conclusion *string `json:"conclusion"`
							DetailsURL string  `json:"detailsUrl"`
							DatabaseID int64   `json:"databaseId"`
							Context    string  `json:"context"`
							State      string  `json:"state"`
							TargetURL  string  `json:"targetUrl"`
							IsRequired bool    `json:"isRequired"`
						} `json:"nodes"`
					} `json:"contexts"`
				} `json:"statusCheckRollup"`
			} `json:"commit"`
		} `json:"nodes"`
	} `json:"commits"`
}

func (s *reviewWatchSession) Watch(ctx context.Context, numbers []int) ([]PRWatch, error) {
	if len(numbers) == 0 {
		return nil, nil
	}
	call, err := s.open(ctx)
	if err != nil {
		return nil, err
	}
	owner, name, _ := strings.Cut(s.repo, "/")
	vars := map[string]any{"owner": owner, "name": name}
	var params, fields []string
	for i, n := range numbers {
		vars["n"+strconv.Itoa(i)] = n
		params = append(params, fmt.Sprintf("$n%d: Int!", i))
		fields = append(fields, fmt.Sprintf("p%d: pullRequest(number: $n%d) { %s }", i, i, fmt.Sprintf(watchFields, i)))
	}
	query := "query($owner: String!, $name: String!, " + strings.Join(params, ", ") + ") { repository(owner: $owner, name: $name) { " +
		strings.Join(fields, " ") + " } }"
	var found struct {
		Repository map[string]*watchedPull `json:"repository"`
	}
	if err := call.graphQL(ctx, query, vars, &found); err != nil {
		return nil, err
	}
	out := make([]PRWatch, 0, len(numbers))
	for i, n := range numbers {
		pull := found.Repository["p"+strconv.Itoa(i)]
		if pull == nil {
			return nil, fmt.Errorf("GitHub has no pull request %s#%d", s.repo, n)
		}
		out = append(out, pull.watch(n))
	}
	return out, nil
}

// watch is what the steward keeps of a pull request read.
func (pull *watchedPull) watch(number int) PRWatch {
	w := PRWatch{Number: number, State: strings.ToLower(pull.State), Draft: pull.IsDraft, HeadSHA: pull.HeadRefOid,
		HeadBranch: pull.HeadRefName, BaseBranch: pull.BaseRefName, Mergeable: pull.Mergeable, MergeState: pull.MergeStateStatus}
	if pull.ReviewDecision != nil {
		w.ReviewDecision = *pull.ReviewDecision
	}
	if at, err := time.Parse(time.RFC3339, pull.UpdatedAt); err == nil {
		w.UpdatedAt = at
	}
	for _, review := range pull.Reviews.Nodes {
		if id, err := strconv.ParseInt(review.FullDatabaseID, 10, 64); err == nil {
			w.DismissedReviews = append(w.DismissedReviews, id)
		}
	}
	if len(pull.Commits.Nodes) == 0 || pull.Commits.Nodes[0].Commit.Rollup == nil {
		return w
	}
	rollup := pull.Commits.Nodes[0].Commit.Rollup
	w.Rollup = rollupState(rollup.State)
	w.ChecksTruncated = rollup.Contexts.PageInfo.HasNextPage
	for _, c := range rollup.Contexts.Nodes {
		check := WatchCheck{Required: c.IsRequired}
		switch c.Typename {
		case "CheckRun":
			check.Name, check.URL, check.RunID, check.State = c.Name, c.DetailsURL, c.DatabaseID, CheckPending
			if c.Status == "COMPLETED" && c.Conclusion != nil {
				check.Conclusion = *c.Conclusion
				switch check.Conclusion {
				case "SUCCESS", "NEUTRAL", "SKIPPED":
					check.State = CheckPassing
				default:
					check.State = CheckFailing
				}
			}
		case "StatusContext":
			check.Name, check.URL, check.Conclusion = c.Context, c.TargetURL, c.State
			check.State = rollupState(c.State)
		default:
			continue
		}
		w.Checks = append(w.Checks, check)
	}
	return w
}

// rollupState maps GitHub's StatusState to a check state.
func rollupState(state string) string {
	switch state {
	case "SUCCESS":
		return CheckPassing
	case "FAILURE", "ERROR":
		return CheckFailing
	case "":
		return ""
	}
	return CheckPending // PENDING, EXPECTED
}
