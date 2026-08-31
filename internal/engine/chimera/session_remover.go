package chimera

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
)

type SessionRemover struct {
	binary string
	run    func(context.Context, string, ...string) ([]byte, error)
}

func NewSessionRemover(binary string) *SessionRemover {
	return &SessionRemover{
		binary: binary,
		run: func(ctx context.Context, name string, args ...string) ([]byte, error) {
			return exec.CommandContext(ctx, name, args...).CombinedOutput()
		},
	}
}

func (r *SessionRemover) Remove(ctx context.Context, sessionID string) (bool, error) {
	output, err := r.run(ctx, r.binary, "sessions", "rm", sessionID, "--exact", "--if-exists", "--json")
	if err != nil {
		message := strings.TrimSpace(string(output))
		if message == "" {
			message = err.Error()
		}
		return false, fmt.Errorf("chimera sessions rm: %s", message)
	}
	var result struct {
		Removed bool `json:"removed"`
	}
	if err := json.Unmarshal(output, &result); err != nil {
		return false, fmt.Errorf("decode chimera sessions rm output: %w", err)
	}
	return result.Removed, nil
}
