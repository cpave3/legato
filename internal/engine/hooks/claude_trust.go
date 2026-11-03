package hooks

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// TrustWorkDir marks dir as trusted in Claude Code's configuration file, so
// a session started there does not stop at the "trust this folder?" dialog.
// It writes what accepting the dialog writes:
// projects["<dir>"].hasTrustDialogAccepted = true in ~/.claude.json, or in
// <CLAUDE_CONFIG_DIR>/.claude.json when env (the session's) or Legato's own
// environment sets that variable.
//
// Call it before the session starts: a sandbox mounts the file as it is
// then.
func (a *ClaudeCodeAdapter) TrustWorkDir(env map[string]string, dir string) error {
	file, err := claudeConfigFile(env)
	if err != nil {
		return err
	}
	// Claude Code keys projects by the real path, which is what getcwd gives
	// it inside a sandbox or out.
	real, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return fmt.Errorf("resolving %s: %w", dir, err)
	}
	if real, err = filepath.Abs(real); err != nil {
		return err
	}
	return trustClaudeProject(file, real)
}

// trustClaudeProject trusts dir in the configuration file, which may be a
// link (Claude Code writes through one). It does nothing when the file is
// missing: Claude Code that never ran has its onboarding to do first.
func trustClaudeProject(file, dir string) error {
	target, err := filepath.EvalSymlinks(file)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	unlock, err := lockClaudeConfig(file)
	if err != nil {
		return err
	}
	defer unlock()
	return trustInFile(target, dir)
}

// Claude Code saves its configuration under a lock (proper-lockfile): the
// folder <file>.lock, made with mkdir, whose time the holder refreshes every
// few seconds and which counts as abandoned once 10 seconds old. Claude
// re-reads the file under it before a save, so a change made under the same
// lock is never written over by a session that started earlier.
const (
	claudeLockStale = 10 * time.Second
	claudeLockWait  = 5 * time.Second
	claudeLockPoll  = 50 * time.Millisecond
)

// lockClaudeConfig takes Claude Code's lock on its configuration file,
// waiting a few seconds for a holder, and returns what releases it.
func lockClaudeConfig(file string) (func(), error) {
	lock := file + ".lock"
	deadline := time.Now().Add(claudeLockWait)
	for {
		err := os.Mkdir(lock, 0o755)
		if err == nil {
			return func() { os.Remove(lock) }, nil
		}
		if !errors.Is(err, fs.ErrExist) {
			return nil, fmt.Errorf("locking %s: %w", file, err)
		}
		if info, statErr := os.Stat(lock); statErr == nil && time.Since(info.ModTime()) > claudeLockStale {
			// Abandoned by a Claude Code that ended while saving.
			os.Remove(lock)
			continue
		}
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("%s is locked by another Claude Code", file)
		}
		time.Sleep(claudeLockPoll)
	}
}

// claudeConfigFile is the file Claude Code keeps its global configuration
// and project trust in: <CLAUDE_CONFIG_DIR>/.claude.json, else
// ~/.claude.json.
func claudeConfigFile(env map[string]string) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	dir := env["CLAUDE_CONFIG_DIR"]
	if dir == "" {
		dir = os.Getenv("CLAUDE_CONFIG_DIR")
	}
	if strings.HasPrefix(dir, "~/") {
		dir = filepath.Join(home, dir[2:])
	}
	if dir == "" {
		dir = home
	}
	return filepath.Join(dir, ".claude.json"), nil
}

func trustInFile(file, dir string) error {
	data, err := os.ReadFile(file)
	if err != nil {
		return err
	}
	var config map[string]json.RawMessage
	if err := json.Unmarshal(data, &config); err != nil {
		return fmt.Errorf("reading %s: %w", file, err)
	}
	projects := map[string]map[string]json.RawMessage{}
	if raw, ok := config["projects"]; ok {
		if err := json.Unmarshal(raw, &projects); err != nil {
			return fmt.Errorf("reading projects in %s: %w", file, err)
		}
	}
	if trustedByClaude(projects, dir) {
		return nil
	}
	project := projects[dir]
	if project == nil {
		project = map[string]json.RawMessage{}
		projects[dir] = project
	}
	project["hasTrustDialogAccepted"] = json.RawMessage("true")
	if config["projects"], err = marshalJSON(projects); err != nil {
		return err
	}
	out, err := marshalJSON(config)
	if err != nil {
		return err
	}
	return writeFileAtomic(file, out)
}

// trustedByClaude reports whether Claude Code would start in dir without
// asking: dir or a parent up to its git root is trusted, or any parent at
// all when dir is in no git repository.
func trustedByClaude(projects map[string]map[string]json.RawMessage, dir string) bool {
	root := gitRoot(dir)
	for at := dir; ; at = filepath.Dir(at) {
		if string(projects[at]["hasTrustDialogAccepted"]) == "true" {
			return true
		}
		if at == root || at == filepath.Dir(at) {
			return false
		}
	}
}

// gitRoot is the nearest folder from dir up that holds a .git, or "".
func gitRoot(dir string) string {
	for at := dir; ; at = filepath.Dir(at) {
		if _, err := os.Lstat(filepath.Join(at, ".git")); err == nil {
			return at
		}
		if at == filepath.Dir(at) {
			return ""
		}
	}
}

// marshalJSON writes v as Claude Code does (two-space indent), without
// escaping <, > and & in the strings it keeps.
func marshalJSON(v any) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return bytes.TrimRight(buf.Bytes(), "\n"), nil
}

// writeFileAtomic replaces file with data through a new file renamed over
// it, as Claude Code saves it, so no reader sees half a file. The file keeps
// its mode.
func writeFileAtomic(file string, data []byte) error {
	info, err := os.Stat(file)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(file), ".claude.json.legato-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Chmod(info.Mode().Perm()); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), file)
}
