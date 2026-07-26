package store

import (
	"context"
	"database/sql"
	"errors"
)

func (s *Store) ClaimArtifact(ctx context.Context, kind, artifactID, claimantTaskID string) (*ArtifactClaim, *ArtifactClaim, error) {
	tx, err := s.db.BeginTxx(ctx, nil)
	if err != nil {
		return nil, nil, err
	}
	defer tx.Rollback()

	var previous ArtifactClaim
	previousErr := tx.GetContext(ctx, &previous,
		"SELECT * FROM artifact_claims WHERE artifact_kind = ? AND artifact_id = ?", kind, artifactID)
	if previousErr != nil && !errors.Is(previousErr, sql.ErrNoRows) {
		return nil, nil, previousErr
	}
	if previousErr == nil && previous.ClaimantTaskID == claimantTaskID {
		return &previous, &previous, tx.Commit()
	}
	generation := 1
	if previousErr == nil {
		generation = previous.Generation + 1
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO artifact_claims (artifact_kind, artifact_id, claimant_task_id, generation)
		VALUES (?, ?, ?, ?)
		ON CONFLICT(artifact_kind, artifact_id) DO UPDATE SET
			claimant_task_id = excluded.claimant_task_id,
			generation = excluded.generation,
			claimed_at = datetime('now'), updated_at = datetime('now')`,
		kind, artifactID, claimantTaskID, generation); err != nil {
		return nil, nil, err
	}
	if previousErr == nil {
		if _, err := tx.ExecContext(ctx, `
			UPDATE artifact_interactions SET claimant_task_id = ?, claim_generation = ?,
				delivered_at = NULL, attempt_count = 0, last_error = '', updated_at = datetime('now')
			WHERE artifact_kind = ? AND artifact_id = ? AND acknowledged_at IS NULL`,
			claimantTaskID, generation, kind, artifactID); err != nil {
			return nil, nil, err
		}
	}
	var current ArtifactClaim
	if err := tx.GetContext(ctx, &current,
		"SELECT * FROM artifact_claims WHERE artifact_kind = ? AND artifact_id = ?", kind, artifactID); err != nil {
		return nil, nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, nil, err
	}
	var old *ArtifactClaim
	if previousErr == nil {
		old = &previous
	}
	return &current, old, nil
}

func (s *Store) GetArtifactClaim(ctx context.Context, kind, artifactID string) (*ArtifactClaim, error) {
	var claim ArtifactClaim
	err := s.db.GetContext(ctx, &claim,
		"SELECT * FROM artifact_claims WHERE artifact_kind = ? AND artifact_id = ?", kind, artifactID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return &claim, err
}

func (s *Store) InsertArtifactInteraction(ctx context.Context, interaction ArtifactInteraction) error {
	_, err := s.db.NamedExecContext(ctx, `
		INSERT INTO artifact_interactions
			(id, artifact_kind, artifact_id, revision_id, thread_id, kind, body,
			 claimant_task_id, claim_generation)
		VALUES (:id, :artifact_kind, :artifact_id, :revision_id, :thread_id, :kind, :body,
		        :claimant_task_id, :claim_generation)`, interaction)
	return err
}

func (s *Store) ListArtifactInteractionsForClaimant(ctx context.Context, claimantTaskID string, unacknowledgedOnly bool) ([]ArtifactInteraction, error) {
	query := "SELECT * FROM artifact_interactions WHERE claimant_task_id = ?"
	if unacknowledgedOnly {
		query += " AND acknowledged_at IS NULL"
	}
	query += " ORDER BY created_at, id"
	var out []ArtifactInteraction
	err := s.db.SelectContext(ctx, &out, query, claimantTaskID)
	return out, err
}

func (s *Store) ListUndeliveredArtifactInteractions(ctx context.Context) ([]ArtifactInteraction, error) {
	var out []ArtifactInteraction
	err := s.db.SelectContext(ctx, &out, `
		SELECT * FROM artifact_interactions
		WHERE delivered_at IS NULL AND acknowledged_at IS NULL
		ORDER BY created_at, id`)
	return out, err
}

func (s *Store) MarkArtifactInteractionDelivery(ctx context.Context, id string, delivered bool, message string) error {
	if delivered {
		_, err := s.db.ExecContext(ctx, `
			UPDATE artifact_interactions SET attempt_count = attempt_count + 1,
				delivered_at = datetime('now'), last_error = '', updated_at = datetime('now')
			WHERE id = ? AND delivered_at IS NULL`, id)
		return err
	}
	_, err := s.db.ExecContext(ctx, `
		UPDATE artifact_interactions SET attempt_count = attempt_count + 1,
			last_error = ?, updated_at = datetime('now') WHERE id = ?`, message, id)
	return err
}

func (s *Store) AcknowledgeArtifactInteractions(ctx context.Context, claimantTaskID string, ids []string) error {
	if len(ids) == 0 {
		return nil
	}
	tx, err := s.db.BeginTxx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, id := range ids {
		if _, err := tx.ExecContext(ctx, `
			UPDATE artifact_interactions SET acknowledged_at = datetime('now'), updated_at = datetime('now')
			WHERE id = ? AND claimant_task_id = ? AND acknowledged_at IS NULL`, id, claimantTaskID); err != nil {
			return err
		}
	}
	return tx.Commit()
}
