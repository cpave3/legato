package service

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"strings"

	"github.com/cpave3/legato/internal/engine/events"
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
	// FailureScenario is how a blocking finding fails: what happens, and
	// what goes wrong. Required for blocking; posted with the finding.
	FailureScenario string `json:"failure_scenario,omitempty"`
	// Critical is why a new blocking finding of a later pass, on code
	// that did not change since the pass before, cannot wait.
	Critical string `json:"critical,omitempty"`
}

// SubmittedRuling rules on an earlier blocking finding: resolved or open,
// with why either way.
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
	l, err := s.loop(ctx, taskID)
	if err != nil {
		return ReviewSubmitResult{}, err
	}
	if reason := strings.TrimSpace(sub.Unable); reason != "" {
		if err := s.unable(ctx, l, loop, *pass, "the reviewer was unable to review: "+reason); err != nil {
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
	moved, err := s.headsMoved(ctx, pass, prs)
	if err != nil {
		// GitHub did not answer: the check before posting decides.
		log.Printf("review passes: %s: reading the heads of pass %d at submit: %v", taskID, pass.Pass, err)
	}
	if moved != "" {
		if err := s.discard(ctx, loop, *pass, moved); err != nil {
			return ReviewSubmitResult{}, err
		}
		return ReviewSubmitResult{Message: "What you reviewed changed while you reviewed (" + moved + "), so Legato set this result aside and will review the new heads. You are done."}, nil
	}

	verdict := VerdictClean
	var findings []store.HandoffReviewFinding
	var ids []string
	for _, f := range sub.Findings {
		if f.Severity == SeverityBlocking {
			verdict = VerdictChanges
		}
		body := strings.TrimSpace(f.Body)
		if scenario := strings.TrimSpace(f.FailureScenario); scenario != "" {
			body += "\n\n**Failure scenario:** " + scenario
		}
		if critical := strings.TrimSpace(f.Critical); critical != "" && f.Severity == SeverityBlocking {
			body += "\n\n**Critical:** " + critical
		}
		finding := store.HandoffReviewFinding{ID: findingID(), TaskID: taskID, Pass: pass.Pass, PR: f.PR, Severity: f.Severity,
			Title: strings.TrimSpace(f.Title), Body: body, Status: "open"}
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
		if err := s.db.RuleHandoffReviewFinding(ctx, taskID, r.Finding, r.Status, pass.Pass, strings.TrimSpace(r.Reason)); err != nil {
			return ReviewSubmitResult{}, err
		}
	}
	pass.State, pass.Verdict, pass.Summary = ReviewPassSubmitted, verdict, strings.TrimSpace(sub.Summary)
	if err := s.db.UpdateHandoffReviewPass(ctx, *pass); err != nil {
		return ReviewSubmitResult{}, err
	}
	publishMilestone(s.opts.Bus, events.WorkMilestonePayload{TaskID: taskID, Slot: SlotReviewer,
		Stage: store.WorkStageFinished, Reason: fmt.Sprintf("pass %d submitted", pass.Pass)})
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
		if f.Severity == SeverityBlocking && strings.TrimSpace(f.FailureScenario) == "" {
			problems = append(problems, label+": a blocking finding needs a failure_scenario: what happens, and what goes wrong. Without one, make it a nit")
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
	problems = append(problems, s.outsideChanges(ctx, pass, prs, sub)...)
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
		if strings.TrimSpace(r.Reason) == "" {
			switch r.Status {
			case "open":
				problems = append(problems, fmt.Sprintf("rulings[%d]: an open ruling needs a reason: say why %s is not fixed", i, r.Finding))
			case "resolved":
				problems = append(problems, fmt.Sprintf("rulings[%d]: a resolved ruling needs a reason: say how %s was fixed, or why you accept the author's answer", i, r.Finding))
			}
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

// outsideChanges lists a later pass's new blocking findings that are not on
// what changed since the pass before and are not critical. A pull request
// the pass before did not review is new in full.
func (s *ReviewPassService) outsideChanges(ctx context.Context, pass *store.HandoffReviewPass, prs []store.HandoffReviewPR, sub ReviewSubmission) []string {
	if pass.Pass < 2 {
		return nil
	}
	previous, err := s.db.ListHandoffReviewPRs(ctx, pass.TaskID, pass.Pass-1)
	if err != nil {
		return nil
	}
	before, heads := map[int]string{}, map[int]string{}
	for _, pr := range previous {
		before[pr.Number] = pr.HeadSHA
	}
	for _, pr := range prs {
		heads[pr.Number] = pr.HeadSHA
	}
	changed := map[int]map[string][][2]int{}
	var problems []string
	for i, f := range sub.Findings {
		from, reviewed := before[f.PR]
		if f.Severity != SeverityBlocking || strings.TrimSpace(f.Critical) != "" || !reviewed {
			continue
		}
		if _, ok := changed[f.PR]; !ok {
			lines, err := s.opts.Checkout.Changed(ctx, pass.Checkout, from, heads[f.PR])
			if err != nil {
				log.Printf("review passes: what changed in #%d since pass %d: %v", f.PR, pass.Pass-1, err)
				lines = nil // unknown: the finding is not held to it
			}
			changed[f.PR] = lines
		}
		lines := changed[f.PR]
		if lines == nil || onChangedLines(f.Place, lines) {
			continue
		}
		problems = append(problems, fmt.Sprintf("findings[%d]: pass %d reviews what changed since pass %d, and this blocking finding is not on those changes. "+
			"Make it a nit (a follow-up), or give critical: why it cannot wait for another change", i, pass.Pass, pass.Pass-1))
	}
	return problems
}

// onChangedLines says whether a place is on, or overlaps, changed lines of
// the head. A finding with no place, or on a deleted line, is not.
func onChangedLines(p *ReviewPlace, changed map[string][][2]int) bool {
	if p == nil || p.Side == "LEFT" {
		return false
	}
	first := p.Line
	if p.StartLine > 0 {
		first = p.StartLine
	}
	for _, r := range changed[p.Path] {
		if first <= r[1] && p.Line >= r[0] {
			return true
		}
	}
	return false
}

// headsMoved says how what a pass reviews differs from what it pinned:
// the checkout (moved), or a head on GitHub, read as the reviewer App, of
// any of layers; "" when nothing moved. An error is GitHub not answering:
// whether a head moved is not known.
func (s *ReviewPassService) headsMoved(ctx context.Context, pass *store.HandoffReviewPass, layers []store.HandoffReviewPR) (string, error) {
	if moved := s.moved(ctx, pass); moved != "" {
		return moved, nil
	}
	var why []string
	for _, pr := range layers {
		now, _, err := s.opts.GitHub.PullRequest(ctx, pr.Repo, pr.Number)
		if err != nil {
			return "", fmt.Errorf("reading the head of #%d: %w", pr.Number, err)
		}
		if now.HeadSHA != pr.HeadSHA {
			why = append(why, fmt.Sprintf("#%d moved from %s to %s on GitHub", pr.Number, short(pr.HeadSHA), short(now.HeadSHA)))
		}
	}
	return strings.Join(why, "; "), nil
}

// beforePost is the check every post of a pass's reviews goes through:
// once for every layer before the first, and for each layer again just
// before its own. A head that moved discards the pass (D2), and nothing
// more of it is posted (false). An error leaves the pass submitted, to be
// tried again on the next tick. Later checks before a write (the loop
// still open) belong here too.
func (s *ReviewPassService) beforePost(ctx context.Context, loop *store.HandoffReview, pass *store.HandoffReviewPass, layers []store.HandoffReviewPR) (bool, error) {
	moved, err := s.headsMoved(ctx, pass, layers)
	if err != nil {
		return false, err
	}
	if moved != "" {
		return false, s.discard(ctx, loop, *pass, moved)
	}
	return true, nil
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

// discard sets a pass's result aside: nothing more of it is posted, its
// findings and rulings do not stand, and it counts toward no limit. The
// same pass is pinned again at the new heads once the author is idle, so
// pushes made by then fold into it.
func (s *ReviewPassService) discard(ctx context.Context, loop *store.HandoffReview, pass store.HandoffReviewPass, why string) error {
	_ = s.opts.Agents.KillAgentInSlot(ctx, loop.TaskID, SlotReviewer)
	pass.State, pass.Reason, pass.EndedAt = ReviewPassDiscarded, why, s.now()
	if err := s.db.DiscardHandoffReviewPass(ctx, pass); err != nil {
		return err
	}
	log.Printf("review passes: %s: pass %d discarded, to be pinned again: %s", loop.TaskID, pass.Pass, why)
	if loop.Pass == pass.Pass {
		loop.Pass = pass.Pass - 1
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
	l, err := s.loop(ctx, taskID)
	if err != nil {
		return err
	}
	settings := loopSettings(loop, cfg.Defaults)
	prs, err := s.db.ListHandoffReviewPRs(ctx, taskID, pass.Pass)
	if err != nil {
		return err
	}
	if ok, err := s.beforePost(ctx, loop, pass, prs); !ok {
		return err
	}
	all, err := s.db.ListHandoffReviewFindings(ctx, taskID)
	if err != nil {
		return err
	}
	items, err := s.db.ListHandoffReviewItems(ctx, taskID)
	if err != nil {
		return err
	}
	var previous map[int]store.HandoffReviewPR
	if pass.Pass > 1 {
		earlier, err := s.db.ListHandoffReviewPRs(ctx, taskID, pass.Pass-1)
		if err != nil {
			return err
		}
		previous = map[int]store.HandoffReviewPR{}
		for _, pr := range earlier {
			previous[pr.Number] = pr
		}
	}
	var failures []string
	for _, pr := range prs {
		if pr.ReviewID != 0 || pr.Event == ReviewKept {
			continue // posted before a retry
		}
		event := ReviewComment
		if prClean(pass.Pass, pr.Number, all, items, l.CountChecks) {
			event = ReviewApprove
			// An approval that stands: the head has not moved and the
			// pass says nothing new of it. No second approval.
			if before, ok := previous[pr.Number]; ok && (before.Event == ReviewApprove || before.Event == ReviewKept) && before.HeadSHA == pr.HeadSHA &&
				!saysAnything(pass.Pass, pr.Number, all) {
				if err := s.db.SetHandoffReviewPRPosted(ctx, taskID, pass.Pass, pr.Number, 0, "", ReviewKept); err != nil {
					return err
				}
				continue
			}
		}
		post := ReviewPost{Repo: pr.Repo, Number: pr.Number, CommitID: pr.HeadSHA, Event: event, Summary: pass.Summary,
			Footer: reviewFooter(settings, pass.Pass), Marker: passReviewMarker(*pass, pr.Number)}
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
		if ok, err := s.beforePost(ctx, loop, pass, []store.HandoffReviewPR{pr}); !ok {
			return err
		}
		posted, err := s.opts.GitHub.PostReview(ctx, post)
		if err != nil {
			var gh *GitHubError
			if errors.As(err, &gh) && gh.RateLimited {
				return err // left submitted: the next Tick tries again
			}
			if errors.As(err, &gh) {
				return s.stuck(ctx, l, loop, fmt.Sprintf("GitHub refused the review of #%d: %v", pr.Number, err))
			}
			return err
		}
		if err := s.db.SetHandoffReviewPRPosted(ctx, taskID, pass.Pass, pr.Number, posted.ID, posted.URL, event); err != nil {
			return err
		}
		for i, index := range posted.Inline {
			if i < len(posted.CommentIDs) && index < len(mine) {
				_ = s.db.SetHandoffReviewFindingComment(ctx, taskID, mine[index].ID, posted.CommentIDs[i])
			}
		}
		// Rulings on findings that have a thread are replies in it; a
		// resolved one resolves the thread.
		for _, f := range all {
			if f.RuledPass != pass.Pass || f.PR != pr.Number || f.CommentID == 0 {
				continue
			}
			if err := s.opts.GitHub.Reply(ctx, AsReviewer, pr.Repo, pr.Number, f.CommentID, rulingLine(f, pass.Pass), rulingMarker(*pass, f.ID)); err != nil {
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
		l.stage(ctx, ReviewStageClean)
		return nil
	}
	if err := s.raiseItems(ctx, taskID, pass.Pass, prs); err != nil {
		return err
	}
	// Two passes in a row that each found new problems: the author fixes
	// these, then the person decides whether another pass is worth it.
	if notConverging(all, pass.Pass) && (settings.MaxPasses == 0 || settings.MaxPasses > pass.Pass) {
		settings.MaxPasses = pass.Pass
		if err := s.setSettings(ctx, loop, settings); err != nil {
			return err
		}
	}
	loop.Phase = ReviewPhaseWaitingForAuthor
	if err := s.saveChanged(ctx, loop); err != nil {
		return err
	}
	l.stage(ctx, ReviewStageAuthor)
	return nil
}

// ReviewKept is a pull request's review in a pass that posted nothing on it:
// its earlier approval stands (head unchanged, nothing new on it).
const ReviewKept = "kept"

// prClean says whether a pass leaves a pull request clean, so its review is
// an approval: no blocking finding of the pass on it, no earlier finding on
// it ruled still open, and, when the loop counts checks, no failing
// counted check on it (one answered by a reply aside). The loop's verdict
// stays the whole stack's.
func prClean(pass, number int, findings []store.HandoffReviewFinding, items []store.HandoffReviewItem, checks bool) bool {
	for _, f := range findings {
		if f.PR != number {
			continue
		}
		if f.Pass == pass && f.Severity == SeverityBlocking {
			return false
		}
		if f.RuledPass == pass && f.Status == "open" {
			return false
		}
	}
	if checks {
		for _, item := range items {
			if item.Kind == ItemCheck && item.PR == number && !item.Closed && item.Answer != AnswerReplied {
				return false
			}
		}
	}
	return true
}

// saysAnything says whether a pass has a finding or a ruling on a pull
// request.
func saysAnything(pass, number int, findings []store.HandoffReviewFinding) bool {
	for _, f := range findings {
		if f.PR == number && (f.Pass == pass || f.RuledPass == pass) {
			return true
		}
	}
	return false
}

// notConverging says whether a pass and the one before it each raised new
// blocking findings, from the third pass on: the fixes keep bringing new
// problems.
func notConverging(findings []store.HandoffReviewFinding, pass int) bool {
	if pass < 3 {
		return false
	}
	raised := map[int]bool{}
	for _, f := range findings {
		if f.Severity == SeverityBlocking {
			raised[f.Pass] = true
		}
	}
	return raised[pass] && raised[pass-1]
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
