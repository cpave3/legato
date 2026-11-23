package gitrepo

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

const zeroID = "0000000000000000000000000000000000000000"

// pushRig is a repository's cache (a bare clone, as Legato keeps) and an
// agent's clone of it, from which pushes are made.
type pushRig struct {
	cache, work string
}

func newPushRig(t *testing.T) pushRig {
	t.Helper()
	requireGit(t)
	work := t.TempDir()
	run(t, work, "init", "-q", "-b", "main")
	write(t, work, "README", "hello\n")
	write(t, work, ".github/workflows/ci.yml", ciYAML("test"))
	run(t, work, "add", ".")
	run(t, work, "commit", "-q", "-m", "first")
	cache := filepath.Join(t.TempDir(), "legato.git")
	run(t, "", "clone", "-q", "--bare", work, cache)
	return pushRig{cache: cache, work: work}
}

func write(t *testing.T, dir, name, content string) {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// ciYAML is a workflow long enough that git sends a change to it as a
// delta against the cache's copy.
func ciYAML(step string) string {
	var b strings.Builder
	b.WriteString("name: ci\non: push\njobs:\n  build:\n    runs-on: ubuntu-latest\n    steps:\n")
	for i := 0; i < 40; i++ {
		b.WriteString("      - run: echo step " + strings.Repeat("x", i) + "\n")
	}
	b.WriteString("      - run: " + step + "\n")
	return b.String()
}

func (p pushRig) commit(t *testing.T, msg string) string {
	t.Helper()
	run(t, p.work, "add", "-A")
	run(t, p.work, "commit", "-q", "--allow-empty", "-m", msg)
	return run(t, p.work, "rev-parse", "HEAD")
}

// pack is what git sends for a push of tip: a thin pack of the objects
// the cache's refs do not reach, as git send-pack makes it.
func (p pushRig) pack(t *testing.T, tip string) []byte {
	t.Helper()
	refs := run(t, "", "--git-dir", p.cache, "for-each-ref", "--format=%(objectname)")
	input := tip + "\n"
	for _, ref := range strings.Fields(refs) {
		input += "^" + ref + "\n"
	}
	cmd := exec.Command("git", "pack-objects", "--revs", "--thin", "--stdout", "-q")
	cmd.Dir = p.work
	cmd.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null")
	cmd.Stdin = strings.NewReader(input)
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("pack-objects: %v", err)
	}
	return out
}

func (p pushRig) changes(t *testing.T, pack []byte, updates ...RefUpdate) map[string][]string {
	t.Helper()
	got, err := Runner{}.ChangedUnder(context.Background(), p.cache, t.TempDir(), bytes.NewReader(pack), updates, ".github/workflows")
	if err != nil {
		t.Fatalf("ChangedUnder: %v", err)
	}
	return got
}

func TestChangedUnderNamesAWorkflowANewBranchAdds(t *testing.T) {
	p := newPushRig(t)
	run(t, p.work, "checkout", "-q", "-b", "legato/x")
	write(t, p.work, ".github/workflows/release.yml", "name: release\n")
	tip := p.commit(t, "release")

	got := p.changes(t, p.pack(t, tip), RefUpdate{Old: zeroID, New: tip, Ref: "refs/heads/legato/x"})
	want := map[string][]string{"refs/heads/legato/x": {".github/workflows/release.yml"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestChangedUnderLeavesOutARefThatChangesNothingThere(t *testing.T) {
	p := newPushRig(t)
	run(t, p.work, "checkout", "-q", "-b", "legato/x")
	write(t, p.work, "README", "changed\n")
	write(t, p.work, ".github/dependabot.yml", "version: 2\n")
	tip := p.commit(t, "docs")

	if got := p.changes(t, p.pack(t, tip), RefUpdate{Old: zeroID, New: tip, Ref: "refs/heads/legato/x"}); len(got) != 0 {
		t.Fatalf("got %v, want nothing", got)
	}
}

func TestChangedUnderReadsAChangeSentAsADeltaAgainstTheCache(t *testing.T) {
	p := newPushRig(t)
	base := run(t, p.work, "rev-parse", "HEAD")
	write(t, p.work, ".github/workflows/ci.yml", ciYAML("lint"))
	tip := p.commit(t, "lint")
	pack := p.pack(t, tip)
	// The pack is thin: it holds a delta whose base only the cache has.
	verify := exec.Command("git", "index-pack", "--stdin")
	verify.Dir = t.TempDir()
	run(t, verify.Dir, "init", "-q", "--bare")
	verify.Stdin = bytes.NewReader(pack)
	if out, err := verify.CombinedOutput(); err == nil {
		t.Fatalf("the pack is not thin, so this test proves nothing:\n%s", out)
	}

	got := p.changes(t, pack, RefUpdate{Old: base, New: tip, Ref: "refs/heads/legato/x"})
	want := map[string][]string{"refs/heads/legato/x": {".github/workflows/ci.yml"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestChangedUnderNamesAWorkflowAPushDeletes(t *testing.T) {
	p := newPushRig(t)
	base := run(t, p.work, "rev-parse", "HEAD")
	run(t, p.work, "rm", "-q", ".github/workflows/ci.yml")
	tip := p.commit(t, "no ci")

	got := p.changes(t, p.pack(t, tip), RefUpdate{Old: base, New: tip, Ref: "refs/heads/legato/x"})
	want := map[string][]string{"refs/heads/legato/x": {".github/workflows/ci.yml"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

// GitHub refuses a merge that brings a branch a workflow change main
// already has, though the pack holds no workflow object.
func TestChangedUnderNamesAWorkflowAMergeOfMainBringsIn(t *testing.T) {
	p := newPushRig(t)
	run(t, p.work, "checkout", "-q", "-b", "legato/x")
	write(t, p.work, "notes.txt", "work\n")
	branch := p.commit(t, "work")
	run(t, p.work, "checkout", "-q", "main")
	write(t, p.work, ".github/workflows/ci.yml", ciYAML("release"))
	p.commit(t, "ci on main")
	// The cache has both: main with its new workflow, and the branch as
	// pushed before.
	run(t, p.work, "push", "-q", p.cache, "main", "legato/x")
	run(t, p.work, "checkout", "-q", "legato/x")
	run(t, p.work, "merge", "-q", "--no-edit", "main")
	tip := run(t, p.work, "rev-parse", "HEAD")

	got := p.changes(t, p.pack(t, tip), RefUpdate{Old: branch, New: tip, Ref: "refs/heads/legato/x"})
	want := map[string][]string{"refs/heads/legato/x": {".github/workflows/ci.yml"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestChangedUnderFailsWhenTheCacheLacksThePacksBases(t *testing.T) {
	p := newPushRig(t)
	// The remote moved on: main has a commit the cache was never fetched.
	write(t, p.work, ".github/workflows/ci.yml", ciYAML("newer"))
	newer := p.commit(t, "newer")
	write(t, p.work, ".github/workflows/ci.yml", ciYAML("newest"))
	tip := p.commit(t, "newest")
	input := tip + "\n^" + newer + "\n"
	cmd := exec.Command("git", "pack-objects", "--revs", "--thin", "--stdout", "-q")
	cmd.Dir = p.work
	cmd.Stdin = strings.NewReader(input)
	pack, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}

	_, err = Runner{}.ChangedUnder(context.Background(), p.cache, t.TempDir(), bytes.NewReader(pack),
		[]RefUpdate{{Old: newer, New: tip, Ref: "refs/heads/legato/x"}}, ".github/workflows")
	if err == nil {
		t.Fatal("a pack the cache cannot complete was read")
	}
}

func TestChangedUnderTakesAPushThatOnlyDeletes(t *testing.T) {
	p := newPushRig(t)
	base := run(t, p.work, "rev-parse", "HEAD")
	got, err := Runner{}.ChangedUnder(context.Background(), p.cache, t.TempDir(), bytes.NewReader(nil),
		[]RefUpdate{{Old: base, New: zeroID, Ref: "refs/heads/legato/x"}}, ".github/workflows")
	if err != nil || len(got) != 0 {
		t.Fatalf("got %v, %v", got, err)
	}
}

func TestChangedUnderLeavesNothingBehind(t *testing.T) {
	p := newPushRig(t)
	run(t, p.work, "checkout", "-q", "-b", "legato/x")
	write(t, p.work, ".github/workflows/release.yml", "name: release\n")
	tip := p.commit(t, "release")
	scratch := t.TempDir()
	before := run(t, "", "--git-dir", p.cache, "count-objects", "-v")
	if _, err := (Runner{}).ChangedUnder(context.Background(), p.cache, scratch, bytes.NewReader(p.pack(t, tip)),
		[]RefUpdate{{Old: zeroID, New: tip, Ref: "refs/heads/legato/x"}}, ".github/workflows"); err != nil {
		t.Fatal(err)
	}
	if entries, _ := os.ReadDir(scratch); len(entries) != 0 {
		t.Fatalf("scratch holds %v", entries)
	}
	if after := run(t, "", "--git-dir", p.cache, "count-objects", "-v"); after != before {
		t.Fatalf("the cache changed:\n%s\n%s", before, after)
	}
}

// A ref moved onto commits GitHub already has brings no new commit, but
// its workflows change all the same; it is counted, to be safe.
func TestChangedUnderNamesAWorkflowARefMovedOntoKnownCommitsChanges(t *testing.T) {
	p := newPushRig(t)
	base := run(t, p.work, "rev-parse", "HEAD")
	write(t, p.work, ".github/workflows/ci.yml", ciYAML("moved"))
	tip := p.commit(t, "ci")
	run(t, p.work, "push", "-q", p.cache, "main")

	got := p.changes(t, p.pack(t, tip), RefUpdate{Old: base, New: tip, Ref: "refs/heads/legato/x"})
	want := map[string][]string{"refs/heads/legato/x": {".github/workflows/ci.yml"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}
