// Package swarm provides utilities for swarm-orchestration scope checks.
//
// Scopes are sets of doublestar-syntax glob patterns matched against
// repo-relative paths. Two scopes "overlap" when at least one file in the
// working tree matches both scopes.
package swarm

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/bmatcuk/doublestar/v4"
)

// MatchScope reports whether path matches any of the provided glob patterns.
// path must be repo-relative and use forward slashes. An empty scope owns
// nothing — MatchScope returns false.
func MatchScope(globs []string, path string) bool {
	if len(globs) == 0 {
		return false
	}
	for _, g := range globs {
		if g == "" {
			continue
		}
		ok, err := doublestar.PathMatch(g, path)
		if err == nil && ok {
			return true
		}
	}
	return false
}

// ScopeOverlaps walks the directory tree rooted at `root` and returns
// (overlap, paths) where `paths` is the list of repo-relative file paths
// matched by both `a` and `b`. An empty scope on either side is treated as
// owning nothing — the function returns (false, nil) immediately.
//
// Directories named ".git", "node_modules", ".cache" and similar large
// generated trees are skipped. The function is best-effort: it walks current
// working-tree files, so two scopes whose only intersection is a file that
// does not yet exist will not be reported as overlapping.
//
// `root` may be empty, in which case the function uses ".".
func ScopeOverlaps(a, b []string, root string) (bool, []string) {
	if len(a) == 0 || len(b) == 0 {
		return false, nil
	}
	if root == "" {
		root = "."
	}

	abs, err := filepath.Abs(root)
	if err != nil {
		return false, nil
	}
	if info, err := os.Stat(abs); err != nil || !info.IsDir() {
		return false, nil
	}

	var overlaps []string
	walkErr := filepath.WalkDir(abs, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			name := d.Name()
			if p == abs {
				return nil
			}
			if name == ".git" || name == "node_modules" || name == ".cache" ||
				name == "vendor" || name == "dist" || name == "build" ||
				name == ".next" || name == ".svelte-kit" || name == ".turbo" {
				return filepath.SkipDir
			}
			return nil
		}
		rel, err := filepath.Rel(abs, p)
		if err != nil {
			return nil
		}
		rel = filepath.ToSlash(rel)
		// strip leading ./ if present
		rel = strings.TrimPrefix(rel, "./")
		if MatchScope(a, rel) && MatchScope(b, rel) {
			overlaps = append(overlaps, rel)
		}
		return nil
	})
	if walkErr != nil {
		return false, nil
	}
	return len(overlaps) > 0, overlaps
}

// ValidateGlob returns an error if a glob pattern is syntactically invalid.
func ValidateGlob(g string) error {
	// doublestar.PathMatch returns an error only on malformed patterns.
	_, err := doublestar.PathMatch(g, "")
	return err
}

// ValidateScope returns the first invalid glob in the slice, or nil if all are valid.
func ValidateScope(globs []string) error {
	for _, g := range globs {
		if g == "" {
			continue
		}
		if err := ValidateGlob(g); err != nil {
			return err
		}
	}
	return nil
}
