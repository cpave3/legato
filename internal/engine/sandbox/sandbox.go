// Package sandbox wraps a command so that it runs with a chosen view of the
// filesystem: bubblewrap on Linux, with the host's network and user, in a
// fresh PID namespace that ends with the command's parent; Seatbelt
// (sandbox-exec) on macOS. What the view holds (the plan) is decided by the
// caller; this package only renders it, with what each backend can do
// (Capabilities).
package sandbox

import (
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

// Kind is how one path appears in the sandbox.
type Kind int

const (
	// ReadOnly binds a host path read-only.
	ReadOnly Kind = iota
	// ReadWrite binds a host path read-write.
	ReadWrite
	// Symlink makes Path a symbolic link to Target, as the host's root has
	// (/bin -> usr/bin).
	Symlink
	// Hide covers a file with an empty one.
	Hide
	// HideDir covers a folder with an empty one.
	HideDir
	// Overlay shows a host path read-only beneath a throwaway layer: what
	// is written there lands in the mount's Upper folder on the host, or in
	// memory without one, and never in the host path. It needs a bwrap with
	// overlays (SupportsOverlay).
	Overlay
)

// Mount is one path of the plan. A bind's host path and its path inside are
// the same unless Dest names another.
type Mount struct {
	Kind Kind
	Path string
	Dest string
	// Target is a Symlink's target.
	Target string
	// Optional skips a bind whose host path does not exist.
	Optional bool
	// Upper and Work are an Overlay's writable layer and overlayfs's work
	// folder, empty folders on one host filesystem. Without them the layer
	// is a tmpfs, held in memory.
	Upper, Work string
}

// Plan is what a sandbox sees, in order: a later mount covers an earlier one.
type Plan struct {
	// Home is the person's home folder; it starts empty and writable, and
	// only what Mounts bind into it is seen.
	Home string
	// HomeSource is the empty host folder the home is, so what is written
	// there lands on disk; a tmpfs, held in memory, when empty.
	HomeSource string
	Mounts     []Mount
	Setenv     map[string]string
	Unsetenv   []string
	Chdir      string
}

// Backend builds a sandbox around a command.
type Backend interface {
	Name() string
	// Available says why the backend cannot be used, or nil.
	Available() error
	// Capabilities says what the backend can show, so a plan is made of
	// only what it can render.
	Capabilities() Capabilities
	// Prefix writes what the backend needs into dir, a folder of the
	// execution's own, and returns the shell words that start a command,
	// which follows them, in a sandbox with plan. It is typed into a
	// terminal, so it stays short.
	Prefix(plan Plan, dir string) (string, error)
}

// Capabilities is what a backend can do beyond allowing and denying paths.
type Capabilities struct {
	// Remap shows a host path at another path (Mount.Dest): /tmp, the gh
	// wrapper over every real gh, the ssh configuration copies.
	Remap bool
	// EmptyHome shows the home folder as an empty one (Plan.HomeSource);
	// without it the agent's HOME is that folder instead.
	EmptyHome bool
	// Overlay shows a path beneath a throwaway layer (Overlay mounts).
	Overlay bool
	// ProcessNamespace hides the host's processes and ends everything
	// inside with the sandbox.
	ProcessNamespace bool
}

// Full is what bwrap can do.
func Full() Capabilities {
	return Capabilities{Remap: true, EmptyHome: true, Overlay: true, ProcessNamespace: true}
}

var errNotFound = errors.New("not found")

// Bwrap is the bubblewrap backend.
type Bwrap struct {
	// Path is the bwrap binary; found on PATH when empty.
	Path string
	// LookPath finds it; exec.LookPath when nil.
	LookPath func(string) (string, error)
	// Help returns the binary's --help; running it when nil.
	Help func(binary string) (string, error)
}

// SupportsOverlay says whether this bwrap has overlays (--tmp-overlay,
// since bubblewrap 0.10.0), read from its --help.
func (b Bwrap) SupportsOverlay() bool {
	bin, err := b.binary()
	if err != nil {
		return false
	}
	help := b.Help
	if help == nil {
		help = func(binary string) (string, error) {
			out, err := exec.Command(binary, "--help").CombinedOutput()
			return string(out), err
		}
	}
	text, err := help(bin)
	return err == nil && strings.Contains(text, "--tmp-overlay")
}

func (b Bwrap) Name() string { return "bwrap" }

// Capabilities are all of them; overlays need bubblewrap 0.10.0 or later.
func (b Bwrap) Capabilities() Capabilities {
	c := Full()
	c.Overlay = b.SupportsOverlay()
	return c
}

// Prefix writes bwrap's options to dir/bwrap.args and the terminal filter
// to dir/seccomp.bpf, and returns the line that reads them (ArgsFileLine).
// Without dir, the options are on the line, and there is no filter.
func (b Bwrap) Prefix(plan Plan, dir string) (string, error) {
	args, err := b.Wrap(plan, []string{"true"})
	if err != nil {
		return "", err
	}
	args = args[:len(args)-1] // bwrap, its options, "--"
	if dir == "" {
		quoted := make([]string, len(args))
		for i, arg := range args {
			quoted[i] = ShellWord(arg)
		}
		return strings.Join(quoted, " "), nil
	}
	return ArgsFileLine(args[0], args[1:len(args)-1], dir)
}

func (b Bwrap) binary() (string, error) {
	if b.Path != "" {
		return b.Path, nil
	}
	look := b.LookPath
	if look == nil {
		look = exec.LookPath
	}
	path, err := look("bwrap")
	if err != nil {
		return "", fmt.Errorf("bubblewrap (bwrap) is not installed, and Legato's sandbox needs it: install it (dnf install bubblewrap, apt install bubblewrap), or turn agents.sandbox.enabled off")
	}
	return path, nil
}

func (b Bwrap) Available() error {
	_, err := b.binary()
	return err
}

func (b Bwrap) Wrap(plan Plan, command []string) ([]string, error) {
	if len(command) == 0 {
		return nil, errors.New("no command to run in the sandbox")
	}
	bin, err := b.binary()
	if err != nil {
		return nil, err
	}
	args := []string{bin,
		"--die-with-parent", "--unshare-pid", "--unshare-ipc", "--unshare-uts", "--unshare-cgroup-try",
		"--proc", "/proc", "--dev", "/dev",
	}
	if plan.Home != "" {
		if !filepath.IsAbs(plan.Home) {
			return nil, fmt.Errorf("home %q is not absolute", plan.Home)
		}
		if plan.HomeSource != "" {
			if !filepath.IsAbs(plan.HomeSource) {
				return nil, fmt.Errorf("home source %q is not absolute", plan.HomeSource)
			}
			args = append(args, "--bind", plan.HomeSource, plan.Home)
		} else {
			args = append(args, "--tmpfs", plan.Home)
		}
	}
	for _, m := range plan.Mounts {
		if !filepath.IsAbs(m.Path) || (m.Dest != "" && !filepath.IsAbs(m.Dest)) {
			return nil, fmt.Errorf("mount %q is not an absolute path", m.Path)
		}
		dest := m.Dest
		if dest == "" {
			dest = m.Path
		}
		switch m.Kind {
		case ReadOnly:
			args = append(args, bindFlag("--ro-bind", m.Optional), m.Path, dest)
		case ReadWrite:
			args = append(args, bindFlag("--bind", m.Optional), m.Path, dest)
		case Symlink:
			if m.Target == "" {
				return nil, fmt.Errorf("symlink %q has no target", m.Path)
			}
			args = append(args, "--symlink", m.Target, dest)
		case Hide:
			args = append(args, "--ro-bind", "/dev/null", dest)
		case HideDir:
			args = append(args, "--tmpfs", dest)
		case Overlay:
			if m.Upper != "" && m.Work != "" {
				args = append(args, "--overlay-src", m.Path, "--overlay", m.Upper, m.Work, dest)
			} else {
				args = append(args, "--overlay-src", m.Path, "--tmp-overlay", dest)
			}
		default:
			return nil, fmt.Errorf("mount %q has an unknown kind", m.Path)
		}
	}
	keys := make([]string, 0, len(plan.Setenv))
	for k := range plan.Setenv {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		args = append(args, "--setenv", k, plan.Setenv[k])
	}
	for _, k := range plan.Unsetenv {
		args = append(args, "--unsetenv", k)
	}
	if plan.Chdir != "" {
		if !filepath.IsAbs(plan.Chdir) {
			return nil, fmt.Errorf("working directory %q is not absolute", plan.Chdir)
		}
		args = append(args, "--chdir", plan.Chdir)
	}
	return append(append(args, "--"), command...), nil
}

func bindFlag(flag string, optional bool) string {
	if optional {
		return flag + "-try"
	}
	return flag
}
