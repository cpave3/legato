package main

import (
	"os/exec"
	"path/filepath"
	"testing"
)

// scripts/stack-status.cjs posts ci/stack, the commit status that stands for
// the CI of the PRs below the top of a GitHub stack (LEG-629). Its tests are
// node's; this runs them with the Go suite.
func TestStackStatusScript(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node is not installed")
	}
	test := filepath.Join("..", "..", "scripts", "stack-status.test.cjs")
	out, err := exec.Command(node, "--test", test).CombinedOutput()
	if err != nil {
		t.Fatalf("node --test %s: %v\n%s", test, err, out)
	}
}
