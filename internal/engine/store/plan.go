package store

import (
	"context"
	"database/sql"
	"errors"
)

func (s *Store) GetPlan(ctx context.Context, id string) (*Plan, error) {
	var plan Plan
	if err := s.db.GetContext(ctx, &plan, "SELECT * FROM plans WHERE id = ?", id); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &plan, nil
}

func (s *Store) GetPlanByTaskName(ctx context.Context, taskID, name string) (*Plan, error) {
	var plan Plan
	if err := s.db.GetContext(ctx, &plan, "SELECT * FROM plans WHERE task_id = ? AND name = ?", taskID, name); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &plan, nil
}

func (s *Store) InsertPlanRevision(ctx context.Context, plan Plan, revision PlanRevision, questions []PlanQuestion) error {
	tx, err := s.db.BeginTxx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.NamedExecContext(ctx, `
		INSERT INTO plans (id, task_id, name, title, summary, status, latest_revision)
		VALUES (:id, :task_id, :name, :title, :summary, :status, :latest_revision)
		ON CONFLICT(task_id, name) DO UPDATE SET title = excluded.title,
			summary = excluded.summary, status = excluded.status,
			latest_revision = excluded.latest_revision, approved_at = NULL,
			rejected_at = NULL, updated_at = datetime('now')`, plan); err != nil {
		return err
	}
	if _, err := tx.NamedExecContext(ctx, `
		INSERT INTO plan_revisions (id, plan_id, revision, markdown, manifest_json)
		VALUES (:id, :plan_id, :revision, :markdown, :manifest_json)`, revision); err != nil {
		return err
	}
	for _, question := range questions {
		if _, err := tx.NamedExecContext(ctx, `
			INSERT INTO plan_questions (id, plan_id, revision_id, question_key, kind, prompt,
				rationale, required, options_json, recommended_json)
			VALUES (:id, :plan_id, :revision_id, :question_key, :kind, :prompt,
				:rationale, :required, :options_json, :recommended_json)`, question); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) GetPlanRevision(ctx context.Context, planID string, revision int) (*PlanRevision, error) {
	var out PlanRevision
	if err := s.db.GetContext(ctx, &out, "SELECT * FROM plan_revisions WHERE plan_id = ? AND revision = ?", planID, revision); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &out, nil
}

func (s *Store) ListPlanQuestions(ctx context.Context, revisionID string) ([]PlanQuestion, error) {
	var out []PlanQuestion
	err := s.db.SelectContext(ctx, &out, "SELECT * FROM plan_questions WHERE revision_id = ? ORDER BY created_at, id", revisionID)
	return out, err
}

func (s *Store) ListPlans(ctx context.Context) ([]Plan, error) {
	var out []Plan
	err := s.db.SelectContext(ctx, &out, "SELECT * FROM plans ORDER BY updated_at DESC, id")
	return out, err
}

func (s *Store) CountUnansweredRequiredPlanQuestions(ctx context.Context, revisionID string) (int, error) {
	var count int
	err := s.db.GetContext(ctx, &count, `
		SELECT COUNT(*) FROM plan_questions q
		LEFT JOIN plan_responses r ON r.question_id = q.id
		WHERE q.revision_id = ? AND q.required = 1 AND r.id IS NULL`, revisionID)
	return count, err
}

func (s *Store) GetPlanQuestionByKey(ctx context.Context, revisionID, key string) (*PlanQuestion, error) {
	var out PlanQuestion
	if err := s.db.GetContext(ctx, &out, "SELECT * FROM plan_questions WHERE revision_id = ? AND question_key = ?", revisionID, key); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &out, nil
}

func (s *Store) UpsertPlanResponse(ctx context.Context, response PlanResponse) error {
	_, err := s.db.NamedExecContext(ctx, `
		INSERT INTO plan_responses (id, plan_id, revision_id, question_id, values_json, text)
		VALUES (:id, :plan_id, :revision_id, :question_id, :values_json, :text)
		ON CONFLICT(question_id) DO UPDATE SET values_json = excluded.values_json,
			text = excluded.text, updated_at = datetime('now')`, response)
	return err
}

func (s *Store) ListPlanResponses(ctx context.Context, revisionID string) ([]PlanResponse, error) {
	var out []PlanResponse
	err := s.db.SelectContext(ctx, &out, "SELECT * FROM plan_responses WHERE revision_id = ? ORDER BY created_at, id", revisionID)
	return out, err
}

func (s *Store) InsertPlanComment(ctx context.Context, comment PlanComment) error {
	_, err := s.db.NamedExecContext(ctx, `
		INSERT INTO plan_comments (id, plan_id, revision_id, body, selection_start,
			selection_end, selected_text, prefix, suffix)
		VALUES (:id, :plan_id, :revision_id, :body, :selection_start,
			:selection_end, :selected_text, :prefix, :suffix)`, comment)
	return err
}

func (s *Store) ListPlanComments(ctx context.Context, planID string) ([]PlanComment, error) {
	var out []PlanComment
	err := s.db.SelectContext(ctx, &out, "SELECT * FROM plan_comments WHERE plan_id = ? ORDER BY created_at, id", planID)
	return out, err
}

func (s *Store) SubmitPlanComments(ctx context.Context, revisionID string) error {
	_, err := s.db.ExecContext(ctx, "UPDATE plan_comments SET submitted_at = datetime('now') WHERE revision_id = ? AND submitted_at IS NULL", revisionID)
	return err
}

func (s *Store) UpdatePlanStatus(ctx context.Context, planID, status string) error {
	approvedExpr, rejectedExpr := "approved_at", "rejected_at"
	if status == "approved" {
		approvedExpr = "datetime('now')"
	}
	if status == "rejected" {
		rejectedExpr = "datetime('now')"
	}
	result, err := s.db.ExecContext(ctx, `UPDATE plans SET status = ?, approved_at = `+approvedExpr+`, rejected_at = `+rejectedExpr+`, updated_at = datetime('now') WHERE id = ?`, status, planID)
	if err != nil {
		return err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) ListPlanRevisions(ctx context.Context, planID string) ([]PlanRevision, error) {
	var out []PlanRevision
	err := s.db.SelectContext(ctx, &out, "SELECT * FROM plan_revisions WHERE plan_id = ? ORDER BY revision ASC", planID)
	return out, err
}

func (s *Store) InsertPlanMessage(ctx context.Context, message PlanMessage, delivered bool) (*PlanMessage, error) {
	result, err := s.db.ExecContext(ctx, `
		INSERT INTO plan_transcript (plan_id, revision_id, thread_id, kind, author, body, delivered_at)
		VALUES (?, ?, ?, ?, ?, ?, CASE WHEN ? THEN datetime('now') ELSE NULL END)`,
		message.PlanID, message.RevisionID, message.ThreadID, message.Kind, message.Author, message.Body, delivered)
	if err != nil {
		return nil, err
	}
	id, err := result.LastInsertId()
	if err != nil {
		return nil, err
	}
	var out PlanMessage
	if err := s.db.GetContext(ctx, &out, "SELECT * FROM plan_transcript WHERE id = ?", id); err != nil {
		return nil, err
	}
	return &out, nil
}

func (s *Store) ListPlanMessages(ctx context.Context, planID string) ([]PlanMessage, error) {
	var out []PlanMessage
	err := s.db.SelectContext(ctx, &out, "SELECT * FROM plan_transcript WHERE plan_id = ? ORDER BY id", planID)
	return out, err
}

func (s *Store) DeletePlan(ctx context.Context, planID string) error {
	result, err := s.db.ExecContext(ctx, "DELETE FROM plans WHERE id = ?", planID)
	if err != nil {
		return err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return ErrNotFound
	}
	return nil
}
