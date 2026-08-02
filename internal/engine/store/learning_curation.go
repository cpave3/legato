package store

import (
	"context"
	"database/sql"
)

const (
	CurationGenerationPrepared  = "prepared"
	CurationGenerationCompleted = "completed"
)

// ListUnlinkedLearningEvidence returns packet candidates in a stable order.
// Evidence already processed into a relation is not new evidence.
func (s *Store) ListUnlinkedLearningEvidence(ctx context.Context, taskID, projectKey string) ([]LearningEvidence, error) {
	items := []LearningEvidence{}
	err := s.db.SelectContext(ctx, &items, `SELECT e.* FROM learning_evidence e
		WHERE NOT EXISTS (SELECT 1 FROM learning_evidence_links x WHERE x.evidence_id = e.id)
		AND ((? <> '' AND e.task_id = ?) OR (? <> '' AND e.project_key = ?))
		ORDER BY e.created_at ASC, e.id ASC`, taskID, taskID, projectKey, projectKey)
	return items, err
}

// ListRelevantLearnings returns user learnings and learnings for exactly the
// requested project. Rejected and superseded history is not curator context.
func (s *Store) ListRelevantLearnings(ctx context.Context, projectKey string) ([]Learning, error) {
	items := []Learning{}
	err := s.db.SelectContext(ctx, &items, `SELECT * FROM learnings
		WHERE status NOT IN ('rejected','superseded')
		AND (scope = 'user' OR (? <> '' AND scope = 'project' AND project_key = ?))
		ORDER BY updated_at DESC, id ASC`, projectKey, projectKey)
	return items, err
}

// CreateLearningCurationGeneration persists a packet idempotently by attempt.
func (s *Store) CreateLearningCurationGeneration(ctx context.Context, generation LearningCurationGeneration) (*LearningCurationGeneration, error) {
	_, err := s.db.NamedExecContext(ctx, `INSERT INTO learning_curation_generations
		(id, workflow_run_id, attempt_id, packet_digest, packet_json, operations_json, status)
		VALUES (:id, :workflow_run_id, :attempt_id, :packet_digest, :packet_json, :operations_json, :status)
		ON CONFLICT(attempt_id) DO NOTHING`, generation)
	if err != nil {
		return nil, err
	}
	stored, err := s.GetLearningCurationGenerationByAttempt(ctx, generation.AttemptID)
	if err != nil {
		return nil, err
	}
	if stored.WorkflowRunID != generation.WorkflowRunID || stored.PacketDigest != generation.PacketDigest || stored.PacketJSON != generation.PacketJSON {
		return nil, ErrConflict
	}
	return stored, nil
}

func (s *Store) GetLearningCurationGenerationByAttempt(ctx context.Context, attemptID string) (*LearningCurationGeneration, error) {
	var generation LearningCurationGeneration
	if err := s.db.GetContext(ctx, &generation, `SELECT * FROM learning_curation_generations WHERE attempt_id = ?`, attemptID); err != nil {
		if err == sql.ErrNoRows {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &generation, nil
}

func (s *Store) ListLearningProposals(ctx context.Context, status string) ([]LearningProposal, error) {
	items := []LearningProposal{}
	query := `SELECT * FROM learning_proposals`
	args := []any{}
	if status != "" {
		query += ` WHERE status = ?`
		args = append(args, status)
	}
	query += ` ORDER BY created_at, id`
	return items, s.db.SelectContext(ctx, &items, query, args...)
}

func (s *Store) ApplyLearningProposalDecision(ctx context.Context, id string, expectedRevision int, accept bool) error {
	tx, err := s.db.BeginTxx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var proposal LearningProposal
	if err := tx.GetContext(ctx, &proposal, `SELECT * FROM learning_proposals WHERE id = ? AND revision = ? AND status = 'pending'`, id, expectedRevision); err != nil {
		if err == sql.ErrNoRows {
			return ErrConflict
		}
		return err
	}
	status := "rejected"
	if accept {
		status = "accepted"
		switch proposal.Operation {
		case "duplicate", "supersede":
			learningStatus := "duplicate"
			if proposal.Operation == "supersede" {
				learningStatus = "superseded"
			}
			result, err := tx.ExecContext(ctx, `UPDATE learnings SET status = ?, canonical_learning_id = ?, revision = revision + 1, updated_at = datetime('now')
					WHERE id = ? AND revision = ? AND status NOT IN ('rejected','superseded','duplicate','incorporated')`,
				learningStatus, proposal.CanonicalLearningID, proposal.LearningID, proposal.TargetLearningRevision)
			if err != nil {
				return err
			}
			rows, err := result.RowsAffected()
			if err != nil {
				return err
			}
			if rows != 1 {
				return ErrConflict
			}
		case "scope":
			result, err := tx.ExecContext(ctx, `UPDATE learnings SET scope = ?, project_key = ?, revision = revision + 1, updated_at = datetime('now')
					WHERE id = ? AND revision = ? AND status NOT IN ('rejected','superseded','duplicate','incorporated')`,
				proposal.Scope, proposal.ProjectKey, proposal.LearningID, proposal.TargetLearningRevision)
			if err != nil {
				return err
			}
			rows, err := result.RowsAffected()
			if err != nil {
				return err
			}
			if rows != 1 {
				return ErrConflict
			}
		case "revise":
			result, err := tx.ExecContext(ctx, `UPDATE learnings SET claim = ?, rationale = ?, kind = ?, scope = ?, project_key = ?, revision = revision + 1, updated_at = datetime('now')
					WHERE id = ? AND revision = ? AND status NOT IN ('rejected','superseded','duplicate','incorporated')`,
				proposal.Claim, proposal.Rationale, proposal.Kind, proposal.Scope, proposal.ProjectKey, proposal.LearningID, proposal.TargetLearningRevision)
			if err != nil {
				return err
			}
			rows, err := result.RowsAffected()
			if err != nil {
				return err
			}
			if rows != 1 {
				return ErrConflict
			}
			if _, err := tx.ExecContext(ctx, `DELETE FROM learnings_fts WHERE learning_id = ?`, proposal.LearningID); err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, `INSERT INTO learnings_fts (learning_id, claim, rationale) VALUES (?, ?, ?)`, proposal.LearningID, proposal.Claim, proposal.Rationale); err != nil {
				return err
			}
		case "discard":
			// Discard classifies evidence as intentionally processed; it does not alter a learning.
		default:
			return ErrConflict
		}
	}
	result, err := tx.ExecContext(ctx, `UPDATE learning_proposals SET status = ?, revision = revision + 1, updated_at = datetime('now') WHERE id = ? AND revision = ? AND status = 'pending'`, status, id, expectedRevision)
	if err != nil {
		return err
	}
	rows, _ := result.RowsAffected()
	if rows != 1 {
		return ErrConflict
	}
	return tx.Commit()
}

func (s *Store) GetLearningCurationGeneration(ctx context.Context, id string) (*LearningCurationGeneration, error) {
	var generation LearningCurationGeneration
	if err := s.db.GetContext(ctx, &generation, `SELECT * FROM learning_curation_generations WHERE id = ?`, id); err != nil {
		if err == sql.ErrNoRows {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &generation, nil
}

// ApplyLearningCuration atomically records all candidate creations, evidence
// relations, destination recommendations, and completion of one generation.
// Exact attempt replays are successful and changed replays conflict.
func (s *Store) ApplyLearningCuration(ctx context.Context, apply LearningCurationApply) (replayed bool, err error) {
	tx, err := s.db.BeginTxx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()

	var generation LearningCurationGeneration
	if err := tx.GetContext(ctx, &generation, `SELECT * FROM learning_curation_generations WHERE attempt_id = ?`, apply.AttemptID); err != nil {
		if err == sql.ErrNoRows {
			return false, ErrNotFound
		}
		return false, err
	}
	if generation.ID != apply.GenerationID || generation.PacketDigest != apply.PacketDigest {
		return false, ErrConflict
	}
	if generation.Status == CurationGenerationCompleted {
		if generation.OperationsJSON == apply.OperationsJSON {
			return true, nil
		}
		return false, ErrConflict
	}
	if generation.Status != CurationGenerationPrepared {
		return false, ErrConflict
	}

	for _, learning := range apply.Learnings {
		if _, err := tx.NamedExecContext(ctx, `INSERT INTO learnings
			(id, scope, project_key, kind, claim, rationale, status, confidence, revision, curator_run_id)
			VALUES (:id, :scope, :project_key, :kind, :claim, :rationale, :status, :confidence, :revision, :curator_run_id)`, learning); err != nil {
			return false, err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO learnings_fts (learning_id, claim, rationale) VALUES (?, ?, ?)`, learning.ID, learning.Claim, learning.Rationale); err != nil {
			return false, err
		}
	}
	for _, link := range apply.Links {
		if _, err := tx.ExecContext(ctx, `INSERT INTO learning_evidence_links (learning_id, evidence_id, relation) VALUES (?, ?, ?)`, link.LearningID, link.EvidenceID, link.Relation); err != nil {
			return false, err
		}
	}
	for _, proposal := range apply.Proposals {
		if _, err := tx.NamedExecContext(ctx, `INSERT INTO learning_proposals
			(id, generation_id, operation, learning_id, canonical_learning_id, scope, project_key, claim, rationale, kind, evidence_ids_json, reason, status, revision, target_learning_revision)
			VALUES (:id, :generation_id, :operation, :learning_id, :canonical_learning_id, :scope, :project_key, :claim, :rationale, :kind, :evidence_ids_json, :reason, :status, :revision, :target_learning_revision)`, proposal); err != nil {
			return false, err
		}
	}
	for _, recommendation := range apply.Incorporations {
		if _, err := tx.NamedExecContext(ctx, `INSERT INTO learning_incorporations
			(id, learning_id, destination, target, action, status, revision, references_json)
			VALUES (:id, :learning_id, :destination, :target, :action, :status, :revision, :references_json)`, recommendation); err != nil {
			return false, err
		}
	}
	result, err := tx.ExecContext(ctx, `UPDATE learning_curation_generations
		SET operations_json = ?, status = ?, completed_at = datetime('now')
		WHERE id = ? AND status = ?`, apply.OperationsJSON, CurationGenerationCompleted, generation.ID, CurationGenerationPrepared)
	if err != nil {
		return false, err
	}
	rows, err := result.RowsAffected()
	if err != nil || rows != 1 {
		if err != nil {
			return false, err
		}
		return false, ErrConflict
	}
	return false, tx.Commit()
}
