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

// Checks are the counted checks of a pull request's head: those branch
// protection requires on its base, else the configured ones, each pending
// until it reports.
func (p *ReviewPublisher) Checks(ctx context.Context, repo, base, head string, configured []string) ([]CheckResult, error) {
	required, err := p.requiredChecks(ctx, repo, base)
	if err != nil {
		return nil, err
	}
	if len(required) == 0 {
		required = configured
	}
	if len(required) == 0 {
		return nil, nil
	}
	call, release, err := p.open(ctx, AsReviewer, repo, false)
	if err != nil {
		return nil, err
	}
	defer release()
	var runs struct {
		CheckRuns []struct {
			ID         int64  `json:"id"`
			Name       string `json:"name"`
			Status     string `json:"status"`
			Conclusion string `json:"conclusion"`
			HTMLURL    string `json:"html_url"`
			Output     struct {
				Title   string `json:"title"`
				Summary string `json:"summary"`
			} `json:"output"`
		} `json:"check_runs"`
	}
	if err := call.do(ctx, http.MethodGet, fmt.Sprintf("/repos/%s/commits/%s/check-runs?per_page=100", repo, url.PathEscape(head)), nil, &runs); err != nil {
		return nil, err
	}
	var statuses struct {
		Statuses []struct {
			Context     string `json:"context"`
			State       string `json:"state"`
			Description string `json:"description"`
			TargetURL   string `json:"target_url"`
		} `json:"statuses"`
	}
	if err := call.do(ctx, http.MethodGet, fmt.Sprintf("/repos/%s/commits/%s/status", repo, url.PathEscape(head)), nil, &statuses); err != nil {
		return nil, err
	}
	out := make([]CheckResult, 0, len(required))
	for _, name := range required {
		result := CheckResult{Name: name, State: CheckPending}
		found := false
		for _, run := range runs.CheckRuns {
			if run.Name != name {
				continue
			}
			found = true
			result.URL = run.HTMLURL
			if run.Status != "completed" {
				break
			}
			result.Conclusion = run.Conclusion
			switch run.Conclusion {
			case "success", "neutral", "skipped":
				result.State = CheckPassing
			default:
				result.State = CheckFailing
				result.Summary = strings.TrimSpace(run.Output.Title + "\n\n" + run.Output.Summary)
				result.Excerpt = p.annotations(ctx, call, repo, run.ID)
			}
			break
		}
		if !found {
			for _, st := range statuses.Statuses {
				if st.Context != name {
					continue
				}
				result.URL, result.Conclusion, result.Summary = st.TargetURL, st.State, st.Description
				switch st.State {
				case "success":
					result.State = CheckPassing
				case "failure", "error":
					result.State = CheckFailing
				}
				break
			}
		}
		out = append(out, result)
	}
	return out, nil
}

// requiredChecks are the checks branch protection requires on base, read
// with a token of their own (administration read); none when the branch is
// not protected or the App may not read it.
func (p *ReviewPublisher) requiredChecks(ctx context.Context, repo, base string) ([]string, error) {
	found, ok := p.channels.broker.lookup(ctx, p.reviewer)
	if !ok || found.jwt == nil || found.upstream == nil {
		return nil, fmt.Errorf("no reviewer GitHub credential named %q", p.reviewer)
	}
	token, release, err := p.channels.githubToken(ctx, found, repo, opReviewProtection)
	if err != nil {
		var gh *GitHubError
		if errors.As(err, &gh) && gh.RateLimited {
			return nil, err
		}
		return nil, nil // the App may not read branch protection
	}
	if release != nil {
		defer release()
	}
	call := &githubCall{p: p, bc: found, token: token}
	var protection struct {
		Contexts []string `json:"contexts"`
		Checks   []struct {
			Context string `json:"context"`
		} `json:"checks"`
	}
	err = call.do(ctx, http.MethodGet, fmt.Sprintf("/repos/%s/branches/%s/protection/required_status_checks", repo, url.PathEscape(base)), nil, &protection)
	var gh *GitHubError
	if errors.As(err, &gh) && !gh.RateLimited && (gh.Status == http.StatusNotFound || gh.Status == http.StatusForbidden) {
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
