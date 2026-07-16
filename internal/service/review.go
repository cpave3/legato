package service

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/cpave3/legato/internal/engine/events"
	gitpkg "github.com/cpave3/legato/internal/engine/git"
	"github.com/cpave3/legato/internal/engine/store"
)

// ErrNoRepository is returned when Legato has no repository directory recorded
// for a task. Linked worktrees, swarm directories, and ordinary agent working
// directories are all supported.
var ErrNoRepository = fmt.Errorf("task has no recorded Git repository; launch an agent in a repository or link a worktree")

// subtaskTrailerPrefix marks conductor checkpoint commits with the swarm
// sub-task they ratify, e.g. "Legato-Subtask: st-0123456789".
const subtaskTrailerPrefix = "Legato-Subtask:"

const (
	reviewSeqNoteBase = 1 << 20 // note steps sort after commits
	reviewSeqDirty    = 1 << 30 // the dirty step always sorts last
)

// ReviewService maintains review tours: commit-skeleton sync, agent
// annotations, the reviewed watermark, and the Q&A transcript.
type ReviewService struct {
	store *store.Store
	tmux  TmuxManager // nil-safe: questions are stored but undeliverable
	bus   *events.Bus // nil-safe
}

func NewReviewService(s *store.Store, tmux TmuxManager, bus *events.Bus) *ReviewService {
	return &ReviewService{store: s, tmux: tmux, bus: bus}
}

// EnsureReviewTour returns the named tour for a task, creating it when absent.
func (r *ReviewService) EnsureReviewTour(ctx context.Context, taskID, name string) (*store.ReviewTour, error) {
	return r.store.EnsureReviewTour(ctx, taskID, name)
}

func (r *ReviewService) resolveTourID(ctx context.Context, taskID, name string) (string, error) {
	tour, err := r.store.EnsureReviewTour(ctx, taskID, name)
	if err != nil {
		return "", err
	}
	return tour.ID, nil
}

func (r *ReviewService) tourTask(ctx context.Context, tourID string) (*store.ReviewTour, *store.Task, error) {
	tour, err := r.store.GetReviewTour(ctx, tourID)
	if err != nil {
		return nil, nil, err
	}
	task, err := r.store.GetTask(ctx, tour.TaskID)
	if err != nil {
		return nil, nil, err
	}
	return tour, task, nil
}

// Sync imports worktree commits into tour steps and refreshes the synthetic
// dirty step. It is idempotent and runs before every read or mutation so the
// tour always reflects the worktree.
func (r *ReviewService) repositoryForTask(ctx context.Context, task *store.Task) (string, error) {
	if task.WorktreePath != nil && *task.WorktreePath != "" {
		return *task.WorktreePath, nil
	}
	if task.SwarmWorkingDir != nil && *task.SwarmWorkingDir != "" {
		return *task.SwarmWorkingDir, nil
	}
	if tour, err := r.store.GetDefaultReviewTour(ctx, task.ID); err == nil && tour.RepositoryPath != "" {
		return tour.RepositoryPath, nil
	}
	if dir, err := r.store.LatestTaskWorkingDir(ctx, task.ID); err == nil && dir != "" {
		return dir, nil
	}
	return "", ErrNoRepository
}

// BeginCapture records the current HEAD as the baseline for an ordinary
// repository session before the agent starts changing files. Linked worktrees
// continue to derive their base from the configured base branch during Sync.
func (r *ReviewService) BeginCapture(ctx context.Context, taskID, repo string) error {
	if repo == "" {
		return ErrNoRepository
	}
	base, err := gitpkg.Head(ctx, repo)
	if err != nil {
		return err
	}
	tourID, err := r.resolveTourID(ctx, taskID, "")
	if err != nil {
		return err
	}
	_, err = r.store.UpdateReviewTour(ctx, tourID, func(tour *store.ReviewTour) {
		if tour.BaseSHA == "" {
			tour.BaseSHA = base
		}
		tour.RepositoryPath = repo
	})
	return err
}

func (r *ReviewService) Sync(ctx context.Context, tourID string) error {
	tour, err := r.store.GetReviewTour(ctx, tourID)
	if err != nil {
		return err
	}
	taskID := tour.TaskID
	task, err := r.store.GetTask(ctx, taskID)
	if err != nil {
		return err
	}
	repo, err := r.repositoryForTask(ctx, task)
	if err != nil {
		return err
	}

	base := tour.BaseSHA
	if task.WorktreeBaseBranch != nil && *task.WorktreeBaseBranch != "" {
		base, err = gitpkg.MergeBase(ctx, repo, *task.WorktreeBaseBranch)
		if err != nil {
			return err
		}
	} else if base == "" {
		// This only occurs for legacy ordinary sessions created before spawn-time
		// baseline capture. Starting at HEAD still permits dirty-step review.
		base, err = gitpkg.Head(ctx, repo)
		if err != nil {
			return err
		}
	}
	commits, err := gitpkg.CommitsAhead(ctx, repo, base)
	if err != nil {
		return err
	}

	if _, err := r.store.UpdateReviewTour(ctx, tourID, func(rt *store.ReviewTour) {
		rt.BaseSHA = base
	}); err != nil {
		return err
	}

	changed := false
	for i, c := range commits {
		subtaskID, narration := extractSubtaskTrailer(c.Body)
		inserted, err := r.store.InsertReviewStep(ctx, store.ReviewStep{
			ID:        generateReviewStepID(),
			TaskID:    taskID,
			TourID:    tourID,
			Kind:      "commit",
			CommitSHA: c.SHA,
			Files:     "[]",
			Title:     c.Subject,
			Narration: narration,
			Seq:       i,
			SubtaskID: subtaskID,
		})
		if err != nil {
			return err
		}
		changed = changed || inserted
	}

	liveSHAs := make([]string, len(commits))
	for i, c := range commits {
		liveSHAs[i] = c.SHA
	}
	if err := r.store.MarkReviewStepsOrphaned(ctx, taskID, liveSHAs); err != nil {
		return err
	}

	dirtyChanged, err := r.syncDirtyStep(ctx, tourID, taskID, repo)
	if err != nil {
		return err
	}
	changed = changed || dirtyChanged

	// New work past the watermark re-enters the review queue.
	if changed {
		tour, err := r.store.GetReviewTour(ctx, tourID)
		if err != nil {
			return err
		}
		if tour.Status == "reviewed" {
			if _, err := r.store.UpdateReviewTour(ctx, tourID, func(rt *store.ReviewTour) {
				rt.Status = "ready"
			}); err != nil {
				return err
			}
		}
		r.publish(tourID, "", "synced")
	}
	return nil
}

// AnnotateArgs carries an agent annotation. SHA anchors to a commit step
// (default: the newest commit). Files with no SHA creates a file-anchored
// note step instead.
type ChapterInclude struct {
	FilePath string
	Hunk     int
}

type ChapterArgs struct {
	Title     string
	Narration string
	Risk      string
	OrderHint *int
	Includes  []ChapterInclude
}

// CreateChapter groups arbitrary base-to-HEAD hunks into one authored review step.
func (r *ReviewService) CreateChapter(ctx context.Context, tourID string, a ChapterArgs) (string, error) {
	if strings.TrimSpace(a.Title) == "" {
		return "", fmt.Errorf("chapter title is required")
	}
	if len(a.Includes) == 0 {
		return "", fmt.Errorf("chapter requires at least one included hunk")
	}
	if err := r.Sync(ctx, tourID); err != nil {
		return "", err
	}
	tour, task, err := r.tourTask(ctx, tourID)
	if err != nil {
		return "", err
	}
	repo, err := r.repositoryForTask(ctx, task)
	if err != nil {
		return "", err
	}
	taskID := tour.TaskID
	raw, err := gitpkg.DiffRange(ctx, repo, tour.BaseSHA, "HEAD")
	if err != nil {
		return "", err
	}
	files := gitpkg.ParseUnifiedDiff(raw)
	stepID := generateReviewStepID()
	members := make([]store.ReviewChapterHunk, 0, len(a.Includes))
	for i, include := range a.Includes {
		if include.Hunk < 1 {
			return "", fmt.Errorf("hunk must be a 1-based positive number")
		}
		var selected *gitpkg.FileDiff
		for j := range files {
			if files[j].OldPath == include.FilePath || files[j].NewPath == include.FilePath {
				selected = &files[j]
				break
			}
		}
		if selected == nil {
			return "", fmt.Errorf("file %q is not in the review diff", include.FilePath)
		}
		if include.Hunk > len(selected.Hunks) {
			return "", fmt.Errorf("file %q has %d hunks; cannot select hunk %d", include.FilePath, len(selected.Hunks), include.Hunk)
		}
		members = append(members, store.ReviewChapterHunk{ID: generateReviewID("rch-"), TaskID: taskID, TourID: tourID,
			StepID: stepID, FilePath: include.FilePath, HunkAnchor: selected.Hunks[include.Hunk-1].Anchor, Seq: i})
	}
	steps, err := r.store.ListReviewSteps(ctx, tourID)
	if err != nil {
		return "", err
	}
	chapterSeq := 0
	for _, step := range steps {
		if step.Kind == "chapter" && step.Seq >= chapterSeq {
			chapterSeq = step.Seq + 1
		}
	}
	err = r.store.InsertReviewChapter(ctx, store.ReviewStep{ID: stepID, TaskID: taskID, TourID: tourID, Kind: "chapter", Files: "[]",
		Title: a.Title, Narration: a.Narration, Risk: a.Risk, OrderHint: a.OrderHint, Seq: chapterSeq}, members)
	if err != nil {
		return "", err
	}
	r.publish(tourID, stepID, "chapter")
	return stepID, nil
}

type AnnotateArgs struct {
	SHA       string
	Text      string
	Files     []string
	Risk      string
	OrderHint *int
	SubtaskID string
	Hunk      *int
}

// Annotate enriches the tour: appends narration, sets risk and reading order
// on a commit step, or records a file-anchored note.
func (r *ReviewService) Annotate(ctx context.Context, tourID string, a AnnotateArgs) (string, error) {
	if err := r.Sync(ctx, tourID); err != nil {
		return "", err
	}
	tour, err := r.store.GetReviewTour(ctx, tourID)
	if err != nil {
		return "", err
	}
	taskID := tour.TaskID

	if a.Hunk != nil {
		return r.insertHunkNote(ctx, tourID, taskID, a)
	}
	if a.SHA == "" && len(a.Files) > 0 {
		return r.insertNoteStep(ctx, tourID, taskID, a)
	}

	steps, err := r.store.ListReviewSteps(ctx, tourID)
	if err != nil {
		return "", err
	}
	target, err := resolveAnnotateTarget(steps, a.SHA)
	if err != nil {
		return "", err
	}
	updated, err := r.store.UpdateReviewStep(ctx, target.ID, func(st *store.ReviewStep) {
		st.Narration = appendNarration(st.Narration, a.Text)
		if a.Risk != "" {
			st.Risk = a.Risk
		}
		if a.OrderHint != nil {
			st.OrderHint = a.OrderHint
		}
		if a.SubtaskID != "" {
			st.SubtaskID = a.SubtaskID
		}
	})
	if err != nil {
		return "", err
	}
	r.publish(tourID, updated.ID, "annotated")
	return updated.ID, nil
}

func (r *ReviewService) insertHunkNote(ctx context.Context, tourID, taskID string, a AnnotateArgs) (string, error) {
	if len(a.Files) != 1 {
		return "", fmt.Errorf("--hunk requires exactly one --file path")
	}
	if *a.Hunk < 1 {
		return "", fmt.Errorf("--hunk must be a 1-based positive number")
	}
	steps, err := r.store.ListReviewSteps(ctx, tourID)
	if err != nil {
		return "", err
	}
	target, err := resolveAnnotateTarget(steps, a.SHA)
	if err != nil {
		return "", err
	}
	files, err := r.StepDiff(ctx, tourID, target.ID)
	if err != nil {
		return "", err
	}
	path := a.Files[0]
	var file *gitpkg.FileDiff
	for i := range files {
		if files[i].OldPath == path || files[i].NewPath == path {
			file = &files[i]
			break
		}
	}
	if file == nil {
		return "", fmt.Errorf("file %q is not in commit %s", path, target.CommitSHA)
	}
	if *a.Hunk > len(file.Hunks) {
		return "", fmt.Errorf("file %q has %d hunks; cannot select hunk %d", path, len(file.Hunks), *a.Hunk)
	}
	id := generateReviewHunkNoteID()
	if err := r.store.InsertReviewHunkNote(ctx, store.ReviewHunkNote{
		ID: id, TaskID: taskID, TourID: tourID, StepID: target.ID, FilePath: path,
		HunkAnchor: file.Hunks[*a.Hunk-1].Anchor, Body: a.Text,
	}); err != nil {
		return "", err
	}
	r.publish(tourID, target.ID, "annotated")
	return id, nil
}

func (r *ReviewService) insertNoteStep(ctx context.Context, tourID, taskID string, a AnnotateArgs) (string, error) {
	filesJSON, err := json.Marshal(a.Files)
	if err != nil {
		return "", err
	}
	steps, err := r.store.ListReviewSteps(ctx, tourID)
	if err != nil {
		return "", err
	}
	noteCount := 0
	for _, s := range steps {
		if s.Kind == "note" {
			noteCount++
		}
	}
	id := generateReviewStepID()
	if _, err := r.store.InsertReviewStep(ctx, store.ReviewStep{
		ID:        id,
		TaskID:    taskID,
		TourID:    tourID,
		Kind:      "note",
		Files:     string(filesJSON),
		Title:     noteTitle(a.Files),
		Narration: a.Text,
		Risk:      a.Risk,
		OrderHint: a.OrderHint,
		Seq:       reviewSeqNoteBase + noteCount,
		SubtaskID: a.SubtaskID,
	}); err != nil {
		return "", err
	}
	r.publish(tourID, id, "annotated")
	return id, nil
}

// resolveAnnotateTarget picks the commit step an annotation lands on: the
// exact SHA (full or abbreviated) when given, otherwise the newest commit.
func resolveAnnotateTarget(steps []store.ReviewStep, sha string) (*store.ReviewStep, error) {
	var newest *store.ReviewStep
	for i := range steps {
		s := &steps[i]
		if s.Kind != "commit" || s.OrphanedAt != nil {
			continue
		}
		if sha != "" && strings.HasPrefix(s.CommitSHA, sha) {
			return s, nil
		}
		if newest == nil || s.Seq > newest.Seq {
			newest = s
		}
	}
	if sha != "" {
		return nil, fmt.Errorf("no review step for commit %q (is it committed in the task worktree?)", sha)
	}
	if newest == nil {
		return nil, fmt.Errorf("no commit steps to annotate; commit your work first or use --file for a note")
	}
	return newest, nil
}

func appendNarration(existing, text string) string {
	if strings.TrimSpace(existing) == "" {
		return text
	}
	if strings.TrimSpace(text) == "" {
		return existing
	}
	return existing + "\n\n" + text
}

func noteTitle(files []string) string {
	if len(files) == 1 {
		return "Note: " + files[0]
	}
	return fmt.Sprintf("Note: %s (+%d more)", files[0], len(files)-1)
}

// Ready marks the tour as awaiting human review. Called by the agent when it
// considers its work complete.
func (r *ReviewService) Ready(ctx context.Context, tourID, summary string) error {
	if err := r.Sync(ctx, tourID); err != nil {
		return err
	}
	tour, task, err := r.tourTask(ctx, tourID)
	if err != nil {
		return err
	}
	taskID := tour.TaskID
	repo, err := r.repositoryForTask(ctx, task)
	if err != nil {
		return err
	}
	head, err := gitpkg.Head(ctx, repo)
	if err != nil {
		return err
	}
	if err := r.rebuildRemainingChapter(ctx, tourID, taskID, repo, head); err != nil {
		return err
	}
	_, err = r.store.UpdateReviewTour(ctx, tourID, func(rt *store.ReviewTour) {
		rt.Status = "ready"
		rt.HeadSHA = head
		if summary != "" {
			rt.Summary = summary
		}
		now := time.Now().UTC().Format("2006-01-02 15:04:05")
		rt.ReadyAt = &now
	})
	if err != nil {
		return err
	}
	r.publish(tourID, "", "ready")
	return nil
}

func (r *ReviewService) rebuildRemainingChapter(ctx context.Context, tourID, taskID, repo, head string) error {
	steps, err := r.store.ListReviewSteps(ctx, tourID)
	if err != nil {
		return err
	}
	covered := map[string]bool{}
	hasAuthored := false
	for _, step := range steps {
		if step.Kind != "chapter" {
			continue
		}
		members, err := r.store.ListReviewChapterHunks(ctx, step.ID)
		if err != nil {
			return err
		}
		generated := len(members) > 0 && members[0].Generated
		if generated {
			if err := r.store.DeleteReviewChapter(ctx, step.ID); err != nil {
				return err
			}
			continue
		}
		hasAuthored = true
		for _, member := range members {
			covered[member.FilePath+"\x00"+member.HunkAnchor] = true
		}
	}
	if !hasAuthored {
		return nil
	}
	tour, err := r.store.GetReviewTour(ctx, tourID)
	if err != nil {
		return err
	}
	raw, err := gitpkg.DiffRange(ctx, repo, tour.BaseSHA, head)
	if err != nil {
		return err
	}
	stepID := generateReviewStepID()
	var members []store.ReviewChapterHunk
	seq := 0
	for _, file := range gitpkg.ParseUnifiedDiff(raw) {
		path := file.NewPath
		if path == "" || path == "/dev/null" {
			path = file.OldPath
		}
		for _, hunk := range file.Hunks {
			if covered[path+"\x00"+hunk.Anchor] || covered[file.OldPath+"\x00"+hunk.Anchor] || covered[file.NewPath+"\x00"+hunk.Anchor] {
				continue
			}
			members = append(members, store.ReviewChapterHunk{ID: generateReviewID("rch-"), TaskID: taskID, TourID: tourID, StepID: stepID, FilePath: path, HunkAnchor: hunk.Anchor, Seq: seq, Generated: true})
			seq++
		}
	}
	if len(members) == 0 {
		return nil
	}
	return r.store.InsertReviewChapter(ctx, store.ReviewStep{ID: stepID, TaskID: taskID, TourID: tourID, Kind: "chapter", Files: "[]", Title: "Remaining changes", Narration: "Changes not assigned to an authored review chapter.", Risk: "unsure", Seq: reviewSeqNoteBase - 1}, members)
}

// Delete discards a task's complete review packet. Git history and task state
// are untouched; the next capture starts a fresh tour.
func (r *ReviewService) Delete(ctx context.Context, tourID string) error {
	tour, err := r.store.GetReviewTour(ctx, tourID)
	if err != nil {
		return err
	}
	if err := r.store.DeleteReviewTour(ctx, tourID); err != nil {
		return err
	}
	r.publishDeleted(tour.ID, tour.TaskID)
	return nil
}

// Complete finishes the human review: stamps every step reviewed, records the
// newest commit SHA as the watermark, and closes the tour.
func (r *ReviewService) Complete(ctx context.Context, tourID string) error {
	if err := r.Sync(ctx, tourID); err != nil {
		return err
	}
	steps, err := r.store.ListReviewSteps(ctx, tourID)
	if err != nil {
		return err
	}
	now := time.Now().UTC().Format("2006-01-02 15:04:05")
	watermark := ""
	maxSeq := -1
	for _, s := range steps {
		if s.ReviewedAt == nil {
			if _, err := r.store.UpdateReviewStep(ctx, s.ID, func(st *store.ReviewStep) {
				st.ReviewedAt = &now
			}); err != nil {
				return err
			}
		}
		if s.Kind == "commit" && s.OrphanedAt == nil && s.Seq > maxSeq {
			maxSeq = s.Seq
			watermark = s.CommitSHA
		}
	}
	if _, err := r.store.UpdateReviewTour(ctx, tourID, func(rt *store.ReviewTour) {
		rt.Status = "reviewed"
		if rt.HeadSHA != "" {
			rt.LastReviewedSHA = rt.HeadSHA
		} else if watermark != "" {
			rt.LastReviewedSHA = watermark
		}
	}); err != nil {
		return err
	}
	r.publish(tourID, "", "reviewed")
	return nil
}

// syncDirtyStep maintains the synthetic step representing uncommitted work:
// created when the worktree is dirty, re-flagged unreviewed when its content
// changes, removed when the worktree is clean. Returns whether anything changed.
func (r *ReviewService) syncDirtyStep(ctx context.Context, tourID, taskID, repo string) (bool, error) {
	diff, err := gitpkg.DiffWorktree(ctx, repo)
	if err != nil {
		return false, err
	}
	status, err := gitpkg.StatusPorcelain(ctx, repo)
	if err != nil {
		return false, err
	}
	steps, err := r.store.ListReviewSteps(ctx, tourID)
	if err != nil {
		return false, err
	}
	var existing *store.ReviewStep
	for i := range steps {
		if steps[i].Kind == "dirty" {
			existing = &steps[i]
			break
		}
	}

	if strings.TrimSpace(status) == "" {
		if existing != nil {
			return true, r.store.DeleteReviewStep(ctx, existing.ID)
		}
		return false, nil
	}

	fingerprint := dirtyFingerprint(status, diff)
	if existing == nil {
		_, err := r.store.InsertReviewStep(ctx, store.ReviewStep{
			ID:               generateReviewStepID(),
			TaskID:           taskID,
			TourID:           tourID,
			Kind:             "dirty",
			Files:            "[]",
			Title:            "Uncommitted changes",
			Seq:              reviewSeqDirty,
			DirtyFingerprint: fingerprint,
		})
		return true, err
	}
	if existing.DirtyFingerprint != fingerprint {
		_, err := r.store.UpdateReviewStep(ctx, existing.ID, func(st *store.ReviewStep) {
			st.DirtyFingerprint = fingerprint
			st.ReviewedAt = nil
		})
		return true, err
	}
	return false, nil
}

// ReviewTourView is the assembled read model for the review UIs.
type ReviewTourView struct {
	Tour      store.ReviewTour       `json:"tour"`
	Steps     []store.ReviewStep     `json:"steps"`
	Messages  []store.ReviewMessage  `json:"messages"`
	HunkNotes []store.ReviewHunkNote `json:"hunk_notes"`
}

// Tour syncs and returns the full tour: header, ordered steps, transcript.
func (r *ReviewService) Tour(ctx context.Context, tourID string) (*ReviewTourView, error) {
	if err := r.Sync(ctx, tourID); err != nil {
		return nil, err
	}
	tour, err := r.store.GetReviewTour(ctx, tourID)
	if err != nil {
		return nil, err
	}
	steps, err := r.store.ListReviewSteps(ctx, tourID)
	if err != nil {
		return nil, err
	}
	if hasAuthoredChapter(ctx, r.store, steps) {
		filtered := make([]store.ReviewStep, 0, len(steps))
		for _, step := range steps {
			if step.Kind == "chapter" {
				filtered = append(filtered, step)
			}
		}
		steps = filtered
	}
	msgs, err := r.store.ListReviewMessages(ctx, tourID)
	if err != nil {
		return nil, err
	}
	hunkNotes, err := r.store.ListReviewHunkNotes(ctx, tourID)
	if err != nil {
		return nil, err
	}
	if steps == nil {
		steps = []store.ReviewStep{}
	}
	if msgs == nil {
		msgs = []store.ReviewMessage{}
	}
	if hunkNotes == nil {
		hunkNotes = []store.ReviewHunkNote{}
	}
	return &ReviewTourView{Tour: *tour, Steps: steps, Messages: msgs, HunkNotes: hunkNotes}, nil
}

func hasAuthoredChapter(ctx context.Context, s *store.Store, steps []store.ReviewStep) bool {
	for _, step := range steps {
		if step.Kind != "chapter" {
			continue
		}
		members, err := s.ListReviewChapterHunks(ctx, step.ID)
		if err == nil {
			for _, member := range members {
				if !member.Generated {
					return true
				}
			}
		}
	}
	return false
}

// StepDiff computes the diff a step anchors to, parsed into the interchange
// format both UIs render.
func (r *ReviewService) StepDiff(ctx context.Context, tourID, stepID string) ([]gitpkg.FileDiff, error) {
	tour, task, err := r.tourTask(ctx, tourID)
	if err != nil {
		return nil, err
	}
	taskID := tour.TaskID
	repo, err := r.repositoryForTask(ctx, task)
	if err != nil {
		return nil, err
	}

	step, err := r.store.GetReviewStepByPrefix(ctx, taskID, stepID)
	if err != nil {
		return nil, err
	}
	if step.TourID != tourID {
		return nil, store.ErrNotFound
	}

	var raw string
	switch step.Kind {
	case "commit":
		if !gitpkg.CommitExists(ctx, repo, step.CommitSHA) {
			return nil, fmt.Errorf("commit %s no longer exists (history was rewritten)", step.CommitSHA)
		}
		raw, err = gitpkg.ShowCommit(ctx, repo, step.CommitSHA)
	case "dirty":
		raw, err = gitpkg.DiffWorktree(ctx, repo)
	case "chapter":
		tour, tourErr := r.store.GetReviewTour(ctx, tourID)
		if tourErr != nil {
			return nil, tourErr
		}
		head := tour.HeadSHA
		if head == "" {
			head = "HEAD"
		}
		raw, err = gitpkg.DiffRange(ctx, repo, tour.BaseSHA, head)
		if err != nil {
			break
		}
		members, memberErr := r.store.ListReviewChapterHunks(ctx, step.ID)
		if memberErr != nil {
			return nil, memberErr
		}
		return filterChapterDiff(gitpkg.ParseUnifiedDiff(raw), members), nil
	case "note":
		var files []string
		if jsonErr := json.Unmarshal([]byte(step.Files), &files); jsonErr != nil {
			return nil, jsonErr
		}
		tour, tourErr := r.store.GetReviewTour(ctx, tourID)
		if tourErr != nil {
			return nil, tourErr
		}
		raw, err = gitpkg.DiffFiles(ctx, repo, tour.BaseSHA, files)
	default:
		return nil, fmt.Errorf("unknown step kind %q", step.Kind)
	}
	if err != nil {
		return nil, err
	}
	return gitpkg.ParseUnifiedDiff(raw), nil
}

func filterChapterDiff(files []gitpkg.FileDiff, members []store.ReviewChapterHunk) []gitpkg.FileDiff {
	byPath := make(map[string]gitpkg.FileDiff, len(files))
	for _, file := range files {
		byPath[file.NewPath] = file
		byPath[file.OldPath] = file
	}
	out := make([]gitpkg.FileDiff, 0)
	indexes := map[string]int{}
	for _, member := range members {
		file, ok := byPath[member.FilePath]
		if !ok {
			continue
		}
		for _, hunk := range file.Hunks {
			if hunk.Anchor != member.HunkAnchor {
				continue
			}
			idx, exists := indexes[member.FilePath]
			if !exists {
				file.Hunks = []gitpkg.Hunk{}
				out = append(out, file)
				idx = len(out) - 1
				indexes[member.FilePath] = idx
			}
			out[idx].Hunks = append(out[idx].Hunks, hunk)
			break
		}
	}
	if out == nil {
		return []gitpkg.FileDiff{}
	}
	return out
}

// ReviewBadgeState is the review summary attached to a task card.
type ReviewBadgeState struct {
	TourID     string `json:"tour_id"`
	Name       string `json:"name"`
	Unreviewed int    `json:"unreviewed"`
	Ready      bool   `json:"ready"`
}

// ReviewBadgeStates returns review badge data keyed by task ID. Ready tours
// are included even when they have no unreviewed steps.
func (r *ReviewService) ReviewBadgeStates(ctx context.Context) (map[string]ReviewBadgeState, error) {
	tours, err := r.store.ListReviewTours(ctx)
	if err != nil {
		return nil, err
	}
	states := make(map[string]ReviewBadgeState, len(tours))
	seenTasks := make(map[string]bool, len(tours))
	for _, listedTour := range tours {
		if seenTasks[listedTour.TaskID] {
			continue
		}
		seenTasks[listedTour.TaskID] = true
		taskTours, err := r.store.ListReviewToursByTask(ctx, listedTour.TaskID)
		if err != nil {
			return nil, err
		}
		state := ReviewBadgeState{}
		for _, tour := range taskTours {
			unreviewed, err := r.unreviewedCount(ctx, tour.ID)
			if err != nil {
				return nil, err
			}
			state.Unreviewed += unreviewed
			if state.TourID == "" || (tour.Status == "ready" && !state.Ready) {
				state.TourID = tour.ID
				state.Name = tour.Name
			}
			state.Ready = state.Ready || tour.Status == "ready"
		}
		states[listedTour.TaskID] = state
	}
	return states, nil
}

func (r *ReviewService) unreviewedCount(ctx context.Context, tourID string) (int, error) {
	steps, err := r.store.ListReviewSteps(ctx, tourID)
	if err != nil {
		return 0, err
	}
	hasChapter := false
	for _, step := range steps {
		hasChapter = hasChapter || step.Kind == "chapter"
	}
	count := 0
	for _, step := range steps {
		if step.ReviewedAt == nil && step.OrphanedAt == nil && (!hasChapter || step.Kind == "chapter") {
			count++
		}
	}
	return count, nil
}

// ReviewQueueItem is one entry in the "needs your review" queue.
type ReviewQueueItem struct {
	TourID     string `json:"tour_id"`
	TaskID     string `json:"task_id"`
	Name       string `json:"name"`
	Title      string `json:"title"`
	Status     string `json:"status"`
	Summary    string `json:"summary"`
	Unreviewed int    `json:"unreviewed"`
}

// Queue lists tasks with reviewable work: tours the agent marked ready,
// completed reviews that gained new steps, and capturing tours whose agent
// died before signalling ready.
func (r *ReviewService) Queue(ctx context.Context) ([]ReviewQueueItem, error) {
	tours, err := r.store.ListReviewTours(ctx)
	if err != nil {
		return nil, err
	}
	for _, tour := range tours {
		// A stale or missing worktree makes only that tour unavailable; it must
		// not hide reviewable work from other tasks.
		_ = r.Sync(ctx, tour.ID)
	}
	tours, err = r.store.ListReviewTours(ctx)
	if err != nil {
		return nil, err
	}
	var items []ReviewQueueItem
	for _, tour := range tours {
		unreviewed, err := r.unreviewedCount(ctx, tour.ID)
		if err != nil {
			return nil, err
		}
		include := false
		switch tour.Status {
		case "ready":
			include = true
		case "reviewed":
			include = unreviewed > 0
		case "capturing":
			include = unreviewed > 0 && !r.agentAlive(ctx, tour.TaskID)
		}
		if !include {
			continue
		}
		title := tour.TaskID
		if task, err := r.store.GetTask(ctx, tour.TaskID); err == nil {
			title = task.Title
		}
		items = append(items, ReviewQueueItem{
			TourID: tour.ID, TaskID: tour.TaskID, Name: tour.Name,
			Title: title, Status: tour.Status, Summary: tour.Summary, Unreviewed: unreviewed,
		})
	}
	return items, nil
}

func (r *ReviewService) agentAlive(ctx context.Context, taskID string) bool {
	if r.tmux == nil {
		return false
	}
	sess, err := r.store.GetAgentSessionByTaskID(ctx, taskID)
	if err != nil {
		return false
	}
	alive, _ := r.tmux.IsAlive(sess.TmuxSession)
	return alive
}

// ErrAgentOffline is returned by AskQuestion when the task has no live agent
// session. The question is still stored (undelivered) so it isn't lost.
var ErrAgentOffline = fmt.Errorf("agent session is not running; question saved but not delivered")

// ErrInvalidLineSelection indicates that selected browser lines no longer
// identify a valid contiguous range in the current step diff.
var ErrInvalidLineSelection = errors.New("invalid review line selection")

// ReviewQuestion is a reviewer message with optional source context.
type ReviewQuestion struct {
	Text      string               `json:"text"`
	Selection *ReviewLineSelection `json:"selection,omitempty"`
}

// ReviewLineSelection identifies a contiguous range by canonical diff identity.
type ReviewLineSelection struct {
	FilePath   string `json:"file_path"`
	HunkAnchor string `json:"hunk_anchor"`
	Start      int    `json:"start"`
	End        int    `json:"end"`
}

// AskQuestion stores a reviewer question on a step and delivers it into the
// task's agent pane (solo agent, or the conductor for swarm tasks — the
// conductor authored the packet and can relay to workers itself).
func (r *ReviewService) AskQuestion(ctx context.Context, tourID, stepID string, question ReviewQuestion) error {
	if err := r.Sync(ctx, tourID); err != nil {
		return err
	}
	tour, err := r.store.GetReviewTour(ctx, tourID)
	if err != nil {
		return err
	}
	taskID := tour.TaskID
	step, err := r.store.GetReviewStepByPrefix(ctx, taskID, stepID)
	if err != nil {
		return err
	}
	if step.TourID != tourID {
		return store.ErrNotFound
	}

	body := question.Text
	if question.Selection != nil {
		excerpt, err := r.reviewSelectionExcerpt(ctx, tourID, step.ID, *question.Selection)
		if err != nil {
			return err
		}
		body += "\n\n" + excerpt
	}

	line := fmt.Sprintf(
		"[legato review] Question on step %s (%q): %s — reply by running: legato review answer %s \"<your answer>\"",
		step.ID, step.Title, body, step.ID)

	delivered := false
	if r.tmux != nil {
		if sess, err := r.store.GetAgentSessionByTaskID(ctx, taskID); err == nil {
			if alive, _ := r.tmux.IsAlive(sess.TmuxSession); alive {
				if err := r.tmux.SendKeysLine(sess.TmuxSession, line); err == nil {
					delivered = true
				}
			}
		}
	}

	if _, err := r.store.InsertReviewMessage(ctx, store.ReviewMessage{
		TaskID: taskID, TourID: tourID, StepID: step.ID, Kind: "question", Author: "user", Body: body,
	}, delivered); err != nil {
		return err
	}
	r.publish(tourID, step.ID, "question")
	if !delivered {
		return ErrAgentOffline
	}
	return nil
}

func (r *ReviewService) reviewSelectionExcerpt(ctx context.Context, tourID, stepID string, selection ReviewLineSelection) (string, error) {
	files, err := r.StepDiff(ctx, tourID, stepID)
	if err != nil {
		return "", err
	}
	for _, file := range files {
		if selection.FilePath != file.NewPath && selection.FilePath != file.OldPath {
			continue
		}
		for _, hunk := range file.Hunks {
			if selection.HunkAnchor != hunk.Anchor {
				continue
			}
			if selection.Start < 0 || selection.End < selection.Start || selection.End >= len(hunk.Lines) {
				return "", fmt.Errorf("%w: line range is outside the selected hunk", ErrInvalidLineSelection)
			}
			var excerpt strings.Builder
			fmt.Fprintf(&excerpt, "Selected lines from %s %s:\n", selection.FilePath, hunk.Header)
			for _, line := range hunk.Lines[selection.Start : selection.End+1] {
				marker := " "
				lineNo := line.NewNo
				if line.Kind == gitpkg.LineAdded {
					marker = "+"
				} else if line.Kind == gitpkg.LineDeleted {
					marker = "-"
					lineNo = line.OldNo
				}
				fmt.Fprintf(&excerpt, "%s%d %s\n", marker, lineNo, line.Text)
			}
			return strings.TrimSuffix(excerpt.String(), "\n"), nil
		}
	}
	return "", fmt.Errorf("%w: file or hunk no longer matches the step diff", ErrInvalidLineSelection)
}

// Answer records an agent's reply to a reviewer question. stepID may be an
// ID prefix (as printed in the delivered question line).
func (r *ReviewService) Answer(ctx context.Context, tourID, stepID, text string) error {
	if err := r.Sync(ctx, tourID); err != nil {
		return err
	}
	tour, err := r.store.GetReviewTour(ctx, tourID)
	if err != nil {
		return err
	}
	taskID := tour.TaskID
	step, err := r.store.GetReviewStepByPrefix(ctx, taskID, stepID)
	if err != nil {
		return err
	}
	if step.TourID != tourID {
		return store.ErrNotFound
	}
	if _, err := r.store.InsertReviewMessage(ctx, store.ReviewMessage{
		TaskID: taskID, TourID: tourID, StepID: step.ID, Kind: "answer", Author: "agent", Body: text,
	}, true); err != nil {
		return err
	}
	r.publish(tourID, step.ID, "answer")
	return nil
}

// SetReviewed toggles the per-step reviewed mark.
func (r *ReviewService) SetReviewed(ctx context.Context, tourID, stepID string, reviewed bool) error {
	if err := r.Sync(ctx, tourID); err != nil {
		return err
	}
	tour, err := r.store.GetReviewTour(ctx, tourID)
	if err != nil {
		return err
	}
	taskID := tour.TaskID
	step, err := r.store.GetReviewStepByPrefix(ctx, taskID, stepID)
	if err != nil {
		return err
	}
	if step.TourID != tourID {
		return store.ErrNotFound
	}
	_, err = r.store.UpdateReviewStep(ctx, step.ID, func(st *store.ReviewStep) {
		if reviewed {
			now := time.Now().UTC().Format("2006-01-02 15:04:05")
			st.ReviewedAt = &now
		} else {
			st.ReviewedAt = nil
		}
	})
	if err != nil {
		return err
	}
	r.publish(tourID, step.ID, "reviewed")
	return nil
}

// extractSubtaskTrailer strips a "Legato-Subtask: <id>" trailer line from a
// commit body, returning the sub-task ID and the remaining narration.
func extractSubtaskTrailer(body string) (subtaskID, narration string) {
	var kept []string
	for _, line := range strings.Split(body, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, subtaskTrailerPrefix) {
			subtaskID = strings.TrimSpace(strings.TrimPrefix(trimmed, subtaskTrailerPrefix))
			continue
		}
		kept = append(kept, line)
	}
	return subtaskID, strings.TrimSpace(strings.Join(kept, "\n"))
}

// generateReviewStepID returns a 13-char id ("rs-" + 10 hex chars).
func generateReviewStepID() string {
	return generateReviewID("rs-")
}

func generateReviewHunkNoteID() string {
	return generateReviewID("rhn-")
}

func generateReviewID(prefix string) string {
	b := make([]byte, 5)
	_, _ = rand.Read(b)
	return prefix + hex.EncodeToString(b)
}

// dirtyFingerprint identifies the current uncommitted state, including index
// and worktree metadata that may not alter the rendered patch.
func dirtyFingerprint(status, diff string) string {
	sum := sha256.Sum256([]byte(status + "\x00" + diff))
	return hex.EncodeToString(sum[:])
}

func (r *ReviewService) publish(tourID, stepID, kind string) {
	if r.bus == nil {
		return
	}
	tour, err := r.store.GetReviewTour(context.Background(), tourID)
	if err != nil {
		return
	}
	r.publishEvent(tourID, tour.TaskID, stepID, kind)
}

func (r *ReviewService) publishDeleted(tourID, taskID string) {
	if r.bus != nil {
		r.publishEvent(tourID, taskID, "", "deleted")
	}
}

func (r *ReviewService) publishEvent(tourID, taskID, stepID, kind string) {
	r.bus.Publish(events.Event{
		Type:    events.EventReviewChanged,
		Payload: events.ReviewChangedPayload{TaskID: taskID, TourID: tourID, StepID: stepID, Kind: kind},
		At:      time.Now(),
	})
}
