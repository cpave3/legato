package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"

	"github.com/cpave3/legato/internal/engine/store"
)

type chimeraSessionCreatedPayload struct {
	Event       string `json:"event"`
	SessionID   string `json:"session_id"`
	SessionName string `json:"session_name"`
	Cwd         string `json:"cwd"`
}

// AgentSessionCreatedFromHook links a newly-created task parent session while
// ignoring unnamed or differently named child sessions.
func AgentSessionCreatedFromHook(s *store.Store, taskID, sessionID string, reader io.Reader) (bool, error) {
	if taskID == "" {
		return false, fmt.Errorf("task ID is required")
	}
	if sessionID == "" {
		return false, fmt.Errorf("session ID is required")
	}
	var payload chimeraSessionCreatedPayload
	decoder := json.NewDecoder(reader)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&payload); err != nil {
		return false, fmt.Errorf("decoding Chimera SessionCreated payload: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return false, fmt.Errorf("decoding Chimera SessionCreated payload: expected one JSON object")
	}
	if payload.Event != "SessionCreated" {
		return false, fmt.Errorf("expected Chimera SessionCreated payload, got event %q", payload.Event)
	}
	if payload.SessionID != sessionID {
		return false, fmt.Errorf("Chimera payload session ID %q does not match environment session ID %q", payload.SessionID, sessionID)
	}
	if payload.SessionName != taskID {
		return false, nil
	}
	if err := AgentSessionCreated(s, taskID, sessionID); err != nil {
		return false, err
	}
	return true, nil
}

// AgentSessionCreated links a newly-created Chimera session to its task.
func AgentSessionCreated(s *store.Store, taskID, sessionID string) error {
	if taskID == "" {
		return fmt.Errorf("task ID is required")
	}
	if sessionID == "" {
		return fmt.Errorf("session ID is required")
	}
	if err := s.SetTaskChimeraSessionID(context.Background(), taskID, sessionID); err != nil {
		return fmt.Errorf("linking Chimera session: %w", err)
	}
	return nil
}
