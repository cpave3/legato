package service

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"time"
)

// WorkflowScratchFolder is the folder of Legato's data folder that holds
// the scratch folders of agent.run steps marked scratch: true (LEG-530):
// <data_dir>/workflow-scratch/<attempt>. The sandbox shows an agent its own
// one (BuildSandboxPlan), as it shows a job clone.
const WorkflowScratchFolder = "workflow-scratch"

// workflowScratchKeep is how long a scratch folder a step kept (it failed,
// or kept its agent) stays for a look once nothing works in it.
const workflowScratchKeep = 7 * 24 * time.Hour

var scratchName = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

// workflowScratch is where an executor makes scratch folders, and how it
// asks whether a kept one is still in use.
type workflowScratch struct {
	root string
	// inUse reports whether an attempt still runs, or its agent does.
	inUse func(ctx context.Context, attemptID string) (bool, error)
}

// SetScratchRoot lets steps say scratch: true: each attempt then works in a
// new folder under root. inUse, when given, keeps the folders of attempts
// still running, or whose agent still runs, from the sweep of old ones.
func (e *AgentRunExecutor) SetScratchRoot(root string, inUse ...func(ctx context.Context, attemptID string) (bool, error)) {
	e.scratch.root = root
	if len(inUse) > 0 {
		e.scratch.inUse = inUse[0]
	}
}

func (e *AgentRunExecutor) makeScratch(ctx context.Context, attemptID string) (string, error) {
	if e.scratch.root == "" {
		return "", fmt.Errorf("with scratch needs Legato's data folder, which this Legato was not given")
	}
	if !scratchName.MatchString(attemptID) {
		return "", fmt.Errorf("scratch folder for attempt %q: not a plain name", attemptID)
	}
	e.sweepScratch(ctx)
	dir := filepath.Join(e.scratch.root, attemptID)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("making the scratch folder: %w", err)
	}
	return dir, nil
}

// isScratch reports whether dir is one of root's scratch folders.
func (e *AgentRunExecutor) isScratch(dir string) bool {
	if e.scratch.root == "" || dir == "" {
		return false
	}
	dir = filepath.Clean(dir)
	return filepath.Dir(dir) == filepath.Clean(e.scratch.root) && scratchName.MatchString(filepath.Base(dir))
}

func (e *AgentRunExecutor) removeScratch(dir string) {
	if !e.isScratch(dir) {
		return
	}
	if err := os.RemoveAll(dir); err != nil {
		log.Printf("workflows: removing scratch folder %s: %v", dir, err)
	}
}

// sweepScratch removes the scratch folders kept for a look once they are a
// week old and nothing works in them.
func (e *AgentRunExecutor) sweepScratch(ctx context.Context) {
	entries, err := os.ReadDir(e.scratch.root)
	if err != nil {
		return
	}
	for _, entry := range entries {
		info, err := entry.Info()
		if err != nil || !entry.IsDir() || time.Since(info.ModTime()) < workflowScratchKeep || !scratchName.MatchString(entry.Name()) {
			continue
		}
		if e.scratch.inUse != nil {
			if used, err := e.scratch.inUse(ctx, entry.Name()); err != nil || used {
				continue
			}
		}
		e.removeScratch(filepath.Join(e.scratch.root, entry.Name()))
	}
}
