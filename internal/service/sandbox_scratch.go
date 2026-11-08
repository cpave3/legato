package service

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strconv"

	"github.com/cpave3/legato/internal/engine/store"
)

// SandboxScratch is where sandboxes keep what is theirs alone, by execution:
// /tmp, the home folder and a throwaway checkout's layer on disk under Root,
// and the small files Legato writes for a spawn (bwrap.args, the ssh
// copies, gh-real) under RuntimeDir. Both are removed when the execution
// ends. Scratch is on disk, not in the runtime folder's tmpfs, which holds
// it in memory, shared by every sandbox, until something deletes it.
type SandboxScratch struct {
	Root       string // <scratch_dir>: <execution>/{tmp,home,overlay}
	RuntimeDir string // <runtime>/legato: sandbox/<execution>
}

// scratchFor is the scratch of a plan's input: its ScratchDir, else the
// data folder's sandbox folder, else (no data folder) the runtime folder's.
func scratchFor(in SandboxPlanInput) SandboxScratch {
	root := in.ScratchDir
	switch {
	case root != "":
	case in.DataDir != "":
		root = filepath.Join(in.DataDir, "sandbox")
	case in.RuntimeDir != "":
		root = filepath.Join(in.RuntimeDir, "sandbox")
	}
	return SandboxScratch{Root: root, RuntimeDir: in.RuntimeDir}
}

// executionFolder is what an execution ID must look like to name a folder:
// one path element, never "." or "..".
var executionFolder = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]*$`)

// Dir is the execution's scratch folder on disk.
func (s SandboxScratch) Dir(executionID string) string {
	return filepath.Join(s.Root, executionID)
}

// Tmp is the folder the execution's sandbox sees as /tmp.
func (s SandboxScratch) Tmp(executionID string) string {
	return filepath.Join(s.Dir(executionID), "tmp")
}

// Home is the folder the execution's sandbox sees as the home folder.
func (s SandboxScratch) Home(executionID string) string {
	return filepath.Join(s.Dir(executionID), "home")
}

// overlay is the layer and work folder of an execution's n-th overlay.
func (s SandboxScratch) overlay(executionID string, n int) (upper, work string) {
	dir := filepath.Join(s.Dir(executionID), "overlay", strconv.Itoa(n))
	return filepath.Join(dir, "upper"), filepath.Join(dir, "work")
}

// runtime is the execution's folder in the runtime folder.
func (s SandboxScratch) runtime(executionID string) string {
	if s.RuntimeDir == "" {
		return ""
	}
	return filepath.Join(s.RuntimeDir, "sandbox", executionID)
}

// Remove deletes an execution's scratch folders. The caller makes sure the
// execution has ended.
func (s SandboxScratch) Remove(executionID string) error {
	if !executionFolder.MatchString(executionID) {
		return fmt.Errorf("execution ID %q names no scratch folder", executionID)
	}
	var errs []error
	for _, path := range s.folders(executionID) {
		if err := removeScratch(path); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// folders are an execution's scratch folders: on disk, and in the runtime
// folder.
func (s SandboxScratch) folders(executionID string) []string {
	var out []string
	if s.Root != "" {
		out = append(out, s.Dir(executionID))
	}
	if s.RuntimeDir != "" {
		out = append(out, s.runtime(executionID))
	}
	return out
}

// removeScratch removes a folder an agent wrote, which may hold folders it
// made unwritable (Go's module cache does).
func removeScratch(path string) error {
	err := os.RemoveAll(path)
	if err == nil || os.IsNotExist(err) {
		return nil
	}
	_ = filepath.WalkDir(path, func(p string, d os.DirEntry, err error) error {
		if err == nil && d.IsDir() {
			_ = os.Chmod(p, 0o700)
		}
		return nil
	})
	return os.RemoveAll(path)
}

// ExecutionStatus says how an execution stands: found false when this
// Legato does not know it.
type ExecutionStatus func(ctx context.Context, executionID string) (status string, found bool, err error)

// StoreExecutionStatus reads an execution's status from the store: running
// while its agent session runs, whatever the execution says.
func StoreExecutionStatus(s *store.Store) ExecutionStatus {
	return func(ctx context.Context, id string) (string, bool, error) {
		execution, err := s.GetExecution(ctx, id)
		if errors.Is(err, store.ErrNotFound) {
			return "", false, nil
		}
		if err != nil {
			return "", false, err
		}
		// An agent session still running counts over the execution's
		// status: its sandbox is still in use.
		if execution.AgentSessionID != nil {
			session, err := s.GetAgentSessionByID(ctx, *execution.AgentSessionID)
			if err != nil && !errors.Is(err, store.ErrNotFound) {
				return "", false, err
			}
			if err == nil && session.Status == "running" {
				return store.ExecutionRunning, true, nil
			}
		}
		return execution.Status, true, nil
	}
}

// Sweep removes the scratch folders of executions that have ended: left
// behind by a crash, a restart, or an end Legato did not see. A folder whose
// execution is running, or that this Legato does not know (another
// Legato's: the runtime folder is shared), is left alone. It returns how
// many executions it cleared.
func (s SandboxScratch) Sweep(ctx context.Context, statusOf ExecutionStatus) (int, error) {
	// Folders are listed before any status is read: an execution is
	// recorded as running before its folders exist, so one started during
	// the sweep is never taken for an ended one.
	ids := map[string]bool{}
	for _, dir := range s.folders("") {
		entries, err := os.ReadDir(dir)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return 0, err
		}
		for _, e := range entries {
			if e.IsDir() && executionFolder.MatchString(e.Name()) {
				ids[e.Name()] = true
			}
		}
	}
	cleared := 0
	var errs []error
	for id := range ids {
		status, found, err := statusOf(ctx, id)
		if err != nil {
			return cleared, err
		}
		if !found || status == store.ExecutionRunning {
			continue
		}
		if err := s.Remove(id); err != nil {
			errs = append(errs, err)
			continue
		}
		cleared++
	}
	return cleared, errors.Join(errs...)
}

// removeLogged removes an ended execution's scratch, logging a
// failure: the sweep tries again later.
func (s SandboxScratch) removeLogged(executionID string) {
	if executionID == "" || (s.Root == "" && s.RuntimeDir == "") {
		return
	}
	if err := s.Remove(executionID); err != nil {
		log.Printf("sandbox: removing the scratch of %s: %v", executionID, err)
	}
}
