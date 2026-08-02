package service

import (
	"context"
	"errors"

	"github.com/cpave3/legato/internal/engine/store"
)

// SetPromptEnabled records an explicit user decision about boot-time prompt use.
// Accepted learnings default disabled; either decision remains reversible.
func (s *LearningService) SetPromptEnabled(ctx context.Context, id string, expectedRevision int, enabled bool) (*LearningView, error) {
	if err := s.store.SetLearningPromptEnabled(ctx, id, expectedRevision, enabled); err != nil {
		if errors.Is(err, store.ErrConflict) {
			return nil, ErrLearningConflict
		}
		return nil, err
	}
	kind := "prompt_disabled"
	if enabled {
		kind = "prompt_enabled"
	}
	s.publish(id, kind)
	return s.Show(ctx, id)
}
