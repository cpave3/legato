package store

import (
	"context"
	"database/sql"
	"errors"
)

// NativeBridgeRegistration is host-private authority for one bridge generation.
// Never include this record in agent replies or public API DTOs.
type NativeBridgeRegistration struct {
	AgentSessionID  int    `db:"agent_session_id" json:"-"`
	ExecutionID     string `db:"execution_id" json:"-"`
	PublicEndpoint  string `db:"public_endpoint" json:"-"`
	ControlEndpoint string `db:"control_endpoint" json:"-"`
	Generation      string `db:"generation" json:"-"`
}

func (s *Store) RegisterNativeBridge(ctx context.Context, r NativeBridgeRegistration) error {
	_, err := s.db.NamedExecContext(ctx, `INSERT INTO native_bridge_registrations (agent_session_id,execution_id,public_endpoint,control_endpoint,generation)
 VALUES (:agent_session_id,:execution_id,:public_endpoint,:control_endpoint,:generation)
 ON CONFLICT(agent_session_id) DO UPDATE SET execution_id=excluded.execution_id,public_endpoint=excluded.public_endpoint,control_endpoint=excluded.control_endpoint,generation=excluded.generation`, r)
	return err
}

func (s *Store) NativeBridgeRegistration(ctx context.Context, sessionID int) (NativeBridgeRegistration, error) {
	var r NativeBridgeRegistration
	err := s.db.GetContext(ctx, &r, `SELECT * FROM native_bridge_registrations WHERE agent_session_id=?`, sessionID)
	if errors.Is(err, sql.ErrNoRows) {
		err = ErrNotFound
	}
	return r, err
}

func (s *Store) RemoveNativeBridge(ctx context.Context, sessionID int, generation string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM native_bridge_registrations WHERE agent_session_id=? AND generation=?`, sessionID, generation)
	return err
}
