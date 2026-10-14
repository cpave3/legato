package libretto

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// fake writes a program named libretto into a new folder and returns a
// Runner that finds it there. body is the shell script after the shebang.
func fake(t *testing.T, body string) Runner {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "libretto")
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+body), 0o755); err != nil {
		t.Fatal(err)
	}
	return Runner{LookPath: func(string) (string, error) { return path, nil }}
}

func TestProbeSaysWhatTheInstalledLibrettoCanDo(t *testing.T) {
	r := fake(t, `[ "$1 $2" = "version --json" ] && echo '{"version": "0.4.0", "manifest": 1, "studio": 5}'`)
	info, err := r.Probe(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if info != (Info{Version: "0.4.0", Manifest: 1, Studio: 5}) {
		t.Errorf("info = %+v", info)
	}
}

func TestProbeTellsAMissingLibrettoFromOneTooOld(t *testing.T) {
	missing := Runner{LookPath: func(string) (string, error) { return "", errors.New("not found") }}
	_, err := missing.Probe(context.Background())
	if !errors.Is(err, ErrNotInstalled) {
		t.Fatalf("with no libretto on PATH: got %v, want ErrNotInstalled", err)
	}
	if text := Explain(err); !strings.Contains(text, "libretto is not installed") || !strings.Contains(text, "install") {
		t.Errorf("explained as %q; want it to say what is missing and what to do", text)
	}

	// A libretto from before the studio has no version command at all.
	_, err = fake(t, `echo 'unknown command "version" for "libretto"' >&2; exit 1`).Probe(context.Background())
	var old *TooOldError
	if !errors.As(err, &old) || old.Studio != 0 {
		t.Fatalf("with a libretto that has no version command: got %v, want a TooOldError at level 0", err)
	}
	// One that has the command, and a studio older than Legato needs.
	_, err = fake(t, `echo '{"version": "0.3.9", "manifest": 1, "studio": 0}'`).Probe(context.Background())
	if !errors.As(err, &old) || old.Version != "0.3.9" {
		t.Fatalf("with an older studio: got %v, want a TooOldError naming 0.3.9", err)
	}
	if text := Explain(err); !strings.Contains(text, "too old") || !strings.Contains(text, "0.3.9") || !strings.Contains(text, "update") {
		t.Errorf("explained as %q; want it to say it is too old, which it is, and what to do", text)
	}
	// Anything else is said as it is.
	if text := Explain(errors.New("the disk is full")); text != "the disk is full" {
		t.Errorf("another error explained as %q", text)
	}
}

func TestStartStudioFindsWhereItListensAndStopsIt(t *testing.T) {
	// The real studio prints where it listens and then serves until its
	// input closes. This one prints the address of a server of the test's.
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { fmt.Fprint(w, "studio") }))
	defer upstream.Close()
	marker := filepath.Join(t.TempDir(), "args")
	r := fake(t, fmt.Sprintf(`echo "$@" > %q
echo "warming up"
echo "studio: %s"
echo "stop it with Ctrl-C"
cat > /dev/null
`, marker, upstream.URL))

	studio, err := r.StartStudio(context.Background(), "/talks/storyboard.json")
	if err != nil {
		t.Fatal(err)
	}
	if studio.URL != upstream.URL {
		t.Errorf("url = %q, want %q", studio.URL, upstream.URL)
	}
	args, _ := os.ReadFile(marker)
	for _, want := range []string{"studio /talks/storyboard.json", "--port 0", "--until-stdin-closes"} {
		if !strings.Contains(string(args), want) {
			t.Errorf("libretto was run with %q; want %q in it", strings.TrimSpace(string(args)), want)
		}
	}
	select {
	case <-studio.Exited:
		t.Fatal("the studio exited while it was in use")
	case <-time.After(100 * time.Millisecond):
	}
	studio.Stop()
	select {
	case <-studio.Exited:
	case <-time.After(5 * time.Second):
		t.Fatal("the studio was still running after Stop")
	}
}

func TestStartStudioSaysWhyAStudioDidNotStart(t *testing.T) {
	_, err := fake(t, `echo "libretto: there is no storyboard at /nowhere.json" >&2; exit 1`).StartStudio(context.Background(), "/nowhere.json")
	if err == nil || !strings.Contains(err.Error(), "there is no storyboard at /nowhere.json") {
		t.Errorf("got %v; want the studio's own words", err)
	}
	missing := Runner{LookPath: func(string) (string, error) { return "", errors.New("not found") }}
	if _, err := missing.StartStudio(context.Background(), "/talks/storyboard.json"); !errors.Is(err, ErrNotInstalled) {
		t.Errorf("with no libretto: got %v, want ErrNotInstalled", err)
	}
}
