package service

import (
	"context"
	"fmt"
)

type ReviewAttentionSource interface {
	Queue(context.Context) ([]ReviewQueueItem, error)
	Complete(context.Context, string) error
}

type ReviewAttentionAdapter struct {
	reviews ReviewAttentionSource
}

func NewReviewAttentionAdapter(reviews ReviewAttentionSource) *ReviewAttentionAdapter {
	return &ReviewAttentionAdapter{reviews: reviews}
}

func (a *ReviewAttentionAdapter) Producer() string { return "reviews" }

func (a *ReviewAttentionAdapter) Snapshot(ctx context.Context) ([]AttentionInput, error) {
	queue, err := a.reviews.Queue(ctx)
	if err != nil {
		return nil, err
	}
	items := make([]AttentionInput, 0, len(queue))
	for _, tour := range queue {
		reason := "Review tour is ready for your review"
		switch tour.Status {
		case "reviewed":
			reason = "Reviewed tour has new work to review"
		case "capturing":
			reason = "Capturing tour's agent ended before signalling ready"
		}
		items = append(items, AttentionInput{
			SourceKey: tour.TourID, SourceRevision: fmt.Sprintf("%s:%d:%s", tour.Status, tour.Unreviewed, tour.UpdatedAt),
			TaskID: tour.TaskID, Title: tour.Title, Reason: reason, Detail: tour.Summary,
			Category: "review", Urgency: "normal",
			Navigation: AttentionNavigation{Kind: "review", Target: tour.TourID},
			Actions: []AttentionAction{
				{ID: "open", Label: "Review changes", Kind: "navigate", Intent: "primary"},
				{ID: "complete", Label: "Mark reviewed", Kind: "execute", RequiresConfirm: true},
			},
		})
	}
	return items, nil
}

func (a *ReviewAttentionAdapter) Execute(ctx context.Context, request AttentionActionRequest) error {
	switch request.ActionID {
	case "complete":
		return a.reviews.Complete(ctx, request.SourceKey)
	default:
		return fmt.Errorf("unsupported review attention action %q", request.ActionID)
	}
}
