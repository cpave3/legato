package store

import (
	"context"
	"database/sql"
	"errors"

	"github.com/jmoiron/sqlx"
)

// The stages of an agent place's work, in the order work moves through
// them. See docs/ai/agent-states.md § Stages.
const (
	WorkStageExploring    = "exploring"
	WorkStagePlanning     = "planning"
	WorkStageImplementing = "implementing"
	WorkStageReviewing    = "reviewing"
	WorkStageFinished     = "finished"
)

// What set a place's stage.
const (
	// WorkStageSourceStart: the agent started a new conversation.
	WorkStageSourceStart = "start"
	// WorkStageSourceOperator: the operator set it.
	WorkStageSourceOperator = "operator"
	// WorkStageSourceAgent: the agent reported it.
	WorkStageSourceAgent = "agent"
	// WorkStageSourceMilestone: something happened to the work (a plan
	// approved, a pull request merged).
	WorkStageSourceMilestone = "milestone"
	// WorkStageSourceEvidence: the agent changed the code.
	WorkStageSourceEvidence = "evidence"
)

// WorkStagePlace names one agent place: a task's slot, or, for a workflow
// agent, its execution (PlaceExecutionID, "" for every other place).
type WorkStagePlace struct {
	TaskID           string `db:"task_id"`
	Slot             string `db:"slot"`
	PlaceExecutionID string `db:"place_execution_id"`
}

func (p WorkStagePlace) normal() WorkStagePlace {
	p.Slot = slotOrMain(p.Slot)
	return p
}

// WorkStage is where a place's work stands.
type WorkStage struct {
	WorkStagePlace
	Stage  string `db:"stage"`
	Reason string `db:"reason"`
	Source string `db:"source"`
	// Since is when the place reached Stage; UpdatedAt when its row last
	// changed (a new reason in the same stage).
	Since       string `db:"since"`
	ExecutionID string `db:"execution_id"`
	UpdatedAt   string `db:"updated_at"`
}

// WorkStageEvent is one change in a place's stage history.
type WorkStageEvent struct {
	ID int64 `db:"id"`
	WorkStagePlace
	FromStage   string `db:"from_stage"`
	Stage       string `db:"stage"`
	Reason      string `db:"reason"`
	Source      string `db:"source"`
	ExecutionID string `db:"execution_id"`
	CreatedAt   string `db:"created_at"`
}

// GetWorkStage is a place's stage, nil when nothing has set one.
func (s *Store) GetWorkStage(ctx context.Context, place WorkStagePlace) (*WorkStage, error) {
	place = place.normal()
	var row WorkStage
	err := s.db.GetContext(ctx, &row, `SELECT task_id, slot, place_execution_id, stage, reason, source, since, execution_id, updated_at
		FROM agent_stages WHERE task_id = ? AND slot = ? AND place_execution_id = ?`, place.TaskID, place.Slot, place.PlaceExecutionID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &row, nil
}

// SetWorkStage writes a place's stage and, in the same transaction, a
// history event. Since moves only when the stage changes. It returns the
// stage the place had before, "" for none.
func (s *Store) SetWorkStage(ctx context.Context, stage WorkStage) (string, error) {
	stage.WorkStagePlace = stage.normal()
	from := ""
	err := withTx(ctx, s.db, func(tx *sqlx.Tx) error {
		err := tx.GetContext(ctx, &from, `SELECT stage FROM agent_stages WHERE task_id = ? AND slot = ? AND place_execution_id = ?`,
			stage.TaskID, stage.Slot, stage.PlaceExecutionID)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO agent_stages (task_id, slot, place_execution_id, stage, reason, source, execution_id)
			VALUES (?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT (task_id, slot, place_execution_id) DO UPDATE SET
				since = CASE WHEN agent_stages.stage = excluded.stage THEN agent_stages.since ELSE excluded.since END,
				stage = excluded.stage, reason = excluded.reason, source = excluded.source,
				execution_id = excluded.execution_id, updated_at = excluded.updated_at`,
			stage.TaskID, stage.Slot, stage.PlaceExecutionID, stage.Stage, stage.Reason, stage.Source, stage.ExecutionID); err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO agent_stage_events (task_id, slot, place_execution_id, from_stage, stage, reason, source, execution_id)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
			stage.TaskID, stage.Slot, stage.PlaceExecutionID, from, stage.Stage, stage.Reason, stage.Source, stage.ExecutionID)
		return err
	})
	return from, err
}

// ListWorkStageEvents is a place's stage history, newest first, at most
// limit events (100 when limit is not positive).
func (s *Store) ListWorkStageEvents(ctx context.Context, place WorkStagePlace, limit int) ([]WorkStageEvent, error) {
	place = place.normal()
	if limit <= 0 {
		limit = 100
	}
	out := []WorkStageEvent{}
	err := s.db.SelectContext(ctx, &out, `SELECT id, task_id, slot, place_execution_id, from_stage, stage, reason, source, execution_id, created_at
		FROM agent_stage_events WHERE task_id = ? AND slot = ? AND place_execution_id = ?
		ORDER BY id DESC LIMIT ?`, place.TaskID, place.Slot, place.PlaceExecutionID, limit)
	return out, err
}

// WorkStageStartedAt is when a place's conversation began: the time of its
// newest start event, which a resume, a reload or a sandbox switch does not
// write. It is "" for a place never started.
func (s *Store) WorkStageStartedAt(ctx context.Context, place WorkStagePlace) (string, error) {
	place = place.normal()
	var at string
	err := s.db.GetContext(ctx, &at, `SELECT created_at FROM agent_stage_events
		WHERE task_id = ? AND slot = ? AND place_execution_id = ? AND source = ?
		ORDER BY id DESC LIMIT 1`, place.TaskID, place.Slot, place.PlaceExecutionID, WorkStageSourceStart)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return at, err
}
