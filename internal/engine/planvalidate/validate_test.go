package planvalidate_test

import (
	"encoding/json"
	"errors"
	"flag"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cpave3/legato/internal/engine/planvalidate"
)

func TestExtractVisualBlocksPreservesKindsOrderAndLines(t *testing.T) {
	markdown := strings.Join([]string{
		"# Plan", "", "`mermaid`", "", "```MERMAID", "flowchart LR", "  A --> B", "```", "",
		"~~~wireframe", "Wireframe mobile", "  Button \"Save\" contained", "~~~", "",
		"```wiremark", "not canonical", "```", "", "```go", "fmt.Println()", "```", "",
	}, "\n")

	blocks, diagnostics := planvalidate.Extract([]byte(markdown))
	if len(diagnostics) != 0 {
		t.Fatalf("diagnostics = %+v", diagnostics)
	}
	if len(blocks) != 2 {
		t.Fatalf("blocks = %+v", blocks)
	}
	if blocks[0].Kind != planvalidate.Mermaid || blocks[0].Index != 1 || blocks[0].Line != 6 || blocks[0].Source != "flowchart LR\n  A --> B\n" {
		t.Fatalf("Mermaid block = %+v", blocks[0])
	}
	if blocks[1].Kind != planvalidate.Wireframe || blocks[1].Index != 1 || blocks[1].Line != 11 || blocks[1].Source != "Wireframe mobile\n  Button \"Save\" contained\n" {
		t.Fatalf("Wireframe block = %+v", blocks[1])
	}
}

func TestExtractReportsUnclosedVisualFenceAtOpeningLine(t *testing.T) {
	blocks, diagnostics := planvalidate.Extract([]byte("# Plan\n\n```mermaid\nflowchart LR\nA-->B\n"))
	if len(blocks) != 0 || len(diagnostics) != 1 {
		t.Fatalf("blocks = %+v, diagnostics = %+v", blocks, diagnostics)
	}
	if diagnostics[0].Kind != planvalidate.Mermaid || diagnostics[0].Line != 3 || !strings.Contains(diagnostics[0].Message, "unclosed") {
		t.Fatalf("diagnostic = %+v", diagnostics[0])
	}
}

func TestValidateInstallsPinnedPackagesOnceAndTypesInstallFailure(t *testing.T) {
	markdown := []byte("```mermaid\nflowchart LR\nA-->B\n```\n")
	cache := t.TempDir()
	installs := 0
	run := func(name string, args []string, dir string, stdin []byte) ([]byte, error) {
		if name == "npm" {
			installs++
			// The whole tree comes from the lockfile Legato ships, so no
			// transitive dependency floats to a release published since.
			if len(args) == 0 || args[0] != "ci" {
				t.Errorf("npm %q, want npm ci", args)
			}
			for _, file := range []string{"package.json", "package-lock.json"} {
				installed, err := os.ReadFile(filepath.Join(dir, file))
				if err != nil {
					t.Errorf("npm ran without %s: %v", file, err)
					continue
				}
				if shipped := readShipped(t, file); string(installed) != string(shipped) {
					t.Errorf("%s in the install folder is not the one Legato ships", file)
				}
			}
			for _, relative := range []string{"node_modules/mermaid/package.json", "node_modules/@wiremark/core/package.json", "node_modules/jsdom/package.json"} {
				path := filepath.Join(dir, relative)
				if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
					return nil, err
				}
				if err := os.WriteFile(path, []byte("{}"), 0o644); err != nil {
					return nil, err
				}
			}
			return nil, nil
		}
		return []byte(`{"diagnostics":[]}`), nil
	}
	options := planvalidate.Options{CacheDir: cache, Run: run}
	for range 2 {
		if diagnostics, err := planvalidate.Validate(markdown, options); err != nil || len(diagnostics) != 0 {
			t.Fatalf("diagnostics = %+v, error = %v", diagnostics, err)
		}
	}
	if installs != 1 {
		t.Fatalf("npm installs = %d, want 1", installs)
	}

	_, err := planvalidate.Validate(markdown, planvalidate.Options{CacheDir: t.TempDir(), Run: func(name string, args []string, dir string, stdin []byte) ([]byte, error) {
		return nil, errors.New("registry unavailable")
	}})
	var dependency *planvalidate.DependencyError
	if !errors.As(err, &dependency) || !strings.Contains(err.Error(), "registry unavailable") {
		t.Fatalf("error = %v", err)
	}
}

func TestValidateUsesPinnedParsersAndMapsDiagnosticsToMarkdownLines(t *testing.T) {
	cache := t.TempDir()
	valid := "# Plan\n\n```mermaid\nflowchart LR\n  A --> B\n```\n\n```wireframe\nWireframe #start mobile\n  Button \"Next\" contained to=#done\n  Link \"Website\" to=https://example.com\nWireframe #done mobile\n  Typography \"Done\"\n```\n"
	diagnostics, err := planvalidate.Validate([]byte(valid), planvalidate.Options{CacheDir: cache})
	if err != nil {
		t.Fatal(err)
	}
	if len(diagnostics) != 0 {
		t.Fatalf("diagnostics = %+v", diagnostics)
	}

	invalid := "# Plan\n\n```mermaid\nnot valid mermaid\n```\n\n```wireframe\nWireframe #start mobile\n  FancyPanel\n```\n\n```wireframe\nWireframe #start mobile\n  Button \"Next\" to=#missing\n```\n"
	diagnostics, err = planvalidate.Validate([]byte(invalid), planvalidate.Options{CacheDir: cache})
	if err != nil {
		t.Fatal(err)
	}
	if len(diagnostics) != 3 {
		t.Fatalf("diagnostics = %+v", diagnostics)
	}
	if diagnostics[0].Kind != planvalidate.Mermaid || diagnostics[0].Line != 4 {
		t.Fatalf("Mermaid diagnostic = %+v", diagnostics[0])
	}
	if diagnostics[1].Kind != planvalidate.Wireframe || diagnostics[1].Index != 1 || diagnostics[1].Line != 9 || !strings.Contains(diagnostics[1].Message, "FancyPanel") {
		t.Fatalf("Wireframe parse diagnostic = %+v", diagnostics[1])
	}
	if diagnostics[2].Kind != planvalidate.Wireframe || diagnostics[2].Index != 2 || diagnostics[2].Line != 14 || !strings.Contains(diagnostics[2].Message, "#missing") {
		t.Fatalf("Wireframe target diagnostic = %+v", diagnostics[2])
	}
}

var updateLock = flag.Bool("update", false, "rewrite node/package-lock.json from node/package.json (needs npm and the registry)")

func readShipped(t *testing.T, file string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("node", file))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// The lockfile pins every package of the tree, each with its integrity hash,
// for exactly the dependencies package.json names. npm ci refuses a lockfile
// that disagrees with package.json; this says so without the network.
// Regenerate it with go test ./internal/engine/planvalidate -run TestLockfile -update.
func TestLockfilePinsTheWholeTreeOfPackageJSON(t *testing.T) {
	if *updateLock {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "package.json"), readShipped(t, "package.json"), 0o644); err != nil {
			t.Fatal(err)
		}
		npm := exec.Command("npm", "install", "--package-lock-only", "--ignore-scripts", "--no-audit", "--no-fund")
		npm.Dir = dir
		if output, err := npm.CombinedOutput(); err != nil {
			t.Fatalf("npm: %v\n%s", err, output)
		}
		lock, err := os.ReadFile(filepath.Join(dir, "package-lock.json"))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join("node", "package-lock.json"), lock, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	var manifest struct {
		Dependencies map[string]string `json:"dependencies"`
	}
	if err := json.Unmarshal(readShipped(t, "package.json"), &manifest); err != nil {
		t.Fatal(err)
	}
	var lock struct {
		LockfileVersion int `json:"lockfileVersion"`
		Packages        map[string]struct {
			Version      string            `json:"version"`
			Resolved     string            `json:"resolved"`
			Integrity    string            `json:"integrity"`
			Dependencies map[string]string `json:"dependencies"`
		} `json:"packages"`
	}
	if err := json.Unmarshal(readShipped(t, "package-lock.json"), &lock); err != nil {
		t.Fatal(err)
	}
	if lock.LockfileVersion < 2 {
		t.Fatalf("lockfileVersion %d", lock.LockfileVersion)
	}
	root := lock.Packages[""]
	if len(root.Dependencies) != len(manifest.Dependencies) {
		t.Fatalf("lockfile root dependencies %v, package.json %v", root.Dependencies, manifest.Dependencies)
	}
	for name, version := range manifest.Dependencies {
		if root.Dependencies[name] != version {
			t.Fatalf("lockfile root has %s %q, package.json %q", name, root.Dependencies[name], version)
		}
		if got := lock.Packages["node_modules/"+name].Version; got != version {
			t.Fatalf("lockfile installs %s %q, package.json pins %q", name, got, version)
		}
	}
	for path, pkg := range lock.Packages {
		if path == "" {
			continue
		}
		if pkg.Version == "" || pkg.Integrity == "" || !strings.HasPrefix(pkg.Resolved, "https://registry.npmjs.org/") {
			t.Fatalf("%s is not pinned: %+v", path, pkg)
		}
	}
}
