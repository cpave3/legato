package codexbridge

import (
	"errors"
	"io/fs"
	"testing"
)

func TestALockfileIsWithdrawnOnlyByTheBridgeItAdvertises(t *testing.T) {
	dir := t.TempDir()
	if _, err := ReadLockfile(dir, "task/1"); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("reading a lockfile nobody wrote: %v", err)
	}
	first := Lockfile{PID: 11, Port: 4000, SessionID: "task/1", URL: "http://127.0.0.1:4000/one", Version: Version}
	if err := WriteLockfile(dir, "task/1", first); err != nil {
		t.Fatal(err)
	}
	if got, err := ReadLockfile(dir, "task/1"); err != nil || got != first {
		t.Fatalf("lockfile = %+v, %v", got, err)
	}

	// A bridge that replaced the first one is not withdrawn by it.
	second := Lockfile{PID: 12, Port: 4001, SessionID: "task/1", URL: "http://127.0.0.1:4001/two", Version: Version}
	if err := WriteLockfile(dir, "task/1", second); err != nil {
		t.Fatal(err)
	}
	RemoveLockfile(dir, "task/1", first.URL)
	if got, err := ReadLockfile(dir, "task/1"); err != nil || got != second {
		t.Fatalf("after the first bridge left: %+v, %v", got, err)
	}

	RemoveLockfile(dir, "task/1", second.URL)
	if _, err := ReadLockfile(dir, "task/1"); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("after its own bridge left: %v", err)
	}
}
