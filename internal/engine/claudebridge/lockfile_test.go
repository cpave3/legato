package claudebridge

import (
	"errors"
	"io/fs"
	"os"
	"testing"
)

func TestLockfileAdvertisesTheBridgeForItsTask(t *testing.T) {
	dir := t.TempDir()
	written := Lockfile{PID: 4242, Port: 38123, CWD: "/work/repo", SessionID: "REX-12", URL: "http://127.0.0.1:38123/tok", StartedAt: 1_700_000_000_000}

	if err := WriteLockfile(dir, "REX-12", written); err != nil {
		t.Fatal(err)
	}

	read, err := ReadLockfile(dir, "REX-12")
	if err != nil {
		t.Fatal(err)
	}
	if read != written {
		t.Fatalf("read = %+v, want %+v", read, written)
	}
	info, err := os.Stat(LockfilePath(dir, "REX-12"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("lockfile mode = %v, want private: it holds the endpoint secret", info.Mode().Perm())
	}
}

func TestLockfileForAnotherTaskIsNotFound(t *testing.T) {
	dir := t.TempDir()
	if err := WriteLockfile(dir, "task-a", Lockfile{URL: "http://127.0.0.1:1/x"}); err != nil {
		t.Fatal(err)
	}

	if _, err := ReadLockfile(dir, "task-b"); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("err = %v, want not-exist", err)
	}
}

func TestLockfileIsRemovedOnlyByTheBridgeThatOwnsIt(t *testing.T) {
	dir := t.TempDir()
	if err := WriteLockfile(dir, "task-a", Lockfile{URL: "http://127.0.0.1:2/new"}); err != nil {
		t.Fatal(err)
	}

	// An older bridge shutting down must not unpublish its replacement.
	RemoveLockfile(dir, "task-a", "http://127.0.0.1:1/old")
	if _, err := ReadLockfile(dir, "task-a"); err != nil {
		t.Fatalf("replacement lockfile was removed: %v", err)
	}

	RemoveLockfile(dir, "task-a", "http://127.0.0.1:2/new")
	if _, err := ReadLockfile(dir, "task-a"); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("err = %v, want the owner's lockfile gone", err)
	}
}

func TestTaskIDsCannotEscapeTheLockfileDirectory(t *testing.T) {
	dir := t.TempDir()
	if err := WriteLockfile(dir, "../../evil", Lockfile{URL: "http://127.0.0.1:1/x"}); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(LockfileDir(dir))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("entries = %v, want the lockfile inside its directory", entries)
	}
}
