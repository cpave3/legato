package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
)

// ReevePeer is another instance whose Reeve this one can message.
type ReevePeer struct {
	InstanceID    string `db:"instance_id" json:"instance_id"`
	Name          string `db:"name" json:"name"`
	Via           string `db:"via" json:"via"` // pairing, worker, prime
	Address       string `db:"address" json:"address,omitempty"`
	CACertificate string `db:"ca_certificate" json:"-"`
	TokenSecret   string `db:"token_secret" json:"-"`
	ReplyGiven    bool   `db:"reply_given" json:"-"`
	CreatedAt     string `db:"created_at" json:"created_at"`
	UpdatedAt     string `db:"updated_at" json:"updated_at"`
}

// SaveReevePeer keeps a peer, replacing what was known of it.
func (s *Store) SaveReevePeer(ctx context.Context, p ReevePeer) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO reeve_peers (instance_id, name, via, address, ca_certificate, token_secret, reply_given)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(instance_id) DO UPDATE SET name = excluded.name, via = excluded.via, address = excluded.address,
			ca_certificate = excluded.ca_certificate, token_secret = excluded.token_secret, reply_given = excluded.reply_given,
			updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')`,
		p.InstanceID, p.Name, p.Via, p.Address, p.CACertificate, p.TokenSecret, p.ReplyGiven)
	return err
}

// GetReevePeer reads one peer; ErrNotFound when there is none.
func (s *Store) GetReevePeer(ctx context.Context, instanceID string) (*ReevePeer, error) {
	var p ReevePeer
	err := s.db.GetContext(ctx, &p, `SELECT * FROM reeve_peers WHERE instance_id = ?`, instanceID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return &p, err
}

// ListReevePeers lists the peers, by name.
func (s *Store) ListReevePeers(ctx context.Context) ([]ReevePeer, error) {
	peers := []ReevePeer{}
	err := s.db.SelectContext(ctx, &peers, `SELECT * FROM reeve_peers ORDER BY name, instance_id`)
	return peers, err
}

// DeleteReevePeer forgets a peer; none is ErrNotFound.
func (s *Store) DeleteReevePeer(ctx context.Context, instanceID string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM reeve_peers WHERE instance_id = ?`, instanceID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	_, _ = s.db.ExecContext(ctx, `DELETE FROM reeve_outbox WHERE instance_id = ?`, instanceID)
	return nil
}

// QueueReeveReply keeps a reply for a peer that could not be reached.
func (s *Store) QueueReeveReply(ctx context.Context, instanceID, text string) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO reeve_outbox (instance_id, text) VALUES (?, ?)`, instanceID, text)
	return err
}

// TakeReeveReplies removes and returns a peer's queued replies, oldest
// first.
func (s *Store) TakeReeveReplies(ctx context.Context, instanceID string) ([]string, error) {
	tx, err := s.db.BeginTxx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	texts := []string{}
	if err := tx.SelectContext(ctx, &texts, `SELECT text FROM reeve_outbox WHERE instance_id = ? ORDER BY id`, instanceID); err != nil {
		return nil, err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM reeve_outbox WHERE instance_id = ?`, instanceID); err != nil {
		return nil, err
	}
	return texts, tx.Commit()
}

// ReeveInboxMessage is a peer's message waiting for the person.
type ReeveInboxMessage struct {
	ID         int64  `db:"id" json:"id"`
	InstanceID string `db:"instance_id" json:"instance_id"`
	Name       string `db:"name" json:"name"`
	Role       string `db:"role" json:"role"`
	Text       string `db:"text" json:"text"`
	CreatedAt  string `db:"created_at" json:"created_at"`
}

// InsertReeveInbox keeps a peer's message for the person.
func (s *Store) InsertReeveInbox(ctx context.Context, m ReeveInboxMessage) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO reeve_inbox (instance_id, name, role, text) VALUES (?, ?, ?, ?)`, m.InstanceID, m.Name, m.Role, m.Text)
	return err
}

// ListReeveInbox lists the messages waiting, oldest first.
func (s *Store) ListReeveInbox(ctx context.Context) ([]ReeveInboxMessage, error) {
	rows := []ReeveInboxMessage{}
	err := s.db.SelectContext(ctx, &rows, `SELECT * FROM reeve_inbox ORDER BY id`)
	return rows, err
}

// TakeReeveInbox removes and returns one peer's waiting messages.
func (s *Store) TakeReeveInbox(ctx context.Context, instanceID string) ([]ReeveInboxMessage, error) {
	rows := []ReeveInboxMessage{}
	if err := s.db.SelectContext(ctx, &rows, `SELECT * FROM reeve_inbox WHERE instance_id = ? ORDER BY id`, instanceID); err != nil {
		return nil, err
	}
	_, err := s.db.ExecContext(ctx, `DELETE FROM reeve_inbox WHERE instance_id = ?`, instanceID)
	return rows, err
}

// SetIntegrationTokenScopes replaces the scopes of a main instance's token
// (the reeve scope on or off).
func (s *Store) SetMainIntegrationTokenScopes(ctx context.Context, instanceID string, scopes []string) error {
	raw, _ := json.Marshal(scopes)
	res, err := s.db.ExecContext(ctx, `UPDATE integration_tokens SET scopes = ? WHERE kind = 'main' AND instance_id = ? AND revoked_at IS NULL`, string(raw), instanceID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// RevokeReevePeerTokens revokes the reeve_peer tokens given to an instance.
func (s *Store) RevokeReevePeerTokens(ctx context.Context, instanceID string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE integration_tokens SET revoked_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now') WHERE kind = 'reeve_peer' AND instance_id = ? AND revoked_at IS NULL`, instanceID)
	return err
}
