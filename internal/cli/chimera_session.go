package cli

import (
	"context"
	"fmt"

	"github.com/cpave3/legato/internal/engine/store"
)

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
