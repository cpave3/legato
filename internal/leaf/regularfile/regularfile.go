// Package regularfile opens files that someone else may control, such as a
// file in a folder a sandboxed agent can write.
//
// It is a leaf package: it imports only the standard library, so any
// Legato package, an engine package included, may use it (see
// docs/adr/0001-stdlib-leaf-packages.md).
package regularfile

import (
	"errors"
	"os"
	"syscall"
)

// ErrNotRegular: the path is not a regular file (a FIFO, a device, a
// folder), so it is not read.
var ErrNotRegular = errors.New("not a regular file")

// Open opens path for reading without blocking (a FIFO with no writer
// would hang the open), then keeps it only if what was opened is a regular
// file, checked on the descriptor so a swap after a path check cannot slip
// through. A symbolic link is followed, as os.Open follows it. A missing
// file is the error os.OpenFile gives (fs.ErrNotExist); anything that is
// not a regular file is ErrNotRegular.
func Open(path string) (*os.File, error) {
	file, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, err
	}
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		_ = file.Close()
		return nil, ErrNotRegular
	}
	return file, nil
}
