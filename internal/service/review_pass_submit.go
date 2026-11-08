package service

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"strings"

	"github.com/cpave3/legato/internal/engine/store"
)

// ReviewSubmission is a reviewer's result, given through submit_review.
type ReviewSubmission struct {
	Summary  string             `json:"summary"`
	Findings []SubmittedFinding `json:"findings,omitempty"`
	Rulings  []SubmittedRuling  `json:"rulings,omitempty"`
	Unable   string             `json:"unable,omitempty"`
}

// SubmittedFinding is one finding of a submission.
type SubmittedFinding struct {
	PR       int          `json:"pr"`
	Severity string       `json:"severity"`
	Title    string       `json:"title"`
	Body     string       `json:"body"`
	Place    *ReviewPlace `json:"place,omitempty"`
}

// SubmittedRuling rules on an earlier blocking finding: resolved, or open
// with why.
type SubmittedRuling struct {
	Finding string `json:"finding"`
	Status  string `json:"status"`
	Reason  string `json:"reason,omitempty"`
}

// ReviewSubmitResult is what the reviewer is told of its submission.
type ReviewSubmitResult struct {
	Verdict  string   `json:"verdict,omitempty"`
	Findings []string `json:"findings,omitempty"`
	Message  string   `json:"message"`
}

// ReviewSubmissionError lists what is wrong with a submission; the reviewer
// fixes it and submits again.
type ReviewSubmissionError struct{ Problems []string }

func (e *ReviewSubmissionError) Error() string {
	return "the review was not accepted; fix these and call submit_review again:\n- " + strings.Join(e.Problems, "\n- ")
}

// Submit takes a reviewer's result for the card's running pass. It checks
// the result against the pinned heads and the earlier blocking findings,
// derives the verdict, and has the review posted. A submission from any
// session but the pass's reviewer is refused.
func (s *ReviewPassService) Submit(ctx context.Context, taskID, executionID string, sub ReviewSubmission) (ReviewSubmitResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	loop, err := s.db.GetHandoffReview(ctx, taskID)
	if err != nil || loop == nil || loop.Phase != ReviewPhaseReviewing {
		return ReviewSubmitResult{}, fmt.Errorf("%w: no review pass is running on %s", ErrNotTheReviewer, taskID)
	}
	pass, err := s.db.GetHandoffReviewPass(ctx, taskID, loop.Pass)
	if err != nil {
		return ReviewSubmitResult{}, err
	}
	if pass.State != ReviewPassReviewing {
		return ReviewSubmitResult{}, fmt.Errorf("pass %d already has its result", pass.Pass)
	}
	if executionID == "" || (pass.ExecutionID != "" && pass.ExecutionID != executionID) {
		return ReviewSubmitResult{}, ErrNotTheReviewer
	}
	h, err := s.db.OpenHandoffForTask(ctx, taskID)
	if err != nil {
		return ReviewSubmitResult{}, err
	}
	if reason := strings.TrimSpace(sub.Unable); reason != "" {
		if err := s.unable(ctx, h, loop, *pass, "the reviewer was unable to review: "+reason); err != nil {
			return ReviewSubmitResult{}, err
		}
		return ReviewSubmitResult{Message: "Legato recorded that you could not review, and posts nothing. You are done."}, nil
	}
	prs, err := s.db.ListHandoffReviewPRs(ctx, taskID, pass.Pass)
	if err != nil {
		return ReviewSubmitResult{}, err
	}
	earlier, err := s.openBlocking(ctx, taskID, pass.Pass)
	if err != nil {
		return ReviewSubmitResult{}, err
	}
	if problems := s.validate(ctx, pass, prs, earlier, sub); len(problems) > 0 {
		return ReviewSubmitResult{}, &ReviewSubmissionError{Problems: problems}
	}
	if moved := s.moved(ctx, pass); moved != "" {
		if err := s.discard(ctx, loop, *pass, moved); err != nil {
			return ReviewSubmitResult{}, err
		}
		return ReviewSubmitResult{Message: "The author's checkout changed while you reviewed (" + moved + "), so Legato set this result aside and will review again. You are done."}, nil
	}

	verdict := VerdictClean
	var findings []store.HandoffReviewFinding
	var ids []string
	for _, f := range sub.Findings {
		if f.Severity == SeverityBlocking {
			verdict = VerdictChanges
		}
		finding := store.HandoffReviewFinding{ID: findingID(), TaskID: taskID, Pass: pass.Pass, PR: f.PR, Severity: f.Severity,
			Title: strings.TrimSpace(f.Title), Body: strings.TrimSpace(f.Body), Status: "open"}
		if f.Place != nil {
			finding.Path, finding.Line, finding.StartLine, finding.Side = f.Place.Path, f.Place.Line, f.Place.StartLine, sideOf(f.Place)
		}
		findings = append(findings, finding)
		ids = append(ids, finding.ID)
	}
	if err := s.db.ReplaceHandoffReviewFindings(ctx, taskID, pass.Pass, findings); err != nil {
		return ReviewSubmitResult{}, err
	}
	for _, r := range sub.Rulings {
		if r.Status == "open" {
			verdict = VerdictChanges
		}
		if err := s.db.RuleHandoffReviewFinding(ctx, r.Finding, r.Status, pass.Pass, strings.TrimSpace(r.Reason)); err != nil {
			return ReviewSubmitResult{}, err
		}
	}
	pass.State, pass.Verdict, pass.Summary = ReviewPassSubmitted, verdict, strings.TrimSpace(sub.Summary)
	if err := s.db.UpdateHandoffReviewPass(ctx, *pass); err != nil {
		return ReviewSubmitResult{}, err
	}
	s.opts.Run(func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		if err := s.post(context.Background(), taskID); err != nil {
			log.Printf("review passes: posting %s: %v", taskID, err)
		}
	})
	return ReviewSubmitResult{Verdict: verdict, Findings: ids,
		Message: fmt.Sprintf("Accepted: the verdict is %s. Legato posts the review to GitHub now. You are done; end your turn.", verdict)}, nil
}

// openBlocking are the blocking findings of earlier passes still open: each
// needs a ruling.
func (s *ReviewPassService) openBlocking(ctx context.Context, taskID string, pass int) ([]store.HandoffReviewFinding, error) {
	all, err := s.db.ListHandoffReviewFindings(ctx, taskID)
	if err != nil {
		return nil, err
	}
	var out []store.HandoffReviewFinding
	for _, f := range all {
		if f.Pass < pass && f.Severity == SeverityBlocking && f.Status == "open" {
			out = append(out, f)
		}
	}
	return out, nil
}

// validate lists what is wrong with a submission.
func (s *ReviewPassService) validate(ctx context.Context, pass *store.HandoffReviewPass, prs []store.HandoffReviewPR, earlier []store.HandoffReviewFinding, sub ReviewSubmission) []string {
	var problems []string
	if strings.TrimSpace(sub.Summary) == "" {
		problems = append(problems, "summary is empty")
	}
	heads := map[int]string{}
	for _, pr := range prs {
		heads[pr.Number] = pr.HeadSHA
	}
	for i, f := range sub.Findings {
		label := fmt.Sprintf("findings[%d]", i)
		head, ok := heads[f.PR]
		if !ok {
			problems = append(problems, fmt.Sprintf("%s: pr %d is not a pull request of this review", label, f.PR))
			continue
		}
		if f.Severity != SeverityBlocking && f.Severity != SeverityNit {
			problems = append(problems, fmt.Sprintf("%s: severity is blocking or nit, not %q", label, f.Severity))
		}
		if strings.TrimSpace(f.Title) == "" || strings.TrimSpace(f.Body) == "" {
			problems = append(problems, label+": title and body are required")
		}
		if p := f.Place; p != nil {
			if p.Side != "" && p.Side != "RIGHT" && p.Side != "LEFT" {
				problems = append(problems, fmt.Sprintf("%s: side is RIGHT or LEFT, not %q", label, p.Side))
				continue
			}
			if p.Path == "" || p.Line <= 0 || (p.StartLine > 0 && p.StartLine > p.Line) {
				problems = append(problems, label+": a place needs a path and a line (and a start_line no later than line)")
				continue
			}
			if p.Side == "LEFT" {
				continue // a deleted line: the base has it, not the head
			}
			lines, exists, err := s.opts.Checkout.Lines(ctx, pass.Checkout, head, p.Path)
			switch {
			case err != nil:
				problems = append(problems, fmt.Sprintf("%s: reading %s: %v", label, p.Path, err))
			case !exists:
				problems = append(problems, fmt.Sprintf("%s: %s does not exist at %s, the head of #%d", label, p.Path, short(head), f.PR))
			case p.Line > lines:
				problems = append(problems, fmt.Sprintf("%s: %s has %d lines at %s, not %d", label, p.Path, lines, short(head), p.Line))
			}
		}
	}
	need := map[string]bool{}
	for _, f := range earlier {
		need[f.ID] = true
	}
	for i, r := range sub.Rulings {
		if !need[r.Finding] {
			problems = append(problems, fmt.Sprintf("rulings[%d]: %q is not an open blocking finding of an earlier pass", i, r.Finding))
			continue
		}
		if r.Status != "resolved" && r.Status != "open" {
			problems = append(problems, fmt.Sprintf("rulings[%d]: status is resolved or open, not %q", i, r.Status))
		}
		if r.Status == "open" && strings.TrimSpace(r.Reason) == "" {
			problems = append(problems, fmt.Sprintf("rulings[%d]: an open ruling needs a reason", i))
		}
		delete(need, r.Finding)
	}
	for _, f := range earlier {
		if need[f.ID] {
			problems = append(problems, fmt.Sprintf("no ruling on %s (%q): rule on every earlier blocking finding", f.ID, f.Title))
		}
	}
	return problems
}

// moved says how the author's checkout differs from the pass's pin: HEAD,
// or the working tree; "" when it does not.
func (s *ReviewPassService) moved(ctx context.Context, pass *store.HandoffReviewPass) string {
	head, clean, err := s.opts.Checkout.State(ctx, pass.Checkout)
	switch {
	case err != nil:
		return "the checkout could not be read: " + err.Error()
	case head != pass.HeadSHA:
		return fmt.Sprintf("HEAD moved from %s to %s", short(pass.HeadSHA), short(head))
	case !clean:
		return "the working tree changed"
	}
	return ""
}

// discard sets a pass's result aside: nothing is posted, and a new pass
// starts once the author is idle.
func (s *ReviewPassService) discard(ctx context.Context, loop *store.HandoffReview, pass store.HandoffReviewPass, why string) error {
	_ = s.opts.Agents.KillAgentInSlot(ctx, loop.TaskID, SlotReviewer)
	pass.State, pass.Reason, pass.EndedAt = ReviewPassDiscarded, why, s.now()
	if err := s.db.UpdateHandoffReviewPass(ctx, pass); err != nil {
		return err
	}
	loop.Phase, loop.Mismatches = ReviewPhaseReady, 0
	return s.saveChanged(ctx, loop)
}

// post posts a submitted pass to GitHub, one review per pull request, then
// its rulings, and moves the loop on. Call with s.mu held.
func (s *ReviewPassService) post(ctx context.Context, taskID string) error {
	cfg := s.config()
	loop, err := s.db.GetHandoffReview(ctx, taskID)
	if err != nil || loop == nil {
		return err
	}
	pass, err := s.db.GetHandoffReviewPass(ctx, taskID, loop.Pass)
	if err != nil || pass.State != ReviewPassSubmitted {
		return err
	}
	h, err := s.db.OpenHandoffForTask(ctx, taskID)
	if err != nil {
		return err
	}
	if moved := s.moved(ctx, pass); moved != "" {
		return s.discard(ctx, loop, *pass, moved)
	}
	settings := loopSettings(loop, cfg.Defaults)
	prs, err := s.db.ListHandoffReviewPRs(ctx, taskID, pass.Pass)
	if err != nil {
		return err
	}
	all, err := s.db.ListHandoffReviewFindings(ctx, taskID)
	if err != nil {
		return err
	}
	event := ReviewComment
	if pass.Verdict == VerdictClean {
		event = ReviewApprove
	}
	var failures []string
	for _, pr := range prs {
		if pr.ReviewID != 0 {
			continue // posted before a retry
		}
		post := ReviewPost{Repo: pr.Repo, Number: pr.Number, CommitID: pr.HeadSHA, Event: event, Summary: pass.Summary,
			Footer: reviewFooter(settings, pass.Pass), Marker: fmt.Sprintf("%s/pass-%d/pr-%d", taskID, pass.Pass, pr.Number)}
		var mine []store.HandoffReviewFinding
		for _, f := range all {
			switch {
			case f.Pass == pass.Pass && f.PR == pr.Number:
				mine = append(mine, f)
				finding := ReviewPostFinding{Title: severityTitle(f), Body: f.Body}
				if f.Path != "" {
					finding.Place = &ReviewPlace{Path: f.Path, Line: f.Line, StartLine: f.StartLine, Side: f.Side}
				}
				post.Findings = append(post.Findings, finding)
			case f.RuledPass == pass.Pass && f.PR == pr.Number && f.CommentID == 0:
				post.Rulings = append(post.Rulings, rulingLine(f, pass.Pass))
			}
		}
		posted, err := s.opts.GitHub.PostReview(ctx, post)
		if err != nil {
			var gh *GitHubError
			if errors.As(err, &gh) && gh.RateLimited {
				return err // left submitted: the next Tick tries again
			}
			if errors.As(err, &gh) {
				return s.stuck(ctx, h, loop, fmt.Sprintf("GitHub refused the review of #%d: %v", pr.Number, err))
			}
			return err
		}
		if err := s.db.SetHandoffReviewPRPosted(ctx, taskID, pass.Pass, pr.Number, posted.ID, posted.URL); err != nil {
			return err
		}
		for i, index := range posted.Inline {
			if i < len(posted.CommentIDs) && index < len(mine) {
				_ = s.db.SetHandoffReviewFindingComment(ctx, mine[index].ID, posted.CommentIDs[i])
			}
		}
		// Rulings on findings that have a thread are replies in it; a
		// resolved one resolves the thread.
		for _, f := range all {
			if f.RuledPass != pass.Pass || f.PR != pr.Number || f.CommentID == 0 {
				continue
			}
			if err := s.opts.GitHub.Reply(ctx, AsReviewer, pr.Repo, pr.Number, f.CommentID, rulingLine(f, pass.Pass)); err != nil {
				failures = append(failures, fmt.Sprintf("replying on %s: %v", f.ID, err))
				continue
			}
			if f.Status == "resolved" {
				if err := s.opts.GitHub.ResolveThread(ctx, pr.Repo, pr.Number, f.CommentID); err != nil {
					failures = append(failures, fmt.Sprintf("resolving the thread of %s (left for a person): %v", f.ID, err))
				}
			}
		}
	}
	_ = s.opts.Agents.KillAgentInSlot(ctx, taskID, SlotReviewer)
	pass.State, pass.EndedAt, pass.Reason = ReviewPassPosted, s.now(), strings.Join(failures, "; ")
	if err := s.db.UpdateHandoffReviewPass(ctx, *pass); err != nil {
		return err
	}
	loop.Reason = ""
	if pass.Verdict == VerdictClean {
		loop.Phase = ReviewPhaseClean
		if err := s.saveChanged(ctx, loop); err != nil {
			return err
		}
		return s.opts.Board.MoveCard(ctx, taskID, cfg.DoneColumn)
	}
	if err := s.raiseItems(ctx, taskID, pass.Pass, prs); err != nil {
		return err
	}
	loop.Phase = ReviewPhaseWaitingForAuthor
	if err := s.saveChanged(ctx, loop); err != nil {
		return err
	}
	return s.opts.Board.MoveCard(ctx, taskID, cfg.WorkColumn)
}

func severityTitle(f store.HandoffReviewFinding) string {
	if f.Severity == SeverityNit {
		return "Nit (optional): " + f.Title
	}
	return f.Title + " [" + f.ID + "]"
}

func rulingLine(f store.HandoffReviewFinding, pass int) string {
	if f.Status == "resolved" {
		reason := f.Ruling
		if reason == "" {
			reason = "fixed"
		}
		return fmt.Sprintf("Resolved in pass %d: %s (%s)", pass, reason, f.Title)
	}
	return fmt.Sprintf("Still open in pass %d: %s (%s)", pass, f.Ruling, f.Title)
}

func reviewFooter(settings ReviewPassSettings, pass int) string {
	model := settings.Model
	if model == "" && settings.Tier != "" {
		model = "tier " + settings.Tier
	}
	who := settings.Agent
	if model != "" {
		who += " (" + model + ")"
	}
	text := fmt.Sprintf("Review pass %d by %s", pass, who)
	if settings.Skill != "" {
		text += " with the " + settings.Skill + " skill"
	}
	return text + ", posted by Legato."
}

func findingID() string {
	b := make([]byte, 4)
	_, _ = rand.Read(b)
	return "f-" + hex.EncodeToString(b)
}
