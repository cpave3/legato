package service

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/cpave3/legato/internal/engine/sandbox"
)

// The walls of Legato's sandbox, on whichever backend this host has:
// bubblewrap on Linux, Seatbelt on macOS. Each test builds the plan, runs
// the backend's own launch line (Prefix) as a pane would, and tries what an
// agent might. The contract is that a credential's content cannot be read:
// bwrap makes it absent, Seatbelt denies it. macOS's own walls are in
// sandbox_walls_darwin_test.go.

const (
	wallLauncherEnv = "LEGATO_TEST_SANDBOX_LAUNCHER"
	wallHelperEnv   = "LEGATO_TEST_WALL_HELPER"
)

// wallLaunch is `legato sandbox exec <spec> -- <command>` for the tests.
func wallLaunch(args []string) int {
	if len(args) < 3 || args[1] != "--" {
		fmt.Fprintln(os.Stderr, "usage: sandbox exec <spec> -- <command>")
		return 2
	}
	err := sandbox.ExecLaunchSpec(args[0], args[2:])
	fmt.Fprintln(os.Stderr, "launch:", err)
	return 1
}

// wallHelper is what the tests run inside a sandbox, from a copy of the
// test binary in the work folder.
func wallHelper(mode string, args []string) int {
	switch mode {
	case "connect":
		c, err := net.DialTimeout("unix", args[0], 5*time.Second)
		if err != nil {
			fmt.Println("refused:", err)
			return 1
		}
		defer c.Close()
		line, _ := bufio.NewReader(c).ReadString('\n')
		fmt.Println("connected:", strings.TrimSpace(line))
		return 0
	default:
		return wallHelperOS(mode, args)
	}
}

type wall struct {
	backend sandbox.Backend
	in      SandboxPlanInput
	base    string
	// helper is the test binary, copied into the work folder.
	helper string
}

// wallFixture is a fake home with credentials in it, a working folder, and
// a runtime folder, sandboxed by this host's backend.
func wallFixture(t *testing.T) wall {
	t.Helper()
	var w wall
	switch runtime.GOOS {
	case "linux":
		w.in = bwrapFixture(t)
		w.backend = sandbox.Bwrap{}
		w.base = filepath.Dir(w.in.Home)
	case "darwin":
		seatbelt := sandbox.Seatbelt{Launcher: os.Args[0]}
		if err := seatbelt.Available(); err != nil {
			t.Skipf("no Seatbelt here: %v", err)
		}
		w.backend = seatbelt
		base, err := os.MkdirTemp("", "lgwall")
		if err == nil {
			base, err = filepath.EvalSymlinks(base)
		}
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.RemoveAll(base) })
		w.base = base
		home := filepath.Join(base, "home")
		for _, dir := range []string{".ssh", ".config/gh", ".config/legato", ".local/share/legato", "work", ".claude", "Library/Keychains"} {
			_ = os.MkdirAll(filepath.Join(home, dir), 0o700)
		}
		_ = os.WriteFile(filepath.Join(home, ".ssh/id_ed25519"), []byte("PRIVATE"), 0o600)
		_ = os.WriteFile(filepath.Join(home, ".config/gh/hosts.yml"), []byte("oauth_token: gho_x"), 0o600)
		_ = os.WriteFile(filepath.Join(home, ".config/legato/config.yaml"), []byte("db: {}"), 0o600)
		runtimeDir := filepath.Join(base, "run", "legato")
		_ = os.MkdirAll(filepath.Join(runtimeDir, "sandbox", "ex-1"), 0o700)
		_ = os.MkdirAll(filepath.Join(runtimeDir, "broker"), 0o700)
		layout := DarwinLayout(os.Getuid())
		w.in = SandboxPlanInput{
			Home: home, WorkDir: filepath.Join(home, "work"), ExecutionID: "ex-1", TaskID: "t1", AgentKind: "claude-code",
			RuntimeDir: runtimeDir, DataDir: filepath.Join(home, ".local/share/legato"), ConfigPath: filepath.Join(home, ".config/legato/config.yaml"),
			BrokerSocket: filepath.Join(runtimeDir, "broker", "broker.sock"), Layout: &layout,
		}
		scratch := scratchFor(w.in)
		for _, dir := range []string{scratch.Tmp("ex-1"), scratch.Home("ex-1")} {
			_ = os.MkdirAll(dir, 0o700)
		}
	default:
		t.Skipf("no sandbox backend on %s", runtime.GOOS)
	}
	w.helper = filepath.Join(w.in.WorkDir, ".wallhelper")
	if err := copyFile(os.Args[0], w.helper, 0o700); err != nil {
		t.Fatal(err)
	}
	return w
}

func copyFile(from, to string, mode os.FileMode) error {
	src, err := os.Open(from)
	if err != nil {
		return err
	}
	defer src.Close()
	dst, err := os.OpenFile(to, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	if _, err := io.Copy(dst, src); err != nil {
		dst.Close()
		return err
	}
	return dst.Close()
}

// run runs a shell script in the sandbox, as a pane runs a launch command:
// the backend's line, then the command. The pane's environment has the
// person's HOME and a token or two.
func (w wall) run(t *testing.T, script string, env ...string) (string, error) {
	t.Helper()
	in := w.in
	caps := w.backend.Capabilities()
	in.Capabilities = &caps
	plan, err := BuildSandboxPlan(in)
	if err != nil {
		t.Fatal(err)
	}
	line, err := w.backend.Prefix(plan, filepath.Join(in.RuntimeDir, "sandbox", in.ExecutionID))
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("/bin/sh", "-c", line+" /bin/sh -c "+shellWord(script))
	cmd.Env = append(os.Environ(), "HOME="+in.Home, "GITHUB_TOKEN=ghp_secret", "OP_SERVICE_ACCOUNT_TOKEN=ops_secret", wallLauncherEnv+"=1")
	cmd.Env = append(cmd.Env, env...)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// listen serves a Unix socket that says hello, until the test ends.
func listen(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	l, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { l.Close() })
	go func() {
		for {
			c, err := l.Accept()
			if err != nil {
				return
			}
			_, _ = c.Write([]byte("hello\n"))
			c.Close()
		}
	}()
}

func TestWallsAnAgentCannotReadTheCredentialsOrLegatosState(t *testing.T) {
	w := wallFixture(t)
	home := w.in.Home
	out, err := w.run(t, fmt.Sprintf(`
for p in %q %q %q; do cat "$p" 2>/dev/null; done
ls %q 2>/dev/null | sed 's/^/LISTED:/'
ls %q 2>/dev/null | sed 's/^/LISTED:/'
echo "token=${GITHUB_TOKEN:-none} op=${OP_SERVICE_ACCOUNT_TOKEN:-none} sandboxed=$LEGATO_SANDBOXED"
`, filepath.Join(home, ".ssh/id_ed25519"), filepath.Join(home, ".config/gh/hosts.yml"), filepath.Join(home, ".config/legato/config.yaml"),
		filepath.Join(home, ".ssh"), filepath.Join(home, ".local/share/legato")))
	if err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	for _, secret := range []string{"PRIVATE", "gho_x", "db: {}", "LISTED:"} {
		if strings.Contains(out, secret) {
			t.Errorf("%q is readable inside:\n%s", secret, out)
		}
	}
	if !strings.Contains(out, "token=none op=none sandboxed=1") {
		t.Errorf("the environment kept a token:\n%s", out)
	}
}

// The work, the agent's tmp and its home are writable, and what is written
// lands on the host: the work in place, tmp and home in the scratch folders.
func TestWallsTheWorkTmpAndHomeAreWritable(t *testing.T) {
	w := wallFixture(t)
	out, err := w.run(t, `cd "$PWD" && echo w > work.txt && echo t > "${TMPDIR:-/tmp}/a" && echo h > "$HOME/b" && echo written`)
	if err != nil || !strings.Contains(out, "written") {
		t.Fatalf("%v: %s", err, out)
	}
	scratch := scratchFor(w.in)
	for path, want := range map[string]string{filepath.Join(w.in.WorkDir, "work.txt"): "w\n", filepath.Join(scratch.Tmp("ex-1"), "a"): "t\n", filepath.Join(scratch.Home("ex-1"), "b"): "h\n"} {
		if got, err := os.ReadFile(path); err != nil || string(got) != want {
			t.Errorf("%s = %q, %v", path, got, err)
		}
	}
	if _, err := os.Stat(filepath.Join(w.in.Home, "b")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("a write reached the person's home (%v)", err)
	}
}

// The broker's socket can be reached; a socket of the host's elsewhere
// cannot.
func TestWallsOnlyTheSessionsSocketsCanBeReached(t *testing.T) {
	w := wallFixture(t)
	listen(t, w.in.BrokerSocket)
	other := filepath.Join(w.base, "host", "other.sock")
	listen(t, other)
	out, _ := w.run(t, fmt.Sprintf("%q %q; %q %q", w.helper, w.in.BrokerSocket, w.helper, other), wallHelperEnv+"=connect")
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) < 2 || !strings.HasPrefix(lines[0], "connected: hello") || strings.Contains(lines[len(lines)-1], "connected") {
		t.Fatalf("broker then another socket:\n%s", out)
	}
}
