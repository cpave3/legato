// Package testenv provides process-level filesystem isolation for test suites
// that exercise Legato's CLI and runtime path discovery.
package testenv

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// Run creates a unique, short-lived environment root, points all supported
// user and runtime path variables into it, runs the test suite, and cleans up.
func Run(run func() int) int {
	root, err := os.MkdirTemp("/tmp", "legato-test-")
	if err != nil {
		fmt.Fprintf(os.Stderr, "create isolated test environment: %v\n", err)
		return 1
	}
	defer removeAll(root)
	keepGoCaches()

	env := map[string]string{
		"HOME":            filepath.Join(root, "home"),
		"XDG_CONFIG_HOME": filepath.Join(root, "config"),
		"XDG_DATA_HOME":   filepath.Join(root, "data"),
		"XDG_RUNTIME_DIR": filepath.Join(root, "run"),
		"LEGATO_HOME":     filepath.Join(root, "legato"),
	}
	for key, value := range env {
		if err := os.MkdirAll(value, 0o700); err != nil {
			fmt.Fprintf(os.Stderr, "create isolated %s: %v\n", key, err)
			return 1
		}
		if err := os.Setenv(key, value); err != nil {
			fmt.Fprintf(os.Stderr, "set isolated %s: %v\n", key, err)
			return 1
		}
	}
	os.Unsetenv("LEGATO_CONFIG")
	// A suite run from inside an agent session must not act as that session
	// or reach the Legato that runs it.
	for _, key := range sessionVars {
		os.Unsetenv(key)
	}

	return run()
}

// keepGoCaches pins Go's settings file and its module and build caches to
// where they are before HOME and XDG_CONFIG_HOME move, so a suite that runs go
// keeps using them instead of filling fresh caches in the test root on every
// run.
func keepGoCaches() {
	if os.Getenv("GOENV") == "" {
		if config, err := os.UserConfigDir(); err == nil {
			os.Setenv("GOENV", filepath.Join(config, "go", "env"))
		}
	}
	if os.Getenv("GOPATH") == "" {
		if home, err := os.UserHomeDir(); err == nil {
			os.Setenv("GOPATH", filepath.Join(home, "go"))
		}
	}
	if os.Getenv("GOCACHE") == "" {
		if cache, err := os.UserCacheDir(); err == nil {
			os.Setenv("GOCACHE", filepath.Join(cache, "go-build"))
		}
	}
}

// removeAll removes the test root even where a suite left folders read-only,
// as Go does with its module cache.
func removeAll(root string) {
	if os.RemoveAll(root) == nil {
		return
	}
	filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err == nil && d.IsDir() {
			os.Chmod(path, 0o700)
		}
		return nil
	})
	os.RemoveAll(root)
}

// sessionVars are what Legato gives an agent session that ties a process to
// that session and to the running Legato.
var sessionVars = []string{
	"LEGATO_TASK_ID", "LEGATO_EXECUTION_ID", "LEGATO_SOCKET", "LEGATO_BIN",
	"LEGATO_BROKER_SOCKET", "LEGATO_BROKER_TOKEN", "LEGATO_SANDBOXED",
	"LEGATO_GITHUB_API_URL", "LEGATO_GITHUB_TOKEN", "LEGATO_GH_REAL", "GH_TOKEN", "GH_CONFIG_DIR",
}
