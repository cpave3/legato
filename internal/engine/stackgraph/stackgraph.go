// Package stackgraph reads the stack of branches Staccato (st) keeps in a
// clone, and writes it as the document st graph import reads. It reads git's
// files through an os.Root at the clone's .git folder and runs nothing: the
// clone may be an agent's, whose git configuration could make git run
// commands of its choosing.
package stackgraph

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"regexp"
	"sort"
	"strings"
)

// State is what git says of one branch of the stack.
type State string

const (
	// Pushed is a branch whose commit is the one its remote-tracking branch
	// (origin) records.
	Pushed State = "pushed"
	// Unpushed is a branch the remote does not have at this commit.
	Unpushed State = "unpushed"
	// Missing is a branch the graph names that does not exist.
	Missing State = "missing"
	// Invalid is a name git would not take as a branch; it is not read.
	Invalid State = "invalid"
)

// Branch is one branch of a stack.
type Branch struct {
	Name    string
	Parent  string
	BaseSHA string
	// HeadSHA is the commit the branch is at, read from git, not the one
	// the graph last recorded; "" when it is missing or invalid.
	HeadSHA string
	State   State
	// Problem says why the branch cannot be imported beyond its state: its
	// parent is not in the stack, or the graph records no base commit.
	Problem string
}

// Stack is a clone's stack: its root (the trunk) and its branches, depth
// first from the root with siblings by name, then any the root does not
// reach, by name.
type Stack struct {
	Root     string
	Branches []Branch
}

var (
	// ErrNoStack is a clone in which st keeps no graph.
	ErrNoStack = errors.New("the clone has no stack")
	// ErrSharedGraph is a clone whose graph st keeps in a shared ref
	// (refs/staccato/...), a git object Legato does not read.
	ErrSharedGraph = errors.New("the stack's graph is kept in a shared ref (st graph share), which Legato does not read")
)

// maxGraph is the largest graph.json read; a real one is a few kilobytes.
const maxGraph = 1 << 20

var objectName = regexp.MustCompile(`^[0-9a-f]{40}([0-9a-f]{24})?$`)

// Read reads the stack of the clone at path.
func Read(clone string) (Stack, error) {
	top, err := os.OpenRoot(clone)
	if err != nil {
		return Stack{}, err
	}
	defer top.Close()
	gitDir, err := top.OpenRoot(".git")
	if err != nil {
		return Stack{}, fmt.Errorf("%s has no .git folder of its own: %w", clone, err)
	}
	defer gitDir.Close()
	fsys := gitDir.FS()

	packed, err := readPacked(fsys)
	if err != nil {
		return Stack{}, err
	}
	if shared(fsys, packed) {
		return Stack{}, ErrSharedGraph
	}
	data, err := readLimited(fsys, "stack/graph.json", maxGraph)
	if errors.Is(err, fs.ErrNotExist) {
		return Stack{}, ErrNoStack
	}
	if err != nil {
		return Stack{}, fmt.Errorf("the stack's graph: %w", err)
	}
	var graph struct {
		Root     string `json:"root"`
		Branches map[string]struct {
			Parent  string `json:"parent"`
			BaseSHA string `json:"base_sha"`
		} `json:"branches"`
	}
	if err := json.Unmarshal(data, &graph); err != nil {
		return Stack{}, fmt.Errorf("the stack's graph is not JSON st wrote: %w", err)
	}
	if graph.Root == "" {
		return Stack{}, errors.New("the stack's graph names no root branch")
	}

	read := map[string]Branch{}
	for name, b := range graph.Branches {
		br := Branch{Name: name, Parent: b.Parent, BaseSHA: b.BaseSHA}
		switch {
		case !ValidBranchName(name):
			br.State = Invalid
		default:
			head, ok, err := ref(fsys, packed, "refs/heads/"+name)
			if err != nil {
				return Stack{}, err
			}
			if !ok {
				br.State = Missing
				break
			}
			br.HeadSHA = head
			remote, ok, err := ref(fsys, packed, "refs/remotes/origin/"+name)
			if err != nil {
				return Stack{}, err
			}
			br.State = Unpushed
			if ok && remote == head {
				br.State = Pushed
			}
		}
		if br.State != Invalid && !objectName.MatchString(br.BaseSHA) {
			br.Problem = "the graph records no base commit for it"
		}
		read[name] = br
	}
	return Stack{Root: graph.Root, Branches: order(graph.Root, read)}, nil
}

// order lists branches depth first from root, siblings by name; branches
// root does not reach follow by name, each with the reason.
func order(root string, branches map[string]Branch) []Branch {
	children := map[string][]string{}
	for name, b := range branches {
		children[b.Parent] = append(children[b.Parent], name)
	}
	for _, c := range children {
		sort.Strings(c)
	}
	var out []Branch
	seen := map[string]bool{}
	var walk func(string)
	walk = func(parent string) {
		for _, name := range children[parent] {
			if seen[name] {
				continue
			}
			seen[name] = true
			out = append(out, branches[name])
			walk(name)
		}
	}
	walk(root)
	var rest []string
	for name := range branches {
		if !seen[name] {
			rest = append(rest, name)
		}
	}
	sort.Strings(rest)
	for _, name := range rest {
		b := branches[name]
		b.Problem = fmt.Sprintf("its parent %s is not on the stack", b.Parent)
		out = append(out, b)
	}
	return out
}

// Importable reports whether st graph import can bring the stack into
// another clone: every branch pushed, valid and on the stack, so that every
// commit the document names can be fetched. why lists what is not.
func (s Stack) Importable() (ok bool, why string) {
	byState := map[State][]string{}
	var problems []string
	for _, b := range s.Branches {
		if b.State != Pushed {
			byState[b.State] = append(byState[b.State], b.Name)
		}
		if b.Problem != "" && b.State != Invalid {
			problems = append(problems, b.Name+": "+b.Problem)
		}
	}
	var parts []string
	for _, st := range []struct {
		state State
		say   string
	}{{Unpushed, "not pushed"}, {Missing, "missing"}, {Invalid, "not valid branch names"}} {
		if names := byState[st.state]; len(names) > 0 {
			parts = append(parts, st.say+": "+strings.Join(names, ", "))
		}
	}
	parts = append(parts, problems...)
	if len(s.Branches) == 0 {
		parts = append(parts, "the stack has no branches")
	}
	return len(parts) == 0, strings.Join(parts, "; ")
}

// documentFormat is the version of st's graph document this package writes.
const documentFormat = 1

type documentBranch struct {
	Name    string `json:"name"`
	Parent  string `json:"parent"`
	BaseSHA string `json:"base_sha"`
	HeadSHA string `json:"head_sha"`
}

// Document is the stack as the document st graph import reads (format 1).
// It is an error when the stack is not importable.
func (s Stack) Document() ([]byte, error) {
	if ok, why := s.Importable(); !ok {
		return nil, errors.New("the stack cannot be imported: " + why)
	}
	doc := struct {
		Format    int              `json:"format"`
		Kind      string           `json:"kind"`
		STVersion string           `json:"st_version"`
		Root      string           `json:"root"`
		Branches  []documentBranch `json:"branches"`
	}{Format: documentFormat, Kind: "staccato.graph", Root: s.Root}
	for _, b := range s.Branches {
		doc.Branches = append(doc.Branches, documentBranch{b.Name, b.Parent, b.BaseSHA, b.HeadSHA})
	}
	return json.MarshalIndent(doc, "", "  ")
}

// ValidBranchName reports whether git would take name as a branch
// (git check-ref-format --branch), without running git.
func ValidBranchName(name string) bool {
	if name == "" || name == "@" || name == "HEAD" || strings.HasPrefix(name, "-") ||
		strings.HasPrefix(name, "/") || strings.HasSuffix(name, "/") || strings.HasSuffix(name, ".") ||
		strings.Contains(name, "..") || strings.Contains(name, "@{") || strings.Contains(name, "//") {
		return false
	}
	for _, r := range name {
		if r < 0x20 || r == 0x7f || strings.ContainsRune(" ~^:?*[\\", r) {
			return false
		}
	}
	for _, part := range strings.Split(name, "/") {
		if strings.HasPrefix(part, ".") || strings.HasSuffix(part, ".lock") {
			return false
		}
	}
	return true
}

// shared reports whether st keeps this clone's graph in a ref, which it
// then reads before .git/stack/graph.json.
func shared(fsys fs.FS, packed map[string]string) bool {
	if _, err := fs.Stat(fsys, "refs/staccato"); err == nil {
		return true
	}
	for name := range packed {
		if strings.HasPrefix(name, "refs/staccato/") {
			return true
		}
	}
	return false
}

// ref is the object a ref names, from its file or packed-refs.
func ref(fsys fs.FS, packed map[string]string, name string) (string, bool, error) {
	data, err := readLimited(fsys, name, 256)
	switch {
	case err == nil:
		sha := strings.TrimSpace(string(data))
		if !objectName.MatchString(sha) {
			return "", false, nil
		}
		return sha, true, nil
	case !errors.Is(err, fs.ErrNotExist) && !errors.Is(err, errNotRegular):
		return "", false, err
	}
	sha, ok := packed[name]
	return sha, ok, nil
}

// readPacked reads packed-refs as ref name to object; none is no error.
func readPacked(fsys fs.FS) (map[string]string, error) {
	data, err := readLimited(fsys, "packed-refs", 1<<26)
	if errors.Is(err, fs.ErrNotExist) {
		return map[string]string{}, nil
	}
	if err != nil {
		return nil, err
	}
	refs := map[string]string{}
	for _, line := range strings.Split(string(data), "\n") {
		if sha, name, ok := strings.Cut(line, " "); ok && objectName.MatchString(sha) {
			refs[name] = sha
		}
	}
	return refs, nil
}

var errNotRegular = errors.New("not a regular file")

// readLimited reads a regular file of at most limit bytes.
func readLimited(fsys fs.FS, name string, limit int64) ([]byte, error) {
	f, err := fsys.Open(name)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("%s: %w", name, errNotRegular)
	}
	data, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("%s is larger than %d bytes", name, limit)
	}
	return data, nil
}
