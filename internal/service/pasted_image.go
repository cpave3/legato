package service

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// A task's ID becomes a folder name, so it must be a plain name.
var pastedImageTaskID = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`)

// ValidPastedImageTaskID reports whether a task ID can name a folder of
// pasted images.
func ValidPastedImageTaskID(taskID string) bool {
	return pastedImageTaskID.MatchString(taskID)
}

// PastedImageDir is where the images the person pasted for a task's agent
// are kept: under the system's temporary directory, because that is a place
// every agent tool lets its agent read (Chimera's read tool takes the
// working directory or /tmp and nothing else). It is not the worktree, where
// the files would show up as changes.
func PastedImageDir(taskID string) string {
	return filepath.Join(os.TempDir(), fmt.Sprintf("legato-%d", os.Getuid()), "pasted-images", taskID)
}

// IsPastedImage reports whether path is a file in the task's folder of
// pasted images. An agent tool is told it may read such a file without
// asking the person, so the answer is about the file the path leads to: a
// path that climbs out of the folder, or a link in it that leads elsewhere,
// is not one.
func IsPastedImage(taskID, path string) bool {
	if !ValidPastedImageTaskID(taskID) || !filepath.IsAbs(path) {
		return false
	}
	dir, err := filepath.EvalSymlinks(PastedImageDir(taskID))
	if err != nil {
		return false
	}
	file, err := filepath.EvalSymlinks(path)
	if err != nil {
		return false
	}
	if filepath.Dir(file) != dir || strings.HasPrefix(filepath.Base(file), ".") {
		return false
	}
	info, err := os.Lstat(file)
	return err == nil && info.Mode().IsRegular()
}
