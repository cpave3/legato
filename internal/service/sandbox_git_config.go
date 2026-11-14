package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// SandboxGitConfigProducer names the Attention producer of git config keys
// sandboxed agents added that make the person's git run a command.
const SandboxGitConfigProducer = "sandbox_git_config"

// SandboxGitConfig watches the git config of a sandboxed agent's checkout.
// The sandbox leaves it writable (an upstream, a remote), and keeps the
// hooks read-only; but the person's own git, outside the sandbox, runs what
// some keys name (core.fsmonitor, core.hooksPath, an alias with !, a
// filter). So the keys that run a command are noted at spawn (Baseline),
// compared when the execution ends (Check), and each one added or changed
// is an Attention item until it is gone, with an action that removes it.
// See docs/ai/sandbox.md § The checkout's git folder.
type SandboxGitConfig struct {
	// Dir holds what Check found, one file per execution:
	// <data_dir>/sandbox-git-config.
	Dir string
}

// gitConfigEntry is one value of a git config key, as git prints it: the
// key with its section and name lower case.
type gitConfigEntry struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

type gitConfigBaseline struct {
	TaskID  string           `json:"task_id"`
	Config  string           `json:"config"`
	Entries []gitConfigEntry `json:"entries"`
}

type gitConfigFinding struct {
	TaskID      string           `json:"task_id"`
	ExecutionID string           `json:"execution_id"`
	Config      string           `json:"config"`
	Entries     []gitConfigEntry `json:"entries"`
	FoundAt     time.Time        `json:"found_at"`
}

const gitConfigBaselineFile = "git-config.json"

// Baseline notes the keys of config that run a command, in runDir (the
// execution's runtime folder, which the sandbox does not see).
func (g *SandboxGitConfig) Baseline(ctx context.Context, runDir, taskID, config string) error {
	entries, err := riskyGitConfig(ctx, config)
	if err != nil {
		return err
	}
	data, err := json.Marshal(gitConfigBaseline{TaskID: taskID, Config: config, Entries: entries})
	if err != nil {
		return err
	}
	if err := os.MkdirAll(runDir, 0o700); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(runDir, gitConfigBaselineFile), data, 0o600)
}

// Check compares the config with its baseline and keeps what was added or
// changed for the Attention producer. An execution with no baseline (not
// sandboxed, or not in a git checkout) is skipped.
func (g *SandboxGitConfig) Check(ctx context.Context, runDir, executionID string) error {
	data, err := os.ReadFile(filepath.Join(runDir, gitConfigBaselineFile))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	var base gitConfigBaseline
	if err := json.Unmarshal(data, &base); err != nil {
		return err
	}
	now, err := riskyGitConfig(ctx, base.Config)
	if err != nil {
		return err
	}
	had := map[gitConfigEntry]bool{}
	for _, e := range base.Entries {
		had[e] = true
	}
	var added []gitConfigEntry
	for _, e := range now {
		if !had[e] {
			added = append(added, e)
		}
	}
	if len(added) == 0 {
		return nil
	}
	finding, err := json.Marshal(gitConfigFinding{TaskID: base.TaskID, ExecutionID: executionID, Config: base.Config, Entries: added, FoundAt: time.Now().UTC()})
	if err != nil {
		return err
	}
	if err := os.MkdirAll(g.Dir, 0o700); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(g.Dir, executionID+".json"), finding, 0o600)
}

func (g *SandboxGitConfig) Producer() string { return SandboxGitConfigProducer }

// SandboxGitConfigWatcher is an agent service that watches its sandboxed
// agents' git config; the runtime registers the watcher as an Attention
// producer.
type SandboxGitConfigWatcher interface {
	SandboxGitConfig() *SandboxGitConfig
}

func (a *agentService) SandboxGitConfig() *SandboxGitConfig {
	if a.sandbox == nil {
		return nil
	}
	return a.sandbox.GitConfig
}

// findings are the kept findings, each with the entries still in its
// config; a finding none of whose entries is left is removed.
func (g *SandboxGitConfig) findings(ctx context.Context) ([]gitConfigFinding, error) {
	files, _ := filepath.Glob(filepath.Join(g.Dir, "*.json"))
	sort.Strings(files)
	var out []gitConfigFinding
	for _, file := range files {
		data, err := os.ReadFile(file)
		if err != nil {
			continue
		}
		var f gitConfigFinding
		if json.Unmarshal(data, &f) != nil {
			continue
		}
		now, err := riskyGitConfig(ctx, f.Config)
		if errors.Is(err, os.ErrNotExist) {
			_ = os.Remove(file) // the checkout is gone
			continue
		}
		if err != nil {
			return nil, err
		}
		present := map[gitConfigEntry]bool{}
		for _, e := range now {
			present[e] = true
		}
		var left []gitConfigEntry
		for _, e := range f.Entries {
			if present[e] {
				left = append(left, e)
			}
		}
		if len(left) == 0 {
			_ = os.Remove(file)
			continue
		}
		f.Entries = left
		out = append(out, f)
	}
	return out, nil
}

func (g *SandboxGitConfig) Snapshot(ctx context.Context) ([]AttentionInput, error) {
	found, err := g.findings(ctx)
	if err != nil {
		return nil, err
	}
	var items []AttentionInput
	for _, f := range found {
		var lines, revision []string
		for _, e := range f.Entries {
			value := e.Value
			if len(value) > 200 {
				value = value[:200] + "…"
			}
			lines = append(lines, fmt.Sprintf("- `%s` = `%s`", e.Key, strings.ReplaceAll(value, "`", "'")))
			revision = append(revision, e.Key+"="+e.Value)
		}
		items = append(items, AttentionInput{
			SignalKind: AttentionSignalSandboxGitConfig,
			SourceKey:  f.ExecutionID, SourceRevision: strings.Join(revision, "\n"),
			TaskID: f.TaskID,
			Title:  "An agent set git config that runs a command",
			Reason: "your own git would run it, outside the sandbox",
			Detail: fmt.Sprintf("A sandboxed agent added these to `%s`. Git runs what they name when you use this checkout. Remove them unless you put them there:\n\n%s",
				f.Config, strings.Join(lines, "\n")),
			Category: "agents", Urgency: "high",
			Navigation: AttentionNavigation{Kind: "task", Target: f.TaskID},
			Actions:    []AttentionAction{{ID: "remove", Label: "Remove them", Kind: "execute", Intent: "primary", RequiresConfirm: true}},
		})
	}
	return items, nil
}

// Execute removes a finding's entries from its config: those exact values,
// nothing else.
func (g *SandboxGitConfig) Execute(ctx context.Context, request AttentionActionRequest) error {
	if request.ActionID != "remove" {
		return fmt.Errorf("unknown action %q", request.ActionID)
	}
	found, err := g.findings(ctx)
	if err != nil {
		return err
	}
	for _, f := range found {
		if f.ExecutionID != request.SourceKey {
			continue
		}
		for _, e := range f.Entries {
			cmd := exec.CommandContext(ctx, "git", "config", "--file", f.Config, "--fixed-value", "--unset-all", e.Key, e.Value)
			cmd.Env = gitConfigEnv()
			if out, err := cmd.CombinedOutput(); err != nil {
				return fmt.Errorf("removing %s from %s: %v: %s", e.Key, f.Config, err, bytes.TrimSpace(out))
			}
		}
		return os.Remove(filepath.Join(g.Dir, f.ExecutionID+".json"))
	}
	return fmt.Errorf("no finding %s", request.SourceKey)
}

// gitConfigEnv runs git with no configuration of the person's or the
// system's: it only reads and writes the one file it is named.
func gitConfigEnv() []string {
	return append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_COUNT=0")
}

// riskyGitConfig lists the entries of a git config file that make git run
// a command, read by git itself (includes not followed: an include is
// itself such an entry).
func riskyGitConfig(ctx context.Context, file string) ([]gitConfigEntry, error) {
	if _, err := os.Stat(file); err != nil {
		return nil, err
	}
	cmd := exec.CommandContext(ctx, "git", "config", "--file", file, "--no-includes", "--list", "-z")
	cmd.Env = gitConfigEnv()
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", file, err)
	}
	var entries []gitConfigEntry
	for _, record := range strings.Split(string(out), "\x00") {
		key, value, _ := strings.Cut(record, "\n")
		if key != "" && gitConfigRunsCommand(key, value) {
			entries = append(entries, gitConfigEntry{Key: key, Value: value})
		}
	}
	return entries, nil
}

// gitConfigRunsCommand says whether git runs, or reads more config from,
// what key names. key is as git prints it: section and name lower case,
// a subsection as written.
func gitConfigRunsCommand(key, value string) bool {
	section, rest, _ := strings.Cut(key, ".")
	name := rest
	if i := strings.LastIndex(rest, "."); i >= 0 {
		name = rest[i+1:]
	}
	switch section {
	case "include", "includeif":
		return name == "path"
	case "core":
		switch name {
		case "fsmonitor":
			v := strings.ToLower(value)
			return v != "" && v != "true" && v != "false" && v != "0" && v != "1" && v != "yes" && v != "no" && v != "on" && v != "off"
		case "hookspath", "sshcommand", "askpass", "editor", "pager", "gitproxy", "alternaterefscommand":
			return true
		}
	case "alias":
		return strings.HasPrefix(strings.TrimSpace(value), "!")
	case "filter":
		return name == "clean" || name == "smudge" || name == "process"
	case "diff":
		return name == "textconv" || name == "command" || name == "external"
	case "merge", "mergetool", "difftool":
		return name == "driver" || name == "cmd"
	case "credential":
		return name == "helper"
	case "gpg":
		return name == "program"
	case "sequence":
		return name == "editor"
	case "pager":
		return true
	case "interactive":
		return name == "difffilter"
	case "uploadpack":
		return name == "packobjectshook"
	case "remote":
		return name == "uploadpack" || name == "receivepack"
	case "submodule":
		return name == "update" && strings.HasPrefix(strings.TrimSpace(value), "!")
	case "sendemail":
		return name == "smtpserver" && strings.HasPrefix(strings.TrimSpace(value), "/")
	case "browser", "web":
		return name == "cmd" || name == "browser"
	}
	return false
}
