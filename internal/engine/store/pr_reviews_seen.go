package store

import (
	"context"
	"strings"
	"time"

	"github.com/jmoiron/sqlx"
)

// SeenReview is a review PR tracking recorded.
type SeenReview struct {
	ID          int64
	State       string
	Login       string
	SubmittedAt time.Time
}

// RecordPRReviews records a PR's reviews as seen and returns those not
// seen before. The first time a PR is recorded it is baselined: its
// reviews are recorded and only those submitted after since (when the task
// began to track the branch; zero for none) are returned, so what a PR had
// before a task owned it never fires.
func (s *Store) RecordPRReviews(ctx context.Context, repo string, number int, since time.Time, reviews []SeenReview) ([]SeenReview, error) {
	var fresh []SeenReview
	err := withTx(ctx, s.db, func(tx *sqlx.Tx) error {
		var baselined int
		if err := tx.GetContext(ctx, &baselined, `SELECT COUNT(*) FROM pr_reviews_seen WHERE repo = ? AND pr_number = ? AND review_id = 0`, repo, number); err != nil {
			return err
		}
		if baselined == 0 {
			if _, err := tx.ExecContext(ctx, `INSERT INTO pr_reviews_seen (repo, pr_number, review_id) VALUES (?, ?, 0)`, repo, number); err != nil {
				return err
			}
		}
		for _, review := range reviews {
			if review.ID == 0 {
				continue
			}
			result, err := tx.ExecContext(ctx, `INSERT INTO pr_reviews_seen (repo, pr_number, review_id, state, login) VALUES (?, ?, ?, ?, ?) ON CONFLICT DO NOTHING`, repo, number, review.ID, review.State, review.Login)
			if err != nil {
				return err
			}
			if n, _ := result.RowsAffected(); n > 0 && (baselined > 0 || (!since.IsZero() && review.SubmittedAt.After(since))) {
				fresh = append(fresh, review)
			}
		}
		return nil
	})
	return fresh, err
}

// RecordGitHubAppLogin keeps a login of one of Legato's own GitHub Apps.
func (s *Store) RecordGitHubAppLogin(ctx context.Context, login string) error {
	login = strings.TrimSpace(login)
	if login == "" {
		return nil
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO github_app_logins (login) VALUES (?) ON CONFLICT DO NOTHING`, login)
	return err
}

// GitHubAppLogins lists the logins of Legato's own GitHub Apps learned.
func (s *Store) GitHubAppLogins(ctx context.Context) ([]string, error) {
	logins := []string{}
	err := s.db.SelectContext(ctx, &logins, `SELECT login FROM github_app_logins ORDER BY login`)
	return logins, err
}
