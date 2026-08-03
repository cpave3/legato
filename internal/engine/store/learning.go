package store

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
)

// CreateLearningReflectionRequest creates a callback target idempotently by attempt.
// A retry may use a new generated ID, but all request-defining fields must match.
func (s *Store) CreateLearningReflectionRequest(ctx context.Context, request LearningReflectionRequest) (*LearningReflectionRequest, error) {
	if request.ObservationsJSON == "" {
		request.ObservationsJSON = "[]"
	}
	_, err := s.db.NamedExecContext(ctx, `INSERT INTO learning_reflection_requests
		(id, workflow_run_id, step_id, attempt_id, task_id, agent_session_id, schema_version, prompt, status, observations_json, error)
		VALUES (:id, :workflow_run_id, :step_id, :attempt_id, :task_id, :agent_session_id, :schema_version, :prompt, :status, :observations_json, :error)
		ON CONFLICT(attempt_id) DO NOTHING`, request)
	if err != nil {
		return nil, err
	}
	stored, err := s.GetLearningReflectionRequestByAttempt(ctx, request.AttemptID)
	if err != nil {
		return nil, err
	}
	if stored.WorkflowRunID != request.WorkflowRunID || stored.StepID != request.StepID || stored.TaskID != request.TaskID ||
		stored.SchemaVersion != request.SchemaVersion || stored.Prompt != request.Prompt || !equalOptionalInt(stored.AgentSessionID, request.AgentSessionID) {
		return nil, ErrConflict
	}
	return stored, nil
}

func equalOptionalInt(a, b *int) bool {
	return a == nil && b == nil || a != nil && b != nil && *a == *b
}

func (s *Store) GetLearningReflectionRequest(ctx context.Context, id string) (*LearningReflectionRequest, error) {
	var request LearningReflectionRequest
	if err := s.db.GetContext(ctx, &request, `SELECT * FROM learning_reflection_requests WHERE id = ?`, id); err != nil {
		if err == sql.ErrNoRows {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &request, nil
}

func (s *Store) GetLearningReflectionRequestByAttempt(ctx context.Context, attemptID string) (*LearningReflectionRequest, error) {
	var request LearningReflectionRequest
	if err := s.db.GetContext(ctx, &request, `SELECT * FROM learning_reflection_requests WHERE attempt_id = ?`, attemptID); err != nil {
		if err == sql.ErrNoRows {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &request, nil
}

// MarkReflectionUnavailable closes a pending callback target when its prompt
// could not be delivered. A non-pending request is an invariant conflict.
func (s *Store) MarkReflectionUnavailable(ctx context.Context, request *LearningReflectionRequest, reason string) error {
	if request == nil || request.ID == "" {
		return fmt.Errorf("reflection request ID is required")
	}
	result, err := s.db.ExecContext(ctx, `UPDATE learning_reflection_requests
		SET status = ?, error = ?, updated_at = datetime('now')
		WHERE id = ? AND status = ?`, ReflectionRequestUnavailable, reason, request.ID, ReflectionRequestPending)
	if err != nil {
		return err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 1 {
		return nil
	}
	var count int
	if err := s.db.GetContext(ctx, &count, `SELECT COUNT(*) FROM learning_reflection_requests WHERE id = ?`, request.ID); err != nil {
		return err
	}
	if count == 0 {
		return ErrNotFound
	}
	return ErrConflict
}

// SubmitLearningReflection atomically persists evidence and completes a pending request.
// It returns replayed=true only when the exact observations document was already accepted.
func (s *Store) SubmitLearningReflection(ctx context.Context, requestID, observationsJSON string, evidence []LearningEvidence) (replayed bool, err error) {
	tx, err := s.db.BeginTxx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	var request LearningReflectionRequest
	if err := tx.GetContext(ctx, &request, `SELECT * FROM learning_reflection_requests WHERE id = ?`, requestID); err != nil {
		if err == sql.ErrNoRows {
			return false, ErrNotFound
		}
		return false, err
	}
	if request.Status != ReflectionRequestPending {
		if request.Status == ReflectionRequestSubmitted && request.ObservationsJSON == observationsJSON {
			return true, nil
		}
		return false, ErrConflict
	}
	for _, item := range evidence {
		if _, err := tx.NamedExecContext(ctx, `INSERT INTO learning_evidence
			(id, source_kind, source_ref, content, content_digest, author, task_id, agent_session_id,
			 workflow_run_id, project_key, project_label, metadata_json)
			VALUES (:id, :source_kind, :source_ref, :content, :content_digest, :author, :task_id, :agent_session_id,
			 :workflow_run_id, :project_key, :project_label, :metadata_json)`, item); err != nil {
			return false, err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO learning_evidence_fts (evidence_id, content) VALUES (?, ?)`, item.ID, item.Content); err != nil {
			return false, err
		}
	}
	result, err := tx.ExecContext(ctx, `UPDATE learning_reflection_requests
		SET status = ?, observations_json = ?, submitted_at = datetime('now'), updated_at = datetime('now')
		WHERE id = ? AND status = ?`, ReflectionRequestSubmitted, observationsJSON, requestID, ReflectionRequestPending)
	if err != nil {
		return false, err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return false, err
	}
	if rows != 1 {
		return false, ErrConflict
	}
	return false, tx.Commit()
}

// UpsertLearningEvidence atomically stores an attributable evidence record and
// its search index entry. Recollection is idempotent by source identity and
// content digest; the first record (and therefore its author and ID) wins.
func (s *Store) UpsertLearningEvidence(ctx context.Context, evidence LearningEvidence) (bool, error) {
	tx, err := s.db.BeginTxx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	result, err := tx.NamedExecContext(ctx, `INSERT INTO learning_evidence
		(id, source_kind, source_ref, content, content_digest, author, task_id,
		 agent_session_id, workflow_run_id, project_key, project_label, metadata_json)
		VALUES (:id, :source_kind, :source_ref, :content, :content_digest, :author, :task_id,
		 :agent_session_id, :workflow_run_id, :project_key, :project_label, :metadata_json)
		ON CONFLICT(source_kind, source_ref, content_digest) DO NOTHING`, evidence)
	if err != nil {
		return false, err
	}
	inserted, err := result.RowsAffected()
	if err != nil {
		return false, err
	}
	if inserted == 1 {
		if _, err := tx.ExecContext(ctx, `INSERT INTO learning_evidence_fts (evidence_id, content) VALUES (?, ?)`, evidence.ID, evidence.Content); err != nil {
			return false, err
		}
	}
	if err := tx.Commit(); err != nil {
		return false, err
	}
	return inserted == 1, nil
}

func (s *Store) ListLearningEvidenceByReflection(ctx context.Context, requestID string) ([]LearningEvidence, error) {
	var evidence []LearningEvidence
	err := s.db.SelectContext(ctx, &evidence, `SELECT * FROM learning_evidence
		WHERE source_kind = 'agent_reflection' AND source_ref LIKE ? ORDER BY source_ref`, "reflection:"+requestID+":%")
	if evidence == nil {
		evidence = []LearningEvidence{}
	}
	return evidence, err
}

type LearningView struct {
	Learning       Learning                `json:"learning"`
	Evidence       []LearningEvidence      `json:"evidence"`
	Incorporations []LearningIncorporation `json:"incorporations"`
}

func (s *Store) InsertExplicitLearning(ctx context.Context, evidence LearningEvidence, learning Learning) error {
	tx, err := s.db.BeginTxx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.NamedExecContext(ctx, `INSERT INTO learning_evidence
		(id, source_kind, source_ref, content, content_digest, author, task_id,
		 agent_session_id, workflow_run_id, project_key, project_label, metadata_json)
		VALUES (:id, :source_kind, :source_ref, :content, :content_digest, :author, :task_id,
		 :agent_session_id, :workflow_run_id, :project_key, :project_label, :metadata_json)`, evidence); err != nil {
		return err
	}
	if _, err := tx.NamedExecContext(ctx, `INSERT INTO learnings
		(id, scope, project_key, kind, category, claim, rationale, status, confidence, revision, curator_run_id)
		VALUES (:id, :scope, :project_key, :kind, :category, :claim, :rationale, :status, :confidence, :revision, :curator_run_id)`, learning); err != nil {
		return err
	}
	for _, tag := range learning.Tags {
		if _, err := tx.ExecContext(ctx, `INSERT INTO learning_tags (learning_id, tag) VALUES (?, ?)`, learning.ID, tag); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO learning_evidence_links (learning_id, evidence_id, relation) VALUES (?, ?, 'support')`, learning.ID, evidence.ID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO learnings_fts (learning_id, claim, rationale) VALUES (?, ?, ?)`, learning.ID, learning.Claim, learning.Rationale); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO learning_evidence_fts (evidence_id, content) VALUES (?, ?)`, evidence.ID, evidence.Content); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) GetLearningByEvidenceSource(ctx context.Context, sourceKind, sourceRef, digest string) (*Learning, error) {
	var learning Learning
	err := s.db.GetContext(ctx, &learning, `SELECT l.* FROM learnings l
		JOIN learning_evidence_links link ON link.learning_id = l.id AND link.relation = 'support'
		JOIN learning_evidence e ON e.id = link.evidence_id
		WHERE e.source_kind = ? AND e.source_ref = ? AND e.content_digest = ? LIMIT 1`, sourceKind, sourceRef, digest)
	if err == sql.ErrNoRows {
		return nil, ErrNotFound
	}
	return &learning, err
}

func (s *Store) UpdateLearningDecision(ctx context.Context, id string, expectedRevision int, status, claim, scope, projectKey string) error {
	tx, err := s.db.BeginTxx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, `UPDATE learnings SET status = ?, claim = CASE WHEN ? = '' THEN claim ELSE ? END,
		scope = CASE WHEN ? = '' THEN scope ELSE ? END, project_key = CASE WHEN ? = '' THEN project_key ELSE ? END,
		revision = revision + 1, updated_at = datetime('now') WHERE id = ? AND revision = ? AND status = 'candidate'`,
		status, claim, claim, scope, scope, projectKey, projectKey, id, expectedRevision)
	if err != nil {
		return err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return ErrConflict
	}
	if claim != "" {
		if _, err := tx.ExecContext(ctx, `UPDATE learnings_fts SET claim = ? WHERE learning_id = ?`, claim, id); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) EditLearning(ctx context.Context, id string, expectedRevision int, claim, rationale, scope, projectKey, category string, tags []string) error {
	tx, err := s.db.BeginTxx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, `UPDATE learnings SET claim = ?, rationale = ?, scope = ?, project_key = ?, category = ?, revision = revision + 1, updated_at = datetime('now')
		WHERE id = ? AND revision = ? AND status IN ('candidate', 'validated', 'action_proposed', 'incorporated', 'stale')`, claim, rationale, scope, projectKey, category, id, expectedRevision)
	if err != nil {
		return err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return ErrConflict
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM learning_tags WHERE learning_id = ?`, id); err != nil {
		return err
	}
	for _, tag := range tags {
		if _, err := tx.ExecContext(ctx, `INSERT INTO learning_tags (learning_id, tag) VALUES (?, ?)`, id, tag); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, `UPDATE learnings_fts SET claim = ?, rationale = ? WHERE learning_id = ?`, claim, rationale, id); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) SetLearningPromptEnabled(ctx context.Context, id string, expectedRevision int, enabled bool) error {
	result, err := s.db.ExecContext(ctx, `UPDATE learnings SET prompt_enabled = ?, prompt_decided = 1,
		revision = revision + 1, updated_at = datetime('now')
		WHERE id = ? AND revision = ? AND status IN ('validated', 'incorporated')`, enabled, id, expectedRevision)
	if err != nil {
		return err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return ErrConflict
	}
	return nil
}

func (s *Store) ValidateLearningIncorporationLinks(ctx context.Context, taskID, workflowRunID, reviewTourID string) error {
	checks := []struct {
		query, id string
	}{{`SELECT COUNT(*) FROM tasks WHERE id = ?`, taskID}, {`SELECT COUNT(*) FROM workflow_runs WHERE id = ?`, workflowRunID}, {`SELECT COUNT(*) FROM review_tours WHERE id = ?`, reviewTourID}}
	for _, check := range checks {
		if check.id == "" {
			continue
		}
		var count int
		if err := s.db.GetContext(ctx, &count, check.query, check.id); err != nil {
			return err
		}
		if count == 0 {
			return ErrNotFound
		}
	}
	return nil
}

func (s *Store) InsertLearningIncorporation(ctx context.Context, incorporation LearningIncorporation, expectedLearningRevision int) error {
	tx, err := s.db.BeginTxx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, `UPDATE learnings SET status = 'action_proposed', revision = revision + 1, updated_at = datetime('now')
		WHERE id = ? AND revision = ? AND status = 'validated'`, incorporation.LearningID, expectedLearningRevision)
	if err != nil {
		return err
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		return ErrConflict
	}
	if _, err := tx.NamedExecContext(ctx, `INSERT INTO learning_incorporations
		(id, learning_id, destination, target, action, status, revision, implementation_task_id, workflow_run_id, review_tour_id, references_json)
		VALUES (:id, :learning_id, :destination, :target, :action, :status, :revision, :implementation_task_id, :workflow_run_id, :review_tour_id, :references_json)`, incorporation); err != nil {
		return err
	}
	return tx.Commit()
}

// ListLearningIncorporations returns incorporations in stable order, optionally
// restricted to the supplied lifecycle statuses.
func (s *Store) ListLearningIncorporations(ctx context.Context, statuses ...string) ([]LearningIncorporation, error) {
	items := []LearningIncorporation{}
	query := `SELECT * FROM learning_incorporations`
	args := make([]any, 0, len(statuses))
	if len(statuses) > 0 {
		placeholders := make([]string, 0, len(statuses))
		for _, status := range statuses {
			placeholders = append(placeholders, "?")
			args = append(args, status)
		}
		query += ` WHERE status IN (` + strings.Join(placeholders, ",") + `)`
	}
	query += ` ORDER BY updated_at DESC, id ASC`
	if err := s.db.SelectContext(ctx, &items, query, args...); err != nil {
		return nil, err
	}
	return items, nil
}

// ListVerifiedLearningIncorporations returns the incorporations eligible for a
// drift check. Older, failed, and already-stale records are retained but are
// deliberately not checked again.
func (s *Store) ListVerifiedLearningIncorporations(ctx context.Context) ([]LearningIncorporation, error) {
	var incorporations []LearningIncorporation
	if err := s.db.SelectContext(ctx, &incorporations, `SELECT * FROM learning_incorporations
		WHERE status = 'verified' ORDER BY created_at, id`); err != nil {
		return nil, err
	}
	if incorporations == nil {
		incorporations = []LearningIncorporation{}
	}
	return incorporations, nil
}

// MarkLearningIncorporationStale atomically marks both sides of an
// incorporation stale. Verification references, digest, result, and timestamp
// are intentionally left intact as historical provenance.
func (s *Store) MarkLearningIncorporationStale(ctx context.Context, id string, expectedRevision int) error {
	tx, err := s.db.BeginTxx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, `UPDATE learning_incorporations
		SET status = 'stale', revision = revision + 1, updated_at = datetime('now')
		WHERE id = ? AND revision = ? AND status = 'verified'`, id, expectedRevision)
	if err != nil {
		return err
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		return ErrConflict
	}
	if _, err := tx.ExecContext(ctx, `UPDATE learnings SET status = 'stale', revision = revision + 1,
		updated_at = datetime('now') WHERE id = (SELECT learning_id FROM learning_incorporations WHERE id = ?)`, id); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) GetLearningIncorporation(ctx context.Context, id string) (*LearningIncorporation, error) {
	var incorporation LearningIncorporation
	if err := s.db.GetContext(ctx, &incorporation, `SELECT * FROM learning_incorporations WHERE id = ?`, id); err != nil {
		if err == sql.ErrNoRows {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &incorporation, nil
}

func (s *Store) SubmitLearningIncorporation(ctx context.Context, id string, expectedRevision int, referencesJSON, verificationKind, verificationResult string) error {
	result, err := s.db.ExecContext(ctx, `UPDATE learning_incorporations SET status = 'verification_pending', references_json = ?,
		verification_kind = ?, verification_result = ?, revision = revision + 1, updated_at = datetime('now')
		WHERE id = ? AND revision = ? AND status IN ('proposed','accepted','in_progress')`,
		referencesJSON, verificationKind, verificationResult, id, expectedRevision)
	if err != nil {
		return err
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		return ErrConflict
	}
	return nil
}

// VerifyLearningIncorporationTarget records a deterministic target verdict. The
// implementation evidence submitted earlier remains intact; targetDigest is the
// digest of the exact file content that passed verification.
func (s *Store) VerifyLearningIncorporationTarget(ctx context.Context, id string, expectedRevision int, passed bool, targetDigest string) error {
	tx, err := s.db.BeginTxx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	status := "failed"
	verifiedAt := "NULL"
	if passed {
		status = "verified"
		verifiedAt = "datetime('now')"
	}
	query := `UPDATE learning_incorporations SET status = ?, target_digest = ?, revision = revision + 1,
		verified_at = ` + verifiedAt + `, updated_at = datetime('now') WHERE id = ? AND revision = ? AND status = 'verification_pending'`
	result, err := tx.ExecContext(ctx, query, status, targetDigest, id, expectedRevision)
	if err != nil {
		return err
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		return ErrConflict
	}
	if passed {
		if _, err := tx.ExecContext(ctx, `UPDATE learnings SET status = 'incorporated', revision = revision + 1,
			updated_at = datetime('now') WHERE id = (SELECT learning_id FROM learning_incorporations WHERE id = ?)`, id); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) VerifyLearningIncorporation(ctx context.Context, id string, expectedRevision int, passed bool, resultText string) error {
	tx, err := s.db.BeginTxx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	status := "failed"
	verifiedAt := "NULL"
	if passed {
		status = "verified"
		verifiedAt = "datetime('now')"
	}
	query := `UPDATE learning_incorporations SET status = ?, verification_result = ?, revision = revision + 1,
		verified_at = ` + verifiedAt + `, updated_at = datetime('now') WHERE id = ? AND revision = ? AND status = 'verification_pending'`
	result, err := tx.ExecContext(ctx, query, status, resultText, id, expectedRevision)
	if err != nil {
		return err
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		return ErrConflict
	}
	if passed {
		if _, err := tx.ExecContext(ctx, `UPDATE learnings SET status = 'incorporated', revision = revision + 1,
			updated_at = datetime('now') WHERE id = (SELECT learning_id FROM learning_incorporations WHERE id = ?)`, id); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) GetLearningView(ctx context.Context, id string) (*LearningView, error) {
	var view LearningView
	if err := s.db.GetContext(ctx, &view.Learning, `SELECT * FROM learnings WHERE id = ?`, id); err != nil {
		if err == sql.ErrNoRows {
			return nil, ErrNotFound
		}
		return nil, err
	}
	if err := s.db.SelectContext(ctx, &view.Evidence, `SELECT e.* FROM learning_evidence e
		JOIN learning_evidence_links link ON link.evidence_id = e.id
		WHERE link.learning_id = ? ORDER BY e.created_at, e.id`, id); err != nil {
		return nil, err
	}
	var tags []string
	if err := s.db.SelectContext(ctx, &tags, `SELECT tag FROM learning_tags WHERE learning_id = ? ORDER BY tag`, id); err != nil {
		return nil, err
	}
	view.Learning.Tags = tags
	if view.Learning.Tags == nil {
		view.Learning.Tags = []string{}
	}
	if view.Evidence == nil {
		view.Evidence = []LearningEvidence{}
	}
	if err := s.db.SelectContext(ctx, &view.Incorporations, `SELECT * FROM learning_incorporations
		WHERE learning_id = ? ORDER BY created_at DESC, id ASC`, id); err != nil {
		return nil, err
	}
	if view.Incorporations == nil {
		view.Incorporations = []LearningIncorporation{}
	}
	return &view, nil
}

// LearningQuery scopes ledger retrieval. Empty fields do not filter. Project
// scope never includes learnings from a different project.
type LearningQuery struct {
	Status     string
	Statuses   []string
	Scope      string
	ProjectKey string
	Category   string
	Tags       []string
	Limit      int
}

const defaultLearningQueryLimit = 50
const maxLearningQueryLimit = 200

func learningQueryLimit(limit int) int {
	if limit <= 0 {
		return defaultLearningQueryLimit
	}
	if limit > maxLearningQueryLimit {
		return maxLearningQueryLimit
	}
	return limit
}

func learningFilters(query LearningQuery, alias string) (string, []any) {
	var clauses []string
	var args []any
	if query.Status != "" {
		clauses = append(clauses, alias+"status = ?")
		args = append(args, query.Status)
	} else if len(query.Statuses) > 0 {
		placeholders := strings.TrimRight(strings.Repeat("?,", len(query.Statuses)), ",")
		clauses = append(clauses, alias+"status IN ("+placeholders+")")
		for _, status := range query.Statuses {
			args = append(args, status)
		}
	}
	if query.Scope != "" {
		clauses = append(clauses, alias+"scope = ?")
		args = append(args, query.Scope)
	}
	if query.ProjectKey != "" {
		clauses = append(clauses, alias+"project_key = ?")
		args = append(args, query.ProjectKey)
	} else if query.Scope == "project" {
		// Project-scoped data requires an explicit project identity.
		clauses = append(clauses, "0")
	}
	if query.Category != "" {
		clauses = append(clauses, alias+"category = ?")
		args = append(args, query.Category)
	}
	for _, tag := range query.Tags {
		clauses = append(clauses, "EXISTS (SELECT 1 FROM learning_tags lt WHERE lt.learning_id = "+alias+"id AND lt.tag = ?)")
		args = append(args, tag)
	}
	if len(clauses) == 0 {
		return "", args
	}
	return " WHERE " + strings.Join(clauses, " AND "), args
}

// ListLearnings lists ledger entries newest first, with ID as a stable tie-breaker.
func (s *Store) ListLearnings(ctx context.Context, query LearningQuery) ([]Learning, error) {
	where, args := learningFilters(query, "l.")
	args = append(args, learningQueryLimit(query.Limit))
	var learnings []Learning
	err := s.db.SelectContext(ctx, &learnings, `SELECT l.* FROM learnings l`+where+
		` ORDER BY l.updated_at DESC, l.id ASC LIMIT ?`, args...)
	if learnings == nil {
		learnings = []Learning{}
	}
	if err == nil {
		err = s.hydrateLearningTags(ctx, learnings)
	}
	return learnings, err
}

// SearchLearnings performs FTS5 search over claims and rationales and then
// applies the same scope filters as ListLearnings.
func (s *Store) SearchLearnings(ctx context.Context, text string, query LearningQuery) ([]Learning, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return []Learning{}, nil
	}
	where, args := learningFilters(query, "l.")
	if where == "" {
		where = " WHERE learnings_fts MATCH ?"
	} else {
		where += " AND learnings_fts MATCH ?"
	}
	args = append(args, text, learningQueryLimit(query.Limit))
	var learnings []Learning
	err := s.db.SelectContext(ctx, &learnings, `SELECT l.* FROM learnings_fts
		JOIN learnings l ON l.id = learnings_fts.learning_id`+where+
		` ORDER BY bm25(learnings_fts), l.updated_at DESC, l.id ASC LIMIT ?`, args...)
	if learnings == nil {
		learnings = []Learning{}
	}
	if err != nil {
		return nil, fmt.Errorf("search learnings: %w", err)
	}
	if err := s.hydrateLearningTags(ctx, learnings); err != nil {
		return nil, err
	}
	return learnings, nil
}

// ListLearningContext returns standing active user learnings plus active
// learnings for exactly one project. Lifecycle state, not evidence, determines
// eligibility for this on-demand context.
func (s *Store) ListLearningContext(ctx context.Context, projectKey string, limit int) ([]Learning, error) {
	args := []any{"validated", "action_proposed", "incorporated"}
	project := "l.scope = 'user'"
	if strings.TrimSpace(projectKey) != "" {
		project += " OR (l.scope = 'project' AND l.project_key = ?)"
		args = append(args, projectKey)
	}
	args = append(args, learningQueryLimit(limit))
	var learnings []Learning
	err := s.db.SelectContext(ctx, &learnings, `SELECT l.* FROM learnings l
		WHERE l.status IN (?, ?, ?) AND (`+project+`)
		ORDER BY CASE l.scope WHEN 'user' THEN 0 ELSE 1 END,
		         l.updated_at DESC, l.id ASC LIMIT ?`, args...)
	if learnings == nil {
		learnings = []Learning{}
	}
	if err == nil {
		err = s.hydrateLearningTags(ctx, learnings)
	}
	return learnings, err
}

func (s *Store) hydrateLearningTags(ctx context.Context, learnings []Learning) error {
	for i := range learnings {
		var tags []string
		if err := s.db.SelectContext(ctx, &tags, `SELECT tag FROM learning_tags WHERE learning_id = ? ORDER BY tag`, learnings[i].ID); err != nil {
			return err
		}
		if tags == nil {
			tags = []string{}
		}
		learnings[i].Tags = tags
	}
	return nil
}
