package sandbox

import (
	"os"
	"path/filepath"
	"syscall"
)

// terminalPath is the /dev/ttys* that f is, or "": the pane's terminal,
// which the profile lets the agent open again by name.
func terminalPath(f *os.File) string {
	info, err := f.Stat()
	if err != nil || info.Mode()&os.ModeCharDevice == 0 {
		return ""
	}
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return ""
	}
	names, _ := filepath.Glob("/dev/ttys[0-9]*")
	for _, name := range names {
		var other syscall.Stat_t
		if syscall.Stat(name, &other) == nil && other.Rdev == st.Rdev {
			return name
		}
	}
	return ""
}
