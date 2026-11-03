package service

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/cpave3/legato/internal/engine/clonesetup"
	"github.com/cpave3/legato/internal/engine/store"
)

// SetPrimaryDirs tells the agent service where each repository's main
// checkout is (its primary_dir), the folder a fresh clone's setup copies
// and links files from. A repository without one has none.
func (a *agentService) SetPrimaryDirs(primaryDir func(repo string) string) {
	a.primaryDir = primaryDir
}

// cloneSetup is how a clone Legato has just made is made ready, before the
// agent starts in it.
type cloneSetup struct {
	// file is the file the setup comes from.
	file string
	// command runs the setup's commands in the pane: run in the agent's
	// sandbox, before the agent tool.
	command string
	// linked are the main checkout's files the clone links to, which the
	// sandbox must show for the links to work.
	linked []string
}

// prepareFreshClone reads the setup of a clone this spawn has just made
// (one joined, made by an earlier execution, was set up then) and does the
// part that takes files from the repository's main checkout, on the host:
// the files to copy and link. The commands are run in the pane, where the
// agent will be. Nil when there is nothing to do.
func (a *agentService) prepareFreshClone(clone store.JobClone, executionID, repo, ref string) *cloneSetup {
	if clone.ExecutionID != executionID {
		return nil
	}
	setup, err := clonesetup.Load(clone.Path)
	if errors.Is(err, clonesetup.ErrNoSetup) {
		return nil
	}
	primary := ""
	if a.primaryDir != nil {
		primary = a.primaryDir(repo)
	}
	prepared := &cloneSetup{file: setup.File}
	if err != nil {
		// The command in the pane says what is wrong with the file, and
		// notes it on the task.
		log.Printf("clone setup of %s: %v", clone.Path, err)
	} else if setup.Empty() {
		return nil
	} else {
		report, provisionErr := clonesetup.Provision(setup, primary, clone.Path)
		if provisionErr != nil {
			log.Printf("clone setup of %s: %v", clone.Path, provisionErr)
		}
		for _, note := range report.Notes {
			log.Printf("clone setup of %s: %s", clone.Path, note)
		}
		prepared.linked = report.Linked
		if len(setup.Run) == 0 {
			return prepared
		}
	}
	binary := a.binaryPath
	if binary == "" {
		binary = "legato"
	}
	prepared.command = shellWord(binary) + " clone setup"
	if ref != "" {
		prepared.command += " --base " + shellWord(ref)
	}
	if primary != "" {
		prepared.command += " --primary " + shellWord(primary)
	}
	return prepared
}

// sandboxResources are the paths the clone's setup needs the sandbox to
// show: the files it links to, read-only.
func (c *cloneSetup) sandboxResources() []Resource {
	if c == nil || len(c.linked) == 0 {
		return nil
	}
	r := Resource{Name: "clone setup"}
	for _, p := range c.linked {
		r.Paths = append(r.Paths, ResourcePath{Path: p, Access: "read"})
	}
	return []Resource{r}
}

// prompt tells the agent its clone was set up and where the record is.
func (c *cloneSetup) prompt(clonePath string) string {
	if c == nil || c.command == "" {
		return ""
	}
	return "## Your clone was set up\n\nBefore you started, Legato ran the setup in this repository's " + c.file +
		" in your working directory (installing its dependencies, say). What it wrote is in " +
		filepath.Join(clonePath, ".git", "legato-setup.log") + ". If that log ends in a failure, fix what you need of it before you rely on the setup, or tell the person."
}

// cloneSetupStatus is the file the pane's shell writes the setup's exit
// status to once it ends, however it ends: in the clone's .git, on the
// host, after the sandbox has gone.
func cloneSetupStatus(clonePath string) string {
	return filepath.Join(clonePath, ".git", "legato-setup.status")
}

// How long a clone's setup may take, and how often its end is looked for.
const (
	cloneSetupTimeout = 30 * time.Minute
	cloneSetupPoll    = 250 * time.Millisecond
)

// waitForCloneSetup records that the task's agent starts after the setup of
// its clone, which ends when status is written.
func (a *agentService) waitForCloneSetup(taskID, status string) {
	a.setupsMu.Lock()
	defer a.setupsMu.Unlock()
	if a.setups == nil {
		a.setups = map[string]string{}
	}
	a.setups[taskID] = status
}

// AwaitCloneSetup returns once the setup of the clone the task's agent was
// just started in has ended: at once when there is none, else when the
// pane's shell has written how it ended, or the agent's pane has gone. It
// gives up after cloneSetupTimeout. The agent tool starts when it returns.
func (a *agentService) AwaitCloneSetup(ctx context.Context, taskID string) error {
	a.setupsMu.Lock()
	status, ok := a.setups[taskID]
	a.setupsMu.Unlock()
	if !ok {
		return nil
	}
	done := func() {
		a.setupsMu.Lock()
		if a.setups[taskID] == status {
			delete(a.setups, taskID)
		}
		a.setupsMu.Unlock()
	}
	deadline := time.Now().Add(cloneSetupTimeout)
	for {
		if _, err := os.Stat(status); err == nil {
			done()
			return nil
		}
		if alive, err := a.tmux.IsAlive("legato-" + taskID); err == nil && !alive {
			done()
			return nil
		}
		if time.Now().After(deadline) {
			done()
			return fmt.Errorf("the setup of %s's clone did not end in %s", taskID, cloneSetupTimeout)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(cloneSetupPoll):
		}
	}
}
