package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"

	"github.com/jmoiron/sqlx"
)

// HandoffBoundary is the persisted review state to which a packet is bound.
type HandoffBoundary struct {
	TourID      string `db:"tour_id"`
	TourStatus  string `db:"tour_status"`
	PassID      string `db:"pass_id"`
	PassStatus  string `db:"pass_status"`
	BaseSHA     string `db:"base_sha"`
	TourHeadSHA string `db:"tour_head_sha"`
	PassHeadSHA string `db:"pass_head_sha"`
	Fingerprint string `db:"-"`
}

type handoffBoundaryChapter struct {
	ID        string                `db:"id" json:"id"`
	Title     string                `db:"title" json:"title"`
	Narration string                `db:"narration" json:"narration"`
	Risk      string                `db:"risk" json:"risk"`
	OrderHint *int                  `db:"order_hint" json:"order_hint,omitempty"`
	Seq       int                   `db:"seq" json:"seq"`
	Hunks     []handoffBoundaryHunk `db:"-" json:"hunks"`
}

type handoffBoundaryHunk struct {
	Path      string `db:"file_path" json:"path"`
	Anchor    string `db:"hunk_anchor" json:"anchor"`
	Seq       int    `db:"seq" json:"seq"`
	Generated bool   `db:"generated" json:"generated"`
}

// HandoffPacketRecord is the store projection for immutable packet content.
type HandoffPacketRecord struct {
	ID                  string `db:"id"`
	TourID              string `db:"tour_id"`
	Revision            int    `db:"revision"`
	PassID              string `db:"pass_id"`
	BaseSHA             string `db:"base_sha"`
	HeadSHA             string `db:"head_sha"`
	Repository          string `db:"repository"`
	BoundaryFingerprint string `db:"boundary_fingerprint"`
	PacketJSON          string `db:"packet_json"`
	Outdated            bool   `db:"outdated"`
	OutdatedReason      string `db:"outdated_reason"`
	CreatedAt           string `db:"created_at"`
	NoteRevision        int    `db:"note_revision"`
	NoteJSON            string `db:"note_json"`
}

type HandoffObservation struct {
	TourID       string `db:"tour_id"`
	CheckedAt    string `db:"checked_at"`
	ObservedBase string `db:"observed_base"`
	ObservedHead string `db:"observed_head"`
	Clean        bool   `db:"clean"`
}

func (s *Store) ReviewHandoffBoundary(ctx context.Context, tourID string) (*HandoffBoundary, error) {
	var boundary *HandoffBoundary
	err := withTx(ctx, s.db, func(tx *sqlx.Tx) error {
		var err error
		boundary, err = reviewHandoffBoundary(ctx, tx, tourID)
		return err
	})
	return boundary, err
}

func reviewHandoffBoundary(ctx context.Context, q sqlx.QueryerContext, tourID string) (*HandoffBoundary, error) {
	var b HandoffBoundary
	err := sqlx.GetContext(ctx, q, &b, `SELECT t.id AS tour_id, t.status AS tour_status,
		p.id AS pass_id, p.status AS pass_status, t.base_sha, t.head_sha AS tour_head_sha,
		p.head_sha AS pass_head_sha
		FROM review_tours t JOIN review_passes p ON p.tour_id = t.id
		WHERE t.id = ? ORDER BY p.number DESC LIMIT 1`, tourID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	var chapters []handoffBoundaryChapter
	if err := sqlx.SelectContext(ctx, q, &chapters, `SELECT id, title, narration, risk, order_hint, seq
		FROM review_steps WHERE pass_id = ? AND kind = 'chapter'
		ORDER BY (order_hint IS NULL), order_hint, seq, id`, b.PassID); err != nil {
		return nil, err
	}
	for i := range chapters {
		if err := sqlx.SelectContext(ctx, q, &chapters[i].Hunks, `SELECT file_path, hunk_anchor, seq, generated
			FROM review_chapter_hunks WHERE step_id = ? ORDER BY seq, id`, chapters[i].ID); err != nil {
			return nil, err
		}
		if chapters[i].Hunks == nil {
			chapters[i].Hunks = []handoffBoundaryHunk{}
		}
	}
	raw, err := json.Marshal(chapters)
	if err != nil {
		return nil, err
	}
	digest := sha256.Sum256(raw)
	b.Fingerprint = hex.EncodeToString(digest[:])
	return &b, nil
}

// InsertHandoffPacket performs the latest-revision and review-boundary checks in
// the same transaction as publication.
func (s *Store) InsertHandoffPacket(ctx context.Context, record HandoffPacketRecord, expectedRevision int, expected HandoffBoundary) error {
	return withTx(ctx, s.db, func(tx *sqlx.Tx) error {
		current, err := reviewHandoffBoundary(ctx, tx, record.TourID)
		if err != nil {
			return err
		}
		if *current != expected || current.TourStatus != "ready" || current.PassStatus != "ready" {
			return ErrConflict
		}
		var latest int
		if err := tx.GetContext(ctx, &latest, `SELECT COALESCE(MAX(revision), 0) FROM handoff_packets WHERE tour_id = ?`, record.TourID); err != nil {
			return err
		}
		if latest != expectedRevision || record.Revision != latest+1 {
			return ErrConflict
		}
		_, err = tx.NamedExecContext(ctx, `INSERT INTO handoff_packets
			(id, tour_id, revision, pass_id, base_sha, head_sha, repository, boundary_fingerprint, packet_json)
			VALUES (:id, :tour_id, :revision, :pass_id, :base_sha, :head_sha, :repository, :boundary_fingerprint, :packet_json)`, record)
		if err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO handoff_notes(packet_id) VALUES (?)`, record.ID); err != nil {
			return err
		}
		return observeHandoff(ctx, tx, HandoffObservation{TourID: record.TourID, CheckedAt: time.Now().UTC().Format(time.RFC3339Nano), ObservedBase: record.BaseSHA, ObservedHead: record.HeadSHA, Clean: true})
	})
}

func (s *Store) ListHandoffPackets(ctx context.Context, tourID string) ([]HandoffPacketRecord, error) {
	if _, err := s.GetReviewTour(ctx, tourID); err != nil {
		return nil, err
	}
	packets := []HandoffPacketRecord{}
	err := s.db.SelectContext(ctx, &packets, `SELECT p.*, n.revision AS note_revision, n.note_json
		FROM handoff_packets p JOIN handoff_notes n ON n.packet_id = p.id
		WHERE p.tour_id = ? ORDER BY p.revision DESC`, tourID)
	return packets, err
}

func (s *Store) GetHandoffPacket(ctx context.Context, tourID, packetID string) (*HandoffPacketRecord, error) {
	var packet HandoffPacketRecord
	err := s.db.GetContext(ctx, &packet, `SELECT p.*, n.revision AS note_revision, n.note_json
		FROM handoff_packets p JOIN handoff_notes n ON n.packet_id = p.id
		WHERE p.tour_id = ? AND p.id = ?`, tourID, packetID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return &packet, err
}

func (s *Store) ReplaceHandoffNote(ctx context.Context, tourID, packetID string, expectedRevision int, noteJSON string) (int, error) {
	var next int
	err := withTx(ctx, s.db, func(tx *sqlx.Tx) error {
		var current int
		if err := tx.GetContext(ctx, &current, `SELECT n.revision FROM handoff_notes n
			JOIN handoff_packets p ON p.id = n.packet_id WHERE p.tour_id = ? AND p.id = ?`, tourID, packetID); errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		} else if err != nil {
			return err
		}
		if current != expectedRevision {
			return ErrConflict
		}
		next = current + 1
		result, err := tx.ExecContext(ctx, `UPDATE handoff_notes SET revision = ?, note_json = ? WHERE packet_id = ? AND revision = ?`, next, noteJSON, packetID, current)
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
		return nil
	})
	return next, err
}

func (s *Store) HandoffObservation(ctx context.Context, tourID string) (*HandoffObservation, error) {
	var observation HandoffObservation
	err := s.db.GetContext(ctx, &observation, `SELECT * FROM handoff_observations WHERE tour_id = ?`, tourID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return &observation, err
}

// ObserveHandoff records an explicit successful Git observation and invalidates
// snapshots that no longer match it.
func (s *Store) ObserveHandoff(ctx context.Context, observation HandoffObservation) error {
	return withTx(ctx, s.db, func(tx *sqlx.Tx) error { return observeHandoff(ctx, tx, observation) })
}

func observeHandoff(ctx context.Context, tx *sqlx.Tx, observation HandoffObservation) error {
	if _, err := tx.NamedExecContext(ctx, `INSERT INTO handoff_observations
			(tour_id, checked_at, observed_base, observed_head, clean) VALUES
			(:tour_id, :checked_at, :observed_base, :observed_head, :clean)
			ON CONFLICT(tour_id) DO UPDATE SET checked_at = excluded.checked_at,
			observed_base = excluded.observed_base, observed_head = excluded.observed_head, clean = excluded.clean`, observation); err != nil {
		return err
	}
	reason := "repository changed since this handoff snapshot"
	if !observation.Clean {
		reason = "repository has uncommitted changes"
	}
	_, err := tx.ExecContext(ctx, `UPDATE handoff_packets SET outdated = 1, outdated_reason = ?
			WHERE tour_id = ? AND (base_sha <> ? OR head_sha <> ? OR ? = 0)`, reason,
		observation.TourID, observation.ObservedBase, observation.ObservedHead, observation.Clean)
	return err
}

func markHandoffsOutdated(ctx context.Context, tx *sqlx.Tx, tourID, reason string) error {
	_, err := tx.ExecContext(ctx, `UPDATE handoff_packets SET outdated = 1, outdated_reason = ? WHERE tour_id = ? AND outdated = 0`, reason, tourID)
	return err
}

func (s *Store) HasHandoffPackets(ctx context.Context, tourID string) (bool, error) {
	var exists bool
	err := s.db.GetContext(ctx, &exists, "SELECT EXISTS(SELECT 1 FROM handoff_packets WHERE tour_id = ?)", tourID)
	return exists, err
}
