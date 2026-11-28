package service

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/cpave3/legato/internal/engine/claudecode"
)

// ClaudeToolName names Claude Code among the managed tools: its command, as
// st and chimera are named for theirs.
const ClaudeToolName = "claude"

// ClaudeToolFolder is where Legato keeps its own Claude Code, under its
// data folder.
var ClaudeToolFolder = filepath.Join("tools", "claude")

// ClaudeChannelStable and ClaudeChannelLatest are Claude Code's release
// channels; stable is the default.
const (
	ClaudeChannelStable = "stable"
	ClaudeChannelLatest = "latest"
)

// ClaudeReleases is Claude Code's release server (claudecode.Releases).
type ClaudeReleases interface {
	// Latest is the version a channel points at.
	Latest(ctx context.Context, channel string) (string, error)
	// Fetch puts a version's checked binary in dir as dir/<version>.
	Fetch(ctx context.Context, version, dir string) (string, error)
}

// ClaudeCodeToolOptions are what a ClaudeCodeTool works with.
type ClaudeCodeToolOptions struct {
	// Folder is Legato's own folder of Claude Code versions
	// (<data_dir>/tools/claude).
	Folder   claudecode.Folder
	Releases ClaudeReleases
	// Override is updates.claude.command: the operator's own claude, a path
	// or a command on PATH. Set, Legato manages nothing and launches it.
	Override string
	// Channel is updates.claude.channel, read at each check; nil or "" is
	// stable.
	Channel func(ctx context.Context) string
	// Installed finds the operator's native install to adopt on the first
	// check (its file and version); nil looks at the claude on PATH.
	Installed func() (path, version string, ok bool)
	// Version asks an overriding claude its version; nil runs --version.
	Version func(ctx context.Context, path string) (string, error)
}

// ClaudeCodeTool keeps Legato's own Claude Code: versions fetched from
// Anthropic's release server into a folder of Legato's, a link named claude
// that agents launch, the version replaced by each update kept as the last
// known good, and a rollback to it. It never touches the operator's own
// install (~/.local/bin/claude and ~/.local/share/claude); on the first
// check it adopts a copy of the version installed there, without
// downloading.
type ClaudeCodeTool struct {
	opts ClaudeCodeToolOptions

	op       sync.Mutex // one Apply or Rollback at a time in this process
	mu       sync.Mutex
	status   ToolStatus
	onChange func()
}

// NewClaudeCodeTool makes one.
func NewClaudeCodeTool(opts ClaudeCodeToolOptions) *ClaudeCodeTool {
	if opts.Installed == nil {
		opts.Installed = nativeClaudeOnPath
	}
	if opts.Version == nil {
		opts.Version = claudeVersion
	}
	return &ClaudeCodeTool{opts: opts, status: ToolStatus{Name: ClaudeToolName, State: UpdateUnchecked}}
}

// ClaudeLaunchCommand is the claude agents launch: the override when set,
// else Legato's link while the version it points at is on disk, else
// claude from PATH. managed says it is Legato's link.
func ClaudeLaunchCommand(folder claudecode.Folder, override string) (command string, managed bool) {
	if override != "" {
		return override, false
	}
	if cur, err := folder.Current(); err == nil && folder.Has(cur) {
		return folder.Command(), true
	}
	return "claude", false
}

// Command is the claude agents launch now (ClaudeLaunchCommand).
func (c *ClaudeCodeTool) Command() (string, bool) {
	return ClaudeLaunchCommand(c.opts.Folder, c.opts.Override)
}

// Name is claude.
func (c *ClaudeCodeTool) Name() string { return ClaudeToolName }

// Status is where Claude Code stands, as last checked.
func (c *ClaudeCodeTool) Status() ToolStatus {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.status
}

func (c *ClaudeCodeTool) channel(ctx context.Context) string {
	if c.opts.Channel != nil {
		if ch := c.opts.Channel(ctx); ch == ClaudeChannelLatest {
			return ch
		}
	}
	return ClaudeChannelStable
}

// Check reads Legato's folder (adopting the operator's native install the
// first time) and asks the release server for the channel's version.
func (c *ClaudeCodeTool) Check(ctx context.Context) ToolStatus {
	return c.set(c.check(ctx))
}

func (c *ClaudeCodeTool) check(ctx context.Context) ToolStatus {
	st := ToolStatus{Name: ClaudeToolName, CheckedAt: time.Now()}
	if c.opts.Override != "" {
		return c.checkOverride(ctx, st)
	}
	st.Channel = c.channel(ctx)
	folder := c.opts.Folder
	cur, err := folder.Current()
	if errors.Is(err, claudecode.ErrNoVersion) {
		cur, err = c.adopt()
		if err == nil && cur == "" {
			st.State = ToolMissing
			st.Error = "No native install of Claude Code was found to start from. Install it with its native installer, and Legato takes a copy of that version."
			return st
		}
	}
	if err != nil {
		st.State, st.Error = UpdateFailed, err.Error()
		return st
	}
	st.Managed, st.Version, st.Path = true, cur, folder.Command()
	st.LastGood, _ = folder.LastGood()
	st.Held, _ = folder.Held()
	st.CanRollback = st.LastGood != "" && st.LastGood != cur
	st.Detail = "Updating fetches it from Anthropic's release server into Legato's own folder, checked against the release's checksum. Running agents keep their version; new agents start on the new one. Your own claude is not touched."
	if !folder.Has(cur) {
		st.State = UpdateFailed
		st.Error = fmt.Sprintf("Claude Code %s is no longer in %s; Update fetches it again", cur, folder.VersionsDir())
		return st
	}
	latest, err := c.opts.Releases.Latest(ctx, st.Channel)
	if err != nil {
		st.State, st.Error = UpdateFailed, err.Error()
		return st
	}
	st.Latest = latest
	st.Available = claudecode.Newer(latest, cur) && latest != st.Held
	st.State = UpdateUpToDate
	if st.Available {
		st.State = UpdateAvailable
	}
	return st
}

// adopt copies the operator's native install into the folder and points
// the link at it: the version, or "" when there is none to adopt.
func (c *ClaudeCodeTool) adopt() (string, error) {
	path, v, ok := c.opts.Installed()
	if !ok {
		return "", nil
	}
	folder := c.opts.Folder
	if !folder.Has(v) {
		if err := folder.Adopt(path, v); err != nil {
			return "", fmt.Errorf("taking a copy of Claude Code %s: %w", v, err)
		}
	}
	if err := folder.SetCurrent(v); err != nil {
		return "", err
	}
	return v, nil
}

func (c *ClaudeCodeTool) checkOverride(ctx context.Context, st ToolStatus) ToolStatus {
	path := c.opts.Override
	if !strings.Contains(path, "/") {
		found, err := exec.LookPath(path)
		if err != nil {
			st.State, st.Path = ToolMissing, path
			st.Error = path + " is not on Legato's PATH (updates.claude.command)"
			return st
		}
		path = found
	}
	st.Path, st.State = path, ToolUnmanaged
	st.Detail = "Your own claude, from updates.claude.command. Legato does not update or roll it back."
	v, err := c.opts.Version(ctx, path)
	if err != nil {
		st.Error = err.Error()
	}
	st.Version = v
	return st
}

// Apply installs the channel's newer version and keeps the one it replaces
// as the last known good; when the version in use has gone from disk, it
// fetches that version again instead. A failure leaves the version in use
// and the last known good as they were.
func (c *ClaudeCodeTool) Apply(ctx context.Context) error {
	c.op.Lock()
	defer c.op.Unlock()
	if c.opts.Override != "" {
		c.Check(ctx)
		return ErrToolNotManaged
	}
	unlock, err := c.opts.Folder.Lock()
	if err != nil {
		return err
	}
	defer unlock()
	st := c.Check(ctx)
	if !st.Managed {
		return ErrToolNotManaged
	}
	folder := c.opts.Folder
	if !folder.Has(st.Version) {
		return c.install(ctx, st, st.Version, func() error { return folder.SetCurrent(st.Version) })
	}
	if !st.Available {
		return ErrNoUpdate
	}
	return c.install(ctx, st, st.Latest, func() error {
		if err := folder.SetLastGood(st.Version); err != nil {
			return err
		}
		if err := folder.SetCurrent(st.Latest); err != nil {
			return err
		}
		return folder.SetHeld("")
	})
}

// Rollback switches to the last known good version, fetching it again if it
// has gone from disk, and holds the version rolled back from so it is not
// offered again. Agents running keep their version; new ones start on the
// last known good.
func (c *ClaudeCodeTool) Rollback(ctx context.Context) error {
	c.op.Lock()
	defer c.op.Unlock()
	if c.opts.Override != "" {
		c.Check(ctx)
		return ErrToolNotManaged
	}
	unlock, err := c.opts.Folder.Lock()
	if err != nil {
		return err
	}
	defer unlock()
	st := c.Check(ctx)
	if !st.Managed {
		return ErrToolNotManaged
	}
	if !st.CanRollback {
		return ErrNoLastGood
	}
	folder := c.opts.Folder
	return c.install(ctx, st, st.LastGood, func() error {
		if err := folder.SetHeld(st.Version); err != nil {
			return err
		}
		return folder.SetCurrent(st.LastGood)
	})
}

// install fetches version when it is not on disk, then switches to it with
// switchTo and checks again.
func (c *ClaudeCodeTool) install(ctx context.Context, st ToolStatus, version string, switchTo func() error) error {
	st.State, st.Error = UpdateInstalling, ""
	c.set(st)
	err := func() error {
		if !c.opts.Folder.Has(version) {
			if _, err := c.opts.Releases.Fetch(ctx, version, c.opts.Folder.VersionsDir()); err != nil {
				return err
			}
		}
		return switchTo()
	}()
	if err != nil {
		st.State, st.Error = UpdateFailed, err.Error()
		c.set(st)
		return fmt.Errorf("claude %s: %w", version, err)
	}
	if after := c.Check(ctx); after.State == UpdateFailed && after.Version != version {
		return errors.New(after.Error)
	}
	return nil
}

func (c *ClaudeCodeTool) set(st ToolStatus) ToolStatus {
	c.mu.Lock()
	c.status = st
	notify := c.onChange
	c.mu.Unlock()
	if notify != nil {
		notify()
	}
	return st
}

func (c *ClaudeCodeTool) setOnChange(fn func()) {
	c.mu.Lock()
	c.onChange = fn
	c.mu.Unlock()
}

// nativeClaudeOnPath is the operator's native install, found through the
// claude on Legato's PATH.
func nativeClaudeOnPath() (string, string, bool) {
	path, err := exec.LookPath("claude")
	if err != nil {
		return "", "", false
	}
	return claudecode.InstalledVersion(path)
}

// claudeVersion runs claude --version, which prints "2.1.326 (Claude Code)".
func claudeVersion(ctx context.Context, path string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, toolTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, path, "--version")
	cmd.WaitDelay = time.Second
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("%s --version: %w", path, err)
	}
	fields := strings.Fields(string(out))
	if len(fields) == 0 {
		return "", fmt.Errorf("%s --version printed nothing", path)
	}
	return fields[0], nil
}
