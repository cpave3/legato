package sandbox

import (
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
)

var updateSeatbelt = flag.Bool("update-seatbelt", false, "rewrite testdata/seatbelt with today's profiles")

// macPlan is what BuildSandboxPlan makes on a Mac, in brief: the system, the
// scratch home and tmp, the work, a cache, a read-only toolchain, a folder
// hidden inside a mount, and the sockets.
func macPlan() Plan {
	return Plan{
		Home:       "/Users/me",
		HomeSource: "/Users/me/.local/share/legato/sandbox/ex-1/home",
		Chdir:      "/Users/me/Projects/app",
		Mounts: []Mount{
			{Kind: ReadOnly, Path: "/usr"},
			{Kind: Symlink, Path: "/etc", Target: "private/etc"},
			{Kind: ReadOnly, Path: "/private/etc"},
			{Kind: ReadWrite, Path: "/Users/me/.local/share/legato/sandbox/ex-1/tmp"},
			{Kind: ReadOnly, Path: "/Users/me/.local/bin"},
			{Kind: ReadWrite, Path: "/Users/me/Library/Caches/go-build"},
			{Kind: ReadWrite, Path: "/Users/me/Projects/app"},
			{Kind: ReadOnly, Path: "/Users/me/Projects/app/.git/hooks"},
			{Kind: HideDir, Path: "/Users/me/Projects/app/secrets"},
			{Kind: Hide, Path: "/Users/me/Projects/app/config/legato.yaml"},
		},
		Setenv:   map[string]string{"HOME": "/Users/me/.local/share/legato/sandbox/ex-1/home", "TMPDIR": "/Users/me/.local/share/legato/sandbox/ex-1/tmp", "LEGATO_SANDBOXED": "1"},
		Unsetenv: []string{"GITHUB_TOKEN"},
		Sockets:  []string{"/private/tmp/legato-501/broker", "/private/tmp/legato-501/ssh/ex-1.sock"},
		Links:    []Link{{Path: "/Users/me/.local/share/legato/sandbox/ex-1/home/.local/bin", Target: "/Users/me/.local/bin"}},
	}
}

func TestTheSeatbeltProfileOfAKnownPlanDoesNotChange(t *testing.T) {
	profile, params, err := Seatbelt{}.Profile(macPlan(), "ex-1")
	if err != nil {
		t.Fatal(err)
	}
	got := profile + "\n;; params\n;; " + strings.Join(params, "\n;; ") + "\n"
	file := filepath.Join("testdata", "seatbelt", "mac.sb")
	if *updateSeatbelt {
		_ = os.MkdirAll(filepath.Dir(file), 0o755)
		if err := os.WriteFile(file, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(file)
	if err != nil {
		t.Fatalf("%v (run with -update-seatbelt to write it)", err)
	}
	if got != string(want) {
		t.Errorf("the profile changed; run with -update-seatbelt if that is meant, and review the diff")
	}
}

// Paths reach the profile only as parameters, so no path can end a string
// and add a rule of its own.
func TestPathsReachTheProfileOnlyAsParameters(t *testing.T) {
	plan := macPlan()
	plan.Mounts = append(plan.Mounts, Mount{Kind: ReadOnly, Path: `/Users/me/odd "path") (allow default`})
	profile, params, err := Seatbelt{}.Profile(plan, "ex-1")
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range plan.Mounts {
		if m.Kind != Symlink && (strings.Contains(profile, `"`+m.Path+`"`) || strings.Contains(profile, m.Path+`"`)) {
			t.Errorf("%s is written into the profile", m.Path)
		}
	}
	if !slices.ContainsFunc(params, func(p string) bool { return strings.HasSuffix(p, `=/Users/me/odd "path") (allow default`) }) {
		t.Errorf("the odd path is not a parameter: %q", params)
	}
	if strings.Contains(profile, "(allow default") {
		t.Error("the profile allows by default")
	}
	checkSExpressions(t, profile)
}

// Every rule about a path comes after every rule about a path that holds
// it: the last rule that matches wins, as bwrap's last mount covers.
func TestACoverComesAfterEveryMountThatHoldsIt(t *testing.T) {
	plan := macPlan()
	profile, params, err := Seatbelt{}.Profile(plan, "ex-1")
	if err != nil {
		t.Fatal(err)
	}
	name := map[string]string{}
	for _, p := range params {
		k, v, _ := strings.Cut(p, "=")
		name[v] = k
	}
	line := func(path, op string) int {
		for i, l := range strings.Split(profile, "\n") {
			if strings.Contains(l, `(param "`+name[path]+`")`) && strings.Contains(l, op) {
				return i
			}
		}
		return -1
	}
	hidden := line("/Users/me/Projects/app/secrets", "(deny file-read* file-write*")
	work := line("/Users/me/Projects/app", "(allow file-read* file-write*")
	hooks := line("/Users/me/Projects/app/.git/hooks", "(deny file-write*")
	if hidden < 0 || work < 0 || hooks < 0 || hidden < work || hooks < work {
		t.Fatalf("work at %d, its read-only hooks at %d, its hidden folder at %d:\n%s", work, hooks, hidden, profile)
	}
}

// A folder that holds a hidden or read-only path inside a writable one
// cannot be renamed, or what it holds would be at a path no rule names.
func TestWhatHoldsACoverInsideWritableWorkCannotBeRenamed(t *testing.T) {
	profile, params, err := Seatbelt{}.Profile(macPlan(), "ex-1")
	if err != nil {
		t.Fatal(err)
	}
	protected := map[string]bool{}
	re := regexp.MustCompile(`\(deny file-write-unlink \(require-all \(vnode-type DIRECTORY\) \(literal \(param "(P\d+)"\)\)\)\)`)
	value := map[string]string{}
	for _, p := range params {
		k, v, _ := strings.Cut(p, "=")
		value[k] = v
	}
	for _, m := range re.FindAllStringSubmatch(profile, -1) {
		protected[value[m[1]]] = true
	}
	for _, want := range []string{"/Users/me/Projects/app", "/Users/me/Projects/app/.git", "/Users/me/Projects/app/.git/hooks", "/Users/me/Projects/app/secrets", "/Users/me/Projects/app/config"} {
		if !protected[want] {
			t.Errorf("%s can be renamed", want)
		}
	}
	for path := range protected {
		if path == "/Users/me" || path == "/Users/me/Projects" {
			t.Errorf("%s, outside every writable mount, is named", path)
		}
	}
}

func TestSeatbeltRefusesWhatItCannotShow(t *testing.T) {
	for _, m := range []Mount{
		{Kind: ReadWrite, Path: "/Users/me/.local/share/legato/sandbox/ex-1/tmp", Dest: "/tmp"},
		{Kind: Overlay, Path: "/Users/me/Projects/app"},
	} {
		plan := macPlan()
		plan.Mounts = append(plan.Mounts, m)
		if _, _, err := (Seatbelt{}).Profile(plan, "ex-1"); err == nil {
			t.Errorf("rendered %+v", m)
		}
	}
	if (Seatbelt{}).Capabilities() != (Capabilities{}) {
		t.Error("Seatbelt claims a capability")
	}
}

// The sockets: the plan's, and DNS's; no other.
func TestOnlyThePlansSocketsCanBeReached(t *testing.T) {
	profile, params, err := Seatbelt{}.Profile(macPlan(), "ex-1")
	if err != nil {
		t.Fatal(err)
	}
	value := map[string]string{}
	for _, p := range params {
		k, v, _ := strings.Cut(p, "=")
		value[k] = v
	}
	re := regexp.MustCompile(`\(allow network-outbound \(remote unix-socket \((?:subpath|path-literal) (?:\(param "(P\d+)"\)|"([^"]+)")\)\)\)`)
	var reached []string
	for _, m := range re.FindAllStringSubmatch(profile, -1) {
		if m[1] != "" {
			reached = append(reached, value[m[1]])
		} else {
			reached = append(reached, m[2])
		}
	}
	slices.Sort(reached)
	want := []string{"/Users/me/.local/share/legato/sandbox/ex-1/home", "/Users/me/.local/share/legato/sandbox/ex-1/tmp", "/Users/me/Library/Caches/go-build",
		"/Users/me/Projects/app", "/private/tmp/legato-501/broker", "/private/tmp/legato-501/ssh/ex-1.sock", "/private/var/run/mDNSResponder"}
	if strings.Join(reached, " ") != strings.Join(want, " ") {
		t.Fatalf("unix sockets reached:\n%q\nwant\n%q", reached, want)
	}
}

// Prefix writes the profile and the launch spec into the execution's own
// folder, makes the plan's links, and returns a short line.
func TestPrefixWritesTheProfileTheSpecAndTheLinks(t *testing.T) {
	dir := t.TempDir()
	home := filepath.Join(dir, "home")
	plan := macPlan()
	plan.Links = []Link{{Path: filepath.Join(home, ".local/bin"), Target: "/Users/me/.local/bin"}, {Path: filepath.Join(dir, "bin", "engram"), Target: "/opt/engram"}}
	if err := os.MkdirAll(filepath.Join(dir, "bin"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "bin", "engram"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	line, err := Seatbelt{Launcher: "/opt/legato bin/legato"}.Prefix(plan, dir)
	if err != nil {
		t.Fatal(err)
	}
	if want := "'/opt/legato bin/legato' sandbox exec " + ShellWord(filepath.Join(dir, "seatbelt.json")) + " --"; line != want {
		t.Fatalf("line %q, want %q", line, want)
	}
	for _, l := range plan.Links {
		if target, err := os.Readlink(l.Path); err != nil || target != l.Target {
			t.Errorf("link %s -> %q, %v", l.Path, target, err)
		}
	}
	data, err := os.ReadFile(filepath.Join(dir, "seatbelt.json"))
	if err != nil {
		t.Fatal(err)
	}
	var spec LaunchSpec
	if err := json.Unmarshal(data, &spec); err != nil {
		t.Fatal(err)
	}
	if spec.Exec != "/usr/bin/sandbox-exec" || spec.Chdir != plan.Chdir || spec.Setenv["HOME"] != plan.HomeSource || !slices.Contains(spec.Unsetenv, "GITHUB_TOKEN") {
		t.Fatalf("spec %+v", spec)
	}
	if info, err := os.Stat(spec.Profile); err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("profile %s: %v", spec.Profile, err)
	}
	argv := spec.Argv("/dev/ttys004", []string{"claude", "--flag"})
	if argv[0] != "/usr/bin/sandbox-exec" || argv[1] != "-f" || argv[2] != spec.Profile || !slices.Contains(argv, "-DTTY=/dev/ttys004") ||
		strings.Join(argv[len(argv)-3:], " ") != "-- claude --flag" {
		t.Fatalf("argv %q", argv)
	}
	env := spec.Environ([]string{"GITHUB_TOKEN=ghp_x", "HOME=/Users/me", "PATH=/usr/bin"})
	if slices.Contains(env, "GITHUB_TOKEN=ghp_x") || !slices.Contains(env, "HOME="+plan.HomeSource) || !slices.Contains(env, "PATH=/usr/bin") {
		t.Fatalf("environment %q", env)
	}
}

// checkSExpressions says the profile's parentheses balance, outside strings
// and comments, and every rule is an operation Seatbelt knows.
func checkSExpressions(t *testing.T, profile string) {
	t.Helper()
	depth, inString := 0, false
	for _, line := range strings.Split(profile, "\n") {
		for i := 0; i < len(line); i++ {
			c := line[i]
			switch {
			case inString && c == '\\':
				i++
			case c == '"':
				inString = !inString
			case inString:
			case c == ';':
				i = len(line)
			case c == '(':
				depth++
			case c == ')':
				depth--
				if depth < 0 {
					t.Fatalf("unbalanced: %s", line)
				}
			}
		}
	}
	if depth != 0 || inString {
		t.Fatalf("unbalanced profile (depth %d)", depth)
	}
	rule := regexp.MustCompile(`(?m)^\((allow|deny|version) ?([a-z*-]*)`)
	known := map[string]bool{"": true, "default": true, "process-fork": true, "process-exec": true, "signal": true, "process-info*": true,
		"mach-priv-task-port": true, "sysctl-read": true, "sysctl-write": true, "iokit-open": true, "iokit-get-properties": true,
		"ipc-posix-sem": true, "ipc-posix-shm": true, "ipc-posix-shm-read*": true, "file-read*": true, "file-write*": true, "file-read-metadata": true,
		"file-ioctl": true, "pseudo-tty": true, "mach-lookup": true, "user-preference-read": true, "network-outbound": true,
		"network-inbound": true, "network-bind": true, "system-socket": true, "file-write-unlink": true, "job-creation": true, "lsopen": true,
		"appleevent-send": true, "file-write-setugid": true, "system-fcntl": true}
	for _, m := range rule.FindAllStringSubmatch(profile, -1) {
		if !known[m[2]] {
			t.Errorf("unknown operation %q", m[2])
		}
	}
}
