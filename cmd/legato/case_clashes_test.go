package main

import (
	"errors"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// scripts/case-clashes.sh is how CI keeps paths that differ only in case out
// of the repository: macOS and Windows see them as one path (LEG-149).
func TestCaseClashes(t *testing.T) {
	script := filepath.Join("..", "..", "scripts", "case-clashes.sh")
	cases := []struct {
		name  string
		paths []string
		clash string // what the output must name; "" when the paths pass
	}{
		{"distinct paths pass", []string{"go.mod", "internal/a.go", "internal/b.go", "web/src/App.tsx"}, ""},
		{"two files that differ only in case fail", []string{"docs/README.md", "docs/readme.md"}, "docs/README.md  docs/readme.md"},
		{"two folders that differ only in case fail", []string{"Docs/a.md", "docs/b.md"}, "Docs  docs"},
		// On macOS the import ./Approvals found approvals.ts.
		{"modules an import names alike fail", []string{"src/voice/Approvals.tsx", "src/voice/approvals.ts", "src/voice/approvals.test.tsx"}, "src/voice/Approvals  src/voice/approvals"},
		// On macOS ./Foo, meant for Foo/index.ts, finds foo.ts first.
		{"a module and a folder an import names alike fail", []string{"src/Foo/index.ts", "src/foo.ts"}, "src/Foo  src/foo"},
		{"files that are not modules may share a name in another case", []string{"src/voice/Widget.tsx", "src/voice/widget.css", "src/voice/widget.test.tsx"}, ""},
		{"one module in two extensions passes", []string{"src/a.ts", "src/a.tsx"}, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			cmd := exec.Command("bash", script, "-")
			cmd.Stdin = strings.NewReader(strings.Join(c.paths, "\n") + "\n")
			out, err := cmd.CombinedOutput()
			var exit *exec.ExitError
			switch {
			case c.clash == "" && (err != nil || len(out) > 0):
				t.Errorf("want a pass, got %v:\n%s", err, out)
			case c.clash != "" && !(errors.As(err, &exit) && exit.ExitCode() == 1):
				t.Errorf("want exit 1, got %v:\n%s", err, out)
			case c.clash != "" && strings.Count(string(out), c.clash) != 1:
				t.Errorf("output does not name %q once:\n%s", c.clash, out)
			}
		})
	}
}

func TestCaseClashesRefusesAnUnknownArgument(t *testing.T) {
	script := filepath.Join("..", "..", "scripts", "case-clashes.sh")
	out, err := exec.Command("bash", script, "--stdin").CombinedOutput()
	var exit *exec.ExitError
	if !errors.As(err, &exit) || exit.ExitCode() != 2 {
		t.Errorf("want exit 2 for --stdin, got %v: %s", err, out)
	}
}
