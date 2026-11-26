package claudeusage

import (
	"errors"
	"os"
	"syscall"
)

// errNotRegular: the path is not a regular file (a FIFO, a device, a
// folder), so it is not read.
var errNotRegular = errors.New("not a regular file")

// openRegular opens a file a sandboxed agent may have replaced: without
// blocking (a FIFO with no writer would hang the open), then only if what
// was opened is a regular file, checked on the descriptor so a swap after
// a path check cannot slip through.
func openRegular(path string) (*os.File, error) {
	file, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, err
	}
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		_ = file.Close()
		return nil, errNotRegular
	}
	return file, nil
}
