package service

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cpave3/legato/internal/engine/sandbox"
)

// bwrapFixture is a fake home with credentials in it, a working folder and
// a runtime folder, outside /tmp, which the sandbox replaces.
func bwrapFixture(t *testing.T) (in SandboxPlanInput) {
	t.Helper()
	if _, err := exec.LookPath("bwrap"); err != nil {
		t.Skip("bwrap is not installed")
	}
	if err := exec.Command("bwrap", "--ro-bind", "/", "/", "--unshare-pid", "true").Run(); err != nil {
		t.Skipf("bwrap cannot run here: %v", err)
	}
	base, err := os.MkdirTemp("/var/tmp", "lgsbx")
	if err != nil {
		t.Skip("no /var/tmp")
	}
	t.Cleanup(func() { _ = os.RemoveAll(base) })
	home := filepath.Join(base, "home")
	for _, dir := range []string{".ssh", ".config/gh", ".config/legato", ".local/share/legato", "work", ".claude"} {
		_ = os.MkdirAll(filepath.Join(home, dir), 0o700)
	}
	_ = os.WriteFile(filepath.Join(home, ".ssh/id_ed25519"), []byte("PRIVATE"), 0o600)
	_ = os.WriteFile(filepath.Join(home, ".config/gh/hosts.yml"), []byte("oauth_token: gho_x"), 0o600)
	_ = os.WriteFile(filepath.Join(home, ".config/legato/config.yaml"), []byte("db: {}"), 0o600)
	runtime := filepath.Join(base, "run", "legato")
	_ = os.MkdirAll(filepath.Join(runtime, "sandbox", "ex-1", "tmp"), 0o700)
	_ = os.MkdirAll(filepath.Join(runtime, "broker"), 0o700)
	return SandboxPlanInput{
		Home: home, WorkDir: filepath.Join(home, "work"), ExecutionID: "ex-1", TaskID: "t1", AgentKind: "claude-code",
		RuntimeDir: runtime, DataDir: filepath.Join(home, ".local/share/legato"), ConfigPath: filepath.Join(home, ".config/legato/config.yaml"),
		BrokerSocket: filepath.Join(runtime, "broker", "broker.sock"),
	}
}

func runInSandbox(t *testing.T, in SandboxPlanInput, script string) (string, error) {
	t.Helper()
	plan, err := BuildSandboxPlan(in)
	if err != nil {
		t.Fatal(err)
	}
	args, err := sandbox.Bwrap{}.Wrap(plan, []string{"sh", "-c", script})
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(args[0], args[1:]...)
	cmd.Env = append(os.Environ(), "GITHUB_TOKEN=ghp_secret", "OP_SERVICE_ACCOUNT_TOKEN=ops_secret")
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func TestInsideTheSandboxCredentialsAreAbsentAndWorkIsWritable(t *testing.T) {
	in := bwrapFixture(t)
	out, err := runInSandbox(t, in, `
for p in "$HOME/.ssh/id_ed25519" "$HOME/.config/gh/hosts.yml" "$HOME/.config/legato/config.yaml" "$HOME/.local/share/legato"; do
  [ -e "$p" ] && echo "VISIBLE $p"
done
echo "token=${GITHUB_TOKEN:-none} op=${OP_SERVICE_ACCOUNT_TOKEN:-none} sandboxed=$LEGATO_SANDBOXED"
touch work.txt && echo wrote-work
echo tmp > /tmp/note && echo wrote-tmp
[ -d "$(dirname "$LEGATO_BROKER_SOCKET_DIR")" ]
[ -d `+in.RuntimeDir+`/broker ] && echo broker-visible
[ -e `+in.RuntimeDir+`/sandbox ] || echo runtime-hidden
`)
	if err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	if strings.Contains(out, "VISIBLE") {
		t.Fatalf("a credential is visible:\n%s", out)
	}
	for _, want := range []string{"token=none op=none sandboxed=1", "wrote-work", "wrote-tmp", "broker-visible"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	if _, err := os.Stat(filepath.Join(in.WorkDir, "work.txt")); err != nil {
		t.Error("the work written inside is not on the host")
	}
	if b, err := os.ReadFile(filepath.Join(in.RuntimeDir, "sandbox", "ex-1", "tmp", "note")); err != nil || string(b) != "tmp\n" {
		t.Errorf("/tmp inside is not the session's host folder: %q %v", b, err)
	}
}

func TestEverythingInsideEndsWithThePane(t *testing.T) {
	in := bwrapFixture(t)
	plan, err := BuildSandboxPlan(in)
	if err != nil {
		t.Fatal(err)
	}
	args, _ := sandbox.Bwrap{}.Wrap(plan, []string{"sh", "-c", "sleep 61237 & sleep 61237"})
	pane := exec.Command("sh", "-c", strings.Join(quoteAll(args), " "))
	if err := pane.Start(); err != nil {
		t.Fatal(err)
	}
	time.Sleep(500 * time.Millisecond)
	if out, _ := exec.Command("pgrep", "-f", "^sleep 61237$").Output(); len(strings.Fields(string(out))) == 0 {
		t.Fatal("the sandboxed processes did not start")
	}
	_ = pane.Process.Kill()
	_ = pane.Wait()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if out, _ := exec.Command("pgrep", "-f", "^sleep 61237$").Output(); len(strings.Fields(string(out))) == 0 {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	out, _ := exec.Command("pgrep", "-af", "^sleep 61237$").Output()
	_ = exec.Command("pkill", "-f", "^sleep 61237$").Run()
	t.Fatalf("processes in the sandbox outlived the pane:\n%s", out)
}

func quoteAll(args []string) []string {
	out := make([]string, len(args))
	for i, a := range args {
		out[i] = shellWord(a)
	}
	return out
}

func TestSSHRunsInsideTheSandboxWithTheConfiguredKnownHosts(t *testing.T) {
	in := bwrapFixture(t)
	if _, err := exec.LookPath("ssh"); err != nil {
		t.Skip("ssh is not installed")
	}
	if _, err := os.Stat("/etc/ssh/ssh_config"); err != nil {
		t.Skip("no /etc/ssh/ssh_config")
	}
	knownHosts := filepath.Join(in.Home, ".ssh", "known_hosts")
	if err := os.WriteFile(knownHosts, []byte("psi.example ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIOMqqnkVzrm0SdG6UOoqKLsabgH5C9okWi0dh2l9GKJl\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	in.SSHConfigDir = filepath.Join(in.RuntimeDir, "sandbox", "ex-1", "ssh")
	in.KnownHosts = knownHosts
	copies, err := writeSandboxSSHConfig("/etc/ssh", in.SSHConfigDir)
	if err != nil {
		t.Fatal(err)
	}
	in.SSHConfig = copies

	out, err := runInSandbox(t, in, `ssh -G psi.example | grep -i '^userknownhostsfile' | cut -d' ' -f2 | xargs cat; test ! -e ~/.ssh/id_ed25519 && echo key-hidden`)
	if err != nil || !strings.Contains(out, "psi.example ssh-ed25519") || !strings.Contains(out, "key-hidden") {
		t.Fatalf("err=%v out=%s", err, out)
	}
}

func TestTheSandboxStartsFromItsArgumentsFile(t *testing.T) {
	in := bwrapFixture(t)
	plan, err := BuildSandboxPlan(in)
	if err != nil {
		t.Fatal(err)
	}
	args, err := sandbox.Bwrap{}.Wrap(plan, []string{"true"})
	if err != nil {
		t.Fatal(err)
	}
	args = args[:len(args)-1]
	line, err := argsFileCommand(args[0], args[1:len(args)-1], filepath.Join(in.RuntimeDir, "sandbox", "ex-1", "bwrap.args"))
	if err != nil {
		t.Fatal(err)
	}
	if len(line) > 300 {
		t.Fatalf("typed line is %d bytes: %s", len(line), line)
	}
	out, err := exec.Command("sh", "-c", line+` sh -c 'test ! -e "$HOME/.ssh/id_ed25519" && test -d "$PWD" && echo inside; ls /proc/self/fd'`).CombinedOutput()
	if err != nil || !strings.Contains(string(out), "inside") {
		t.Fatalf("err=%v out=%s", err, out)
	}
	t.Logf("open fds inside: %q", strings.Fields(strings.SplitN(string(out), "\n", 2)[1]))
}

func TestInsideTheSandboxEveryGHIsLegatosWrapper(t *testing.T) {
	in := bwrapFixture(t)
	base := filepath.Dir(in.Home)
	// Two gh on the host, one a link to the other, as Homebrew installs it.
	cellar := filepath.Join(base, "cellar")
	bin := filepath.Join(base, "brew-bin")
	_ = os.MkdirAll(cellar, 0o755)
	_ = os.MkdirAll(bin, 0o755)
	realGH := filepath.Join(cellar, "gh")
	_ = os.WriteFile(realGH, []byte("#!/bin/sh\necho \"real gh $*\"\n"), 0o755)
	_ = os.Symlink(realGH, filepath.Join(bin, "gh"))
	wrapperDir := filepath.Join(in.RuntimeDir, "bin")
	_ = os.MkdirAll(wrapperDir, 0o700)
	wrapper := filepath.Join(wrapperDir, "gh")
	_ = os.WriteFile(wrapper, []byte("#!/bin/sh\necho wrapper\nexec \"$LEGATO_GH_REAL\" \"$@\"\n"), 0o700)
	in.CredentialWrapperDir = wrapperDir
	in.Extra = append(in.Extra, SandboxPath{Path: cellar}, SandboxPath{Path: bin})
	in.GH = SandboxGH{Wrapper: wrapper, Real: realGH, Overlays: []string{realGH}, RealMount: filepath.Join(in.RuntimeDir, "sandbox", "ex-1", "gh-real")}
	_ = os.WriteFile(in.GH.RealMount, nil, 0o600)

	// Whatever order a login shell puts PATH in, gh is the wrapper.
	out, err := runInSandbox(t, in, `PATH=`+bin+`:$PATH gh pr list; `+realGH+` --version`)
	if err != nil || strings.Count(out, "wrapper") != 2 || !strings.Contains(out, "real gh pr list") || !strings.Contains(out, "real gh --version") {
		t.Fatalf("err=%v out=%s", err, out)
	}
}
