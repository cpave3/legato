package testenv

import (
	"os"
	"path/filepath"
	"testing"
)

// A suite that builds Go code must keep using the person's Go caches. With
// HOME moved into the test root, Go would fill a fresh module cache there on
// every run, hundreds of megabytes downloaded again each time.
func TestRunKeepsGosCachesOutsideTheTestRoot(t *testing.T) {
	t.Setenv("GOPATH", "")
	t.Setenv("GOCACHE", "")
	t.Setenv("GOMODCACHE", "")
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no home directory")
	}
	cache, err := os.UserCacheDir()
	if err != nil {
		t.Skip("no cache directory")
	}

	var gopath, gocache, root string
	Run(func() int {
		root = filepath.Dir(os.Getenv("HOME"))
		gopath, gocache = os.Getenv("GOPATH"), os.Getenv("GOCACHE")
		return 0
	})
	t.Cleanup(func() { os.Setenv("HOME", home) })

	if gopath != filepath.Join(home, "go") {
		t.Errorf("GOPATH = %q, want %q", gopath, filepath.Join(home, "go"))
	}
	if gocache != filepath.Join(cache, "go-build") {
		t.Errorf("GOCACHE = %q, want %q", gocache, filepath.Join(cache, "go-build"))
	}
	if root == "" {
		t.Fatal("run never saw the test root")
	}
}

// Go makes its module cache read-only, so a plain RemoveAll leaves anything
// like it behind. The test root must go whatever a suite wrote into it.
func TestRunRemovesReadOnlyFoldersItsSuiteMade(t *testing.T) {
	home, _ := os.UserHomeDir()
	t.Cleanup(func() { os.Setenv("HOME", home) })

	var root string
	Run(func() int {
		root = filepath.Dir(os.Getenv("HOME"))
		dir := filepath.Join(os.Getenv("HOME"), "go", "pkg", "mod", "example.com@v1")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com\n"), 0o444); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(dir, 0o555); err != nil {
			t.Fatal(err)
		}
		return 0
	})

	if _, err := os.Stat(root); !os.IsNotExist(err) {
		t.Fatalf("test root %s still there after Run (err %v)", root, err)
	}
}
