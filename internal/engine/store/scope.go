package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/jmoiron/sqlx"
)

// The states of a pin Legato keeps. A draft lives only in the extension.
const (
	ScopeSent      = "sent"
	ScopeDelivered = "delivered"
	ScopeFixed     = "fixed"
	ScopeWontFix   = "wont_fix"
)

// ScopeBatch is the pins the operator sent together with one press of Send.
type ScopeBatch struct {
	ID          string  `db:"id"`
	Origin      string  `db:"origin"`
	CreatedAt   string  `db:"created_at"`
	DeliveredAt *string `db:"delivered_at"`
	// DeliveryError is why the last push of the batch failed, or empty.
	DeliveryError string `db:"delivery_error"`
}

// ScopePin is a comment the operator placed on an element of a live page,
// with the context captured there.
type ScopePin struct {
	ID       string `db:"id"`
	BatchID  string `db:"batch_id"`
	Origin   string `db:"origin"`
	Seq      int    `db:"seq"`
	URL      string `db:"url"`
	Path     string `db:"path"`
	Comment  string `db:"comment"`
	State    string `db:"state"`
	Note     string `db:"note"`
	Selector string `db:"selector"`
	// ContextJSON is the captured page, target and styles; SourceJSON the
	// component source, when the extension found one.
	ContextJSON    string  `db:"context_json"`
	SourceJSON     *string `db:"source_json"`
	Screenshot     string  `db:"screenshot"`
	ResolvedByTask string  `db:"resolved_by_task"`
	CreatedAt      string  `db:"created_at"`
	UpdatedAt      string  `db:"updated_at"`
	DeliveredAt    *string `db:"delivered_at"`
	ResolvedAt     *string `db:"resolved_at"`
}

// ScopeSubscription is a task slot's claim on an origin.
type ScopeSubscription struct {
	Origin    string `db:"origin"`
	TaskID    string `db:"task_id"`
	Slot      string `db:"slot"`
	CreatedAt string `db:"created_at"`
}

// ScopePinFilter narrows ListScopePins. Origins nil means every origin; an
// empty, non-nil slice means none.
type ScopePinFilter struct {
	Origins []string
	States  []string
}

// InsertScopeBatch keeps a batch and its pins in one go.
func (s *Store) InsertScopeBatch(ctx context.Context, batch ScopeBatch, pins []ScopePin) error {
	return withTx(ctx, s.db, func(tx *sqlx.Tx) error {
		if _, err := tx.NamedExecContext(ctx, `
			INSERT INTO scope_batches (id, origin, created_at, delivered_at, delivery_error)
			VALUES (:id, :origin, :created_at, :delivered_at, :delivery_error)`, batch); err != nil {
			return err
		}
		for _, pin := range pins {
			if pin.ContextJSON == "" {
				pin.ContextJSON = "{}"
			}
			if _, err := tx.NamedExecContext(ctx, `
				INSERT INTO scope_pins (id, batch_id, origin, seq, url, path, comment, state, note, selector,
					context_json, source_json, screenshot, resolved_by_task, created_at, updated_at, delivered_at, resolved_at)
				VALUES (:id, :batch_id, :origin, :seq, :url, :path, :comment, :state, :note, :selector,
					:context_json, :source_json, :screenshot, :resolved_by_task, :created_at, :updated_at, :delivered_at, :resolved_at)`, pin); err != nil {
				return err
			}
		}
		return nil
	})
}

// ListScopePins returns pins, oldest batch first and in the order sent.
func (s *Store) ListScopePins(ctx context.Context, filter ScopePinFilter) ([]ScopePin, error) {
	if filter.Origins != nil && len(filter.Origins) == 0 {
		return nil, nil
	}
	query := `SELECT p.* FROM scope_pins p JOIN scope_batches b ON b.id = p.batch_id WHERE 1 = 1`
	var args []any
	if filter.Origins != nil {
		in, inArgs, err := sqlx.In(` AND p.origin IN (?)`, filter.Origins)
		if err != nil {
			return nil, err
		}
		query += in
		args = append(args, inArgs...)
	}
	if len(filter.States) > 0 {
		in, inArgs, err := sqlx.In(` AND p.state IN (?)`, filter.States)
		if err != nil {
			return nil, err
		}
		query += in
		args = append(args, inArgs...)
	}
	query += ` ORDER BY b.created_at, b.rowid, p.seq`
	var pins []ScopePin
	err := s.db.SelectContext(ctx, &pins, s.db.Rebind(query), args...)
	return pins, err
}

// GetScopePin returns one pin.
func (s *Store) GetScopePin(ctx context.Context, id string) (*ScopePin, error) {
	var pin ScopePin
	err := s.db.GetContext(ctx, &pin, `SELECT * FROM scope_pins WHERE id = ?`, id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("pin %s: %w", id, ErrNotFound)
	}
	return &pin, err
}

// MarkScopePinsDelivered records that the agent was told of these pins.
// Only pins still sent change, and their batches count as delivered.
func (s *Store) MarkScopePinsDelivered(ctx context.Context, ids []string, at string) error {
	if len(ids) == 0 {
		return nil
	}
	return withTx(ctx, s.db, func(tx *sqlx.Tx) error {
		query, args, err := sqlx.In(`UPDATE scope_pins SET state = 'delivered', delivered_at = ?, updated_at = ?
			WHERE state = 'sent' AND id IN (?)`, at, at, ids)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, tx.Rebind(query), args...); err != nil {
			return err
		}
		query, args, err = sqlx.In(`UPDATE scope_batches SET delivered_at = COALESCE(delivered_at, ?), delivery_error = ''
			WHERE id IN (SELECT batch_id FROM scope_pins WHERE id IN (?))`, at, ids)
		if err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, tx.Rebind(query), args...)
		return err
	})
}

// SetScopeBatchesUndelivered records why a push of these batches failed.
func (s *Store) SetScopeBatchesUndelivered(ctx context.Context, batchIDs []string, reason string) error {
	if len(batchIDs) == 0 {
		return nil
	}
	query, args, err := sqlx.In(`UPDATE scope_batches SET delivery_error = ? WHERE id IN (?)`, reason, batchIDs)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, s.db.Rebind(query), args...)
	return err
}

// GetScopeBatch returns one batch.
func (s *Store) GetScopeBatch(ctx context.Context, id string) (*ScopeBatch, error) {
	var batch ScopeBatch
	err := s.db.GetContext(ctx, &batch, `SELECT * FROM scope_batches WHERE id = ?`, id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("batch %s: %w", id, ErrNotFound)
	}
	return &batch, err
}

// scopePinChanged turns an update that changed nothing into ErrNotFound
// for a missing pin and ErrConflict for one in another state.
func (s *Store) scopePinChanged(ctx context.Context, result sql.Result, err error, id string) error {
	if err != nil {
		return err
	}
	if n, err := result.RowsAffected(); err != nil || n > 0 {
		return err
	}
	if _, err := s.GetScopePin(ctx, id); err != nil {
		return err
	}
	return fmt.Errorf("pin %s: %w", id, ErrConflict)
}

// ResolveScopePin marks a sent or delivered pin fixed or won't fix.
func (s *Store) ResolveScopePin(ctx context.Context, id, state, note, taskID, at string) error {
	if state != ScopeFixed && state != ScopeWontFix {
		return fmt.Errorf("pin state %q is not an outcome", state)
	}
	result, err := s.db.ExecContext(ctx, `UPDATE scope_pins
		SET state = ?, note = ?, resolved_by_task = ?, resolved_at = ?, updated_at = ?
		WHERE id = ? AND state IN ('sent', 'delivered')`, state, note, taskID, at, at, id)
	return s.scopePinChanged(ctx, result, err, id)
}

// ReopenScopePin puts a fixed or won't-fix pin back to sent with a new
// comment.
func (s *Store) ReopenScopePin(ctx context.Context, id, comment, at string) error {
	result, err := s.db.ExecContext(ctx, `UPDATE scope_pins
		SET state = 'sent', comment = ?, note = '', resolved_by_task = '', resolved_at = NULL, delivered_at = NULL, updated_at = ?
		WHERE id = ? AND state IN ('fixed', 'wont_fix')`, comment, at, id)
	return s.scopePinChanged(ctx, result, err, id)
}

// DeleteSentScopePin withdraws a pin no agent has been told of, and its
// batch when it was the last pin there.
func (s *Store) DeleteSentScopePin(ctx context.Context, id string) error {
	var batchID string
	if err := s.db.GetContext(ctx, &batchID, `SELECT batch_id FROM scope_pins WHERE id = ?`, id); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("pin %s: %w", id, ErrNotFound)
		}
		return err
	}
	result, err := s.db.ExecContext(ctx, `DELETE FROM scope_pins WHERE id = ? AND state = 'sent'`, id)
	if err := s.scopePinChanged(ctx, result, err, id); err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `DELETE FROM scope_batches WHERE id = ? AND NOT EXISTS (SELECT 1 FROM scope_pins WHERE batch_id = ?)`, batchID, batchID)
	return err
}

// SetScopeSubscription gives an origin to a slot and returns the
// subscription it replaced, if another slot had it.
func (s *Store) SetScopeSubscription(ctx context.Context, sub ScopeSubscription) (*ScopeSubscription, error) {
	sub.Slot = slotOrMain(sub.Slot)
	var previous *ScopeSubscription
	err := withTx(ctx, s.db, func(tx *sqlx.Tx) error {
		var old ScopeSubscription
		err := tx.GetContext(ctx, &old, `SELECT * FROM scope_subscriptions WHERE origin = ?`, sub.Origin)
		switch {
		case err == nil:
			if old.TaskID != sub.TaskID || old.Slot != sub.Slot {
				previous = &old
			}
		case !errors.Is(err, sql.ErrNoRows):
			return err
		}
		_, err = tx.NamedExecContext(ctx, `INSERT INTO scope_subscriptions (origin, task_id, slot, created_at)
			VALUES (:origin, :task_id, :slot, :created_at)
			ON CONFLICT(origin) DO UPDATE SET task_id = excluded.task_id, slot = excluded.slot, created_at = excluded.created_at`, sub)
		return err
	})
	return previous, err
}

// GetScopeSubscription returns the subscription of an origin.
func (s *Store) GetScopeSubscription(ctx context.Context, origin string) (*ScopeSubscription, error) {
	var sub ScopeSubscription
	err := s.db.GetContext(ctx, &sub, `SELECT * FROM scope_subscriptions WHERE origin = ?`, origin)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("subscription of %s: %w", origin, ErrNotFound)
	}
	return &sub, err
}

// DeleteScopeSubscription ends a slot's subscription to an origin, and
// says whether it had one.
func (s *Store) DeleteScopeSubscription(ctx context.Context, origin, taskID, slot string) (bool, error) {
	result, err := s.db.ExecContext(ctx, `DELETE FROM scope_subscriptions WHERE origin = ? AND task_id = ? AND slot = ?`, origin, taskID, slotOrMain(slot))
	if err != nil {
		return false, err
	}
	n, err := result.RowsAffected()
	return n > 0, err
}

// ScopeSubscriptionsOfSlot returns a slot's subscriptions, by origin.
func (s *Store) ScopeSubscriptionsOfSlot(ctx context.Context, taskID, slot string) ([]ScopeSubscription, error) {
	var subs []ScopeSubscription
	err := s.db.SelectContext(ctx, &subs, `SELECT * FROM scope_subscriptions WHERE task_id = ? AND slot = ? ORDER BY origin`, taskID, slotOrMain(slot))
	return subs, err
}

// ListScopeSubscriptions returns every subscription, by origin.
func (s *Store) ListScopeSubscriptions(ctx context.Context) ([]ScopeSubscription, error) {
	var subs []ScopeSubscription
	err := s.db.SelectContext(ctx, &subs, `SELECT * FROM scope_subscriptions ORDER BY origin`)
	return subs, err
}

// DeleteScopeSubscriptionsOfSlot ends every subscription of a slot and
// returns their origins.
func (s *Store) DeleteScopeSubscriptionsOfSlot(ctx context.Context, taskID, slot string) ([]string, error) {
	var origins []string
	err := withTx(ctx, s.db, func(tx *sqlx.Tx) error {
		if err := tx.SelectContext(ctx, &origins, `SELECT origin FROM scope_subscriptions WHERE task_id = ? AND slot = ? ORDER BY origin`, taskID, slotOrMain(slot)); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, `DELETE FROM scope_subscriptions WHERE task_id = ? AND slot = ?`, taskID, slotOrMain(slot))
		return err
	})
	return origins, err
}

// ScopeSlotStatus says whether a task slot has a running agent ("running"),
// only an interrupted one (AgentSessionInterrupted), or neither ("").
func (s *Store) ScopeSlotStatus(ctx context.Context, taskID, slot string) (string, error) {
	var statuses []string
	err := s.db.SelectContext(ctx, &statuses, `SELECT DISTINCT status FROM agent_sessions
		WHERE task_id = ? AND slot = ? AND status IN ('running', 'interrupted')`, taskID, slotOrMain(slot))
	if err != nil {
		return "", err
	}
	joined := "," + strings.Join(statuses, ",") + ","
	switch {
	case strings.Contains(joined, ",running,"):
		return "running", nil
	case strings.Contains(joined, ","+AgentSessionInterrupted+","):
		return AgentSessionInterrupted, nil
	}
	return "", nil
}

// TaskArchived says whether a task is archived; a missing task is
// ErrNotFound.
func (s *Store) TaskArchived(ctx context.Context, taskID string) (bool, error) {
	var archived bool
	err := s.db.GetContext(ctx, &archived, `SELECT archived_at IS NOT NULL FROM tasks WHERE id = ?`, taskID)
	if errors.Is(err, sql.ErrNoRows) {
		return false, fmt.Errorf("task %s: %w", taskID, ErrNotFound)
	}
	return archived, err
}
