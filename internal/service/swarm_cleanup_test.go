package service

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/cpave3/legato/internal/engine/swarm"
)

// TestFinishCleansUpRuntimeFiles verifies that Finish removes the conductor's
// agent dir, each subtask's agent dir, and plan files matching parentID-*.yaml,
// while leaving unrelated files untouched.
func TestFinishCleansUpRuntimeFiles(t *testing.T) {
	legatoHome := t.TempDir()
	t.Setenv("LEGATO_HOME", legatoHome)

	sw, _, st, _ := newTestSwarmService(t)
	seedParentTask(t, st, "parent-1")
	seedSubtask(t, st, "st-aaaaaaa01", "parent-1", "done")
	seedSubtask(t, st, "st-aaaaaaa02", "parent-1", "done")

	// Create stub files via the new path helpers so each dir exists.
	parentDir, err := swarm.AgentDir("parent-1")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(parentDir, "role-prompt.md"), []byte("conductor role"), 0o600); err != nil {
		t.Fatal(err)
	}

	for _, id := range []string{"st-aaaaaaa01", "st-aaaaaaa02"} {
		d, err := swarm.AgentDir(id)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(d, "brief.md"), []byte("brief"), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	plansDir, err := swarm.PlansDir()
	if err != nil {
		t.Fatal(err)
	}
	planFile := filepath.Join(plansDir, "parent-1-12345.yaml")
	if err := os.WriteFile(planFile, []byte("plan"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Unrelated plan file belonging to another parent.
	otherPlan := filepath.Join(plansDir, "other-67890.yaml")
	if err := os.WriteFile(otherPlan, []byte("other plan"), 0o644); err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	if err := sw.Finish(ctx, "parent-1", "all done"); err != nil {
		t.Fatalf("Finish: %v", err)
	}

	// Verify cleanup.
	for _, p := range []string{parentDir, filepath.Join(legatoHome, "agents", "st-aaaaaaa01"), filepath.Join(legatoHome, "agents", "st-aaaaaaa02")} {
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Errorf("expected %s to be removed", p)
		}
	}
	if _, err := os.Stat(planFile); !os.IsNotExist(err) {
		t.Error("expected plan file to be removed")
	}
	if _, err := os.Stat(otherPlan); err != nil {
		t.Errorf("expected unrelated plan file to remain: %v", err)
	}
}
