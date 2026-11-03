package stackgraph_test

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/cpave3/legato/internal/engine/stackgraph"
)

// TestTheDocumentMatchesSTsOwnExport builds a stack with the real st and
// checks that Legato writes the document st graph export writes. It is
// skipped where st cannot export.
func TestTheDocumentMatchesSTsOwnExport(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if exec.CommandContext(ctx, "st", "graph", "export", "--help").Run() != nil {
		t.Skip("st graph export is not available")
	}
	r := newRepo(t)
	home := t.TempDir()
	st := func(args ...string) []byte {
		t.Helper()
		cmd := exec.Command("st", args...)
		cmd.Dir = r.dir
		cmd.Env = append(os.Environ(), "HOME="+home, "GIT_CONFIG_NOSYSTEM=1",
			"GIT_AUTHOR_NAME=a", "GIT_AUTHOR_EMAIL=a@b", "GIT_COMMITTER_NAME=a", "GIT_COMMITTER_EMAIL=a@b")
		out, err := cmd.Output()
		if err != nil {
			t.Fatalf("st %v: %v\n%s", args, err, out)
		}
		return out
	}
	st("new", "legato/t1-a")
	r.git("commit", "-q", "--allow-empty", "-m", "a")
	st("append", "legato/t1-b")
	r.git("commit", "-q", "--allow-empty", "-m", "b")
	r.git("checkout", "-q", "legato/t1-a")
	st("append", "legato/t1-c")
	r.git("commit", "-q", "--allow-empty", "-m", "c")
	r.git("push", "-q", "origin", "legato/t1-a", "legato/t1-b", "legato/t1-c")

	theirs := st("graph", "export", "--all")
	s, err := stackgraph.Read(r.dir)
	if err != nil {
		t.Fatal(err)
	}
	ours, err := s.Document()
	if err != nil {
		t.Fatal(err)
	}
	normal := func(data []byte) string {
		var doc map[string]any
		if err := json.Unmarshal(data, &doc); err != nil {
			t.Fatalf("%v\n%s", err, data)
		}
		delete(doc, "st_version")
		out, _ := json.Marshal(doc)
		return string(out)
	}
	if normal(ours) != normal(theirs) {
		t.Errorf("Legato's document differs from st's:\nours   %s\ntheirs %s", normal(ours), normal(theirs))
	}
}
