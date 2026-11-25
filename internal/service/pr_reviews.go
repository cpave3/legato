package service

import (
	"context"
	"log"
	"strings"
	"time"

	"github.com/cpave3/legato/internal/engine/events"
	"github.com/cpave3/legato/internal/engine/github"
	"github.com/cpave3/legato/internal/engine/store"
)

// SetGitHubAppLogins names the bot logins of Legato's own GitHub Apps
// (github.app_logins), whose reviews never fire a trigger; those learned
// from reviews Legato posted are added from the store. A name without the
// "[bot]" suffix gets it (botLogins).
func (p *prTrackingService) SetGitHubAppLogins(logins []string) {
	p.appLogins = botLogins(logins)
}

// legatoApp says whether a login is one of Legato's own Apps.
func (p *prTrackingService) legatoApp(ctx context.Context, login string) bool {
	learned, _ := p.store.GitHubAppLogins(ctx)
	for _, l := range append(append([]string{}, p.appLogins...), learned...) {
		if strings.EqualFold(strings.TrimSpace(l), login) {
			return true
		}
	}
	return false
}

// observeReviews records a refreshed PR's reviews and publishes
// EventPRChangesRequested for each new one requesting changes, unless one
// of Legato's Apps wrote it. The reviews come with the refresh, so this
// costs no GitHub call. The operator's own reviews count.
func (p *prTrackingService) observeReviews(ctx context.Context, row store.TaskBranch, status *github.PRStatus) {
	if status == nil || status.Number == 0 {
		return
	}
	seen := make([]store.SeenReview, 0, len(status.Reviews))
	for _, review := range status.Reviews {
		submitted, _ := time.Parse(time.RFC3339, review.SubmittedAt)
		seen = append(seen, store.SeenReview{ID: review.ID, State: review.State, Login: review.Login, SubmittedAt: submitted})
	}
	fresh, err := p.store.RecordPRReviews(ctx, row.Repo, status.Number, parseAddedAt(row.AddedAt), seen)
	if err != nil {
		log.Printf("pr tracking: reviews of %s#%d: %v", row.Repo, status.Number, err)
		return
	}
	for _, review := range fresh {
		if review.State != "CHANGES_REQUESTED" || p.legatoApp(ctx, review.Login) || p.bus == nil {
			continue
		}
		p.bus.Publish(events.Event{Type: events.EventPRChangesRequested, At: time.Now(), Payload: events.PRChangesRequestedPayload{
			TaskID: row.TaskID, Repo: row.Repo, Number: status.Number, URL: status.URL, ReviewID: review.ID, Reviewer: review.Login,
		}})
	}
}

func parseAddedAt(text string) time.Time {
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339, time.DateTime} {
		if t, err := time.Parse(layout, text); err == nil {
			return t
		}
	}
	return time.Time{}
}
