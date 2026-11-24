package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/cpave3/legato/internal/engine/ipc"
	"github.com/cpave3/legato/internal/engine/store"
	"github.com/cpave3/legato/internal/tui"
)

// The upgrade restart (spec LEG-559 M7, D13). A new binary that finds
// migrations pending must not migrate under an old Legato still running
// on the same data folder, which would go on working on tables it does not
// know. So it:
//  1. broadcasts ipcUpgradeExit for its data folder: every other TUI and
//     `legato serve` on that folder exits (the TUI says "Legato updated:
//     restart"; serve exits with exitRestart, so systemd starts it again);
//  2. waits, at most upgradeExitWait, for them to be gone, which it knows
//     by the folder's process lock (holdLegatoProcess);
//  3. opens the database without migrating, backs it up beside itself
//     (store.Unmigrated.Backup), then migrates.

// ipcUpgradeExit asks every other Legato process on one data folder to
// exit: a newer binary is about to migrate its database. Content holds the
// data folder, PID the sender, which does not exit on its own message, and
// Schema the sender's newest schema revision: only a process that knows
// less exits, so two new processes that both found the migrations pending
// never stop each other.
const ipcUpgradeExit = "upgrade_exit"

// upgradeExitWait is how long an upgrade waits for the other processes to
// exit before it migrates anyway.
const upgradeExitWait = 30 * time.Second

// processLockFile is the lock, in the data folder, that each running TUI
// and `legato serve` holds shared for as long as it runs. An upgrade takes
// it exclusively, which it can only once they have all exited; the
// operating system lets a process's hold go when it ends.
const processLockFile = "processes.lock"

// upgradeExitMessage asks the other Legato processes on dataDir that know
// a schema older than schema to exit.
func upgradeExitMessage(dataDir string, pid, schema int) ipc.Message {
	return ipc.Message{Type: ipcUpgradeExit, Content: dataFolderKey(dataDir), PID: pid, Schema: schema}
}

// isUpgradeExitFor says whether msg asks the process pid, on dataDir and
// knowing the schema known, to exit for an upgrade: one on another data
// folder, the sender, or one that knows the sender's schema, does not.
func isUpgradeExitFor(msg ipc.Message, dataDir string, pid, known int) bool {
	return msg.Type == ipcUpgradeExit && msg.Content != "" && msg.PID != pid && msg.Schema > known && msg.Content == dataFolderKey(dataDir)
}

// onUpgradeExit is a TUI's or serve's first look at an IPC message: it
// says whether msg asks this process (pid, on dataDir, knowing the schema
// known) to exit for an upgrade, and calls exit the first time one does.
func onUpgradeExit(dataDir string, pid, known int, exit func()) func(ipc.Message) bool {
	var once sync.Once
	return func(msg ipc.Message) bool {
		if !isUpgradeExitFor(msg, dataDir, pid, known) {
			return false
		}
		once.Do(exit)
		return true
	}
}

// serveExitForUpgrade is how `legato serve` exits for an upgrade: it shuts
// down as on SIGTERM and exits with exitRestart, so systemd (or the
// hot-reload supervisor) starts it again on the new binary.
func serveExitForUpgrade(signalSelf func(), requested *atomic.Bool) func() {
	return func() {
		log.Print(tui.LegatoUpdatedNotice)
		requested.Store(true)
		signalSelf()
	}
}

// dataFolderKey names a data folder the same way from every process.
func dataFolderKey(dir string) string {
	if abs, err := filepath.Abs(dir); err == nil {
		dir = abs
	}
	if resolved, err := filepath.EvalSymlinks(dir); err == nil {
		dir = resolved
	}
	return filepath.Clean(dir)
}

// upgradeOpener opens a store, running the upgrade restart first when the
// database has migrations pending.
type upgradeOpener struct {
	dataDir   string
	pid       int
	broadcast func(ipc.Message)
	wait      time.Duration
	poll      time.Duration
}

// newUpgradeOpener is the opener of this process, broadcasting over IPC.
func newUpgradeOpener(dataDir string) upgradeOpener {
	return upgradeOpener{dataDir: dataDir, pid: os.Getpid(), broadcast: ipc.Broadcast, wait: upgradeExitWait, poll: 100 * time.Millisecond}
}

// open opens the database options name, migrated. With nothing pending, or
// a new database (which no other process can be using), it migrates at
// once; otherwise it runs the upgrade restart first.
func (o upgradeOpener) open(ctx context.Context, options store.Options) (*store.Store, error) {
	fresh := true
	if info, err := os.Stat(options.Path); err == nil && info.Size() > 0 {
		fresh = false
	}
	u, err := store.OpenWithoutMigrating(options)
	if err != nil {
		return nil, err
	}
	pending, err := u.Pending(ctx)
	if err != nil {
		u.Close()
		return nil, err
	}
	if len(pending) == 0 || fresh {
		return u.Migrate(ctx)
	}

	log.Printf("upgrade: %d migrations pending (newest %s); asking the other Legato processes on %s to exit", len(pending), pending[len(pending)-1], o.dataDir)
	o.broadcast(upgradeExitMessage(o.dataDir, o.pid, store.SupportedSchema()))
	release, exited := awaitLegatoProcessesExit(ctx, o.dataDir, o.wait, o.poll, func() bool {
		// Another new process may migrate first; then there is nothing
		// to wait for.
		still, err := u.Pending(ctx)
		return err != nil || len(still) > 0
	})
	defer release()
	if !exited {
		if still, err := u.Pending(ctx); err == nil && len(still) == 0 {
			log.Printf("upgrade: another Legato process migrated the database first")
		} else {
			log.Printf("upgrade: other Legato processes on %s still run after %s; migrating anyway", o.dataDir, o.wait)
		}
	}

	backup, err := u.Backup(ctx)
	if err != nil {
		u.Close()
		return nil, fmt.Errorf("backing up the database before migrating it: %w", err)
	}
	if backup != "" {
		log.Printf("upgrade: the database before the migration is at %s", backup)
	}
	return u.Migrate(ctx)
}

// holdLegatoProcess takes this process's shared hold on the data folder's
// process lock, which an upgrade waits on, and returns its release. A hold
// it cannot take (an upgrade migrating right now, past a few seconds) is
// logged and skipped: the process runs, and an upgrade just does not wait
// for it.
func holdLegatoProcess(dataDir string) func() {
	file, err := openProcessLock(dataDir)
	if err != nil {
		log.Printf("process lock: %v", err)
		return func() {}
	}
	deadline := time.Now().Add(10 * time.Second)
	for {
		err := syscall.Flock(int(file.Fd()), syscall.LOCK_SH|syscall.LOCK_NB)
		if err == nil {
			return func() { file.Close() }
		}
		if !errors.Is(err, syscall.EWOULDBLOCK) || time.Now().After(deadline) {
			log.Printf("process lock: %v", err)
			file.Close()
			return func() {}
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// awaitLegatoProcessesExit waits, at most wait, until no other process
// holds the data folder's process lock, polling every poll, and holds it
// exclusively then (so no process starts its hold during the migration).
// It stops early, without the lock, when pending says there is nothing
// left to migrate. exited says whether the others are gone; release lets
// the lock go.
func awaitLegatoProcessesExit(ctx context.Context, dataDir string, wait, poll time.Duration, pending func() bool) (release func(), exited bool) {
	file, err := openProcessLock(dataDir)
	if err != nil {
		log.Printf("process lock: %v", err)
		return func() {}, false
	}
	deadline := time.Now().Add(wait)
	for {
		if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err == nil {
			return func() { file.Close() }, true
		}
		if !pending() || ctx.Err() != nil || !time.Now().Before(deadline) {
			file.Close()
			return func() {}, false
		}
		time.Sleep(min(poll, time.Until(deadline)))
	}
}

func openProcessLock(dataDir string) (*os.File, error) {
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		return nil, err
	}
	return os.OpenFile(filepath.Join(dataDir, processLockFile), os.O_CREATE|os.O_RDWR, 0o600)
}
