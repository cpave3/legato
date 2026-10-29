// Package testenv provides process-level filesystem isolation for test suites
// that exercise Legato's CLI and runtime path discovery.
package testenv

import (
	"fmt"
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
	defer os.RemoveAll(root)

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

// sessionVars are what Legato gives an agent session that ties a process to
// that session and to the running Legato.
var sessionVars = []string{
	"LEGATO_TASK_ID", "LEGATO_EXECUTION_ID", "LEGATO_SOCKET", "LEGATO_BIN",
	"LEGATO_BROKER_SOCKET", "LEGATO_BROKER_TOKEN", "LEGATO_SANDBOXED",
	"LEGATO_GITHUB_API_URL", "LEGATO_GITHUB_TOKEN", "LEGATO_GH_REAL", "GH_TOKEN", "GH_CONFIG_DIR",
}
