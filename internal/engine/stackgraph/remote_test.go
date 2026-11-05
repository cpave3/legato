package stackgraph_test

import (
	"reflect"
	"testing"

	"github.com/cpave3/legato/internal/engine/stackgraph"
)

func TestOnRemoteNamesTheBranchesOriginHas(t *testing.T) {
	r := newRepo(t)
	r.branch("legato/t1", "main")
	r.git("push", "-q", "origin", "legato/t1")
	r.git("commit", "-q", "--allow-empty", "-m", "more, not pushed")
	r.branch("legato/t1-b", "legato/t1")
	r.branch("legato/t1-c", "legato/t1-b")
	r.git("push", "-q", "origin", "legato/t1-c")
	r.git("pack-refs", "--all") // one way or the other, both are read

	got, err := stackgraph.OnRemote(r.dir, "legato/t1", "legato/t1-b", "legato/t1-c", "../escape", "legato/none")
	if err != nil {
		t.Fatal(err)
	}
	// A branch with commits since its push is still on the remote.
	if want := []string{"legato/t1", "legato/t1-c"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("on the remote: %v, want %v", got, want)
	}
}

func TestOnRemoteOfAFolderThatIsNoCloneIsAnError(t *testing.T) {
	if _, err := stackgraph.OnRemote(t.TempDir(), "main"); err == nil {
		t.Fatal("no error for a folder without .git")
	}
}
