package service

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

// States of a counted check on a head.
const (
	CheckPending = "pending"
	CheckPassing = "passing"
	CheckFailing = "failing"
)

// CheckResult is one counted check on a pull request's head.
type CheckResult struct {
	Name       string
	State      string
	Conclusion string
	Summary    string
	URL        string
	// Excerpt is a few lines of what the check reported (its annotations),
	// untrusted like all of GitHub's text.
	Excerpt string
}

// Checks are the counted checks of a pull request's head (countChecks,
// LEG-711): those branch protection requires on its base, else the
// configured ones, each pending until it reports, then every other check
// on the head. A failing one of those others that the base fails too, at
// the newest commit of base that finished running it, is
// CheckFailingOnBase; the base is read only when one fails.
func (p *ReviewPublisher) Checks(ctx context.Context, repo, base, head string, configured []string) ([]CheckResult, error) {
	required, err := p.requiredChecks(ctx, repo, base)
	if err != nil {
		return nil, err
	}
	if len(required) == 0 {
		required = configured
	}
	call, release, err := p.open(ctx, AsReviewer, repo, false)
	if err != nil {
		return nil, err
	}
	defer release()
	checks, runs, err := p.commitChecks(ctx, call, repo, head)
	if err != nil {
		return nil, err
	}
	var history [][]WatchCheck
	read := false
	var readErr error
	onBase := func(name string) bool {
		if !read {
			read = true
			history, readErr = p.baseHistory(ctx, call, repo, base)
		}
		return readErr == nil && failingOnBase(history, name)
	}
	counted := countChecks(checks, required, true, onBase)
	if readErr != nil {
		return nil, readErr
	}
	out := make([]CheckResult, 0, len(counted))
	for _, c := range counted {
		result := CheckResult{Name: c.Name, State: c.State, Conclusion: c.Conclusion, URL: c.URL}
		if run, ok := runs[c.RunID]; ok && c.RunID != 0 && c.State == CheckFailing {
			result.Summary = strings.TrimSpace(run.Output.Title + "\n\n" + run.Output.Summary)
			result.Excerpt = p.annotations(ctx, call, repo, c.RunID)
		} else if c.RunID == 0 {
			result.Summary = c.description
		}
		out = append(out, result)
	}
	return out, nil
}

// checkRun is a check run as REST lists it.
type checkRun struct {
	ID         int64  `json:"id"`
	Name       string `json:"name"`
	Status     string `json:"status"`
	Conclusion string `json:"conclusion"`
	HTMLURL    string `json:"html_url"`
	Output     struct {
		Title   string `json:"title"`
		Summary string `json:"summary"`
	} `json:"output"`
}

// commitChecks are the check runs and commit statuses of a commit (or a
// branch's head), as checks, with the runs by ID: a run is pending until it
// completes, then passing (success, neutral, skipped) or failing; a status
// takes its state.
func (p *ReviewPublisher) commitChecks(ctx context.Context, call *githubCall, repo, ref string) ([]WatchCheck, map[int64]checkRun, error) {
	var runs struct {
		CheckRuns []checkRun `json:"check_runs"`
	}
	if err := call.do(ctx, http.MethodGet, fmt.Sprintf("/repos/%s/commits/%s/check-runs?per_page=100", repo, url.PathEscape(ref)), nil, &runs); err != nil {
		return nil, nil, err
	}
	var statuses struct {
		Statuses []struct {
			Context     string `json:"context"`
			State       string `json:"state"`
			Description string `json:"description"`
			TargetURL   string `json:"target_url"`
		} `json:"statuses"`
	}
	if err := call.do(ctx, http.MethodGet, fmt.Sprintf("/repos/%s/commits/%s/status", repo, url.PathEscape(ref)), nil, &statuses); err != nil {
		return nil, nil, err
	}
	var checks []WatchCheck
	byID := map[int64]checkRun{}
	names := map[string]bool{}
	for _, run := range runs.CheckRuns {
		byID[run.ID] = run
		names[run.Name] = true
		c := WatchCheck{Name: run.Name, URL: run.HTMLURL, RunID: run.ID, State: CheckPending}
		if run.Status == "completed" {
			c.Conclusion = run.Conclusion
			switch run.Conclusion {
			case "success", "neutral", "skipped":
				c.State = CheckPassing
			default:
				c.State = CheckFailing
			}
		}
		checks = append(checks, c)
	}
	for _, st := range statuses.Statuses {
		if names[st.Context] {
			continue // a check run of that name stands for it
		}
		c := WatchCheck{Name: st.Context, URL: st.TargetURL, Conclusion: st.State, State: CheckPending, description: st.Description}
		switch st.State {
		case "success":
			c.State = CheckPassing
		case "failure", "error":
			c.State = CheckFailing
		}
		checks = append(checks, c)
	}
	return checks, byID, nil
}

// baseHistory are the checks of base's newest commits, newest first
// (baseHistoryDepth).
func (p *ReviewPublisher) baseHistory(ctx context.Context, call *githubCall, repo, base string) ([][]WatchCheck, error) {
	var commits []struct {
		SHA string `json:"sha"`
	}
	if err := call.do(ctx, http.MethodGet, fmt.Sprintf("/repos/%s/commits?sha=%s&per_page=%d", repo, url.QueryEscape(base), baseHistoryDepth), nil, &commits); err != nil {
		return nil, err
	}
	var history [][]WatchCheck
	for _, commit := range commits {
		checks, _, err := p.commitChecks(ctx, call, repo, commit.SHA)
		if err != nil {
			return nil, err
		}
		history = append(history, checks)
	}
	return history, nil
}

// requiredChecks are the checks branch protection requires on base, read
// with a token of their own (administration read); none when the branch is
// not protected or the App may not read it.
func (p *ReviewPublisher) requiredChecks(ctx context.Context, repo, base string) ([]string, error) {
	found, ok := p.channels.broker.lookup(ctx, p.reviewer)
	if !ok || found.jwt == nil || found.upstream == nil {
		return nil, fmt.Errorf("no reviewer GitHub credential named %q", p.reviewer)
	}
	if err := p.channels.githubBackingOff(found, repo); err != nil {
		return nil, err
	}
	token, release, err := p.channels.githubToken(ctx, found, repo, opReviewProtection)
	if err != nil {
		if errors.As(err, new(*GitHubRateLimit)) {
			return nil, err
		}
		return nil, nil // the App may not read branch protection
	}
	if release != nil {
		defer release()
	}
	call := &githubCall{p: p, bc: found, repo: repo, token: token}
	var protection struct {
		Contexts []string `json:"contexts"`
		Checks   []struct {
			Context string `json:"context"`
		} `json:"checks"`
	}
	err = call.do(ctx, http.MethodGet, fmt.Sprintf("/repos/%s/branches/%s/protection/required_status_checks", repo, url.PathEscape(base)), nil, &protection)
	var gh *GitHubError
	if errors.As(err, &gh) && (gh.Status == http.StatusNotFound || gh.Status == http.StatusForbidden) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	var names []string
	for _, c := range protection.Checks {
		if !seen[c.Context] {
			seen[c.Context] = true
			names = append(names, c.Context)
		}
	}
	for _, c := range protection.Contexts {
		if !seen[c] {
			seen[c] = true
			names = append(names, c)
		}
	}
	return names, nil
}

// annotations are a check run's first few annotations, as path:line:
// message lines.
func (p *ReviewPublisher) annotations(ctx context.Context, call *githubCall, repo string, runID int64) string {
	var notes []struct {
		Path      string `json:"path"`
		StartLine int    `json:"start_line"`
		Message   string `json:"message"`
	}
	if err := call.do(ctx, http.MethodGet, fmt.Sprintf("/repos/%s/check-runs/%d/annotations?per_page=10", repo, runID), nil, &notes); err != nil {
		return ""
	}
	var lines []string
	for _, n := range notes {
		line := strings.TrimSpace(n.Message)
		if len(line) > 300 {
			line = line[:300] + "…"
		}
		lines = append(lines, fmt.Sprintf("%s:%d: %s", n.Path, n.StartLine, line))
	}
	return strings.Join(lines, "\n")
}
