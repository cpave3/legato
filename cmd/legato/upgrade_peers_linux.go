package main

import (
	"os"
	"path/filepath"
	"strconv"
)

// usesFile says whether the process pid has the database at path open, or
// its WAL or shared-memory file: a Legato on that database keeps them open
// for as long as it runs. A process whose open files cannot be read
// counts as using it.
func usesFile(pid int, path string) bool {
	want := map[string]bool{}
	for _, name := range []string{path, path + "-wal", path + "-shm"} {
		want[filepath.Clean(name)] = true
		if abs, err := filepath.Abs(name); err == nil {
			want[abs] = true
		}
	}
	if dir, err := filepath.EvalSymlinks(filepath.Dir(path)); err == nil {
		base := filepath.Base(path)
		for _, suffix := range []string{"", "-wal", "-shm"} {
			want[filepath.Join(dir, base+suffix)] = true
		}
	}
	fds := filepath.Join("/proc", strconv.Itoa(pid), "fd")
	entries, err := os.ReadDir(fds)
	if err != nil {
		return true
	}
	for _, e := range entries {
		if target, err := os.Readlink(filepath.Join(fds, e.Name())); err == nil && want[target] {
			return true
		}
	}
	return false
}
