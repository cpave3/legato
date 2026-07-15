package review

import (
	gitpkg "github.com/cpave3/legato/internal/engine/git"
	"github.com/cpave3/legato/internal/service"
)

// QueueLoadedMsg carries the review queue.
type QueueLoadedMsg struct {
	Items []service.ReviewQueueItem
	Err   error
}

// TourLoadedMsg carries a task's full tour.
type TourLoadedMsg struct {
	View *service.ReviewTourView
	Err  error
}

// DiffLoadedMsg carries the parsed diff for the focused step.
type DiffLoadedMsg struct {
	StepID string
	Files  []gitpkg.FileDiff
	Err    error
}

// ActionDoneMsg signals a mutation (reviewed toggle, question, complete)
// finished; the tour should reload.
type ActionDoneMsg struct {
	TourID string
	Info   string
	Err    error
}

// ReturnToBoardMsg asks the app to switch back to the board view.
type ReturnToBoardMsg struct{}

// ReviewChangedMsg is forwarded by the app when a review_changed event
// arrives on the bus (e.g. an agent answered a question).
type ReviewChangedMsg struct {
	TourID string
}
