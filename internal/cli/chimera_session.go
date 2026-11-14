package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"

	"github.com/cpave3/legato/internal/engine/store"
	"github.com/cpave3/legato/internal/service"
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
	return AgentSessionCreatedFromHookFor(s, taskID, "", sessionID, reader)
}

// AgentSessionCreatedFromHookFor is AgentSessionCreatedFromHook for the
// session of executionID: the session is kept in that agent's slot, and is
// its own when it carries that slot's name ("" means the main slot's).
func AgentSessionCreatedFromHookFor(s *store.Store, taskID, executionID, sessionID string, reader io.Reader) (bool, error) {
	if session, err := s.GetRunningAgentSession(context.Background(), taskID, executionID); err == nil && session.HookSource == store.HookSourceMod {
		return false, nil
	}
	slot := callerSlot(s, taskID, executionID)
	register, err := SessionCreatedPayloadNamesTask(service.AgentSlotKey(taskID, slot), sessionID, reader)
	if err != nil || !register {
		return false, err
	}
	if err := s.SetChimeraResumeInSlot(context.Background(), taskID, slot, sessionID); err != nil {
		return false, fmt.Errorf("linking Chimera session: %w", err)
	}
	return true, nil
}

// AgentSessionCreatedFor links a Chimera session to the slot of the agent of
// executionID ("" for the main slot).
func AgentSessionCreatedFor(s *store.Store, taskID, executionID, sessionID string) error {
	if taskID == "" {
		return fmt.Errorf("task ID is required")
	}
	if sessionID == "" {
		return fmt.Errorf("session ID is required")
	}
	if session, err := s.GetRunningAgentSession(context.Background(), taskID, executionID); err == nil && session.HookSource == store.HookSourceMod {
		return nil
	}
	if err := s.SetChimeraResumeInSlot(context.Background(), taskID, callerSlot(s, taskID, executionID), sessionID); err != nil {
		return fmt.Errorf("linking Chimera session: %w", err)
	}
	return nil
}

// callerSlot is the slot of the running agent of executionID on the task,
// the main slot when there is none.
func callerSlot(s *store.Store, taskID, executionID string) string {
	if executionID == "" {
		return store.SlotMain
	}
	session, err := s.GetRunningAgentSession(context.Background(), taskID, executionID)
	if err != nil || session.ExecutionID != executionID {
		return store.SlotMain
	}
	return session.Slot
}

// SessionCreatedPayloadNamesTask checks a Chimera SessionCreated payload and
// reports whether it is the task's own parent session, which is the one to
// link (a child or differently named session is not).
func SessionCreatedPayloadNamesTask(taskID, sessionID string, reader io.Reader) (bool, error) {
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
	return payload.SessionName == taskID, nil
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
