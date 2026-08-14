package service

import (
	"context"
	"fmt"
)

type reviewAttentionSource interface {
	Queue(context.Context) ([]ReviewQueueItem, error)
}

type ReviewAttentionAdapter struct {
	reviews reviewAttentionSource
}

func NewReviewAttentionAdapter(reviews reviewAttentionSource) *ReviewAttentionAdapter {
	return &ReviewAttentionAdapter{reviews: reviews}
}

func (a *ReviewAttentionAdapter) Producer() string { return "reviews" }

func (a *ReviewAttentionAdapter) Snapshot(ctx context.Context) ([]AttentionInput, error) {
	queue, err := a.reviews.Queue(ctx)
	if err != nil {
		return nil, err
	}
	items := make([]AttentionInput, 0, len(queue))
	for _, review := range queue {
		detail := review.Summary
		if detail == "" {
			detail = fmt.Sprintf("%d review item(s) are waiting", review.Unreviewed)
		}
		items = append(items, AttentionInput{
			SignalKind:     AttentionSignalReviewReady,
			SourceKey:      review.TourID,
			SourceRevision: fmt.Sprintf("%s:%s:%s:%s:%d", review.PassID, review.HeadSHA, review.ActivityAt, review.UpdatedAt, review.Unreviewed),
			TaskID:         review.TaskID,
			Title:          review.Title,
			Reason:         "Ready for your review",
			Detail:         detail,
			Category:       "review",
			Urgency:        "normal",
			Navigation:     AttentionNavigation{Kind: "review", Target: review.TourID},
			Actions:        []AttentionAction{{ID: "open", Label: "Open review", Kind: "navigate"}},
		})
	}
	return items, nil
}
