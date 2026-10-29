package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

// SecretRow is one value of Legato's own secret store, sealed.
type SecretRow struct {
	Name       string `db:"name"`
	Type       string `db:"type"`
	Nonce      []byte `db:"nonce"`
	Ciphertext []byte `db:"ciphertext"`
	CreatedAt  string `db:"created_at"`
	UpdatedAt  string `db:"updated_at"`
}

// PutSecret stores a sealed value, replacing one of the same name.
func (s *Store) PutSecret(ctx context.Context, row SecretRow) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO secrets (name, type, nonce, ciphertext) VALUES (?, ?, ?, ?)
		ON CONFLICT(name) DO UPDATE SET type = excluded.type, nonce = excluded.nonce,
			ciphertext = excluded.ciphertext, updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')`,
		row.Name, row.Type, row.Nonce, row.Ciphertext)
	return err
}

func (s *Store) GetSecret(ctx context.Context, name string) (*SecretRow, error) {
	var row SecretRow
	if err := s.db.GetContext(ctx, &row, "SELECT * FROM secrets WHERE name = ?", name); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &row, nil
}

func (s *Store) DeleteSecret(ctx context.Context, name string) error {
	result, err := s.db.ExecContext(ctx, "DELETE FROM secrets WHERE name = ?", name)
	if err != nil {
		return err
	}
	if n, _ := result.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// ListSecrets returns the stored secrets by name.
func (s *Store) ListSecrets(ctx context.Context) ([]SecretRow, error) {
	rows := []SecretRow{}
	err := s.db.SelectContext(ctx, &rows, "SELECT * FROM secrets ORDER BY name")
	return rows, err
}

// CredentialGrant is what an agent session may do with a credential.
type CredentialGrant struct {
	ID             string  `db:"id"`
	ExecutionID    string  `db:"execution_id"`
	TaskID         string  `db:"task_id"`
	Credential     string  `db:"credential"`
	OperationsJSON string  `db:"operations_json"`
	ResourcesJSON  string  `db:"resources_json"`
	Mode           string  `db:"mode"`
	UsesLeft       *int    `db:"uses_left"`
	ExpiresAt      *string `db:"expires_at"`
	ApprovalID     string  `db:"approval_id"`
	CreatedAt      string  `db:"created_at"`
	EndedAt        *string `db:"ended_at"`
}

func (s *Store) InsertCredentialGrant(ctx context.Context, g CredentialGrant) error {
	if g.OperationsJSON == "" {
		g.OperationsJSON = "[]"
	}
	if g.ResourcesJSON == "" {
		g.ResourcesJSON = "[]"
	}
	_, err := s.db.NamedExecContext(ctx, `
		INSERT INTO credential_grants
			(id, execution_id, task_id, credential, operations_json, resources_json, mode, uses_left, expires_at, approval_id)
		VALUES
			(:id, :execution_id, :task_id, :credential, :operations_json, :resources_json, :mode, :uses_left, :expires_at, :approval_id)`, g)
	return err
}

// LiveCredentialGrants are an execution's grants for a credential that have
// neither ended, expired by now, nor run out of uses.
func (s *Store) LiveCredentialGrants(ctx context.Context, executionID, credential string, now time.Time) ([]CredentialGrant, error) {
	grants := []CredentialGrant{}
	err := s.db.SelectContext(ctx, &grants, `
		SELECT * FROM credential_grants
		WHERE execution_id = ? AND credential = ? AND ended_at IS NULL
		  AND (expires_at IS NULL OR expires_at > ?)
		  AND (uses_left IS NULL OR uses_left > 0)
		ORDER BY created_at, rowid`, executionID, credential, now.UTC().Format(time.RFC3339Nano))
	return grants, err
}

// UseCredentialGrant counts one use of a grant with a number of uses. It
// returns ErrConflict when none are left.
func (s *Store) UseCredentialGrant(ctx context.Context, id string) error {
	result, err := s.db.ExecContext(ctx, `
		UPDATE credential_grants SET uses_left = uses_left - 1,
			ended_at = CASE WHEN uses_left = 1 THEN strftime('%Y-%m-%dT%H:%M:%fZ', 'now') ELSE ended_at END
		WHERE id = ? AND ended_at IS NULL AND (uses_left IS NULL OR uses_left > 0)`, id)
	if err != nil {
		return err
	}
	return expectOneRow(result)
}

// EndCredentialGrants ends every grant of an execution, as when it ends.
func (s *Store) EndCredentialGrants(ctx context.Context, executionID string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE credential_grants SET ended_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
		WHERE execution_id = ? AND ended_at IS NULL`, executionID)
	return err
}

// EndCredentialGrant ends one grant early.
func (s *Store) EndCredentialGrant(ctx context.Context, id string) error {
	result, err := s.db.ExecContext(ctx, `UPDATE credential_grants SET ended_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
		WHERE id = ? AND ended_at IS NULL`, id)
	if err != nil {
		return err
	}
	return expectOneRow(result)
}

// ListLiveCredentialGrants lists every live grant, for the person to see.
func (s *Store) ListLiveCredentialGrants(ctx context.Context, now time.Time) ([]CredentialGrant, error) {
	grants := []CredentialGrant{}
	err := s.db.SelectContext(ctx, &grants, `
		SELECT * FROM credential_grants
		WHERE ended_at IS NULL AND (expires_at IS NULL OR expires_at > ?) AND (uses_left IS NULL OR uses_left > 0)
		ORDER BY created_at, rowid`, now.UTC().Format(time.RFC3339Nano))
	return grants, err
}

// CredentialAuditEntry is one row of the credential audit log.
type CredentialAuditEntry struct {
	Seq         int64  `db:"seq" json:"seq"`
	At          string `db:"at" json:"at"`
	Event       string `db:"event" json:"event"`
	ExecutionID string `db:"execution_id" json:"execution_id,omitempty"`
	TaskID      string `db:"task_id" json:"task_id,omitempty"`
	AgentKind   string `db:"agent_kind" json:"agent_kind,omitempty"`
	WorkspaceID string `db:"workspace_id" json:"workspace_id,omitempty"`
	Credential  string `db:"credential" json:"credential,omitempty"`
	Operation   string `db:"operation" json:"operation,omitempty"`
	Resource    string `db:"resource" json:"resource,omitempty"`
	Amount      string `db:"amount" json:"amount,omitempty"`
	ApprovalID  string `db:"approval_id" json:"approval_id,omitempty"`
	DecidedBy   string `db:"decided_by" json:"decided_by,omitempty"`
	Detail      string `db:"detail" json:"detail,omitempty"`
	PrevHash    string `db:"prev_hash" json:"prev_hash"`
	Hash        string `db:"hash" json:"hash"`
}

// auditHash is the SHA-256 of the previous hash and the entry's canonical
// JSON (every field but the hashes and the sequence number).
func auditHash(prev string, e CredentialAuditEntry) string {
	body, _ := json.Marshal([]string{e.At, e.Event, e.ExecutionID, e.TaskID, e.AgentKind, e.WorkspaceID, e.Credential, e.Operation, e.Resource, e.Amount, e.ApprovalID, e.DecidedBy, e.Detail})
	sum := sha256.Sum256(append([]byte(prev+"\n"), body...))
	return hex.EncodeToString(sum[:])
}

// AppendCredentialAudit adds a row at the end of the chain. The transaction
// takes the write lock first, so appends from several processes keep one chain.
func (s *Store) AppendCredentialAudit(ctx context.Context, e CredentialAuditEntry) (*CredentialAuditEntry, error) {
	tx, err := s.db.BeginTxx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var prev string
	if err := tx.GetContext(ctx, &prev, "SELECT hash FROM credential_audit ORDER BY seq DESC LIMIT 1"); err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	if e.At == "" {
		e.At = time.Now().UTC().Format("2006-01-02T15:04:05.000Z")
	}
	e.PrevHash = prev
	e.Hash = auditHash(prev, e)
	result, err := tx.NamedExecContext(ctx, `
		INSERT INTO credential_audit
			(at, event, execution_id, task_id, agent_kind, workspace_id, credential, operation, resource, amount, approval_id, decided_by, detail, prev_hash, hash)
		VALUES
			(:at, :event, :execution_id, :task_id, :agent_kind, :workspace_id, :credential, :operation, :resource, :amount, :approval_id, :decided_by, :detail, :prev_hash, :hash)`, e)
	if err != nil {
		return nil, err
	}
	if e.Seq, err = result.LastInsertId(); err != nil {
		return nil, err
	}
	return &e, tx.Commit()
}

// CredentialAuditFilter selects audit rows. Empty fields match everything.
type CredentialAuditFilter struct {
	Event       string
	Credential  string
	TaskID      string
	ExecutionID string
	Since       time.Time
	Limit       int
}

func (f CredentialAuditFilter) where() (string, []any) {
	var clauses []string
	var args []any
	for _, c := range []struct{ column, value string }{{"event", f.Event}, {"credential", f.Credential}, {"task_id", f.TaskID}, {"execution_id", f.ExecutionID}} {
		if c.value != "" {
			clauses = append(clauses, c.column+" = ?")
			args = append(args, c.value)
		}
	}
	if !f.Since.IsZero() {
		clauses = append(clauses, "at >= ?")
		args = append(args, f.Since.UTC().Format("2006-01-02T15:04:05.000Z"))
	}
	if len(clauses) == 0 {
		return "", nil
	}
	return " WHERE " + strings.Join(clauses, " AND "), args
}

// ListCredentialAudit returns audit rows, newest first.
func (s *Store) ListCredentialAudit(ctx context.Context, f CredentialAuditFilter) ([]CredentialAuditEntry, error) {
	where, args := f.where()
	query := "SELECT * FROM credential_audit" + where + " ORDER BY seq DESC"
	if f.Limit > 0 {
		query += " LIMIT ?"
		args = append(args, f.Limit)
	}
	entries := []CredentialAuditEntry{}
	err := s.db.SelectContext(ctx, &entries, query, args...)
	return entries, err
}

// CountCredentialAudit counts audit rows, for limits.
func (s *Store) CountCredentialAudit(ctx context.Context, f CredentialAuditFilter) (int, error) {
	where, args := f.where()
	var n int
	err := s.db.GetContext(ctx, &n, "SELECT COUNT(*) FROM credential_audit"+where, args...)
	return n, err
}

// VerifyCredentialAudit walks the chain and returns the sequence number of
// the first row whose hashes do not match, or 0 when the chain holds.
func (s *Store) VerifyCredentialAudit(ctx context.Context) (int64, error) {
	rows, err := s.db.QueryxContext(ctx, "SELECT * FROM credential_audit ORDER BY seq")
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	prev := ""
	for rows.Next() {
		var e CredentialAuditEntry
		if err := rows.StructScan(&e); err != nil {
			return 0, err
		}
		if e.PrevHash != prev || e.Hash != auditHash(prev, e) {
			return e.Seq, nil
		}
		prev = e.Hash
	}
	return 0, rows.Err()
}
