package store

import (
	"context"
	"time"
)

// peerMessageTime is how a peer message's time is kept: UTC with
// milliseconds, so the text sorts as the time does.
const peerMessageTime = "2006-01-02T15:04:05.000Z"

// PeerMessageKeep is how long a peer message is kept.
const PeerMessageKeep = 24 * time.Hour

// PeerMessageLink is the peer messages one agent sent another since some
// time: how many, and when the last was sent.
type PeerMessageLink struct {
	SenderTaskID    string `db:"sender_task_id"`
	RecipientTaskID string `db:"recipient_task_id"`
	Count           int    `db:"count"`
	LastAt          string `db:"last_at"`
}

// RecordPeerMessage keeps that sender messaged recipient at at (never what
// it said), and drops messages older than PeerMessageKeep.
func (s *Store) RecordPeerMessage(ctx context.Context, sender, recipient string, at time.Time) error {
	if _, err := s.db.ExecContext(ctx, `INSERT INTO peer_messages (sender_task_id, recipient_task_id, sent_at) VALUES (?, ?, ?)`,
		sender, recipient, at.UTC().Format(peerMessageTime)); err != nil {
		return err
	}
	_, err := s.db.ExecContext(ctx, `DELETE FROM peer_messages WHERE sent_at < ?`, at.Add(-PeerMessageKeep).UTC().Format(peerMessageTime))
	return err
}

// PeerMessageLinksSince counts the peer messages sent at or after since, by
// sender and recipient, in that order.
func (s *Store) PeerMessageLinksSince(ctx context.Context, since time.Time) ([]PeerMessageLink, error) {
	out := []PeerMessageLink{}
	err := s.db.SelectContext(ctx, &out, `
		SELECT sender_task_id, recipient_task_id, COUNT(*) AS count, MAX(sent_at) AS last_at
		FROM peer_messages WHERE sent_at >= ?
		GROUP BY sender_task_id, recipient_task_id
		ORDER BY sender_task_id, recipient_task_id`, since.UTC().Format(peerMessageTime))
	return out, err
}
