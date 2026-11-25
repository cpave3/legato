package main

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cpave3/legato/internal/engine/ipc"
	"github.com/cpave3/legato/internal/engine/store"
)

// databaseAtFirstMigration makes a database as Legato's first release left
// it: the first migration only, counted by user_version (before the ledger).
func databaseAtFirstMigration(t *testing.T, path string) {
	t.Helper()
	init, err := os.ReadFile(filepath.Join("..", "..", "internal", "engine", "store", "migrations", "001_init.sql"))
	if err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(string(init)); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("PRAGMA user_version = 1"); err != nil {
		t.Fatal(err)
	}
}

// newestMigration is the binary's newest migration, by file name.
func newestMigration(t *testing.T) string {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join("..", "..", "internal", "engine", "store", "migrations"))
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), ".sql") {
			names = append(names, entry.Name())
		}
	}
	sort.Strings(names)
	return names[len(names)-1]
}

// recorder stands in for the IPC broadcast: it keeps what was sent and
// runs react, the other processes' answer.
type recorder struct {
	mu    sync.Mutex
	sent  []ipc.Message
	react func(ipc.Message)
}

func (r *recorder) broadcast(msg ipc.Message) {
	r.mu.Lock()
	r.sent = append(r.sent, msg)
	r.mu.Unlock()
	if r.react != nil {
		r.react(msg)
	}
}

func (r *recorder) messages() []ipc.Message {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]ipc.Message(nil), r.sent...)
}

func countColumns(t *testing.T, path, name string) int {
	t.Helper()
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var n int
	if err := db.QueryRow(`SELECT count(*) FROM column_mappings WHERE column_name = ?`, name).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// A new binary that finds migrations pending asks the other Legato
// processes on its data folder to exit, waits for them, backs up the
// database, then migrates (spec LEG-559 M7, D13, AC 45).
func TestAnUpgradeAsksTheOthersToExitThenWaitsThenBacksUpThenMigrates(t *testing.T) {
	ctx := context.Background()
	dataDir := t.TempDir()
	path := filepath.Join(dataDir, "legato.db")
	databaseAtFirstMigration(t, path)

	// An old process on the folder: it writes once more as it exits,
	// which the backup must hold.
	exitOld := holdLegatoProcess(dataDir)
	t.Cleanup(exitOld)
	otherPID := os.Getpid() + 1
	sent := &recorder{react: func(msg ipc.Message) {
		if !isUpgradeExitFor(msg, dataDir, otherPID, store.SupportedSchema()-1) {
			return
		}
		go func() {
			time.Sleep(150 * time.Millisecond)
			db, err := sql.Open("sqlite", path)
			if err == nil {
				_, err = db.Exec(`INSERT INTO column_mappings(column_name, jira_statuses) VALUES ('written on the way out', '[]')`)
				db.Close()
			}
			if err != nil {
				t.Error(err)
			}
			exitOld()
		}()
	}}

	db, err := upgradeOpener{dataDir: dataDir, pid: os.Getpid(), broadcast: sent.broadcast, wait: 30 * time.Second, poll: 10 * time.Millisecond}.open(ctx, store.Options{Path: path})
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	msgs := sent.messages()
	if len(msgs) != 1 {
		t.Fatalf("broadcast %d messages, want 1: %+v", len(msgs), msgs)
	}
	if isUpgradeExitFor(msgs[0], t.TempDir(), otherPID, store.SupportedSchema()-1) {
		t.Fatal("a Legato on another data folder would exit")
	}
	if isUpgradeExitFor(msgs[0], dataDir, os.Getpid(), store.SupportedSchema()-1) {
		t.Fatal("the upgrading process would exit on its own message")
	}

	backup := store.BackupPath(path, newestMigration(t))
	if n := countColumns(t, backup, "written on the way out"); n != 1 {
		t.Fatalf("the backup holds %d of the old process's last writes, want 1: it was taken before the old process exited", n)
	}
	old, err := store.OpenWithoutMigrating(store.Options{Path: backup})
	if err != nil {
		t.Fatal(err)
	}
	pending, err := old.Pending(ctx)
	old.Close()
	if err != nil || len(pending) == 0 {
		t.Fatalf("the backup has pending %v, %v: want the earlier schema", pending, err)
	}

	status, err := db.SchemaStatus(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if status.Current != status.Supported {
		t.Fatalf("schema = %+v, want migrated", status)
	}
}

// A process that does not exit holds the upgrade back only for the wait;
// then the backup and the migration go ahead.
func TestAnUpgradeWaitsAtMostItsWaitForTheOthers(t *testing.T) {
	ctx := context.Background()
	dataDir := t.TempDir()
	path := filepath.Join(dataDir, "legato.db")
	databaseAtFirstMigration(t, path)
	t.Cleanup(holdLegatoProcess(dataDir)) // never exits

	sent := &recorder{}
	wait := 300 * time.Millisecond
	start := time.Now()
	db, err := upgradeOpener{dataDir: dataDir, pid: os.Getpid(), broadcast: sent.broadcast, wait: wait, poll: 10 * time.Millisecond}.open(ctx, store.Options{Path: path})
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if elapsed := time.Since(start); elapsed < wait || elapsed > wait+10*time.Second {
		t.Fatalf("opened after %s, want about %s", elapsed, wait)
	}
	if len(sent.messages()) != 1 {
		t.Fatalf("broadcast %+v, want one message", sent.messages())
	}
	if _, err := os.Stat(store.BackupPath(path, newestMigration(t))); err != nil {
		t.Fatalf("no backup: %v", err)
	}
}

// With nothing to migrate, or a new database, no process is asked to exit
// and nothing is backed up.
func TestAnOpenWithNothingToMigrateAsksNoOneToExit(t *testing.T) {
	ctx := context.Background()
	dataDir := t.TempDir()
	current := filepath.Join(dataDir, "current.db")
	db, err := store.New(current)
	if err != nil {
		t.Fatal(err)
	}
	db.Close()
	fresh := filepath.Join(dataDir, "fresh.db")
	t.Cleanup(holdLegatoProcess(dataDir))

	for _, path := range []string{current, fresh} {
		sent := &recorder{}
		start := time.Now()
		db, err := upgradeOpener{dataDir: dataDir, pid: os.Getpid(), broadcast: sent.broadcast, wait: 30 * time.Second, poll: 10 * time.Millisecond}.open(ctx, store.Options{Path: path})
		if err != nil {
			t.Fatal(err)
		}
		db.Close()
		if msgs := sent.messages(); len(msgs) != 0 {
			t.Fatalf("%s: broadcast %+v, want nothing", filepath.Base(path), msgs)
		}
		if elapsed := time.Since(start); elapsed > 10*time.Second {
			t.Fatalf("%s: waited %s", filepath.Base(path), elapsed)
		}
	}
	backups, _ := filepath.Glob(filepath.Join(dataDir, "*.before-*"))
	if len(backups) != 0 {
		t.Fatalf("backups %v, want none", backups)
	}
}

// A TUI or serve exits once when a newer Legato on its own data folder
// asks; one on another folder, the sender itself, or one asked by a Legato
// no newer than itself, carries on.
func TestAProcessExitsOnceWhenANewerLegatoOnItsDataFolderAsks(t *testing.T) {
	dataDir := t.TempDir()
	pid := os.Getpid()
	exits := 0
	const known = 140
	handle := onUpgradeExit(dataDir, pid, known, func() { exits++ })

	for _, msg := range []ipc.Message{
		upgradeExitMessage(t.TempDir(), pid+1, known+1),
		upgradeExitMessage(dataDir, pid, known+1),
		// Another process of the same binary, which read the migrations
		// as pending just before this one ran them, asks too late.
		upgradeExitMessage(dataDir, pid+1, known),
		{Type: "task_update", Content: dataDir, PID: pid + 1},
	} {
		if handle(msg) {
			t.Fatalf("handled %+v", msg)
		}
	}
	if exits != 0 {
		t.Fatalf("exited %d times on messages not for it", exits)
	}

	for range 2 {
		if !handle(upgradeExitMessage(dataDir, pid+1, known+1)) {
			t.Fatal("the upgrade's message was not handled")
		}
	}
	if exits != 1 {
		t.Fatalf("exited %d times, want 1", exits)
	}
}

// serve, asked to exit for an upgrade, shuts down and exits with
// exitRestart, so systemd starts it again on the new binary.
func TestServeExitsForARestartWhenANewerLegatoAsks(t *testing.T) {
	var requested atomic.Bool
	signalled := false
	serveExitForUpgrade(func() { signalled = true }, &requested)()
	if !signalled || !requested.Load() {
		t.Fatalf("signalled %v, restart requested %v: want both", signalled, requested.Load())
	}
}

// olderBuildRestarts is how a Legato from before the upgrade restart reads
// an IPC message (v0.29.0's isRestartFor): it knows only `restart`, scoped
// by the config path, and on it re-executes the binary on disk, which after
// an update is the new one.
func olderBuildRestarts(msg ipc.Message, configPath string) bool {
	return msg.Type == "restart" && msg.Content != "" && filepath.Clean(msg.Content) == filepath.Clean(configPath)
}

// olderPeers stands in for the other Legato processes that listen for IPC
// and use the database: an older build holds no process lock, so the
// upgrade knows of it only this way.
type olderPeers struct {
	mu   sync.Mutex
	pids map[int]bool
}

func (p *olderPeers) list(string) []int {
	p.mu.Lock()
	defer p.mu.Unlock()
	var pids []int
	for pid := range p.pids {
		pids = append(pids, pid)
	}
	return pids
}

func (p *olderPeers) gone(pid int) {
	p.mu.Lock()
	delete(p.pids, pid)
	p.mu.Unlock()
}

// An older Legato knows neither upgrade_exit nor the process lock: the
// upgrade also sends it the `restart` it acts on, scoped as it scopes it,
// and waits for it to stop listening before it backs up and migrates.
func TestAnUpgradeTellsAnOlderLegatoToRestartAndWaitsForIt(t *testing.T) {
	ctx := context.Background()
	dataDir := t.TempDir()
	path := filepath.Join(dataDir, "legato.db")
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	databaseAtFirstMigration(t, path)

	olderPID := os.Getpid() + 1
	peers := &olderPeers{pids: map[int]bool{olderPID: true}}
	restarts := 0
	sent := &recorder{react: func(msg ipc.Message) {
		if !olderBuildRestarts(msg, configPath) {
			return
		}
		restarts++
		go func() {
			time.Sleep(150 * time.Millisecond)
			db, err := sql.Open("sqlite", path)
			if err == nil {
				_, err = db.Exec(`INSERT INTO column_mappings(column_name, jira_statuses) VALUES ('written by the older build', '[]')`)
				db.Close()
			}
			if err != nil {
				t.Error(err)
			}
			peers.gone(olderPID)
		}()
	}}

	db, err := upgradeOpener{dataDir: dataDir, configPath: configPath, pid: os.Getpid(), broadcast: sent.broadcast, peers: peers.list, wait: 30 * time.Second, poll: 10 * time.Millisecond}.open(ctx, store.Options{Path: path})
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	if restarts != 1 {
		t.Fatalf("the older build was asked to restart %d times, want 1: %+v", restarts, sent.messages())
	}
	if n := countColumns(t, store.BackupPath(path, newestMigration(t)), "written by the older build"); n != 1 {
		t.Fatalf("the backup holds %d of the older build's last writes, want 1: the upgrade did not wait for it", n)
	}
}

// An older Legato that never restarts holds the upgrade back only for the
// wait.
func TestAnUpgradeWaitsAtMostItsWaitForAnOlderLegato(t *testing.T) {
	ctx := context.Background()
	dataDir := t.TempDir()
	path := filepath.Join(dataDir, "legato.db")
	databaseAtFirstMigration(t, path)
	peers := &olderPeers{pids: map[int]bool{os.Getpid() + 1: true}} // never goes

	wait := 300 * time.Millisecond
	start := time.Now()
	db, err := upgradeOpener{dataDir: dataDir, configPath: filepath.Join(dataDir, "config.yaml"), pid: os.Getpid(), broadcast: (&recorder{}).broadcast, peers: peers.list, wait: wait, poll: 10 * time.Millisecond}.open(ctx, store.Options{Path: path})
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if elapsed := time.Since(start); elapsed < wait || elapsed > wait+10*time.Second {
		t.Fatalf("opened after %s, want about %s", elapsed, wait)
	}
	if status, err := db.SchemaStatus(ctx); err != nil || status.Current != status.Supported {
		t.Fatalf("schema = %+v, %v: want migrated", status, err)
	}
}

// A Legato of this build gets the upgrade's `restart` too: it neither
// restarts nor exits on it (upgrade_exit is the message for it), so a
// process that already knows the new schema never loops, and the upgrade
// is not stopped.
func TestALegatoOfThisBuildIgnoresTheUpgradesRestart(t *testing.T) {
	dataDir := t.TempDir()
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	pid := os.Getpid()
	exits := 0
	const known = 140
	handle := onUpgradeExit(dataDir, pid, known, func() { exits++ })

	for _, schema := range []int{known, known + 1} {
		msg := upgradeRestartMessage(configPath, pid+1, schema)
		if !olderBuildRestarts(msg, configPath) {
			t.Fatalf("an older build would not act on %+v", msg)
		}
		if !handle(msg) {
			t.Fatalf("%+v would reach the restart handler", msg)
		}
	}
	if exits != 0 {
		t.Fatalf("exited %d times on the upgrade's restart", exits)
	}
	if handle(restartMessage(configPath)) {
		t.Fatal("`legato restart` no longer restarts a Legato of this build")
	}
}

// The upgrade waits for the other processes listening on a Legato socket
// that use its database: not for itself, and not for a Legato of another
// instance sharing the runtime folder.
func TestAnUpgradeWaitsOnlyForTheProcessesOnItsDatabase(t *testing.T) {
	const self, onIt, elsewhere = 10, 11, 12
	uses := func(pid int, path string) bool { return path == "/data/legato.db" && pid != elsewhere }
	got := upgradePeers([]int{self, onIt, elsewhere}, self, "/data/legato.db", uses)
	if len(got) != 1 || got[0] != onIt {
		t.Fatalf("waits for %v, want [%d]", got, onIt)
	}
}

// An older build told to restart comes back on the new binary while the
// first upgrade still waits, and finds the migrations pending too. It
// waits behind the first upgrade and broadcasts nothing: a second
// `restart` could reach an older build already shutting down, whose
// second SIGTERM would end it without its restart.
func TestASecondUpgradeWaitsBehindTheFirstAndBroadcastsNothing(t *testing.T) {
	ctx := context.Background()
	dataDir := t.TempDir()
	path := filepath.Join(dataDir, "legato.db")
	databaseAtFirstMigration(t, path)
	releaseFirst := holdUpgrade(t, dataDir)

	sent := &recorder{}
	done := make(chan error, 1)
	go func() {
		db, err := upgradeOpener{dataDir: dataDir, configPath: filepath.Join(dataDir, "config.yaml"), pid: os.Getpid(), broadcast: sent.broadcast, wait: 30 * time.Second, poll: 10 * time.Millisecond}.open(ctx, store.Options{Path: path})
		if err == nil {
			db.Close()
		}
		done <- err
	}()

	time.Sleep(200 * time.Millisecond)
	select {
	case err := <-done:
		t.Fatalf("opened (%v) while the first upgrade ran", err)
	default:
	}
	// The first upgrade migrates and lets go.
	first, err := store.New(path)
	if err != nil {
		t.Fatal(err)
	}
	first.Close()
	releaseFirst()

	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the second upgrade never opened")
	}
	if msgs := sent.messages(); len(msgs) != 0 {
		t.Fatalf("the second upgrade broadcast %+v, want nothing", msgs)
	}
}

// holdUpgrade holds the data folder's upgrade lock as a running upgrade
// does, until the returned release.
func holdUpgrade(t *testing.T, dataDir string) func() {
	t.Helper()
	release, waited := beginUpgrade(context.Background(), dataDir, time.Second, 10*time.Millisecond)
	if waited {
		t.Fatal("another upgrade held the lock")
	}
	var once sync.Once
	stop := func() { once.Do(release) }
	t.Cleanup(stop)
	return stop
}
