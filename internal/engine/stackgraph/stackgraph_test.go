package stackgraph_test

import (
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cpave3/legato/internal/engine/stackgraph"
)

type repo struct {
	t    *testing.T
	dir  string
	bare string
}

func (r repo) git(args ...string) string {
	r.t.Helper()
	cmd := exec.Command("git", append([]string{"-c", "user.name=a", "-c", "user.email=a@b", "-c", "init.defaultBranch=main"}, args...)...)
	cmd.Dir = r.dir
	cmd.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "HOME="+r.t.TempDir())
	out, err := cmd.CombinedOutput()
	if err != nil {
		r.t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

// newRepo is a clone with an origin it can push to, on main with one commit.
func newRepo(t *testing.T) repo {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	r := repo{t: t, dir: t.TempDir(), bare: t.TempDir()}
	r.git("init", "-q", "--bare", r.bare)
	r.git("init", "-q")
	r.git("commit", "-q", "--allow-empty", "-m", "base")
	r.git("remote", "add", "origin", r.bare)
	r.git("push", "-q", "origin", "main")
	return r
}

// branch makes a branch with one commit on parent and returns its commit.
func (r repo) branch(name, parent string) string {
	r.git("checkout", "-q", "-b", name, parent)
	r.git("commit", "-q", "--allow-empty", "-m", name)
	return r.git("rev-parse", "HEAD")
}

type entry struct {
	Name    string `json:"name"`
	Parent  string `json:"parent"`
	BaseSHA string `json:"base_sha"`
	HeadSHA string `json:"head_sha"`
}

// writeGraph writes .git/stack/graph.json as st does.
func (r repo) writeGraph(root string, branches ...entry) {
	r.t.Helper()
	g := map[string]any{"version": 7, "root": root, "branches": map[string]entry{}}
	for _, b := range branches {
		g["branches"].(map[string]entry)[b.Name] = b
	}
	data, _ := json.MarshalIndent(g, "", "  ")
	if err := os.MkdirAll(filepath.Join(r.dir, ".git", "stack"), 0o755); err != nil {
		r.t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(r.dir, ".git", "stack", "graph.json"), data, 0o644); err != nil {
		r.t.Fatal(err)
	}
}

func names(s stackgraph.Stack) []string {
	var out []string
	for _, b := range s.Branches {
		out = append(out, b.Name)
	}
	return out
}

func TestAStackIsReadFromGitsFiles(t *testing.T) {
	r := newRepo(t)
	base := r.git("rev-parse", "main")
	a := r.branch("legato/t1-a", "main")
	b := r.branch("legato/t1-b", "legato/t1-a")
	r.git("push", "-q", "origin", "legato/t1-a", "legato/t1-b")
	r.git("pack-refs", "--all") // one way or the other, both are read
	r.writeGraph("main",
		entry{"legato/t1-a", "main", base, a},
		entry{"legato/t1-b", "legato/t1-a", a, "0000000000000000000000000000000000000000"}, // st's record lags
	)

	s, err := stackgraph.Read(r.dir)
	if err != nil {
		t.Fatal(err)
	}
	if s.Root != "main" || strings.Join(names(s), " ") != "legato/t1-a legato/t1-b" {
		t.Fatalf("stack = %+v", s)
	}
	if s.Branches[1].Parent != "legato/t1-a" || s.Branches[1].BaseSHA != a || s.Branches[1].HeadSHA != b {
		t.Errorf("legato/t1-b = %+v; want head %s, the branch's commit", s.Branches[1], b)
	}
	for _, br := range s.Branches {
		if br.State != stackgraph.Pushed {
			t.Errorf("%s is %s, want pushed", br.Name, br.State)
		}
	}
	if ok, why := s.Importable(); !ok {
		t.Errorf("not importable: %s", why)
	}
}

func TestBranchesNotOnTheRemoteAreUnpushedAndThoseGoneAreMissing(t *testing.T) {
	r := newRepo(t)
	base := r.git("rev-parse", "main")
	a := r.branch("legato/t1-a", "main")
	r.git("push", "-q", "origin", "legato/t1-a")
	r.git("commit", "-q", "--allow-empty", "-m", "more")
	a2 := r.git("rev-parse", "HEAD")
	b := r.branch("legato/t1-b", "legato/t1-a")
	r.writeGraph("main",
		entry{"legato/t1-a", "main", base, a2},
		entry{"legato/t1-b", "legato/t1-a", a2, b},
		entry{"legato/t1-c", "legato/t1-b", b, b},
	)
	_ = a

	s, err := stackgraph.Read(r.dir)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]stackgraph.State{"legato/t1-a": stackgraph.Unpushed, "legato/t1-b": stackgraph.Unpushed, "legato/t1-c": stackgraph.Missing}
	for _, br := range s.Branches {
		if br.State != want[br.Name] {
			t.Errorf("%s is %s, want %s", br.Name, br.State, want[br.Name])
		}
	}
	ok, why := s.Importable()
	if ok || !strings.Contains(why, "legato/t1-a") || !strings.Contains(why, "legato/t1-c") {
		t.Errorf("importable = %v, %q", ok, why)
	}
	if _, err := s.Document(); err == nil {
		t.Error("a document was made for a stack that cannot be imported")
	}
}

func TestBranchNamesFromTheGraphAreCheckedBeforeTheyAreRead(t *testing.T) {
	r := newRepo(t)
	base := r.git("rev-parse", "main")
	// A file outside the clone's .git that holds a commit, as a ref would.
	if err := os.WriteFile(filepath.Join(r.dir, "outside"), []byte(base+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	bad := []string{"../../outside", "a..b", "-x", "x.lock", "a b", "a\x01b", "/x", "x/", "a@{1}", "a~1", "a:b", "@"}
	var entries []entry
	for _, name := range bad {
		entries = append(entries, entry{name, "main", base, base})
	}
	r.writeGraph("main", entries...)

	s, err := stackgraph.Read(r.dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Branches) != len(bad) {
		t.Fatalf("branches = %v", names(s))
	}
	for _, br := range s.Branches {
		if br.State != stackgraph.Invalid || br.HeadSHA != "" {
			t.Errorf("%q is %s with head %q, want invalid and unread", br.Name, br.State, br.HeadSHA)
		}
	}
	if ok, _ := s.Importable(); ok {
		t.Error("a stack with invalid names is importable")
	}
}

func TestReadingAStackRunsNothingFromTheClone(t *testing.T) {
	r := newRepo(t)
	base := r.git("rev-parse", "main")
	a := r.branch("legato/t1-a", "main")
	r.writeGraph("main", entry{"legato/t1-a", "main", base, a})
	// Set last: any git the test itself ran after it would run it.
	marker := filepath.Join(t.TempDir(), "ran")
	r.git("config", "core.fsmonitor", "touch "+marker)
	if _, err := stackgraph.Read(r.dir); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(marker); err == nil {
		t.Fatal("the clone's fsmonitor ran")
	}
}

func TestACloneWithoutAGraphHasNoStack(t *testing.T) {
	r := newRepo(t)
	if _, err := stackgraph.Read(r.dir); !errors.Is(err, stackgraph.ErrNoStack) {
		t.Fatalf("err = %v, want ErrNoStack", err)
	}
}

func TestASharedGraphIsNotRead(t *testing.T) {
	r := newRepo(t)
	blob := r.git("hash-object", "-w", "--stdin")
	r.git("update-ref", "refs/staccato/graphs/abc", blob)
	if _, err := stackgraph.Read(r.dir); !errors.Is(err, stackgraph.ErrSharedGraph) {
		t.Fatalf("err = %v, want ErrSharedGraph", err)
	}
}

func TestAGraphThatCannotBeReadIsAnError(t *testing.T) {
	r := newRepo(t)
	path := filepath.Join(r.dir, ".git", "stack", "graph.json")
	_ = os.MkdirAll(filepath.Dir(path), 0o755)
	for name, data := range map[string][]byte{
		"not json": []byte("{not json"),
		"too big":  append([]byte(`{"root":"main","branches":{},"x":"`), append([]byte(strings.Repeat("x", 1<<20)), '"', '}')...),
		"no root":  []byte(`{"branches":{}}`),
	} {
		if err := os.WriteFile(path, data, 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := stackgraph.Read(r.dir); err == nil || errors.Is(err, stackgraph.ErrNoStack) {
			t.Errorf("%s: err = %v", name, err)
		}
	}
}

func TestABranchOffTheStackIsNotImportable(t *testing.T) {
	r := newRepo(t)
	base := r.git("rev-parse", "main")
	a := r.branch("legato/t1-a", "main")
	r.git("push", "-q", "origin", "legato/t1-a")
	r.writeGraph("main", entry{"legato/t1-a", "legato/t1-gone", base, a})
	s, err := stackgraph.Read(r.dir)
	if err != nil {
		t.Fatal(err)
	}
	if ok, why := s.Importable(); ok || !strings.Contains(why, "legato/t1-gone") {
		t.Errorf("importable = %v, %q", ok, why)
	}
}

func TestTheDocumentIsStaccatosFormatOne(t *testing.T) {
	r := newRepo(t)
	base := r.git("rev-parse", "main")
	a := r.branch("a", "main")
	c := r.branch("c", "a")
	b := r.branch("b", "a")
	d := r.branch("d", "main")
	r.git("push", "-q", "origin", "a", "b", "c", "d")
	r.writeGraph("main", entry{"d", "main", base, d}, entry{"c", "a", a, c}, entry{"b", "a", a, b}, entry{"a", "main", base, a})

	s, err := stackgraph.Read(r.dir)
	if err != nil {
		t.Fatal(err)
	}
	data, err := s.Document()
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Format   int     `json:"format"`
		Kind     string  `json:"kind"`
		Root     string  `json:"root"`
		Branches []entry `json:"branches"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatal(err)
	}
	if doc.Format != 1 || doc.Kind != "staccato.graph" || doc.Root != "main" {
		t.Fatalf("document = %s", data)
	}
	want := []entry{{"a", "main", base, a}, {"b", "a", a, b}, {"c", "a", a, c}, {"d", "main", base, d}}
	if len(doc.Branches) != len(want) {
		t.Fatalf("branches = %+v", doc.Branches)
	}
	for i := range want {
		if doc.Branches[i] != want[i] {
			t.Errorf("branch %d = %+v, want %+v", i, doc.Branches[i], want[i])
		}
	}
}
