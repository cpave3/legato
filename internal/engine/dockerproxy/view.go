// Package dockerproxy serves a Docker API socket that passes an agent's
// requests to the real daemon only when they keep within what the agent's
// sandbox may see and touch: deny by default, its containers, networks and
// volumes labelled with its execution and kept apart from everyone else's,
// and bind mounts only of what its sandbox shows, with no more access. It
// knows nothing of Legato; the caller says what the sandbox shows (View).
package dockerproxy

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Kind is how one path appears in a sandbox.
type Kind string

const (
	ReadOnly  Kind = "ro"
	ReadWrite Kind = "rw"
	// Symlink makes Dest a link to Target.
	Symlink Kind = "link"
	// Hide covers a file; HideDir covers a folder.
	Hide    Kind = "hide"
	HideDir Kind = "hidedir"
)

// Mount is one path of a sandbox: the host path Source shown at Dest. Later
// mounts cover earlier ones, as bubblewrap mounts them.
type Mount struct {
	Kind   Kind   `json:"kind"`
	Source string `json:"source,omitempty"`
	Dest   string `json:"dest"`
	Target string `json:"target,omitempty"`
}

// View is what a sandbox sees: nothing but its mounts.
type View struct {
	Mounts []Mount `json:"mounts"`
	// EvalSymlinks resolves a host path; filepath.EvalSymlinks when nil.
	EvalSymlinks func(string) (string, error) `json:"-"`
}

// errOutside is a path the sandbox does not show.
var errOutside = errors.New("not in this agent's sandbox")

// effective is the mount that shows path in the sandbox, the last that holds
// it, and its index; -1 when none does.
func (v View) effective(path string) int {
	for i := len(v.Mounts) - 1; i >= 0; i-- {
		if within(path, filepath.Clean(v.Mounts[i].Dest)) {
			return i
		}
	}
	return -1
}

// toHost is the host path the sandbox shows at path, following the links
// the sandbox makes.
func (v View) toHost(path string) (string, error) {
	for hops := 0; hops < 40; hops++ {
		i := v.effective(path)
		if i < 0 {
			return "", errOutside
		}
		m := v.Mounts[i]
		dest := filepath.Clean(m.Dest)
		rel := strings.TrimPrefix(strings.TrimPrefix(path, dest), "/")
		switch m.Kind {
		case Hide, HideDir:
			return "", errors.New("hidden from this agent's sandbox")
		case Symlink:
			target := m.Target
			if !filepath.IsAbs(target) {
				target = filepath.Join(filepath.Dir(dest), target)
			}
			path = filepath.Join(target, rel)
		case ReadOnly, ReadWrite:
			return filepath.Join(m.Source, rel), nil
		default:
			return "", fmt.Errorf("unknown mount kind %q", m.Kind)
		}
	}
	return "", errors.New("too many links")
}

// Resolve is the host path a container may bind for path, as the sandbox
// names it, with every link resolved on the host, and whether the container
// may only read it. A path the sandbox does not show, or shows only through
// a link the host resolves elsewhere, is refused.
func (v View) Resolve(path string) (host string, readOnly bool, err error) {
	if !filepath.IsAbs(path) {
		return "", false, fmt.Errorf("%s: a bind mount's source must be an absolute path", path)
	}
	path = filepath.Clean(path)
	host, err = v.toHost(path)
	if err != nil {
		return "", false, fmt.Errorf("%s: %w", path, err)
	}
	eval := v.EvalSymlinks
	if eval == nil {
		eval = filepath.EvalSymlinks
	}
	real, err := eval(host)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", false, fmt.Errorf("%s: does not exist on the host; make it first", path)
		}
		return "", false, fmt.Errorf("%s: %w", path, err)
	}
	// The resolved path must itself be shown by the sandbox, by the mount
	// that shows it there and not one covering it.
	found, writable := false, false
	for i, m := range v.Mounts {
		if (m.Kind != ReadOnly && m.Kind != ReadWrite) || !within(real, filepath.Clean(m.Source)) {
			continue
		}
		inside := filepath.Join(m.Dest, strings.TrimPrefix(strings.TrimPrefix(real, filepath.Clean(m.Source)), "/"))
		if v.effective(inside) != i {
			continue
		}
		found = true
		writable = writable || m.Kind == ReadWrite
	}
	if !found {
		return "", false, fmt.Errorf("%s: %w", path, errOutside)
	}
	return real, !writable, nil
}

func within(path, dir string) bool {
	return path == dir || dir == "/" || strings.HasPrefix(path, dir+"/")
}
