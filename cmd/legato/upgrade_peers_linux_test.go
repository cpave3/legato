package main

import (
	"os"
	"path/filepath"
	"testing"
)

// On Linux a process uses the database when it has the file, or its WAL,
// open.
func TestUsesFileSeesWhatAProcessHasOpen(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "legato.db")
	if usesFile(os.Getpid(), path) {
		t.Fatal("uses a file it has not opened")
	}
	f, err := os.Create(path + "-wal")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if !usesFile(os.Getpid(), path) {
		t.Fatal("does not see the open WAL")
	}
}
