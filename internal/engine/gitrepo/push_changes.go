package gitrepo

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

// RefUpdate is one ref a push updates, from Old to New. An ID of zeros is
// no commit: Old for a ref the push creates, New for one it deletes.
type RefUpdate struct {
	Old, New, Ref string
}

// ChangedUnder reads a push against a bare cache of the repository it goes
// to, and says, by ref, which paths under dir the push's new commits change:
// added, changed or deleted, against any of a commit's parents (so a merge
// that brings in a change counts), and between the ref's old and new
// commits. A new commit is one no ref of the cache, nor the ref's old
// commit, reaches. Refs that change nothing under dir are left out.
//
// pack is the pack the push carries, thin against the remote's refs: it is
// indexed in a scratch repository under scratch (the system's temporary
// directory when empty) that borrows the cache's objects, so neither the
// cache nor anything the push carries runs or stays. A pack whose bases
// the cache lacks (the cache is older than the remote) is an error: fetch
// the cache and read the push again.
func (r Runner) ChangedUnder(ctx context.Context, cache, scratch string, pack io.Reader, updates []RefUpdate, dir string) (map[string][]string, error) {
	dir = strings.Trim(dir, "/")
	objects, err := filepath.Abs(filepath.Join(cache, "objects"))
	if err != nil {
		return nil, err
	}
	if info, err := os.Stat(objects); err != nil || !info.IsDir() {
		return nil, fmt.Errorf("%s is not a repository", cache)
	}
	tmp, err := os.MkdirTemp(scratch, "legato-push-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(tmp)
	if _, err := r.run(ctx, "", nil, "init", "-q", "--bare", "--template=", tmp); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Join(tmp, "objects", "info"), 0o700); err != nil {
		return nil, err
	}
	if err := os.WriteFile(filepath.Join(tmp, "objects", "info", "alternates"), []byte(objects+"\n"), 0o600); err != nil {
		return nil, err
	}
	body := bufio.NewReader(pack)
	if _, err := body.Peek(1); err == nil { // a push that only deletes carries no pack
		if _, err := r.runInput(ctx, body, "--git-dir", tmp, "index-pack", "--stdin", "--fix-thin"); err != nil {
			return nil, fmt.Errorf("reading the push's pack: %w", err)
		}
	}
	known, err := r.run(ctx, "", nil, "--git-dir", cache, "for-each-ref", "--format=%(objectname)")
	if err != nil {
		return nil, err
	}
	out := map[string][]string{}
	for _, u := range updates {
		if isZero(u.New) {
			continue
		}
		paths := map[string]bool{}
		walk := u.New + "\n"
		for _, ref := range strings.Fields(known) {
			walk += "^" + ref + "\n"
		}
		oldKnown := !isZero(u.Old) && r.has(ctx, tmp, u.Old)
		if oldKnown {
			walk += "^" + u.Old + "\n"
		}
		commits, err := r.runInput(ctx, strings.NewReader(walk), "--git-dir", tmp, "rev-list", "--stdin")
		if err != nil {
			return nil, fmt.Errorf("the commits %s pushes: %w", u.Ref, err)
		}
		if commits != "" {
			changed, err := r.runInput(ctx, strings.NewReader(commits+"\n"), "--git-dir", tmp, "diff-tree", "--stdin", "-r", "-m", "--root", "--no-commit-id", "--name-only", "-z", "--", dir)
			if err != nil {
				return nil, fmt.Errorf("what %s changes: %w", u.Ref, err)
			}
			addPaths(paths, changed, dir)
		}
		if oldKnown {
			changed, err := r.run(ctx, "", nil, "--git-dir", tmp, "diff-tree", "-r", "--name-only", "-z", u.Old, u.New, "--", dir)
			if err != nil {
				return nil, fmt.Errorf("what %s changes: %w", u.Ref, err)
			}
			addPaths(paths, changed, dir)
		}
		if len(paths) == 0 {
			continue
		}
		list := make([]string, 0, len(paths))
		for p := range paths {
			list = append(list, p)
		}
		sort.Strings(list)
		out[u.Ref] = list
	}
	return out, nil
}

func addPaths(into map[string]bool, nulSeparated, dir string) {
	for _, p := range strings.Split(nulSeparated, "\x00") {
		if strings.HasPrefix(p, dir+"/") {
			into[p] = true
		}
	}
}

func isZero(id string) bool { return strings.Trim(id, "0") == "" }

// has reports whether the repository at gitDir (or its alternates) has a
// commit.
func (r Runner) has(ctx context.Context, gitDir, id string) bool {
	_, err := r.run(ctx, "", nil, "--git-dir", gitDir, "cat-file", "-e", id+"^{commit}")
	return err == nil
}

// runInput is run without a token, with stdin as git's input. Its output
// is not trimmed of NULs.
func (r Runner) runInput(ctx context.Context, stdin io.Reader, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, r.git(), append(append([]string{}, hardened...), args...)...)
	cmd.Env = r.env()
	cmd.Stdin = stdin
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("git %s: %w: %s", args[2], err, lastLine(stderr.String()))
	}
	return strings.TrimSpace(stdout.String()), nil
}
