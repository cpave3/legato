package service

// ReviewEngineStatus says whether some Legato on this data folder runs the
// review engine (the broker holder, spec LEG-559 D9), and, when none does,
// why: what a card's menu says in place of its review items.
type ReviewEngineStatus struct {
	Running bool   `json:"running"`
	Reason  string `json:"reason,omitempty"`
}

// The reasons a card's menu gives for offering no review.
const (
	// ReviewPassesOffReason names the setting that is off.
	ReviewPassesOffReason = "Review passes are off: turn on review_passes.enabled (Settings › Review passes)."
	// ReviewEngineStoppedReason: review passes are on, but no Legato on the
	// data folder holds the engine.
	ReviewEngineStoppedReason = "Review passes need `legato serve` running."
)

// ReviewEngineStatusOf is the engine's status from this process's setting
// and whether some process on the data folder runs the engine. A running
// engine wins: the holder's setting is the one that counts.
func ReviewEngineStatusOf(enabled, running bool) ReviewEngineStatus {
	switch {
	case running:
		return ReviewEngineStatus{Running: true}
	case !enabled:
		return ReviewEngineStatus{Reason: ReviewPassesOffReason}
	default:
		return ReviewEngineStatus{Reason: ReviewEngineStoppedReason}
	}
}
