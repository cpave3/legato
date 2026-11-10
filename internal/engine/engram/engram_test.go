package engram

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

// fakeEngram writes a shell script that answers as the engram CLI does under
// the contract Legato relies on (LEG-107), and returns its path. body is the
// script's case arms, matched against "$*".
func fakeEngram(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "engram")
	script := "#!/bin/sh\ncase \"$*\" in\n" + body + "\n*) echo \"unexpected: $*\" >&2; exit 1;;\nesac\n"
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

const whereJSON = `{"version":"v0.2.0","binary":"/opt/engram/bin/engram","store":{"root":"/home/me/.local/share/engram","source":"default","exists":true}}`

func TestProbeReadsWhere(t *testing.T) {
	bin := fakeEngram(t, `"where --json") echo '`+whereJSON+`';;`)
	got, err := Runner{Command: bin}.Probe(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	want := Where{Version: "v0.2.0", Binary: "/opt/engram/bin/engram", Store: Store{Root: "/home/me/.local/share/engram", Source: "default", Exists: true}}
	if got.Version != want.Version || got.Binary != want.Binary || got.Store != want.Store || got.Place != nil {
		t.Fatalf("Probe = %+v, want %+v", got, want)
	}
}

func TestProbeWithoutAnEngramCommandIsNotInstalled(t *testing.T) {
	r := Runner{LookPath: func(string) (string, error) { return "", errors.New("not found") }}
	if _, err := r.Probe(context.Background()); !errors.Is(err, ErrNotInstalled) {
		t.Fatalf("Probe error = %v, want ErrNotInstalled", err)
	}
	r = Runner{Command: filepath.Join(t.TempDir(), "absent")}
	if _, err := r.Probe(context.Background()); !errors.Is(err, ErrNotInstalled) {
		t.Fatalf("Probe of a missing command error = %v, want ErrNotInstalled", err)
	}
}

func TestProbeFindsEngramOnPathWhenNoCommandIsSet(t *testing.T) {
	bin := fakeEngram(t, `"where --json") echo '`+whereJSON+`';;`)
	var asked string
	r := Runner{LookPath: func(name string) (string, error) { asked = name; return bin, nil }}
	if _, err := r.Probe(context.Background()); err != nil {
		t.Fatal(err)
	}
	if asked != "engram" {
		t.Fatalf("looked up %q, want engram", asked)
	}
}

func TestProbeOfAnEngramWithoutWhereFails(t *testing.T) {
	// An engram from before `where` says "unknown command" and exits 2.
	bin := fakeEngram(t, `"where --json") echo 'unknown command "where"' >&2; exit 2;;`)
	_, err := Runner{Command: bin}.Probe(context.Background())
	if err == nil || !strings.Contains(err.Error(), "unknown command") {
		t.Fatalf("Probe error = %v, want engram's message", err)
	}
}

func TestProbeTimesOut(t *testing.T) {
	bin := fakeEngram(t, `"where --json") sleep 5;;`)
	start := time.Now()
	_, err := Runner{Command: bin, ProbeTimeout: 100 * time.Millisecond}.Probe(context.Background())
	if !errors.Is(err, ErrTimeout) {
		t.Fatalf("Probe error = %v, want ErrTimeout", err)
	}
	if time.Since(start) > 3*time.Second {
		t.Fatal("Probe waited for the command instead of timing out")
	}
}

func TestListReadsIdentities(t *testing.T) {
	bin := fakeEngram(t, `"identity list --json") echo '[{"name":"argus","created":"2026-10-01T09:00:00Z","summary":"Watches PRs"},{"name":"mneme","created":"2026-10-02T09:00:00Z","summary":""}]';;`)
	got, err := Runner{Command: bin}.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Name != "argus" || got[0].Summary != "Watches PRs" || got[1].Name != "mneme" {
		t.Fatalf("List = %+v", got)
	}
	if !got[0].Created.Equal(time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC)) {
		t.Fatalf("created = %v", got[0].Created)
	}
}

func TestListWithNoStoreIsEmpty(t *testing.T) {
	bin := fakeEngram(t, `"identity list --json") echo '[]';;`)
	got, err := Runner{Command: bin}.List(context.Background())
	if err != nil || len(got) != 0 {
		t.Fatalf("List = %+v, %v", got, err)
	}
}

func TestPlaceReadsTheDirectorysScopes(t *testing.T) {
	dir := t.TempDir()
	bin := fakeEngram(t, `"where --dir `+dir+` --json") echo '{"version":"v0.2.0","binary":"/b","store":{"root":"/s","source":"default","exists":true},"place":{"primary":"repo:github.com/cpave3/legato","scopes":["repo:github.com/cpave3/legato","dir:/home/me/legato"]}}';;`)
	got, err := Runner{Command: bin}.Place(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	if got.Primary != "repo:github.com/cpave3/legato" || !slices.Equal(got.Scopes, []string{"repo:github.com/cpave3/legato", "dir:/home/me/legato"}) {
		t.Fatalf("Place = %+v", got)
	}
}

func TestPlaceWithoutAPlaceFails(t *testing.T) {
	dir := t.TempDir()
	bin := fakeEngram(t, `"where --dir `+dir+` --json") echo '`+whereJSON+`';;`)
	if _, err := (Runner{Command: bin}).Place(context.Background(), dir); err == nil {
		t.Fatal("Place without a place in the answer did not fail")
	}
}

func TestRepoKey(t *testing.T) {
	cases := map[[2]string]string{
		{"github.com", "cpave3/legato"}:     "repo:github.com/cpave3/legato",
		{"GitHub.com", "CPave3/Legato"}:     "repo:github.com/cpave3/legato",
		{"", "cpave3/legato"}:               "repo:github.com/cpave3/legato",
		{"git.example.com", "team/Service"}: "repo:git.example.com/team/service",
	}
	for in, want := range cases {
		if got := RepoKey(in[0], in[1]); got != want {
			t.Errorf("RepoKey(%q, %q) = %q, want %q", in[0], in[1], got, want)
		}
	}
}

func TestPlaceKeyKinds(t *testing.T) {
	if KindOf("repo:github.com/a/b") != KindRepo || KindOf("dir:/x") != KindFolder {
		t.Fatal("KindOf misreads a prefix")
	}
	if Label("repo:github.com/a/b") != "github.com/a/b" || Label("dir:/home/me/x") != "/home/me/x" {
		t.Fatal("Label keeps the prefix")
	}
}

func TestBootReturnsThePacketAndStderrApart(t *testing.T) {
	dir := t.TempDir()
	bin := fakeEngram(t, `"boot") [ "$ENGRAM_IDENTITY" = mneme ] || exit 9; [ "$(pwd)" = "`+dir+`" ] || exit 8; [ -z "$LEAK" ] || exit 7; echo "# mneme"; echo "## Keeping yourself current"; echo "warning: index rebuilt" >&2;;`)
	t.Setenv("LEAK", "from legato")
	got, err := Runner{Command: bin}.Boot(context.Background(), []string{"ENGRAM_IDENTITY=mneme"}, dir)
	if err != nil {
		t.Fatalf("Boot: %v", err)
	}
	if got.Packet != "# mneme\n## Keeping yourself current\n" {
		t.Fatalf("packet = %q", got.Packet)
	}
	if got.Stderr != "warning: index rebuilt\n" {
		t.Fatalf("stderr = %q", got.Stderr)
	}
}

func TestBootFailuresAreTyped(t *testing.T) {
	cases := []struct {
		name string
		arm  string
		want error
	}{
		{"unknown engram", `"boot") echo 'unknown identity "ghost"' >&2; exit 3;;`, ErrUnknownIdentity},
		{"missing store", `"boot") echo 'no store at /x' >&2; exit 4;;`, ErrMissingStore},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			bin := fakeEngram(t, tc.arm)
			_, err := Runner{Command: bin}.Boot(context.Background(), nil, t.TempDir())
			if !errors.Is(err, tc.want) {
				t.Fatalf("Boot error = %v, want %v", err, tc.want)
			}
			var e *Error
			if !errors.As(err, &e) || e.Message == "" {
				t.Fatalf("Boot error %v carries no engram message", err)
			}
		})
	}
}

func TestBootOtherFailureKeepsEngramsMessage(t *testing.T) {
	bin := fakeEngram(t, `"boot") echo 'disk is full' >&2; exit 1;;`)
	_, err := Runner{Command: bin}.Boot(context.Background(), nil, t.TempDir())
	var e *Error
	if !errors.As(err, &e) || e.Code != 1 || e.Message != "disk is full" {
		t.Fatalf("Boot error = %#v", err)
	}
	if errors.Is(err, ErrUnknownIdentity) || errors.Is(err, ErrMissingStore) {
		t.Fatal("an I/O error reads as a typed failure")
	}
}

func TestBootTimesOut(t *testing.T) {
	bin := fakeEngram(t, `"boot") sleep 5;;`)
	_, err := Runner{Command: bin, BootTimeout: 100 * time.Millisecond}.Boot(context.Background(), nil, t.TempDir())
	if !errors.Is(err, ErrTimeout) {
		t.Fatalf("Boot error = %v, want ErrTimeout", err)
	}
}

func TestEndSession(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "ended")
	bin := fakeEngram(t, `"session end") echo "$ENGRAM_SESSION" > `+marker+`;;`)
	if err := (Runner{Command: bin}).EndSession(context.Background(), []string{"ENGRAM_SESSION=ex-1"}, t.TempDir()); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(marker)
	if strings.TrimSpace(string(got)) != "ex-1" {
		t.Fatalf("session end saw ENGRAM_SESSION=%q", got)
	}
}

func TestEnvironmentIsExplicit(t *testing.T) {
	t.Setenv("ENGRAM_IDENTITY", "from-legato")
	bin := fakeEngram(t, `"where --json") [ -z "$ENGRAM_IDENTITY" ] || { echo leaked >&2; exit 1; }; [ "$ENGRAM_HOME" = /store ] || { echo "home=$ENGRAM_HOME" >&2; exit 1; }; echo '`+whereJSON+`';;`)
	if _, err := (Runner{Command: bin, Env: []string{"ENGRAM_HOME=/store"}}).Probe(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestHostEnvironmentKeepsOnlyWhatFindsTheStore(t *testing.T) {
	env := HostEnvironment(func(k string) string {
		return map[string]string{"HOME": "/home/me", "PATH": "/bin", "XDG_DATA_HOME": "/data", "ENGRAM_HOME": "/s", "ENGRAM_IDENTITY": "x", "SECRET": "y"}[k]
	})
	slices.Sort(env)
	want := []string{"ENGRAM_HOME=/s", "HOME=/home/me", "PATH=/bin", "XDG_DATA_HOME=/data"}
	if !slices.Equal(env, want) {
		t.Fatalf("HostEnvironment = %v, want %v", env, want)
	}
}
