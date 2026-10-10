package claudebridge

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
)

// Lockfile advertises a running bridge so Legato can bind (or re-bind) a
// Claude Code agent to it. It has the shape of a Chimera instance lockfile, so
// one discovery routine verifies both.
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

// LockfileDir is where bridges advertise themselves under Legato's runtime
// directory. It is a directory of its own: Legato broadcasts IPC to every
// socket directly inside the runtime directory.
func LockfileDir(runtimeDir string) string {
	return filepath.Join(runtimeDir, "claude-bridge")
}

// LockfilePath is the lockfile of the bridge serving taskID.
func LockfilePath(runtimeDir, taskID string) string {
	return filepath.Join(LockfileDir(runtimeDir), url.PathEscape(taskID)+".json")
}

// WriteLockfile publishes a bridge for taskID, replacing any earlier one.
func WriteLockfile(runtimeDir, taskID string, lockfile Lockfile) error {
	if err := os.MkdirAll(LockfileDir(runtimeDir), 0o700); err != nil {
		return fmt.Errorf("creating bridge lockfile directory: %w", err)
	}
	data, err := json.Marshal(lockfile)
	if err != nil {
		return err
	}
	path := LockfilePath(runtimeDir, taskID)
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
	data, err := os.ReadFile(LockfilePath(runtimeDir, taskID))
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
	os.Remove(LockfilePath(runtimeDir, taskID))
}
