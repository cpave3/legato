// Package logfile is Legato's log file: one file in the data folder that
// every Legato process appends to, rotated when it grows past a cap, with a
// few old files kept beside it.
package logfile

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// Defaults for Options left zero.
const (
	DefaultMaxBytes = 10 << 20
	DefaultKeep     = 3
)

// Options bound the file. MaxBytes is the size past which the next write
// rotates it; Keep is how many old files (`.1` newest) are kept.
type Options struct {
	MaxBytes int64
	Keep     int
}

// Path is the log file of the data folder dataDir.
func Path(dataDir string) string {
	return filepath.Join(dataDir, "logs", "legato.log")
}

// Writer appends to the log file. It is safe for one process's goroutines;
// several processes may share the file, since every write appends.
type Writer struct {
	path string
	opts Options
	mu   sync.Mutex
	f    *os.File
}

// Open opens (or creates) the log file at path, making its folder.
func Open(path string, opts Options) (*Writer, error) {
	if opts.MaxBytes <= 0 {
		opts.MaxBytes = DefaultMaxBytes
	}
	if opts.Keep <= 0 {
		opts.Keep = DefaultKeep
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	w := &Writer{path: path, opts: opts}
	if err := w.open(); err != nil {
		return nil, err
	}
	return w, nil
}

func (w *Writer) open() error {
	f, err := os.OpenFile(w.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	w.f = f
	return nil
}

// Write appends p, rotating first when p would take the file past its cap.
// The size is read from the file itself, so a rotation by another process
// is noticed.
func (w *Writer) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.f == nil {
		return 0, os.ErrClosed
	}
	if size := w.size(); size > 0 && size+int64(len(p)) > w.opts.MaxBytes {
		if err := w.rotate(); err != nil {
			return 0, err
		}
	}
	return w.f.Write(p)
}

// size is the current file's size at its path (another process may have
// rotated it: then this one reopens it).
func (w *Writer) size() int64 {
	info, err := os.Stat(w.path)
	if err != nil {
		_ = w.f.Close()
		_ = w.open()
		return 0
	}
	if own, err := w.f.Stat(); err == nil && !os.SameFile(own, info) {
		_ = w.f.Close()
		_ = w.open()
	}
	return info.Size()
}

func (w *Writer) rotate() error {
	_ = w.f.Close()
	w.f = nil
	_ = os.Remove(fmt.Sprintf("%s.%d", w.path, w.opts.Keep))
	for i := w.opts.Keep - 1; i >= 1; i-- {
		_ = os.Rename(fmt.Sprintf("%s.%d", w.path, i), fmt.Sprintf("%s.%d", w.path, i+1))
	}
	if err := os.Rename(w.path, w.path+".1"); err != nil && !os.IsNotExist(err) {
		return err
	}
	return w.open()
}

// Close closes the file.
func (w *Writer) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.f == nil {
		return nil
	}
	err := w.f.Close()
	w.f = nil
	return err
}
