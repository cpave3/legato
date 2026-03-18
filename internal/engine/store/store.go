package store

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"os"
	"path/filepath"

	"github.com/jmoiron/sqlx"
	_ "modernc.org/sqlite"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

type Store struct {
	db *sqlx.DB
}

func New(dbPath string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(dbPath), 0700); err != nil {
		return nil, fmt.Errorf("creating db directory: %w", err)
	}

	db, err := sqlx.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("opening database: %w", err)
	}

	if _, err := db.Exec("PRAGMA journal_mode=WAL"); err != nil {
		db.Close()
		return nil, fmt.Errorf("enabling WAL: %w", err)
	}
	if _, err := db.Exec("PRAGMA foreign_keys=ON"); err != nil {
		db.Close()
		return nil, fmt.Errorf("enabling foreign keys: %w", err)
	}

	s := &Store{db: db}
	if err := s.migrate(); err != nil {
		db.Close()
		return nil, fmt.Errorf("running migrations: %w", err)
	}

	return s, nil
}

func (s *Store) Close() error {
	return s.db.Close()
}

func (s *Store) migrate() error {
	var version int
	if err := s.db.Get(&version, "PRAGMA user_version"); err != nil {
		return err
	}

	migrations := []string{"001_init.sql", "002_stale_and_move_tracking.sql", "003_rename_jira_to_remote.sql", "004_agent_sessions.sql"}

	for i := version; i < len(migrations); i++ {
		data, err := migrationsFS.ReadFile("migrations/" + migrations[i])
		if err != nil {
			return fmt.Errorf("reading migration %s: %w", migrations[i], err)
		}

		tx, err := s.db.Begin()
		if err != nil {
			return err
		}

		if _, err := tx.Exec(string(data)); err != nil {
			tx.Rollback()
			return fmt.Errorf("executing migration %s: %w", migrations[i], err)
		}

		if _, err := tx.Exec(fmt.Sprintf("PRAGMA user_version = %d", i+1)); err != nil {
			tx.Rollback()
			return err
		}

		if err := tx.Commit(); err != nil {
			return err
		}
	}

	return nil
}

// Ticket CRUD

func (s *Store) CreateTicket(ctx context.Context, t Ticket) error {
	_, err := s.db.NamedExecContext(ctx, `
		INSERT INTO tickets (id, summary, description, description_md, status, remote_status,
			priority, issue_type, assignee, labels, epic_key, epic_name, url,
			created_at, updated_at, remote_updated_at, sort_order, stale_at, local_move_at)
		VALUES (:id, :summary, :description, :description_md, :status, :remote_status,
			:priority, :issue_type, :assignee, :labels, :epic_key, :epic_name, :url,
			:created_at, :updated_at, :remote_updated_at, :sort_order, :stale_at, :local_move_at)`, t)
	return err
}

func (s *Store) GetTicket(ctx context.Context, id string) (*Ticket, error) {
	var t Ticket
	err := s.db.GetContext(ctx, &t, "SELECT * FROM tickets WHERE id = ?", id)
	if err == sql.ErrNoRows {
		return nil, ErrNotFound
	}
	return &t, err
}

// ErrNotFound is returned when a record is not found.
var ErrNotFound = fmt.Errorf("not found")

func (s *Store) ListTicketsByStatus(ctx context.Context, status string) ([]Ticket, error) {
	var tickets []Ticket
	err := s.db.SelectContext(ctx, &tickets,
		"SELECT * FROM tickets WHERE status = ? ORDER BY sort_order ASC", status)
	return tickets, err
}

func (s *Store) UpdateTicket(ctx context.Context, t Ticket) error {
	_, err := s.db.NamedExecContext(ctx, `
		UPDATE tickets SET
			summary = :summary, description = :description, description_md = :description_md,
			status = :status, remote_status = :remote_status, priority = :priority,
			issue_type = :issue_type, assignee = :assignee, labels = :labels,
			epic_key = :epic_key, epic_name = :epic_name, url = :url,
			updated_at = :updated_at, remote_updated_at = :remote_updated_at, sort_order = :sort_order,
			stale_at = :stale_at, local_move_at = :local_move_at
		WHERE id = :id`, t)
	return err
}

func (s *Store) DeleteTicket(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, "DELETE FROM tickets WHERE id = ?", id)
	return err
}

// UpsertTicket inserts a ticket or updates it if it already exists.
func (s *Store) UpsertTicket(ctx context.Context, t Ticket) error {
	_, err := s.db.NamedExecContext(ctx, `
		INSERT INTO tickets (id, summary, description, description_md, status, remote_status,
			priority, issue_type, assignee, labels, epic_key, epic_name, url,
			created_at, updated_at, remote_updated_at, sort_order, stale_at, local_move_at)
		VALUES (:id, :summary, :description, :description_md, :status, :remote_status,
			:priority, :issue_type, :assignee, :labels, :epic_key, :epic_name, :url,
			:created_at, :updated_at, :remote_updated_at, :sort_order, :stale_at, :local_move_at)
		ON CONFLICT(id) DO UPDATE SET
			summary = :summary, description = :description, description_md = :description_md,
			status = :status, remote_status = :remote_status, priority = :priority,
			issue_type = :issue_type, assignee = :assignee, labels = :labels,
			epic_key = :epic_key, epic_name = :epic_name, url = :url,
			updated_at = :updated_at, remote_updated_at = :remote_updated_at, sort_order = :sort_order,
			stale_at = :stale_at, local_move_at = :local_move_at`, t)
	return err
}

// ListAllTickets returns all tickets in the store.
func (s *Store) ListAllTickets(ctx context.Context) ([]Ticket, error) {
	var tickets []Ticket
	err := s.db.SelectContext(ctx, &tickets, "SELECT * FROM tickets ORDER BY id")
	return tickets, err
}

// ListTicketIDs returns all ticket IDs in the store.
func (s *Store) ListTicketIDs(ctx context.Context) ([]string, error) {
	var ids []string
	err := s.db.SelectContext(ctx, &ids, "SELECT id FROM tickets ORDER BY id")
	return ids, err
}

// Column Mapping CRUD

func (s *Store) CreateColumnMapping(ctx context.Context, m ColumnMapping) error {
	_, err := s.db.NamedExecContext(ctx, `
		INSERT INTO column_mappings (column_name, remote_statuses, remote_transition, sort_order)
		VALUES (:column_name, :remote_statuses, :remote_transition, :sort_order)`, m)
	return err
}

func (s *Store) ListColumnMappings(ctx context.Context) ([]ColumnMapping, error) {
	var mappings []ColumnMapping
	err := s.db.SelectContext(ctx, &mappings,
		"SELECT * FROM column_mappings ORDER BY sort_order ASC")
	return mappings, err
}

func (s *Store) UpdateColumnMapping(ctx context.Context, m ColumnMapping) error {
	_, err := s.db.NamedExecContext(ctx, `
		UPDATE column_mappings SET
			column_name = :column_name, remote_statuses = :remote_statuses,
			remote_transition = :remote_transition, sort_order = :sort_order
		WHERE id = :id`, m)
	return err
}

func (s *Store) DeleteColumnMapping(ctx context.Context, id int) error {
	_, err := s.db.ExecContext(ctx, "DELETE FROM column_mappings WHERE id = ?", id)
	return err
}

// Sync Log

func (s *Store) InsertSyncLog(ctx context.Context, entry SyncLogEntry) error {
	_, err := s.db.NamedExecContext(ctx, `
		INSERT INTO sync_log (ticket_id, action, detail)
		VALUES (:ticket_id, :action, :detail)`, entry)
	return err
}

func (s *Store) ListSyncLogs(ctx context.Context, ticketID string) ([]SyncLogEntry, error) {
	var entries []SyncLogEntry
	err := s.db.SelectContext(ctx, &entries,
		"SELECT * FROM sync_log WHERE ticket_id = ? ORDER BY created_at DESC, id DESC", ticketID)
	return entries, err
}
