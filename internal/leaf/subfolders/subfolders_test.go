package subfolders

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestListNamesTheFoldersInAFolderFollowingLinks(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"b", "a", ".hidden"} {
		if err := os.Mkdir(filepath.Join(dir, name), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "file.md"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(t.TempDir(), filepath.Join(dir, "linked")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(dir, "missing"), filepath.Join(dir, "dangling")); err != nil {
		t.Fatal(err)
	}

	got, err := List(dir)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"a", "b", "linked"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("List = %v, want %v", got, want)
	}
}

func TestListOfAMissingFolderIsEmpty(t *testing.T) {
	got, err := List(filepath.Join(t.TempDir(), "absent"))
	if err != nil || len(got) != 0 {
		t.Fatalf("List = %v, %v; want nothing and no error", got, err)
	}
}
