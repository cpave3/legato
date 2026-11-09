package logfile_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cpave3/legato/internal/engine/logfile"
)

func TestWriterAppendsToTheFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "logs", "legato.log")
	w, err := logfile.Open(path, logfile.Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	if _, err := w.Write([]byte("first line\n")); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(path)
	if string(got) != "first line\n" {
		t.Fatalf("log file = %q", got)
	}
}

func TestWriterRotatesPastItsCapAndKeepsAFew(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "legato.log")
	w, err := logfile.Open(path, logfile.Options{MaxBytes: 20, Keep: 2})
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	for _, line := range []string{"aaaaaaaaaaaaaaa\n", "bbbbbbbbbbbbbbb\n", "ccccccccccccccc\n", "ddddddddddddddd\n"} {
		if _, err := w.Write([]byte(line)); err != nil {
			t.Fatal(err)
		}
	}
	read := func(name string) string {
		b, _ := os.ReadFile(filepath.Join(dir, name))
		return string(b)
	}
	if got := read("legato.log"); !strings.HasPrefix(got, "ddd") {
		t.Fatalf("current file = %q, want the newest line", got)
	}
	if got := read("legato.log.1"); !strings.HasPrefix(got, "ccc") {
		t.Fatalf("legato.log.1 = %q", got)
	}
	if got := read("legato.log.2"); !strings.HasPrefix(got, "bbb") {
		t.Fatalf("legato.log.2 = %q", got)
	}
	if _, err := os.Stat(filepath.Join(dir, "legato.log.3")); !os.IsNotExist(err) {
		t.Fatalf("legato.log.3 kept, want only 2 old files")
	}
}

func TestWriterContinuesAnExistingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legato.log")
	os.WriteFile(path, []byte("before\n"), 0o600)
	w, err := logfile.Open(path, logfile.Options{})
	if err != nil {
		t.Fatal(err)
	}
	w.Write([]byte("after\n"))
	w.Close()
	got, _ := os.ReadFile(path)
	if string(got) != "before\nafter\n" {
		t.Fatalf("log file = %q", got)
	}
}

func TestPathIsUnderTheDataFolder(t *testing.T) {
	if got := logfile.Path("/data"); got != filepath.Join("/data", "logs", "legato.log") {
		t.Fatalf("Path = %q", got)
	}
}
