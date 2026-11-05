package stackgraph

import (
	"fmt"
	"os"
)

// OnRemote lists which of the named branches the clone's origin has: those
// with a remote-tracking branch (refs/remotes/origin/<name>), whatever
// commit it records, in the order given. A name git would not take as a
// branch is left out. Like Read, it reads git's files and runs nothing.
func OnRemote(clone string, names ...string) ([]string, error) {
	top, err := os.OpenRoot(clone)
	if err != nil {
		return nil, err
	}
	defer top.Close()
	gitDir, err := top.OpenRoot(".git")
	if err != nil {
		return nil, fmt.Errorf("%s has no .git folder of its own: %w", clone, err)
	}
	defer gitDir.Close()
	fsys := gitDir.FS()
	packed, err := readPacked(fsys)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, name := range names {
		if !ValidBranchName(name) {
			continue
		}
		_, ok, err := ref(fsys, packed, "refs/remotes/origin/"+name)
		if err != nil {
			return nil, err
		}
		if ok {
			out = append(out, name)
		}
	}
	return out, nil
}
