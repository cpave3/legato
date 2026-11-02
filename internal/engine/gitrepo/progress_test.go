package gitrepo

import (
	"context"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestParseProgress(t *testing.T) {
	for _, tc := range []struct {
		line string
		want Progress
		ok   bool
	}{
		{"Receiving objects:  38% (1234/3245), 310.00 MiB | 3.00 MiB/s", Progress{Phase: "Receiving objects", Percent: 38, Received: "310.00 MiB", Rate: "3.00 MiB/s"}, true},
		{"Receiving objects: 100% (3245/3245), 812.11 MiB | 4.20 MiB/s, done.", Progress{Phase: "Receiving objects", Percent: 100, Received: "812.11 MiB", Rate: "4.20 MiB/s"}, true},
		{"Resolving deltas:  45% (100/222)", Progress{Phase: "Resolving deltas", Percent: 45}, true},
		{"remote: Counting objects: 100% (12/12), done.", Progress{Phase: "Counting objects", Percent: 100}, true},
		{"remote: Enumerating objects: 1234, done.", Progress{}, false},
		{"Cloning into bare repository 'x.git'...", Progress{}, false},
		{"", Progress{}, false},
	} {
		got, ok := ParseProgress(tc.line)
		if ok != tc.ok || got != tc.want {
			t.Errorf("ParseProgress(%q) = %+v, %v; want %+v, %v", tc.line, got, ok, tc.want, tc.ok)
		}
	}
}

func TestCloneBareAndFetchReportProgress(t *testing.T) {
	requireGit(t)
	root := t.TempDir()
	upstream(t, root, "acme/widget")
	srv := server(t, root, testToken)
	r := Runner{AskPass: self(t)}

	var mu sync.Mutex
	var seen []Progress
	record := func(p Progress) { mu.Lock(); seen = append(seen, p); mu.Unlock() }

	dest := filepath.Join(t.TempDir(), "widget.git")
	if err := r.CloneBare(context.Background(), srv.URL+"/acme/widget.git", dest, []byte(testToken), record); err != nil {
		t.Fatalf("CloneBare: %v", err)
	}
	if len(seen) == 0 {
		t.Fatal("a clone reported no progress")
	}
	last := seen[len(seen)-1]
	if last.Percent != 100 || last.Phase == "" {
		t.Fatalf("the last progress = %+v, want a phase at 100%%", last)
	}

	if err := r.Fetch(context.Background(), dest, []byte("wrong"), record); err == nil || strings.Contains(err.Error(), "%") {
		t.Fatalf("a refused fetch: %v; want git's reason, not a progress line", err)
	}
}
