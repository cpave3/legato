package main

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// scripts/migration-order.sh is how CI keeps a new migration from sorting
// before one already released or on main: a database's ledger records
// migrations by position, so one that sorts in the middle breaks every
// database made before it (LEG-249).
func TestMigrationOrder(t *testing.T) {
	script := filepath.Join("..", "..", "scripts", "migration-order.sh")
	released := []string{"001_init.sql", "116_skill_states.sql", "118_oauth_attempts.sql", "119_mcp_connections.sql"}
	cases := []struct {
		name    string
		current []string
		refused string // the file the output must name; "" when it passes
	}{
		{"nothing new passes", released, ""},
		{"a new migration that sorts last passes", append(released[:4:4], "120_mcp_stdio.sql"), ""},
		{"one with the newest number but a later name passes", append(released[:4:4], "119_n_later.sql"), ""},
		{"one that sorts before the newest fails", append(released[:4:4], "117_agent_docker_host_network.sql"), "117_agent_docker_host_network.sql"},
		{"one with the newest number and an earlier name fails", append(released[:4:4], "119_a_earlier.sql"), "119_a_earlier.sql"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dir := t.TempDir()
			releasedFile := filepath.Join(dir, "released")
			currentFile := filepath.Join(dir, "current")
			if err := os.WriteFile(releasedFile, []byte(strings.Join(released, "\n")+"\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(currentFile, []byte(strings.Join(c.current, "\n")+"\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			out, err := exec.Command("bash", script, "--lists", releasedFile, currentFile).CombinedOutput()
			var exit *exec.ExitError
			switch {
			case c.refused == "" && err != nil:
				t.Errorf("want a pass, got %v:\n%s", err, out)
			case c.refused != "" && !(errors.As(err, &exit) && exit.ExitCode() == 1):
				t.Errorf("want exit 1, got %v:\n%s", err, out)
			case c.refused != "" && (!strings.Contains(string(out), c.refused) || !strings.Contains(string(out), "rename it") || !strings.Contains(string(out), "119_mcp_connections.sql")):
				t.Errorf("output does not name %q, the newest and the rename:\n%s", c.refused, out)
			}
		})
	}
}

func TestMigrationOrderRefusesNoArguments(t *testing.T) {
	script := filepath.Join("..", "..", "scripts", "migration-order.sh")
	out, err := exec.Command("bash", script).CombinedOutput()
	var exit *exec.ExitError
	if !errors.As(err, &exit) || exit.ExitCode() != 2 {
		t.Errorf("want exit 2 with no arguments, got %v: %s", err, out)
	}
}
