package codexbridge

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
)

// Lockfile advertises a running bridge so Legato can bind (or re-bind) a
// Codex agent to it. It has the shape of a Chimera instance lockfile, so one
// discovery routine verifies both.
type Lockfile struct {
	PID       int    `json:"pid"`
	Port      int    `json:"port"`
	CWD       string `json:"cwd"`
	SessionID string `json:"sessionId"`
	StartedAt int64  `json:"startedAt"`
	Version   string `json:"version"`
	// URL is the bridge's endpoint including its secret path.
	URL string `json:"url"`
}

// Version names this bridge in its lockfile and in GET /v1/instance.
const Version = protocolVersion

// StateDir is where a session's bridge keeps what it shares with the rest of
// Legato, under Legato's runtime directory: its lockfile, the app server's
// token and its log. It is a directory of its own: Legato broadcasts IPC to
// every socket directly inside the runtime directory.
func StateDir(runtimeDir string) string {
	return filepath.Join(runtimeDir, "codex-bridge")
}

// StatePath is the file of taskID's bridge with the given suffix (".json"
// for its lockfile).
func StatePath(runtimeDir, taskID, suffix string) string {
	return filepath.Join(StateDir(runtimeDir), url.PathEscape(taskID)+suffix)
}

// WriteLockfile publishes a bridge for taskID, replacing any earlier one.
func WriteLockfile(runtimeDir, taskID string, lockfile Lockfile) error {
	if err := os.MkdirAll(StateDir(runtimeDir), 0o700); err != nil {
		return fmt.Errorf("creating bridge lockfile directory: %w", err)
	}
	data, err := json.Marshal(lockfile)
	if err != nil {
		return err
	}
	path := StatePath(runtimeDir, taskID, ".json")
	temporary := fmt.Sprintf("%s.%d.tmp", path, os.Getpid())
	if err := os.WriteFile(temporary, data, 0o600); err != nil {
		return fmt.Errorf("writing bridge lockfile: %w", err)
	}
	if err := os.Rename(temporary, path); err != nil {
		os.Remove(temporary)
		return fmt.Errorf("publishing bridge lockfile: %w", err)
	}
	return nil
}

// ReadLockfile returns the bridge advertised for taskID. The error wraps
// fs.ErrNotExist when none is.
func ReadLockfile(runtimeDir, taskID string) (Lockfile, error) {
	var lockfile Lockfile
	data, err := os.ReadFile(StatePath(runtimeDir, taskID, ".json"))
	if err != nil {
		return lockfile, err
	}
	if err := json.Unmarshal(data, &lockfile); err != nil {
		return lockfile, fmt.Errorf("parsing bridge lockfile: %w", err)
	}
	return lockfile, nil
}

// RemoveLockfile withdraws taskID's lockfile if it still advertises
// endpointURL. A bridge that was replaced leaves its successor's in place.
func RemoveLockfile(runtimeDir, taskID, endpointURL string) {
	lockfile, err := ReadLockfile(runtimeDir, taskID)
	if err != nil || lockfile.URL != endpointURL {
		return
	}
	os.Remove(StatePath(runtimeDir, taskID, ".json"))
}
