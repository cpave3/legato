package store

import (
	"regexp"
	"testing"
)

func TestGenerateTaskIDFormat(t *testing.T) {
	id := GenerateTaskID()
	if len(id) != 8 {
		t.Errorf("ID length = %d, want 8", len(id))
	}
	if !regexp.MustCompile(`^[a-z0-9]{8}$`).MatchString(id) {
		t.Errorf("ID %q does not match [a-z0-9]{8}", id)
	}
}

func TestGenerateTaskIDNoCollisions(t *testing.T) {
	seen := make(map[string]bool, 1000)
	for i := 0; i < 1000; i++ {
		id := GenerateTaskID()
		if seen[id] {
			t.Fatalf("collision at iteration %d: %s", i, id)
		}
		seen[id] = true
	}
}
