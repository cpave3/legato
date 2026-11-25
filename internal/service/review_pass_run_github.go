package service

import (
	"context"
	"fmt"
	"net/http"

	"github.com/cpave3/legato/internal/engine/store"
)

// Installed checks the reviewer App can read repo: a token for it is made,
// which needs the App's installation to select the repository.
func (p *ReviewPublisher) Installed(ctx context.Context, repo string) error {
	call, release, err := p.open(ctx, AsReviewer, repo, false)
	if err != nil {
		return err
	}
	defer release()
	var answer struct {
		FullName string `json:"full_name"`
	}
	return call.do(ctx, http.MethodGet, "/repos/"+repo, nil, &answer)
}

// PullRequest is one pull request, read as the reviewer App, with its state:
// open, closed or merged.
func (p *ReviewPublisher) PullRequest(ctx context.Context, repo string, number int) (ReviewGitHubPR, string, error) {
	call, release, err := p.open(ctx, AsReviewer, repo, false)
	if err != nil {
		return ReviewGitHubPR{}, "", err
	}
	defer release()
	var pr struct {
		Number  int    `json:"number"`
		State   string `json:"state"`
		Merged  bool   `json:"merged"`
		Draft   bool   `json:"draft"`
		Title   string `json:"title"`
		Body    string `json:"body"`
		HTMLURL string `json:"html_url"`
		// Association is the author's with the repository.
		Association string `json:"author_association"`
		User        struct {
			Login string `json:"login"`
		} `json:"user"`
		Head struct {
			Ref  string `json:"ref"`
			SHA  string `json:"sha"`
			Repo *struct {
				FullName string `json:"full_name"`
			} `json:"repo"`
		} `json:"head"`
		Base struct {
			Ref string `json:"ref"`
		} `json:"base"`
	}
	if err := call.do(ctx, http.MethodGet, fmt.Sprintf("/repos/%s/pulls/%d", repo, number), nil, &pr); err != nil {
		return ReviewGitHubPR{}, "", err
	}
	state := pr.State
	if pr.Merged {
		state = "merged"
	}
	headRepo := ""
	if pr.Head.Repo != nil {
		headRepo = pr.Head.Repo.FullName
	}
	return ReviewGitHubPR{Number: pr.Number, Branch: pr.Head.Ref, Base: pr.Base.Ref, Title: pr.Title, Body: pr.Body, URL: pr.HTMLURL,
		HeadSHA: pr.Head.SHA, Author: pr.User.Login, AuthorAssociation: pr.Association, HeadRepo: headRepo, Draft: pr.Draft}, state, nil
}

// MarkReady marks a pull request ready for review as the author App, host
// side (CredentialChannels.MarkReadyAsAuthor); false when it was not a
// draft.
func (p *ReviewPublisher) MarkReady(ctx context.Context, repo string, number int) (bool, error) {
	return p.channels.MarkReadyAsAuthor(ctx, repo, number)
}

// ReviewRunRepositories are a run's review clones: the repository cache's job
// clones, reset with git on the host.
type ReviewRunRepositories struct {
	Repos    *RepositoryService
	Checkout GitReviewCheckout
}

func (r ReviewRunRepositories) PullHead(ctx context.Context, repo string, number int) error {
	return r.Repos.PullHead(ctx, repo, number)
}

func (r ReviewRunRepositories) JobClone(ctx context.Context, req JobCloneRequest) (store.JobClone, error) {
	return r.Repos.JobClone(ctx, req)
}

func (r ReviewRunRepositories) RemoveJobClone(ctx context.Context, id int64) error {
	return r.Repos.RemoveJobClone(ctx, id)
}

func (r ReviewRunRepositories) Reset(ctx context.Context, dir, branch, sha string) error {
	return r.Checkout.Reset(ctx, dir, branch, sha)
}

// Reset puts a clone's branch at sha, checked out, with nothing else in the
// working tree.
func (c GitReviewCheckout) Reset(ctx context.Context, dir, branch, sha string) error {
	if _, err := c.git(ctx, dir, "checkout", "--quiet", "--force", "-B", branch, sha); err != nil {
		return err
	}
	_, err := c.git(ctx, dir, "clean", "--quiet", "-fd")
	return err
}
