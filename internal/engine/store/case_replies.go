package store

import (
	"context"
	"time"
)

// Integration connection states.
const (
	IntegrationConnected    = "connected"
	IntegrationDisconnected = "disconnected"
)

// Integration is Legato's registration as an OAuth client of an
// integration. Its refresh token is in the sealed secret store.
type Integration struct {
	Name          string `db:"name"`
	URL           string `db:"url"`
	Issuer        string `db:"issuer"`
	TokenEndpoint string `db:"token_endpoint"`
	Resource      string `db:"resource"`
	ClientID      string `db:"client_id"`
	State         string `db:"state"`
	ConnectedAt   string `db:"connected_at"`
}

// SaveIntegration records a connection, replacing one of the same name, as
// connected.
func (s *Store) SaveIntegration(ctx context.Context, in Integration) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO integrations (name, url, issuer, token_endpoint, resource, client_id, state)
		VALUES (?, ?, ?, ?, ?, ?, 'connected')
		ON CONFLICT(name) DO UPDATE SET url = excluded.url, issuer = excluded.issuer, token_endpoint = excluded.token_endpoint,
			resource = excluded.resource, client_id = excluded.client_id, state = 'connected',
			connected_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')`,
		in.Name, in.URL, in.Issuer, in.TokenEndpoint, in.Resource, in.ClientID)
	return err
}

func (s *Store) GetIntegration(ctx context.Context, name string) (*Integration, error) {
	var in Integration
	if err := s.db.GetContext(ctx, &in, `SELECT * FROM integrations WHERE name = ?`, name); err != nil {
		return nil, notFound(err)
	}
	return &in, nil
}

func (s *Store) SetIntegrationState(ctx context.Context, name, state string) error {
	return rowChanged(s.db.ExecContext(ctx, `UPDATE integrations SET state = ? WHERE name = ?`, state, name))
}

func (s *Store) DeleteIntegration(ctx context.Context, name string) error {
	return rowChanged(s.db.ExecContext(ctx, `DELETE FROM integrations WHERE name = ?`, name))
}

// Case reply states.
const (
	CaseReplyPending = "pending"
	CaseReplySent    = "sent"
	CaseReplyFailed  = "failed"
)

// CaseReply is a reply from an agent waiting to be sent to its integration.
type CaseReply struct {
	ID            string
	DelegationID  string
	CaseID        string
	TaskID        string
	Body          string
	State         string
	Attempts      int
	NextAttemptAt time.Time
	LastError     string
	CreatedAt     time.Time
	SentAt        *time.Time
}

type caseReplyRow struct {
	ID            string  `db:"id"`
	DelegationID  string  `db:"delegation_id"`
	CaseID        string  `db:"case_id"`
	TaskID        string  `db:"task_id"`
	Body          string  `db:"body"`
	State         string  `db:"state"`
	Attempts      int     `db:"attempts"`
	NextAttemptAt string  `db:"next_attempt_at"`
	LastError     string  `db:"last_error"`
	CreatedAt     string  `db:"created_at"`
	SentAt        *string `db:"sent_at"`
}

// outboxTime is a fixed-width UTC time, so stored times compare as text.
const outboxTime = "2006-01-02T15:04:05.000Z"

func formatOutboxTime(t time.Time) string { return t.UTC().Format(outboxTime) }

func (r caseReplyRow) reply() CaseReply {
	out := CaseReply{ID: r.ID, DelegationID: r.DelegationID, CaseID: r.CaseID, TaskID: r.TaskID, Body: r.Body, State: r.State, Attempts: r.Attempts, LastError: r.LastError}
	out.NextAttemptAt, _ = time.Parse(outboxTime, r.NextAttemptAt)
	out.CreatedAt, _ = time.Parse(outboxTime, r.CreatedAt)
	if r.SentAt != nil {
		at, _ := time.Parse(outboxTime, *r.SentAt)
		out.SentAt = &at
	}
	return out
}

func (s *Store) InsertCaseReply(ctx context.Context, r CaseReply) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO case_reply_outbox (id, delegation_id, case_id, task_id, body, state, next_attempt_at, created_at)
		VALUES (?, ?, ?, ?, ?, 'pending', ?, ?)`, r.ID, r.DelegationID, r.CaseID, r.TaskID, r.Body, formatOutboxTime(r.NextAttemptAt), formatOutboxTime(r.CreatedAt))
	return uniqueConflict(err)
}

func (s *Store) GetCaseReply(ctx context.Context, id string) (*CaseReply, error) {
	var row caseReplyRow
	if err := s.db.GetContext(ctx, &row, `SELECT * FROM case_reply_outbox WHERE id = ?`, id); err != nil {
		return nil, notFound(err)
	}
	r := row.reply()
	return &r, nil
}

// DueCaseReplies lists pending replies whose next attempt is due, oldest
// first.
func (s *Store) DueCaseReplies(ctx context.Context, now time.Time) ([]CaseReply, error) {
	var rows []caseReplyRow
	if err := s.db.SelectContext(ctx, &rows, `SELECT * FROM case_reply_outbox WHERE state = 'pending' AND next_attempt_at <= ? ORDER BY created_at, rowid`, formatOutboxTime(now)); err != nil {
		return nil, err
	}
	out := make([]CaseReply, 0, len(rows))
	for _, row := range rows {
		out = append(out, row.reply())
	}
	return out, nil
}

// ListCaseRepliesForTask lists a task's replies, newest first.
func (s *Store) ListCaseRepliesForTask(ctx context.Context, taskID string) ([]CaseReply, error) {
	var rows []caseReplyRow
	if err := s.db.SelectContext(ctx, &rows, `SELECT * FROM case_reply_outbox WHERE task_id = ? ORDER BY created_at DESC, rowid DESC`, taskID); err != nil {
		return nil, err
	}
	out := make([]CaseReply, 0, len(rows))
	for _, row := range rows {
		out = append(out, row.reply())
	}
	return out, nil
}

func (s *Store) MarkCaseReplySent(ctx context.Context, id string, at time.Time) error {
	return rowChanged(s.db.ExecContext(ctx, `UPDATE case_reply_outbox SET state = 'sent', attempts = attempts + 1, sent_at = ?, last_error = '' WHERE id = ?`, formatOutboxTime(at), id))
}

// RescheduleCaseReply records a failed attempt that will be tried again.
func (s *Store) RescheduleCaseReply(ctx context.Context, id string, next time.Time, lastError string) error {
	return rowChanged(s.db.ExecContext(ctx, `UPDATE case_reply_outbox SET attempts = attempts + 1, next_attempt_at = ?, last_error = ? WHERE id = ?`, formatOutboxTime(next), lastError, id))
}

// HoldCaseReply sets a reply's next attempt without counting one: the
// connection, not the reply, is what failed.
func (s *Store) HoldCaseReply(ctx context.Context, id string, next time.Time, lastError string) error {
	return rowChanged(s.db.ExecContext(ctx, `UPDATE case_reply_outbox SET next_attempt_at = ?, last_error = ? WHERE id = ?`, formatOutboxTime(next), lastError, id))
}

func (s *Store) MarkCaseReplyFailed(ctx context.Context, id, lastError string) error {
	return rowChanged(s.db.ExecContext(ctx, `UPDATE case_reply_outbox SET state = 'failed', attempts = attempts + 1, last_error = ? WHERE id = ?`, lastError, id))
}

// CountCaseReplies counts replies by state.
func (s *Store) CountCaseReplies(ctx context.Context) (map[string]int, error) {
	var rows []struct {
		State string `db:"state"`
		N     int    `db:"n"`
	}
	if err := s.db.SelectContext(ctx, &rows, `SELECT state, COUNT(*) AS n FROM case_reply_outbox GROUP BY state`); err != nil {
		return nil, err
	}
	counts := map[string]int{}
	for _, r := range rows {
		counts[r.State] = r.N
	}
	return counts, nil
}

// ListFailedCaseRepliesSince lists replies that failed, created after since.
func (s *Store) ListFailedCaseRepliesSince(ctx context.Context, since time.Time) ([]CaseReply, error) {
	var rows []caseReplyRow
	if err := s.db.SelectContext(ctx, &rows, `SELECT * FROM case_reply_outbox WHERE state = 'failed' AND created_at >= ? ORDER BY created_at, rowid`, formatOutboxTime(since)); err != nil {
		return nil, err
	}
	out := make([]CaseReply, 0, len(rows))
	for _, row := range rows {
		out = append(out, row.reply())
	}
	return out, nil
}
