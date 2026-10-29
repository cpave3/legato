package store

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// Device statuses.
const (
	DevicePending = "pending"
	DeviceActive  = "active"
	DeviceRemoved = "removed"
)

// ApprovalDevice is a device that can sign approval decisions.
type ApprovalDevice struct {
	ID            string  `db:"id" json:"id"`
	Kind          string  `db:"kind" json:"kind"`
	Name          string  `db:"name" json:"name"`
	PublicKey     []byte  `db:"public_key" json:"-"`
	SecurityLevel string  `db:"security_level" json:"security_level,omitempty"`
	Status        string  `db:"status" json:"status"`
	ApprovalID    string  `db:"approval_id" json:"approval_id,omitempty"`
	CreatedAt     string  `db:"created_at" json:"created_at"`
	LastUsedAt    *string `db:"last_used_at" json:"last_used_at,omitempty"`
	RemovedAt     *string `db:"removed_at" json:"removed_at,omitempty"`
}

func (s *Store) InsertApprovalDevice(ctx context.Context, d ApprovalDevice) error {
	_, err := s.db.NamedExecContext(ctx, `
		INSERT INTO approval_devices (id, kind, name, public_key, security_level, status, approval_id)
		VALUES (:id, :kind, :name, :public_key, :security_level, :status, :approval_id)`, d)
	return err
}

func (s *Store) GetApprovalDevice(ctx context.Context, id string) (*ApprovalDevice, error) {
	var d ApprovalDevice
	if err := s.db.GetContext(ctx, &d, "SELECT * FROM approval_devices WHERE id = ?", id); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &d, nil
}

// ListApprovalDevices lists devices of a status ("" for every one), oldest
// first.
func (s *Store) ListApprovalDevices(ctx context.Context, status string) ([]ApprovalDevice, error) {
	devices := []ApprovalDevice{}
	query, args := "SELECT * FROM approval_devices", []any{}
	if status != "" {
		query += " WHERE status = ?"
		args = append(args, status)
	}
	err := s.db.SelectContext(ctx, &devices, query+" ORDER BY created_at, rowid", args...)
	return devices, err
}

// SetApprovalDeviceStatus moves a device to a status.
func (s *Store) SetApprovalDeviceStatus(ctx context.Context, id, status string) error {
	result, err := s.db.ExecContext(ctx, `UPDATE approval_devices SET status = ?,
		removed_at = CASE WHEN ? = 'removed' THEN strftime('%Y-%m-%dT%H:%M:%fZ', 'now') ELSE removed_at END
		WHERE id = ?`, status, status, id)
	if err != nil {
		return err
	}
	return expectOneRow(result)
}

// TouchApprovalDevice records a device's use.
func (s *Store) TouchApprovalDevice(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE approval_devices SET last_used_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now') WHERE id = ?`, id)
	return err
}

// RemoveAllApprovalDevices removes every device that is not removed yet,
// and returns how many.
func (s *Store) RemoveAllApprovalDevices(ctx context.Context) (int64, error) {
	result, err := s.db.ExecContext(ctx, `UPDATE approval_devices SET status = 'removed', removed_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now') WHERE status != 'removed'`)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

// ApprovalChallenge is a one-time challenge, by its hash.
type ApprovalChallenge struct {
	Hash       string    `db:"hash"`
	Purpose    string    `db:"purpose"`
	ApprovalID string    `db:"approval_id"`
	DeviceID   string    `db:"device_id"`
	ExpiresAt  time.Time `db:"-"`
	Expires    string    `db:"expires_at"`
	UsedAt     *string   `db:"used_at"`
}

const challengeTime = "2006-01-02T15:04:05.000Z"

func (s *Store) CreateApprovalChallenge(ctx context.Context, c ApprovalChallenge) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO approval_challenges (hash, purpose, approval_id, device_id, expires_at) VALUES (?, ?, ?, ?, ?)`,
		c.Hash, c.Purpose, c.ApprovalID, c.DeviceID, c.ExpiresAt.UTC().Format(challengeTime))
	return err
}

// SpendApprovalChallenge marks a challenge used and returns it. A used,
// expired or unknown one is ErrNotFound.
func (s *Store) SpendApprovalChallenge(ctx context.Context, hash string, now time.Time) (*ApprovalChallenge, error) {
	at := now.UTC().Format(challengeTime)
	result, err := s.db.ExecContext(ctx, `UPDATE approval_challenges SET used_at = ? WHERE hash = ? AND used_at IS NULL AND expires_at > ?`, at, hash, at)
	if err != nil {
		return nil, err
	}
	if n, _ := result.RowsAffected(); n != 1 {
		return nil, ErrNotFound
	}
	var c ApprovalChallenge
	if err := s.db.GetContext(ctx, &c, "SELECT * FROM approval_challenges WHERE hash = ?", hash); err != nil {
		return nil, err
	}
	c.ExpiresAt, _ = time.Parse(challengeTime, c.Expires)
	return &c, nil
}

// InstanceValue returns a value made once for this Legato: the stored one,
// or make()'s, stored now.
func (s *Store) InstanceValue(ctx context.Context, key string, make func() string) (string, error) {
	if _, err := s.db.ExecContext(ctx, `INSERT INTO instance_values (key, value) VALUES (?, ?) ON CONFLICT(key) DO NOTHING`, key, make()); err != nil {
		return "", err
	}
	var value string
	err := s.db.GetContext(ctx, &value, "SELECT value FROM instance_values WHERE key = ?", key)
	return value, err
}
