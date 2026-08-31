package chimera

import (
	"context"
	"slices"
	"testing"
)

func TestSessionRemoverUsesExactIdempotentStructuredCommand(t *testing.T) {
	remover := NewSessionRemover("/opt/chimera")
	var binary string
	var args []string
	remover.run = func(_ context.Context, name string, commandArgs ...string) ([]byte, error) {
		binary = name
		args = append([]string(nil), commandArgs...)
		return []byte(`{"removed":false}`), nil
	}
	removed, err := remover.Remove(context.Background(), "chimera-full-id")
	if err != nil {
		t.Fatal(err)
	}
	if removed || binary != "/opt/chimera" {
		t.Fatalf("removed=%v binary=%q", removed, binary)
	}
	want := []string{"sessions", "rm", "chimera-full-id", "--exact", "--if-exists", "--json"}
	if !slices.Equal(args, want) {
		t.Fatalf("args=%v want=%v", args, want)
	}
}
