package handofftest

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/cpave3/legato/internal/engine/store"
	"github.com/cpave3/legato/internal/service"
)

// The bundled ticket-to-merged-pr, end to end (spec LEG-559 stories 93 and
// 94, AC 19 and 32; LEG-598): the card's own agent implements in a fresh
// clone and reports its pull request, review-loop reviews it, marks it ready
// and stewards it, then the run closes the Burnlite ticket and stops the
// agent only on merged.

// ship is ticket-to-merged-pr on the Burnlite card LEG-7: its agent has
// opened #12 (a draft) from the card's branch and reported it, and the
// review-loop child is open.
type ship struct {
	reviewRun
	parent *store.WorkflowRunView
}

func startShip(t *testing.T, opts ReviewPassOptions) ship {
	t.Helper()
	s := launchShip(t, opts)
	s.reportPR(t)
	if child := s.child(t); child.Run.Status != "running" {
		t.Fatalf("review-loop %q: %s (%s)", child.Run.Status, child.Run.Error, stepStates(child))
	}
	return s
}

// launchShip starts ticket-to-merged-pr on LEG-7, whose agent opens the
// draft #12 from the card's branch, which PR tracking links to the card.
func launchShip(t *testing.T, opts ReviewPassOptions) ship {
	t.Helper()
	ctx := context.Background()
	worker := StartWorker(t, WorkerOptions{Name: "erebus", GitHubRepositories: []string{"acme/*"}, ReviewPasses: &opts})
	worker.Checkout.File("greet.go", 30)
	r := reviewRun{worker: worker, runs: StartReviewRuns(t, worker)}
	provider, key := service.BurnliteIntegration, "LEG-7"
	if err := worker.Store.CreateTask(ctx, store.Task{ID: "LEG-7", Title: "Fix login", Status: "Doing", Provider: &provider, RemoteID: &key}); err != nil {
		t.Fatal(err)
	}

	parent, err := r.runs.StartExample(ctx, "ticket-to-merged-pr", map[string]string{"task": "LEG-7", "repo": "acme/widget"})
	if err != nil {
		t.Fatal(err)
	}
	if parent.Run.Status != "running" {
		t.Fatalf("run %q: %s", parent.Run.Status, parent.Run.Error)
	}
	// The card's own agent, in a fresh clone on the card's branch.
	if !r.cardAgent(t) {
		t.Fatal("the card has no agent")
	}
	clones := worker.Repos.Clones()
	if len(clones) != 1 || clones[0].TaskID != "LEG-7" || clones[0].Repo != "acme/widget" || clones[0].FreshBranch != "legato/leg-7" {
		t.Fatalf("clones = %+v", clones)
	}

	// It opens a draft pull request from that branch and reports it.
	worker.ReviewGitHub.OpenPR(12, "legato/leg-7", "bbb222")
	worker.ReviewGitHub.Draft(12, true)
	// PR tracking links it to the card, as it does the agent's branch.
	if _, err := worker.Store.TrackTaskBranches(ctx, "LEG-7", []store.BranchRef{{Repo: "acme/widget", Branch: "legato/leg-7"}}, service.BranchSourceAgent); err != nil {
		t.Fatal(err)
	}
	if err := worker.Store.SetTaskBranchPR(ctx, "LEG-7", "acme/widget", "legato/leg-7", &store.PRMeta{PRNumber: 12, Repo: "acme/widget", Branch: "legato/leg-7"}); err != nil {
		t.Fatal(err)
	}
	return ship{reviewRun: r, parent: parent}
}

// reportPR is the agent completing implement with #12.
func (s ship) reportPR(t *testing.T) {
	t.Helper()
	ctx := context.Background()
	implement := latestAttempt(t, s.show(t, s.parent.Run.ID), "implement")
	if _, err := s.runs.Workflows.Complete(ctx, s.parent.Run.ID, "implement", implement.ID, map[string]string{"pr_number": "12"}, nil); err != nil {
		t.Fatal(err)
	}
	s.runs.Open(ctx)
}

// child is the review-loop run the parent's review step started.
func (s ship) child(t *testing.T) *store.WorkflowRunView {
	t.Helper()
	return s.childOf(t, s.show(t, s.parent.Run.ID), "review")
}

// approved runs pass 1 clean and confirms it, so the steward watches.
func (s ship) approved(t *testing.T) {
	t.Helper()
	s.tick()
	s.submit(t, s.child(t).Run.TaskID, service.ReviewSubmission{Summary: "Looks right."})
	s.tick()
	s.tick()
	if child := s.child(t); !s.watching(t, child) {
		t.Fatalf("the steward is not watching: %s", stepStates(child))
	}
}

// ended is the parent once it has ended, which it must have succeeded.
func (s ship) ended(t *testing.T) *store.WorkflowRunView {
	t.Helper()
	s.worker.Settle()
	parent := s.show(t, s.parent.Run.ID)
	if parent.Run.Status != "succeeded" {
		t.Fatalf("parent %q (%s): %s; child: %s", parent.Run.Status, parent.Run.Error, stepStates(parent), stepStates(s.child(t)))
	}
	return parent
}

// ran says which of the parent's outcome steps ran, in order.
func ran(t *testing.T, parent *store.WorkflowRunView) []string {
	t.Helper()
	var out []string
	for _, id := range []string{"close-ticket", "note-stopped", "note-running", "stop"} {
		if stepOf(t, parent, id).Status == "succeeded" {
			out = append(out, id)
		}
	}
	return out
}

func (r reviewRun) cardAgent(t *testing.T) bool {
	t.Helper()
	_, err := r.worker.Store.GetAgentSessionByTaskID(context.Background(), "LEG-7")
	return err == nil
}

// notes are the card's notes.
func (r reviewRun) notes(t *testing.T) []string {
	t.Helper()
	notes, err := r.worker.Store.ListTaskNotes(context.Background(), "LEG-7")
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, n := range notes {
		out = append(out, n.Body)
	}
	return out
}

func TestTicketToMergedPRCallsReviewLoopWithTheRepoPRAndScopeStack(t *testing.T) {
	s := startShip(t, ReviewPassOptions{})
	child := s.child(t)
	if child.Run.WorkflowName != "review-loop" {
		t.Fatalf("child workflow %q", child.Run.WorkflowName)
	}
	var inputs map[string]any
	if err := json.Unmarshal([]byte(child.Run.InputsJSON), &inputs); err != nil {
		t.Fatal(err)
	}
	for name, want := range map[string]any{"repo": "acme/widget", "pr_number": float64(12), "scope": "stack",
		"agent_author": true, "ready": true, "steward": true, "reviewer": ""} {
		if inputs[name] != want {
			t.Errorf("child input %s = %#v, want %#v (inputs %s)", name, inputs[name], want, child.Run.InputsJSON)
		}
	}
	// The card is the author: its pull request's loop is linked to it.
	if rec := s.record(t, child); rec.LinkedTask != "LEG-7" {
		t.Fatalf("loop linked to %q", rec.LinkedTask)
	}
}

// AC 32 on ticket-to-merged-pr: reviewed, marked ready, stewarded, and once
// every layer has merged the ticket closes and the card's agent stops.
func TestTicketToMergedPRClosesTheTicketAndStopsTheAgentOnceMerged(t *testing.T) {
	s := startShip(t, ReviewPassOptions{})
	s.approved(t)
	if got := s.worker.ReviewGitHub.Readied(); len(got) != 1 || got[0] != 12 {
		t.Fatalf("readied %v; want #12 marked ready after approval", got)
	}
	if !s.cardAgent(t) || len(s.runs.Burnlite.Closed()) != 0 {
		t.Fatal("the ticket closed or the agent stopped before the merge")
	}

	s.worker.ReviewGitHub.MergePR(12)
	s.watchTick()
	s.tick()

	parent := s.ended(t)
	if outputs(t, parent, "review")["outcome"] != "merged" {
		t.Fatalf("review outputs %v", outputs(t, parent, "review"))
	}
	if got := strings.Join(ran(t, parent), ","); got != "close-ticket,stop" {
		t.Fatalf("ran %s", got)
	}
	if closed := s.runs.Burnlite.Closed(); len(closed) != 1 || closed[0] != "LEG-7|completed|Merged in https://github.com/acme/widget/pull/12" {
		t.Fatalf("closed %v", closed)
	}
	if s.cardAgent(t) {
		t.Fatal("the card's agent still runs after the merge")
	}
	if notes := s.notes(t); len(notes) != 0 {
		t.Fatalf("notes %q", notes)
	}
}

// Each outcome reaches the parent's if: (AC 19). closed and idle note the
// card and stop its agent; stuck and ended note it and leave the agent
// running; none of them closes the ticket.
func TestTicketToMergedPRNotesTheCardOnEveryOtherOutcome(t *testing.T) {
	cases := []struct {
		outcome string
		opts    ReviewPassOptions
		end     func(t *testing.T, s ship)
		stopped bool
	}{
		{outcome: "closed", stopped: true, end: func(t *testing.T, s ship) {
			s.approved(t)
			s.worker.ReviewGitHub.ClosePR(12)
			s.watchTick()
			s.tick()
		}},
		{outcome: "idle", stopped: true, end: func(t *testing.T, s ship) {
			s.approved(t)
			s.worker.Clock.Advance(7*24*time.Hour + time.Minute)
			s.watchTick()
			s.tick()
		}},
		{outcome: "ended", end: func(t *testing.T, s ship) {
			s.approved(t)
			if err := s.stewardAct(t, s.child(t), "end"); err != nil {
				t.Fatal(err)
			}
			s.tick()
		}},
		{outcome: "stuck", opts: ReviewPassOptions{MaxPasses: 1}, end: func(t *testing.T, s ship) {
			s.tick()
			result := s.submit(t, s.child(t).Run.TaskID, service.ReviewSubmission{Summary: "One problem.", Findings: []service.SubmittedFinding{blockingFinding}})
			s.tick()
			// The card's agent fixes it and pushes: a second pass is past
			// the limit of one.
			if err := service.AnswerReviewItem(context.Background(), s.worker.Store, "LEG-7", result.Findings[0], service.AnswerFixed, "Guarded the empty name.", s.worker.Clock.Now()); err != nil {
				t.Fatal(err)
			}
			s.worker.ReviewGitHub.Push(12, "ccc333")
			s.worker.Checkout.Change("ccc333", "greet.go", 3, 5)
			s.tick()
			s.worker.Clock.Advance(3 * time.Minute)
			s.tick()
			s.tick()
			if parent := s.show(t, s.parent.Run.ID); parent.Run.Status != "running" {
				t.Fatalf("the parent %q while the loop is stuck", parent.Run.Status)
			}
			ended := false
			for _, item := range s.attention(t) {
				if item.SignalKind != service.AttentionSignalWorkflowReviewStuck {
					continue
				}
				if err := service.NewWorkflowAttentionAdapter(s.runs.Workflows).Execute(context.Background(), service.AttentionActionRequest{SourceKey: item.SourceKey, ActionID: "end", Confirmed: true}); err != nil {
					t.Fatal(err)
				}
				ended = true
			}
			if !ended {
				t.Fatalf("no stuck item: %+v", s.attention(t))
			}
			s.tick()
		}},
	}
	for _, tc := range cases {
		t.Run(tc.outcome, func(t *testing.T) {
			s := startShip(t, tc.opts)
			tc.end(t, s)

			parent := s.ended(t)
			if got := outputs(t, parent, "review")["outcome"]; got != tc.outcome {
				t.Fatalf("outcome %v, want %s", got, tc.outcome)
			}
			want := "note-running"
			if tc.stopped {
				want = "note-stopped,stop"
			}
			if got := strings.Join(ran(t, parent), ","); got != want {
				t.Fatalf("ran %s, want %s", got, want)
			}
			if closed := s.runs.Burnlite.Closed(); len(closed) != 0 {
				t.Fatalf("the ticket closed on %s: %v", tc.outcome, closed)
			}
			if s.cardAgent(t) == tc.stopped {
				t.Fatalf("the card's agent running = %v on %s", !tc.stopped, tc.outcome)
			}
			notes := s.notes(t)
			if len(notes) != 1 || !strings.Contains(notes[0], "ended "+tc.outcome) || !strings.Contains(notes[0], "#12") {
				t.Fatalf("notes %q", notes)
			}
		})
	}
}

// A review-loop that fails (here refused: another loop holds #12) pauses
// the parent's review step for the operator, closing nothing and stopping
// nothing.
func TestARefusedReviewLoopPausesTicketToMergedPR(t *testing.T) {
	s := launchShip(t, ReviewPassOptions{})
	if _, err := s.runs.StartReviewLoop(context.Background(), pr12); err != nil {
		t.Fatal(err)
	}
	s.reportPR(t)

	parent := s.show(t, s.parent.Run.ID)
	review := stepOf(t, parent, "review")
	if parent.Run.Status != "waiting" || review.Status != service.WorkflowStepPaused || review.ErrorCode != service.ReviewRefusalLoopActive {
		t.Fatalf("parent %q, review %q (%s): %s", parent.Run.Status, review.Status, review.ErrorCode, review.Error)
	}
	if got := ran(t, parent); len(got) != 0 {
		t.Fatalf("ran %v", got)
	}
	if !s.cardAgent(t) || len(s.runs.Burnlite.Closed()) != 0 {
		t.Fatal("the ticket closed or the agent stopped")
	}
}
