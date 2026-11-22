package handofftest

import (
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cpave3/legato/internal/engine/events"
	"github.com/cpave3/legato/internal/service"
)

// milestones records the stage milestones a worker publishes, as
// "slot:stage:reason" (with "back" when it may move the stage back).
type milestones struct {
	mu   sync.Mutex
	seen []string
}

func recordMilestones(t *testing.T, w *Worker) *milestones {
	t.Helper()
	m := &milestones{}
	sub := w.Bus.Subscribe(events.EventWorkMilestone)
	done := make(chan struct{})
	go func() {
		defer close(done)
		for event := range sub {
			p := event.Payload.(events.WorkMilestonePayload)
			slot := p.Slot
			if slot == "" {
				slot = "main"
			}
			line := fmt.Sprintf("%s/%s:%s:%s", p.TaskID, slot, p.Stage, p.Reason)
			if p.Back {
				line += ":back"
			}
			m.mu.Lock()
			m.seen = append(m.seen, line)
			m.mu.Unlock()
		}
	}()
	t.Cleanup(func() { w.Bus.Unsubscribe(sub); <-done })
	return m
}

// has waits for a milestone of the task's slot that starts with want.
func (m *milestones) has(t *testing.T, want string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		m.mu.Lock()
		seen := append([]string(nil), m.seen...)
		m.mu.Unlock()
		for _, line := range seen {
			if strings.HasPrefix(line, want) {
				return
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("milestones = %q, want one starting %q", seen, want)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// A review pass on the author's pull requests puts the author in review;
// the reviewer reviews, and is finished when it submits; the author is
// back to implementing only once its feedback is delivered.
func TestAReviewLoopMovesTheAuthorsAndTheReviewersStages(t *testing.T) {
	r := newReviewed(t, ReviewPassOptions{})
	m := recordMilestones(t, r.worker)

	r.finish(t)
	m.has(t, r.card+"/main:reviewing:review pass 1")
	m.has(t, r.card+"/reviewer:reviewing:pass 1:back")

	if _, err := r.submit(t, service.ReviewSubmission{Summary: "One bug.", Findings: []service.SubmittedFinding{
		{PR: 7, Severity: "blocking", Title: "Retries never stop", Body: "No bound.", FailureScenario: "A login never returns.", Place: &service.ReviewPlace{Path: "login.go", Line: 20}},
	}}); err != nil {
		t.Fatal(err)
	}
	m.has(t, r.card+"/reviewer:finished:pass 1 submitted")
	m.mu.Lock()
	for _, line := range m.seen {
		if strings.HasPrefix(line, r.card+"/main:implementing") {
			m.mu.Unlock()
			t.Fatalf("the author was implementing before its feedback was delivered: %q", m.seen)
		}
	}
	m.mu.Unlock()

	r.tick() // feedback goes to the author
	m.has(t, r.card+"/main:implementing:fixing review feedback · pass 1:back")
}
