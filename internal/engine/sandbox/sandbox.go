// Package sandbox wraps a command so that it runs with a chosen view of the
// filesystem: bubblewrap, with the host's network and user, in a fresh PID
// namespace that ends with the command's parent. What the view holds (the
// plan) is decided by the caller; this package only renders it.
package sandbox

import (
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"sort"
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
}

// Plan is what a sandbox sees, in order: a later mount covers an earlier one.
type Plan struct {
	// Home is the person's home folder; it starts empty and writable, and
	// only what Mounts bind into it is seen.
	Home     string
	Mounts   []Mount
	Setenv   map[string]string
	Unsetenv []string
	Chdir    string
}

// Backend builds a sandbox around a command.
type Backend interface {
	Name() string
	// Available says why the backend cannot be used, or nil.
	Available() error
	// Wrap returns the command line that runs command in a sandbox with plan.
	Wrap(plan Plan, command []string) ([]string, error)
}

var errNotFound = errors.New("not found")

// Bwrap is the bubblewrap backend.
type Bwrap struct {
	// Path is the bwrap binary; found on PATH when empty.
	Path string
	// LookPath finds it; exec.LookPath when nil.
	LookPath func(string) (string, error)
}

func (b Bwrap) Name() string { return "bwrap" }

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
		args = append(args, "--tmpfs", plan.Home)
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
