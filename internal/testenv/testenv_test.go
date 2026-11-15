package testenv

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestRunIsolatesInheritedExtensionAndBrokerEnvironment(t *testing.T) {
	forbidden := []string{"CHIMERA_MOD_DIRS", "CHIMERA_SERVER_SECRET", "CLAUDE_CODE_PLUGIN_DIRS", "LEGATO_TASK_ID", "LEGATO_EXECUTION_ID", "LEGATO_BROKER_SOCKET", "LEGATO_BROKER_TOKEN", "BASH_ENV", "ZDOTDIR", "LEGATO_USER_BASH_ENV", "LEGATO_USER_ZDOTDIR", "TMUX"}
	if os.Getenv("TESTENV_FIXTURE_CHILD") == "1" {
		for _, key := range forbidden {
			if _, exists := os.LookupEnv(key); exists {
				t.Fatalf("fixture inherited %s", key)
			}
		}
		return
	}
	for _, key := range forbidden {
		t.Setenv(key, "nonsecret-canary")
	}
	for _, key := range []string{"HOME", "XDG_CONFIG_HOME", "XDG_DATA_HOME", "XDG_RUNTIME_DIR", "TMUX_TMPDIR", "LEGATO_HOME"} {
		t.Setenv(key, os.Getenv(key))
	}
	status := Run(func() int {
		if os.Getenv("TMUX_TMPDIR") != filepath.Join(filepath.Dir(os.Getenv("HOME")), "tmux") {
			t.Error("fixture does not have a private tmux socket directory")
			return 1
		}
		child := exec.Command(os.Args[0], "-test.run=^TestRunIsolatesInheritedExtensionAndBrokerEnvironment$")
		child.Env = append(os.Environ(), "TESTENV_FIXTURE_CHILD=1")
		if output, err := child.CombinedOutput(); err != nil {
			t.Errorf("fixture child: %s", output)
			return 1
		}
		return 0
	})
	if status != 0 {
		t.Fatal("test fixture inherited launching extension environment")
	}
}

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
