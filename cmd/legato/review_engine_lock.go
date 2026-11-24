package main

import (
	"os"
	"path/filepath"
	"sync"
	"syscall"

	"github.com/cpave3/legato/config"
	"github.com/cpave3/legato/internal/service"
)

// reviewEngineLockFile is the lock, in the data folder, that the Legato
// running the review engine holds: the broker holder, while it ticks (spec
// LEG-559 D9). Any process can ask whether some Legato on the folder runs
// the engine (reviewEngineRunning); the operating system lets the lock go
// when its holder ends.
const reviewEngineLockFile = "review-engine.lock"

// reviewEngineLock is this process's hold on the engine's lock.
type reviewEngineLock struct {
	dir  string
	mu   sync.Mutex
	file *os.File
}

// hold takes the lock unless this process has it, and says whether it has
// it now. Another process holding it, or a probe at that instant, is a
// false; the next tick tries again.
func (l *reviewEngineLock) hold() bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.file != nil {
		return true
	}
	if err := os.MkdirAll(l.dir, 0o700); err != nil {
		return false
	}
	file, err := os.OpenFile(filepath.Join(l.dir, reviewEngineLockFile), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return false
	}
	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		file.Close()
		return false
	}
	l.file = file
	return true
}

// release lets the lock go.
func (l *reviewEngineLock) release() {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.file != nil {
		l.file.Close()
		l.file = nil
	}
}

// reviewEngineRunning says whether some Legato on the data folder dir runs
// the review engine: a shared lock is refused while the holder has its
// exclusive one. Probes share, so two never read each other as the engine.
func reviewEngineRunning(dir string) bool {
	file, err := os.OpenFile(filepath.Join(dir, reviewEngineLockFile), os.O_RDWR, 0o600)
	if err != nil {
		return false // never held on this folder
	}
	defer file.Close()
	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_SH|syscall.LOCK_NB); err != nil {
		return true
	}
	_ = syscall.Flock(int(file.Fd()), syscall.LOCK_UN)
	return false
}

// reviewEngineStatus is what the card menus (web and TUI) are told of the
// review engine: offered while any Legato on the data folder holds it, else
// why not (spec LEG-559 D9). Asked at each menu, so a holder that started
// or ended since shows.
func reviewEngineStatus(cfg *config.Config) func() service.ReviewEngineStatus {
	return func() service.ReviewEngineStatus {
		return service.ReviewEngineStatusOf(cfg.ReviewPasses.Enabled, reviewEngineRunning(config.ResolveDataDir(cfg)))
	}
}
