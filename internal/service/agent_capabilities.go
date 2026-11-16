package service

import (
	"context"
	"errors"
	"log"
	"os"
	"path/filepath"

	"github.com/cpave3/legato/internal/engine/skills"
	"github.com/cpave3/legato/internal/engine/store"
)

// Agent capabilities are what Legato gives an agent beyond its tool and its
// work, the same whichever tool runs it and wherever: today the operator's
// skills (LEG-216). The seams are meant for more:
//
//   - A CapabilitySource chooses what an agent gets from a CapabilityRequest.
//     SkillService gives every agent every skill that is on; a profile (a
//     named set of skills and MCP tools, LEG-215) would be another source
//     choosing by the request's repository, role or kind.
//   - AgentCapabilities is what was chosen. MCP broker tools (LEG-212) would
//     be another field beside Skills.
//   - Each execution gets a folder of its own under CapabilityFolders, which
//     the sandbox mounts read-only and which goes when the execution ends; a
//     tool's adapter says how its tool finds what is in it (SkillsAdapter).
//     A capability is copied in at the start, so what an agent has does not
//     change while it runs: a change reaches agents started or reloaded
//     afterwards.
//
// See docs/ai/skills.md.

// CapabilityRequest is the agent a CapabilitySource chooses for.
type CapabilityRequest struct {
	TaskID      string
	ExecutionID string
	AgentKind   string
	Role        string
	Slot        string
	WorkDir     string
}

// AgentCapabilities is what an agent is given.
type AgentCapabilities struct {
	Skills []skills.Skill
}

// CapabilitySource chooses an agent's capabilities.
type CapabilitySource interface {
	Capabilities(ctx context.Context, req CapabilityRequest) (AgentCapabilities, error)
}

// SkillsAdapter is an adapter whose tool can load skills from a folder
// Legato makes for one execution.
type SkillsAdapter interface {
	// SkillsBundle writes in root what the tool needs around a folder of
	// skills (a plugin's or a mod's manifest), adds to env what makes the
	// tool load it, and returns the folder each skill's folder goes in.
	SkillsBundle(root string, env map[string]string) (skillsDir string, err error)
}

// CapabilityFolders holds each execution's capabilities, one folder per
// execution under Root (<data_dir>/capabilities), and one per tool inside
// it. The sandbox shows an execution its own folder, read-only.
type CapabilityFolders struct {
	Root string
}

// Dir is an execution's folder. Empty without a Root or for an execution
// ID that cannot name a folder.
func (c CapabilityFolders) Dir(executionID string) string {
	if c.Root == "" || !executionFolder.MatchString(executionID) {
		return ""
	}
	return filepath.Join(c.Root, executionID)
}

// Remove removes an execution's folder.
func (c CapabilityFolders) Remove(executionID string) error {
	dir := c.Dir(executionID)
	if dir == "" {
		return nil
	}
	return os.RemoveAll(dir)
}

func (c CapabilityFolders) removeLogged(executionID string) {
	if err := c.Remove(executionID); err != nil {
		log.Printf("capabilities: removing the folder of %s: %v", executionID, err)
	}
}

// Sweep removes the folders of executions that have ended, as
// SandboxScratch.Sweep does: a running execution's, or one this Legato does
// not know, is left alone. It returns how many it removed.
func (c CapabilityFolders) Sweep(ctx context.Context, statusOf ExecutionStatus) (int, error) {
	if c.Root == "" {
		return 0, nil
	}
	entries, err := os.ReadDir(c.Root)
	if os.IsNotExist(err) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	cleared := 0
	var errs []error
	for _, entry := range entries {
		id := entry.Name()
		if !entry.IsDir() || !executionFolder.MatchString(id) {
			continue
		}
		status, found, err := statusOf(ctx, id)
		if err != nil {
			return cleared, err
		}
		if !found || status == store.ExecutionRunning {
			continue
		}
		if err := c.Remove(id); err != nil {
			errs = append(errs, err)
			continue
		}
		cleared++
	}
	return cleared, errors.Join(errs...)
}

// giveCapabilities copies what the source chooses for an agent into its
// execution's folder, where its tool's adapter says the tool finds it, and
// adds to env what the tool needs. A failure leaves the agent without what
// failed, logged: a capability is an addition, never a reason not to start.
func (a *agentService) giveCapabilities(ctx context.Context, req CapabilityRequest, adapter AIToolAdapter, env map[string]string) {
	if a.capabilities == nil {
		return
	}
	bundler, ok := adapter.(SkillsAdapter)
	dir := a.capabilityFolders.Dir(req.ExecutionID)
	if !ok || dir == "" {
		return
	}
	given, err := a.capabilities.Capabilities(ctx, req)
	if err != nil {
		log.Printf("capabilities: choosing for %s (%s): %v", req.TaskID, req.ExecutionID, err)
		return
	}
	if len(given.Skills) == 0 {
		return
	}
	// The env is changed only once every skill is in place, so a tool is
	// never pointed at a bundle that failed.
	bundleEnv := map[string]string{}
	for k, v := range env {
		bundleEnv[k] = v
	}
	skillsDir, err := bundler.SkillsBundle(filepath.Join(dir, adapter.Name()), bundleEnv)
	if err != nil {
		log.Printf("capabilities: the skills bundle of %s (%s): %v", req.TaskID, req.ExecutionID, err)
		a.capabilityFolders.removeLogged(req.ExecutionID)
		return
	}
	for _, skill := range given.Skills {
		if err := skills.Copy(skill, filepath.Join(skillsDir, skill.Name)); err != nil {
			log.Printf("capabilities: skill %s for %s (%s): %v", skill.Name, req.TaskID, req.ExecutionID, err)
		}
	}
	for k, v := range bundleEnv {
		env[k] = v
	}
}
