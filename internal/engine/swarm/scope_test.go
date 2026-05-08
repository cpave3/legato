package swarm

import (
	"os"
	"path/filepath"
	"sort"
	"testing"
)

func TestMatchScope(t *testing.T) {
	cases := []struct {
		name  string
		globs []string
		path  string
		want  bool
	}{
		{"empty globs", nil, "api/server.go", false},
		{"empty string globs", []string{""}, "api/server.go", false},
		{"file-level exact match", []string{"go.mod"}, "go.mod", true},
		{"file-level miss", []string{"go.mod"}, "go.sum", false},
		{"directory glob match", []string{"api/**"}, "api/server.go", true},
		{"directory glob deep match", []string{"api/**"}, "api/v2/handlers/users.go", true},
		{"directory glob root only", []string{"api/*"}, "api/server.go", true},
		{"directory glob root only deep miss", []string{"api/*"}, "api/v2/handlers.go", false},
		{"doublestar everywhere", []string{"**/*.test.ts"}, "web/src/foo.test.ts", true},
		{"character class", []string{"src/[abc]/*.go"}, "src/a/foo.go", true},
		{"character class miss", []string{"src/[abc]/*.go"}, "src/d/foo.go", false},
		{"multi-glob first hits", []string{"api/**", "go.mod"}, "api/x.go", true},
		{"multi-glob second hits", []string{"api/**", "go.mod"}, "go.mod", true},
		{"multi-glob none hit", []string{"api/**", "go.mod"}, "web/main.tsx", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := MatchScope(tc.globs, tc.path); got != tc.want {
				t.Errorf("MatchScope(%v, %q) = %v, want %v", tc.globs, tc.path, got, tc.want)
			}
		})
	}
}

// makeRepo builds a temp directory tree with the given files. Each entry can be
// either a directory (string ending in "/") or a file. Directories are auto-created.
func makeRepo(t *testing.T, files []string) string {
	t.Helper()
	dir := t.TempDir()
	for _, f := range files {
		full := filepath.Join(dir, f)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestScopeOverlaps(t *testing.T) {
	cases := []struct {
		name      string
		files     []string
		a, b      []string
		wantHit   bool
		wantPaths []string
	}{
		{
			name:    "disjoint",
			files:   []string{"api/srv.go", "web/main.tsx"},
			a:       []string{"api/**"},
			b:       []string{"web/**"},
			wantHit: false,
		},
		{
			name:      "nested directory",
			files:     []string{"src/a.go", "src/lib/b.go"},
			a:         []string{"src/**"},
			b:         []string{"src/lib/**"},
			wantHit:   true,
			wantPaths: []string{"src/lib/b.go"},
		},
		{
			name:      "identical",
			files:     []string{"api/x.go", "api/y.go"},
			a:         []string{"api/**"},
			b:         []string{"api/**"},
			wantHit:   true,
			wantPaths: []string{"api/x.go", "api/y.go"},
		},
		{
			name:      "file vs directory",
			files:     []string{"go.mod", "go.sum"},
			a:         []string{"go.mod"},
			b:         []string{"**"},
			wantHit:   true,
			wantPaths: []string{"go.mod"},
		},
		{
			name:    "empty scope returns nothing",
			files:   []string{"x.go"},
			a:       []string{},
			b:       []string{"**"},
			wantHit: false,
		},
		{
			name:    "empty repo",
			files:   nil,
			a:       []string{"api/**"},
			b:       []string{"api/v2/**"},
			wantHit: false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := makeRepo(t, tc.files)
			hit, paths := ScopeOverlaps(tc.a, tc.b, root)
			if hit != tc.wantHit {
				t.Errorf("hit = %v, want %v (paths=%v)", hit, tc.wantHit, paths)
			}
			sort.Strings(paths)
			sort.Strings(tc.wantPaths)
			if tc.wantPaths != nil && !equalSlices(paths, tc.wantPaths) {
				t.Errorf("paths = %v, want %v", paths, tc.wantPaths)
			}
		})
	}
}

func TestScopeOverlapsSkipsGitDir(t *testing.T) {
	root := makeRepo(t, []string{".git/objects/abc", "src/foo.go"})
	hit, _ := ScopeOverlaps([]string{"**"}, []string{".git/**"}, root)
	if hit {
		t.Error("expected .git/ to be skipped")
	}
}

func TestValidateScope(t *testing.T) {
	if err := ValidateScope([]string{"api/**", "go.mod"}); err != nil {
		t.Errorf("valid globs returned error: %v", err)
	}
	if err := ValidateScope([]string{"["}); err == nil {
		t.Error("expected error for malformed glob")
	}
}

func equalSlices(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
