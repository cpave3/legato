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

	return run()
}
