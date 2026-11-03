package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Tool states beside the update states they share (unchecked, up_to_date,
// available, installing, failed).
const (
	// ToolMissing is a tool not found on Legato's PATH.
	ToolMissing = "missing"
	// ToolUnmanaged is a tool that is not a release build (built from source
	// or with go install), which the updater leaves alone.
	ToolUnmanaged = "unmanaged"
)

// ErrToolNotManaged is a tool the updater does not look after: unknown,
// missing, or not a release build.
var ErrToolNotManaged = errors.New("the tool is not a release build Legato looks after")

// errToolTimedOut is a tool that did not answer in time: broken, not
// unmanaged.
var errToolTimedOut = errors.New("did not finish in time")

// toolTimeout is how long one command of a tool may take.
const toolTimeout = 2 * time.Minute

// ToolStatus is where the updates of a command-line tool stand.
type ToolStatus struct {
	Name string `json:"name"`
	// Path is the file the tool's command leads to, links followed.
	Path      string    `json:"path,omitempty"`
	Managed   bool      `json:"managed"`
	Version   string    `json:"version,omitempty"`
	Latest    string    `json:"latest,omitempty"`
	Available bool      `json:"available"`
	State     string    `json:"state"`
	Error     string    `json:"error,omitempty"`
	CheckedAt time.Time `json:"checked_at,omitzero"`
}

// ToolUpdaterOptions tune a ToolUpdater.
type ToolUpdaterOptions struct {
	// Timeout limits each command; 0 is two minutes.
	Timeout time.Duration
}

// ToolUpdater keeps a command-line tool that updates itself current: it
// finds it on Legato's PATH, asks it its version and whether a newer
// release exists, and has it install that release. The tool downloads and
// checks its release itself; Legato only runs it. Only st is known.
type ToolUpdater struct {
	name    string
	timeout time.Duration

	mu       sync.Mutex
	status   ToolStatus
	onChange func()
}

// NewSTUpdater looks after Staccato's st, which updates itself with
// `st update`.
func NewSTUpdater(opts ToolUpdaterOptions) *ToolUpdater {
	if opts.Timeout <= 0 {
		opts.Timeout = toolTimeout
	}
	return &ToolUpdater{name: "st", timeout: opts.Timeout, status: ToolStatus{Name: "st", State: UpdateUnchecked}}
}

// Name is the tool's command.
func (u *ToolUpdater) Name() string { return u.name }

// Status is where the tool's updates stand.
func (u *ToolUpdater) Status() ToolStatus {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.status
}

// Check finds the tool, asks its version and, for a release build, whether
// a newer release exists.
func (u *ToolUpdater) Check(ctx context.Context) ToolStatus {
	st := ToolStatus{Name: u.name, CheckedAt: time.Now()}
	path, err := exec.LookPath(u.name)
	if err != nil {
		st.State = ToolMissing
		return u.set(st)
	}
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		path = resolved
	}
	st.Path = path
	var ver struct {
		Version string `json:"version"`
		Release bool   `json:"release"`
	}
	if err := u.runJSON(ctx, path, &ver, "version", "--json"); err != nil {
		// An st with no version command predates its releases; one that
		// does not answer is broken.
		st.State, st.Error = ToolUnmanaged, err.Error()
		if errors.Is(err, errToolTimedOut) {
			st.State = UpdateFailed
		}
		return u.set(st)
	}
	st.Version = ver.Version
	if !ver.Release {
		st.State = ToolUnmanaged
		return u.set(st)
	}
	st.Managed = true
	var check struct {
		Latest    string `json:"latest"`
		Available bool   `json:"available"`
	}
	if err := u.runJSON(ctx, path, &check, "update", "--check", "--json"); err != nil {
		st.State, st.Error = UpdateFailed, err.Error()
		return u.set(st)
	}
	st.Latest, st.Available, st.State = check.Latest, check.Available, UpdateUpToDate
	if check.Available {
		st.State = UpdateAvailable
	}
	return u.set(st)
}

// Apply installs the newest release with the tool's own update command,
// then checks again: ErrToolNotManaged for a tool it does not look after,
// ErrNoUpdate when nothing newer is out.
func (u *ToolUpdater) Apply(ctx context.Context) error {
	st := u.Status()
	if st.State == UpdateUnchecked {
		st = u.Check(ctx)
	}
	if !st.Managed {
		return ErrToolNotManaged
	}
	if !st.Available {
		return ErrNoUpdate
	}
	st.State, st.Error = UpdateInstalling, ""
	u.set(st)
	if _, err := u.run(ctx, st.Path, "update"); err != nil {
		st.State, st.Error = UpdateFailed, err.Error()
		u.set(st)
		return fmt.Errorf("%s update: %w", u.name, err)
	}
	if after := u.Check(ctx); after.State == UpdateFailed {
		return errors.New(after.Error)
	}
	return nil
}

func (u *ToolUpdater) set(st ToolStatus) ToolStatus {
	u.mu.Lock()
	u.status = st
	notify := u.onChange
	u.mu.Unlock()
	if notify != nil {
		notify()
	}
	return st
}

func (u *ToolUpdater) setOnChange(fn func()) {
	u.mu.Lock()
	u.onChange = fn
	u.mu.Unlock()
}

// run runs the tool and returns what it printed; a failure is its last
// line of error output (or of output), or how it ended.
func (u *ToolUpdater) run(ctx context.Context, path string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, u.timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, path, args...)
	cmd.WaitDelay = time.Second
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	if err == nil {
		return stdout.Bytes(), nil
	}
	if ctx.Err() != nil {
		return nil, fmt.Errorf("%s %s %w (%s)", u.name, strings.Join(args, " "), errToolTimedOut, u.timeout)
	}
	if line := lastLine(stderr.String()); line != "" {
		return nil, errors.New(line)
	}
	if line := lastLine(stdout.String()); line != "" {
		return nil, errors.New(line)
	}
	return nil, err
}

func (u *ToolUpdater) runJSON(ctx context.Context, path string, into any, args ...string) error {
	out, err := u.run(ctx, path, args...)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(out, into); err != nil {
		return fmt.Errorf("%s %s printed no JSON it understood: %w", u.name, strings.Join(args, " "), err)
	}
	return nil
}

func lastLine(s string) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	return strings.TrimSpace(lines[len(lines)-1])
}
