// Package engram runs the engram CLI: the program that keeps engrams, named
// individuals each with its own memories, open threads and handoffs, which
// an agent session can be started as. Legato does not need engram to run.
// This package knows nothing of Legato; it runs the binary with an explicit
// environment and working directory and reads what it prints, following the
// contract written down in docs/ai/engram.md § What Legato needs of engram.
package engram

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

// Timeouts. engram answers in milliseconds; these only stop a hung one.
const (
	DefaultProbeTimeout = 2 * time.Second
	DefaultBootTimeout  = 5 * time.Second
)

// engram's exit codes past success (0), error (1) and usage (2).
const (
	exitUnknownIdentity = 3
	exitMissingStore    = 4
)

var (
	// ErrNotInstalled says there is no engram command to run.
	ErrNotInstalled = errors.New("engram is not installed")
	// ErrTimeout says engram did not answer in time.
	ErrTimeout = errors.New("engram did not answer in time")
	// ErrUnknownIdentity says the store has no engram of that name.
	ErrUnknownIdentity = errors.New("unknown engram")
	// ErrMissingStore says engram's store folder does not exist.
	ErrMissingStore = errors.New("engram's store is missing")
)

// Error is engram exiting with a failure: its exit code and what it wrote
// on stderr. errors.Is matches ErrUnknownIdentity and ErrMissingStore by
// the code.
type Error struct {
	Code    int
	Message string
}

func (e *Error) Error() string {
	if e.Message == "" {
		return fmt.Sprintf("engram exited with code %d", e.Code)
	}
	return "engram: " + e.Message
}

func (e *Error) Is(target error) bool {
	switch target {
	case ErrUnknownIdentity:
		return e.Code == exitUnknownIdentity
	case ErrMissingStore:
		return e.Code == exitMissingStore
	}
	return false
}

// Where is what `engram where --json` says: the binary, its version and
// the store it would use.
type Where struct {
	Version string `json:"version"`
	Binary  string `json:"binary"`
	Store   Store  `json:"store"`
	// Place is set only when asked about a directory.
	Place *Place `json:"place,omitempty"`
}

// Store is engram's store root and where that came from: ENGRAM_HOME,
// XDG_DATA_HOME or default.
type Store struct {
	Root   string `json:"root"`
	Source string `json:"source"`
	Exists bool   `json:"exists"`
}

// Place is how engram names a directory: the primary key new records get,
// and every scope that counts as that place, primary first.
type Place struct {
	Primary string   `json:"primary"`
	Scopes  []string `json:"scopes"`
}

// Identity is one engram in the store.
type Identity struct {
	Name    string    `json:"name"`
	Created time.Time `json:"created"`
	Summary string    `json:"summary"`
}

// BootResult is a boot packet, and what engram wrote on stderr beside it,
// which is for the log and never part of the packet.
type BootResult struct {
	Packet string
	Stderr string
}

// Runner runs engram. The zero value finds it on PATH with an empty
// environment.
type Runner struct {
	// Command is the engram binary; looked up on PATH as "engram" when
	// empty.
	Command string
	// LookPath finds the command; exec.LookPath when nil.
	LookPath func(name string) (string, error)
	// Env is the environment every call starts from (see HostEnvironment).
	// Boot and EndSession add theirs on top. Nothing else is inherited.
	Env []string
	// Timeouts; the defaults when zero.
	ProbeTimeout time.Duration
	BootTimeout  time.Duration
}

// HostEnvironment picks from getenv what engram needs to find its store
// and git: HOME, PATH, XDG_DATA_HOME and ENGRAM_HOME. It never passes on
// which engram or session the caller is, so a Legato started inside an
// engram session does not lend it to the engrams it runs.
func HostEnvironment(getenv func(string) string) []string {
	var env []string
	for _, key := range []string{"HOME", "PATH", "XDG_DATA_HOME", "ENGRAM_HOME"} {
		if v := getenv(key); v != "" {
			env = append(env, key+"="+v)
		}
	}
	return env
}

// Binary resolves the command Runner would run.
func (r Runner) Binary() (string, error) {
	if r.Command != "" {
		info, err := os.Stat(r.Command)
		if err != nil || info.IsDir() {
			return "", fmt.Errorf("%w: %s", ErrNotInstalled, r.Command)
		}
		return r.Command, nil
	}
	look := r.LookPath
	if look == nil {
		look = exec.LookPath
	}
	path, err := look("engram")
	if err != nil {
		return "", ErrNotInstalled
	}
	return path, nil
}

// Probe asks engram where it is and which store it uses. It needs no
// engram and starts no session.
func (r Runner) Probe(ctx context.Context) (Where, error) {
	var where Where
	err := r.runJSON(ctx, r.probeTimeout(), "", &where, "where", "--json")
	return where, err
}

// List gives the engrams in the store.
func (r Runner) List(ctx context.Context) ([]Identity, error) {
	var ids []Identity
	if err := r.runJSON(ctx, r.probeTimeout(), "", &ids, "identity", "list", "--json"); err != nil {
		return nil, err
	}
	return ids, nil
}

// Place gives a directory's place as engram computes it.
func (r Runner) Place(ctx context.Context, dir string) (Place, error) {
	var where Where
	if err := r.runJSON(ctx, r.probeTimeout(), "", &where, "where", "--dir", dir, "--json"); err != nil {
		return Place{}, err
	}
	if where.Place == nil || where.Place.Primary == "" {
		return Place{}, errors.New("engram where gave no place for " + dir)
	}
	return *where.Place, nil
}

// Boot runs `engram boot` in dir with env added to the Runner's, and
// returns the packet from stdout. ErrUnknownIdentity, ErrMissingStore and
// ErrTimeout are told apart; anything else is an *Error.
func (r Runner) Boot(ctx context.Context, env []string, dir string) (BootResult, error) {
	stdout, stderr, err := r.run(ctx, r.bootTimeout(), env, dir, "boot")
	return BootResult{Packet: stdout, Stderr: stderr}, err
}

// EndSession marks the session env names as over without a handoff.
func (r Runner) EndSession(ctx context.Context, env []string, dir string) error {
	_, _, err := r.run(ctx, r.bootTimeout(), env, dir, "session", "end")
	return err
}

func (r Runner) probeTimeout() time.Duration {
	if r.ProbeTimeout > 0 {
		return r.ProbeTimeout
	}
	return DefaultProbeTimeout
}

func (r Runner) bootTimeout() time.Duration {
	if r.BootTimeout > 0 {
		return r.BootTimeout
	}
	return DefaultBootTimeout
}

func (r Runner) runJSON(ctx context.Context, timeout time.Duration, dir string, out any, args ...string) error {
	stdout, _, err := r.run(ctx, timeout, nil, dir, args...)
	if err != nil {
		return err
	}
	if err := json.Unmarshal([]byte(stdout), out); err != nil {
		return fmt.Errorf("engram %s: unreadable answer: %w", strings.Join(args, " "), err)
	}
	return nil
}

func (r Runner) run(ctx context.Context, timeout time.Duration, env []string, dir string, args ...string) (string, string, error) {
	bin, err := r.Binary()
	if err != nil {
		return "", "", err
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Dir = dir
	cmd.Env = append(append([]string{}, r.Env...), env...)
	if cmd.Env == nil {
		cmd.Env = []string{}
	}
	// A child that keeps the pipes open must not hold the call past its
	// timeout.
	cmd.WaitDelay = 200 * time.Millisecond
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err = cmd.Run()
	if ctx.Err() == context.DeadlineExceeded {
		return stdout.String(), stderr.String(), ErrTimeout
	}
	if err != nil {
		var exit *exec.ExitError
		if errors.As(err, &exit) {
			return stdout.String(), stderr.String(), &Error{Code: exit.ExitCode(), Message: strings.TrimSpace(stderr.String())}
		}
		return stdout.String(), stderr.String(), err
	}
	return stdout.String(), stderr.String(), nil
}

// The kinds of place a binding is kept under.
const (
	KindRepo   = "repo"
	KindFolder = "folder"
)

// RepoKey is the place key of a GitHub-style repository, built without a
// checkout: repo:<host>/<owner>/<name>, lowercase. host defaults to
// github.com.
func RepoKey(host, ownerName string) string {
	if host == "" {
		host = "github.com"
	}
	return "repo:" + strings.ToLower(host+"/"+strings.Trim(ownerName, "/"))
}

// KindOf says whether a place key names a repository or a folder.
func KindOf(key string) string {
	if strings.HasPrefix(key, "repo:") {
		return KindRepo
	}
	return KindFolder
}

// Label is a place key without its prefix, as the person reads it.
func Label(key string) string {
	if _, rest, ok := strings.Cut(key, ":"); ok {
		return rest
	}
	return key
}
