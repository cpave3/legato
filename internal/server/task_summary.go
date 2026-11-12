package server

import (
	"time"

	"github.com/cpave3/legato/internal/engine/store"
	"github.com/cpave3/legato/internal/service"
)

// TaskSummaryResponse is GET /api/tasks/{id}?view=summary: a task in brief,
// for an agent or the Reeve checking on it, without its description.
type TaskSummaryResponse struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Column      string `json:"column"`
	WorkspaceID *int   `json:"workspace_id,omitempty"`
	Provider    string `json:"provider,omitempty"`
	RemoteID    string `json:"remote_id,omitempty"`
	UpdatedAt   string `json:"updated_at"`
	// PullRequests are the task's PRs in stack order, without discovery's
	// details.
	PullRequests []TaskSummaryPR `json:"pull_requests"`
	// Notes is how many notes the task has; LatestNote the newest, its
	// body cut at summaryNoteChars.
	Notes      int             `json:"notes"`
	LatestNote *store.TaskNote `json:"latest_note,omitempty"`
}

// TaskSummaryPR is one pull request of a task in brief.
type TaskSummaryPR struct {
	Repo           string `json:"repo,omitempty"`
	Branch         string `json:"branch,omitempty"`
	PRNumber       int    `json:"pr_number"`
	PRURL          string `json:"pr_url,omitempty"`
	State          string `json:"state,omitempty"`
	IsDraft        bool   `json:"is_draft,omitempty"`
	ReviewDecision string `json:"review_decision,omitempty"`
	CheckStatus    string `json:"check_status,omitempty"`
}

const summaryNoteChars = 1000

func taskSummary(d *service.CardDetail) TaskSummaryResponse {
	resp := TaskSummaryResponse{
		ID: d.ID, Title: d.Title, Column: d.Status, WorkspaceID: d.WorkspaceID,
		Provider: d.Provider, RemoteID: d.RemoteID, UpdatedAt: d.UpdatedAt.Format(time.RFC3339),
		PullRequests: []TaskSummaryPR{}, Notes: len(d.Notes),
	}
	for _, pr := range d.PullRequests {
		resp.PullRequests = append(resp.PullRequests, TaskSummaryPR{
			Repo: pr.Repo, Branch: pr.Branch, PRNumber: pr.PRNumber, PRURL: pr.PRURL, State: pr.State,
			IsDraft: pr.IsDraft, ReviewDecision: pr.ReviewDecision, CheckStatus: pr.CheckStatus,
		})
	}
	if n := len(d.Notes); n > 0 {
		latest := d.Notes[n-1]
		if r := []rune(latest.Body); len(r) > summaryNoteChars {
			latest.Body = string(r[:summaryNoteChars]) + "…"
		}
		resp.LatestNote = &latest
	}
	return resp
}
