package service

import (
	"fmt"
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
	// Outside /tmp, which the sandbox replaces; LEGATO_BWRAP_TEST_DIR names
	// another folder where there is no /var/tmp (inside Legato's own
	// sandbox).
	parent := "/var/tmp"
	if dir := os.Getenv("LEGATO_BWRAP_TEST_DIR"); dir != "" {
		parent = dir
	}
	base, err := os.MkdirTemp(parent, "lgsbx")
	if err != nil {
		t.Skipf("no %s", parent)
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
	_ = os.MkdirAll(filepath.Join(runtime, "sandbox", "ex-1"), 0o700)
	_ = os.MkdirAll(filepath.Join(runtime, "broker"), 0o700)
	in = SandboxPlanInput{
		Home: home, WorkDir: filepath.Join(home, "work"), ExecutionID: "ex-1", TaskID: "t1", AgentKind: "claude-code",
		RuntimeDir: runtime, DataDir: filepath.Join(home, ".local/share/legato"), ConfigPath: filepath.Join(home, ".config/legato/config.yaml"),
		BrokerSocket: filepath.Join(runtime, "broker", "broker.sock"),
	}
	scratch := scratchFor(in)
	_ = os.MkdirAll(scratch.Tmp("ex-1"), 0o700)
	_ = os.MkdirAll(scratch.Home("ex-1"), 0o700)
	return in
}

// What an agent writes to /tmp and its home lands in its scratch folders on
// disk, where the host can read it and Legato can remove it.
func TestInsideTheSandboxTmpAndHomeAreTheScratchFolders(t *testing.T) {
	in := bwrapFixture(t)
	out, err := runInSandbox(t, in, `echo t > /tmp/a && echo h > "$HOME/b" && mkdir -p "$HOME/.cache/x" && ls -a "$HOME"`)
	if err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	if strings.Contains(out, ".ssh") {
		t.Fatalf("the home shows the person's: %s", out)
	}
	scratch := scratchFor(in)
	for path, want := range map[string]string{filepath.Join(scratch.Tmp("ex-1"), "a"): "t\n", filepath.Join(scratch.Home("ex-1"), "b"): "h\n"} {
		if got, err := os.ReadFile(path); err != nil || string(got) != want {
			t.Errorf("%s = %q, %v", path, got, err)
		}
	}
	if _, err := os.Stat(filepath.Join(in.Home, "b")); !os.IsNotExist(err) {
		t.Errorf("the write reached the person's home (%v)", err)
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
	// HOME is the fixture's, as an agent's pane has the person's: the test
	// process's own is under /tmp, which the sandbox replaces.
	cmd.Env = append(os.Environ(), "HOME="+in.Home, "GITHUB_TOKEN=ghp_secret", "OP_SERVICE_ACCOUNT_TOKEN=ops_secret")
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
	if b, err := os.ReadFile(filepath.Join(scratchFor(in).Tmp("ex-1"), "note")); err != nil || string(b) != "tmp\n" {
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
	line, err := sandbox.Bwrap{}.Prefix(plan, filepath.Join(in.RuntimeDir, "sandbox", "ex-1"))
	if err != nil {
		t.Fatal(err)
	}
	if len(line) > 300 {
		t.Fatalf("typed line is %d bytes: %s", len(line), line)
	}
	cmd := exec.Command("sh", "-c", line+` sh -c 'test ! -e "$HOME/.ssh/id_ed25519" && test -d "$PWD" && echo inside; ls /proc/self/fd'`)
	cmd.Env = append(os.Environ(), "HOME="+in.Home)
	out, err := cmd.CombinedOutput()
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
	in.GH = SandboxWrapper{Wrapper: wrapper, Real: realGH, Overlays: []string{realGH}, RealMount: filepath.Join(in.RuntimeDir, "sandbox", "ex-1", "gh-real")}
	_ = os.WriteFile(in.GH.RealMount, nil, 0o600)

	// Whatever order a login shell puts PATH in, gh is the wrapper.
	out, err := runInSandbox(t, in, `PATH=`+bin+`:$PATH gh pr list; `+realGH+` --version`)
	if err != nil || strings.Count(out, "wrapper") != 2 || !strings.Contains(out, "real gh pr list") || !strings.Contains(out, "real gh --version") {
		t.Fatalf("err=%v out=%s", err, out)
	}
}

func TestALinkedToolInTheToolchainFoldersWorksInside(t *testing.T) {
	in := bwrapFixture(t)
	base := filepath.Dir(in.Home)
	elsewhere := filepath.Join(base, "Projects", "staccato")
	_ = os.MkdirAll(elsewhere, 0o755)
	_ = os.WriteFile(filepath.Join(elsewhere, "st"), []byte("#!/bin/sh\necho stacked\n"), 0o755)
	_ = os.WriteFile(filepath.Join(elsewhere, "notes.txt"), []byte("not the tool"), 0o644)
	bin := filepath.Join(in.Home, ".local", "bin")
	_ = os.MkdirAll(bin, 0o755)
	_ = os.Symlink(filepath.Join(elsewhere, "st"), filepath.Join(bin, "st"))
	// A link into a credential is not followed.
	_ = os.Symlink(filepath.Join(in.Home, ".ssh", "id_ed25519"), filepath.Join(bin, "sneaky"))
	in.Extra = append(in.Extra, SandboxPath{Path: bin})

	out, err := runInSandbox(t, in, filepath.Join(bin, "st")+`; cat `+filepath.Join(bin, "sneaky")+` 2>/dev/null || echo key-closed; test -e `+filepath.Join(elsewhere, "notes.txt")+` || echo only-the-tool; echo x > `+filepath.Join(elsewhere, "st")+` 2>/dev/null || echo read-only`)
	if err != nil {
		t.Fatalf("err=%v out=%s", err, out)
	}
	for _, want := range []string{"stacked", "key-closed", "only-the-tool", "read-only"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in %s", want, out)
		}
	}
}

func TestSkillsLinkedThroughAChainOfLinksAreReadableInside(t *testing.T) {
	in := bwrapFixture(t)
	base := filepath.Dir(in.Home)
	registry := filepath.Join(base, "registry", "skills")
	_ = os.MkdirAll(filepath.Join(registry, "diagnose"), 0o755)
	_ = os.WriteFile(filepath.Join(registry, "diagnose", "SKILL.md"), []byte("# Diagnose"), 0o644)
	_ = os.MkdirAll(filepath.Join(registry, "other"), 0o755)
	// ~/.claude/skills/diagnose -> ../../.agents/skills/diagnose -> registry/diagnose
	agents := filepath.Join(in.Home, ".agents", "skills")
	_ = os.MkdirAll(agents, 0o755)
	_ = os.Symlink(filepath.Join(registry, "diagnose"), filepath.Join(agents, "diagnose"))
	skills := filepath.Join(in.Home, ".claude", "skills")
	_ = os.MkdirAll(skills, 0o755)
	_ = os.Symlink("../../.agents/skills/diagnose", filepath.Join(skills, "diagnose"))
	_ = os.Symlink("../../.ssh", filepath.Join(skills, "keys"))

	out, err := runInSandbox(t, in, `cat `+filepath.Join(skills, "diagnose", "SKILL.md")+`; echo; cat `+filepath.Join(skills, "keys", "id_ed25519")+` 2>/dev/null || echo keys-closed; test -e `+filepath.Join(registry, "other")+` || echo only-linked; touch `+filepath.Join(registry, "diagnose", "x")+` 2>/dev/null || echo read-only`)
	if err != nil {
		t.Fatalf("err=%v out=%s", err, out)
	}
	for _, want := range []string{"# Diagnose", "keys-closed", "only-linked", "read-only"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in %s", want, out)
		}
	}
}

// A job clone is in Legato's data folder, which the sandbox covers when a
// mount holds it (a resource of ~/.local/share, or a /home that is a
// folder). The agent still starts in the clone, and the rest stays hidden.
func TestAnAgentStartsInItsJobCloneWhenTheDataFolderIsCovered(t *testing.T) {
	in := bwrapFixture(t)
	clone := filepath.Join(in.DataDir, "jobs", "ex-1", "widget")
	if err := os.MkdirAll(clone, 0o700); err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(filepath.Join(clone, "main.go"), []byte("package main"), 0o600)
	_ = os.WriteFile(filepath.Join(in.DataDir, "legato.db"), []byte("SQLite"), 0o600)
	in.WorkDir, in.JobDir = clone, clone
	in.Extra = []SandboxPath{{Path: filepath.Dir(in.DataDir), Access: "read"}}

	out, err := runInSandbox(t, in, `pwd; [ -f main.go ] && echo clone-visible; [ -e `+in.DataDir+`/legato.db ] && echo DB-VISIBLE; true`)

	if err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	if !strings.Contains(out, clone) || !strings.Contains(out, "clone-visible") {
		t.Errorf("the agent did not start in its clone:\n%s", out)
	}
	if strings.Contains(out, "DB-VISIBLE") {
		t.Errorf("Legato's database is visible:\n%s", out)
	}
}

// A fresh clone is set up by `legato clone setup`, run in the agent's
// sandbox: it needs no configuration or database there, and what it does
// lands in the clone.
func TestACloneIsSetUpInsideTheSandbox(t *testing.T) {
	if testing.Short() {
		t.Skip("builds the legato binary")
	}
	in := bwrapFixture(t)
	bin := filepath.Join(t.TempDir(), "legato")
	build := exec.Command("go", "build", "-o", bin, "github.com/cpave3/legato/cmd/legato")
	build.Env = append(os.Environ(), "CGO_ENABLED=0")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}
	clone := filepath.Join(in.DataDir, "jobs", "ex-1", "widget")
	for _, args := range [][]string{{"init", "-q", "-b", "main", clone}, {"-C", clone, "commit", "-q", "--allow-empty", "-m", "c"}} {
		cmd := exec.Command("git", args...)
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
	}
	_ = os.WriteFile(filepath.Join(clone, ".legato.toml"), []byte("[setup]\nrun = [\"echo \\\"deps for $YG_BRANCH\\\" > installed.txt\"]\n"), 0o644)
	in.WorkDir, in.JobDir, in.BinaryPath = clone, clone, bin

	out, err := runInSandbox(t, in, bin+" clone setup")

	if err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	if got, _ := os.ReadFile(filepath.Join(clone, "installed.txt")); strings.TrimSpace(string(got)) != "deps for main" {
		t.Errorf("installed.txt = %q; out:\n%s", got, out)
	}
	if log, _ := os.ReadFile(filepath.Join(clone, ".git", "legato-setup.log")); !strings.Contains(string(log), "this clone is set up") {
		t.Errorf("log = %q", log)
	}
}

// Pasted images live under the host's /tmp, which the sandbox covers with its
// own. The task's folder still shows through at the path the message names,
// read only, and other tasks' folders beside it do not.
func TestInsideTheSandboxAnAgentReadsItsTasksPastedImageAndNoOthers(t *testing.T) {
	in := bwrapFixture(t)
	root, err := os.MkdirTemp("/tmp", "lgpaste")
	if err != nil {
		t.Skip("no /tmp")
	}
	t.Cleanup(func() { _ = os.RemoveAll(root) })
	own, other := filepath.Join(root, "pasted-images", "t1"), filepath.Join(root, "pasted-images", "t2")
	for _, dir := range []string{own, other} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	image := filepath.Join(own, "pasted-1.png")
	_ = os.WriteFile(image, []byte("PNG-OWN"), 0o600)
	_ = os.WriteFile(filepath.Join(other, "pasted-1.png"), []byte("PNG-OTHER"), 0o600)
	in.PastedImagesDir = own

	script := fmt.Sprintf(`cat %q; echo; (echo x > %q) 2>/dev/null && echo WROTE; ls %q 2>/dev/null && echo LISTED; true`,
		image, filepath.Join(own, "new.png"), other)
	out, err := runInSandbox(t, in, script)
	if err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	if !strings.Contains(out, "PNG-OWN") {
		t.Errorf("the agent cannot read its pasted image at %s: %q", image, out)
	}
	if strings.Contains(out, "WROTE") {
		t.Errorf("the pasted images folder is writable inside: %q", out)
	}
	if strings.Contains(out, "LISTED") || strings.Contains(out, "PNG-OTHER") {
		t.Errorf("another task's pasted images show inside: %q", out)
	}
}
