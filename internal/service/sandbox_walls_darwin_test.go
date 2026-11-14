//go:build darwin

package service

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
	"unsafe"

	"golang.org/x/sys/unix"
)

// macOS's own walls: what Seatbelt must close that bwrap's namespaces close
// by themselves. Measured first by cmd/validate/seatbelt-probe (LEG-168).

func wallHelperOS(mode string, _ []string) int {
	switch mode {
	case "tiocsti":
		tty, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
		if err != nil {
			fmt.Println("no terminal:", err)
			return 1
		}
		defer tty.Close()
		if err := unix.IoctlSetPointerInt(int(tty.Fd()), unix.TIOCSTI, int('#')); err != nil {
			fmt.Println("TIOCSTI refused:", err)
			return 1
		}
		fmt.Println("TIOCSTI ALLOWED")
		return 0
	}
	fmt.Println("unknown helper", mode)
	return 2
}

// A Keychain item that /usr/bin/security saved (as gh saves its token)
// cannot be read: not by /usr/bin/security, which the sandbox does not run,
// nor by a copy of it, as securityd is closed.
func TestWallsOnAMacTheKeychainCannotBeRead(t *testing.T) {
	w := wallFixture(t)
	sec := "/usr/bin/security"
	kc := filepath.Join(w.base, "wall.keychain")
	before, _ := exec.Command(sec, "list-keychains", "-d", "user").Output()
	for _, args := range [][]string{
		{"create-keychain", "-p", "wall", kc}, {"unlock-keychain", "-p", "wall", kc},
		{"add-generic-password", "-s", "legato-wall", "-a", "wall", "-w", "wall-secret-31", kc},
	} {
		if out, err := exec.Command(sec, args...).CombinedOutput(); err != nil {
			t.Skipf("security %s: %v %s", args[0], err, out)
		}
	}
	t.Cleanup(func() {
		_ = exec.Command(sec, "delete-keychain", kc).Run()
		if after, _ := exec.Command(sec, "list-keychains", "-d", "user").Output(); !bytes.Equal(after, before) {
			var list []string
			for _, l := range strings.Fields(string(before)) {
				list = append(list, strings.Trim(l, `"`))
			}
			_ = exec.Command(sec, append([]string{"list-keychains", "-d", "user", "-s"}, list...)...).Run()
		}
	})
	if out, _ := exec.Command(sec, "find-generic-password", "-s", "legato-wall", "-w", kc).Output(); !strings.Contains(string(out), "wall-secret-31") {
		t.Skip("the item cannot be read outside the sandbox either")
	}
	copied := filepath.Join(w.in.WorkDir, "security-copy")
	if err := copyFile(sec, copied, 0o755); err != nil {
		t.Fatal(err)
	}
	out, _ := w.run(t, fmt.Sprintf("%s find-generic-password -s legato-wall -w %q; %q find-generic-password -s legato-wall -w %q", sec, kc, copied, kc))
	if strings.Contains(out, "wall-secret-31") {
		t.Fatalf("a Keychain item was read inside:\n%s", out)
	}
}

// The shared /tmp holds the tmux server's socket, launchd's ssh-agent and
// every Legato's runtime folder: nothing in it is seen or reached.
func TestWallsOnAMacTheSharedTmpIsClosed(t *testing.T) {
	w := wallFixture(t)
	dir, err := os.MkdirTemp("/private/tmp", "lgwall")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	sock := filepath.Join(dir, "s.sock")
	listen(t, sock)
	_ = os.WriteFile(filepath.Join(dir, "note"), []byte("host-note-77"), 0o600)
	out, _ := w.run(t, fmt.Sprintf("cat %q; ls %q | sed 's/^/LISTED:/'; %q %q; echo x > /tmp/lgwall-write && echo WROTE", filepath.Join(dir, "note"), dir, w.helper, sock), wallHelperEnv+"=connect")
	for _, leak := range []string{"host-note-77", "LISTED:", "connected", "WROTE"} {
		if strings.Contains(out, leak) {
			t.Errorf("%s inside:\n%s", leak, out)
		}
	}
	_ = os.Remove("/private/tmp/lgwall-write")
}

// Nothing inside can start something outside the sandbox: LaunchServices
// (open), Apple Events (osascript), launchd jobs, the pasteboard, sudo.
func TestWallsOnAMacNothingStartsOutsideTheSandbox(t *testing.T) {
	w := wallFixture(t)
	marker := filepath.Join(w.base, "escaped")
	label := "dev.legato.wall." + strconv.Itoa(os.Getpid())
	t.Cleanup(func() { _ = exec.Command("/bin/launchctl", "remove", label).Run() })
	launchctl := filepath.Join(w.in.WorkDir, "launchctl-copy")
	if err := copyFile("/bin/launchctl", launchctl, 0o755); err != nil {
		t.Fatal(err)
	}
	out, _ := w.run(t, fmt.Sprintf(`
/usr/bin/open -g -a /System/Applications/Calculator.app && echo OPENED
/usr/bin/osascript -e 'return "apple-events"'
/bin/launchctl submit -l %[1]s -- /usr/bin/touch %[2]q && echo SUBMITTED
%[3]q submit -l %[1]s -- /usr/bin/touch %[2]q && echo SUBMITTED-COPY
echo clipboard | /usr/bin/pbcopy && echo COPIED
/usr/bin/sudo -n true && echo ROOT
true`, label, marker, launchctl))
	for _, leak := range []string{"OPENED", "apple-events", "SUBMITTED", "COPIED", "ROOT"} {
		if strings.Contains(out, leak) {
			t.Errorf("%s:\n%s", leak, out)
		}
	}
	time.Sleep(time.Second)
	if _, err := os.Stat(marker); err == nil {
		t.Error("a launchd job ran outside the sandbox")
	}
}

// A process outside cannot be signalled; the sandbox's own can.
func TestWallsOnAMacProcessesOutsideCannotBeSignalled(t *testing.T) {
	w := wallFixture(t)
	victim := exec.Command("/bin/sleep", "300")
	if err := victim.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() { _ = victim.Wait(); close(done) }()
	t.Cleanup(func() { _ = victim.Process.Kill(); <-done })
	out, _ := w.run(t, fmt.Sprintf("kill -TERM %d && echo SIGNALLED; sleep 30 & kill -TERM $! && echo OWN-ENDED", victim.Process.Pid))
	select {
	case <-done:
		t.Fatalf("a process outside was ended:\n%s", out)
	case <-time.After(time.Second):
	}
	if strings.Contains(out, "SIGNALLED") || !strings.Contains(out, "OWN-ENDED") {
		t.Fatalf("outside then own:\n%s", out)
	}
}

// The agent cannot push input into the pane's terminal, which the pane's
// host shell would read after it exits.
func TestWallsOnAMacNothingCanBeTypedIntoThePane(t *testing.T) {
	w := wallFixture(t)
	out := w.runInTerminal(t, fmt.Sprintf("%q", w.helper), wallHelperEnv+"=tiocsti")
	if !strings.Contains(out, "TIOCSTI refused") {
		t.Fatalf("TIOCSTI was not refused:\n%s", out)
	}
}

// A hidden file in the work cannot be uncovered by renaming a folder that
// holds it, by a hard link, or by a copy-on-write clone.
func TestWallsOnAMacACoverCannotBeMovedAside(t *testing.T) {
	w := wallFixture(t)
	config := filepath.Join(w.in.WorkDir, "conf", "legato.yaml")
	if err := os.MkdirAll(filepath.Dir(config), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(config, []byte("hidden-config-55"), 0o600); err != nil {
		t.Fatal(err)
	}
	w.in.ConfigPath = config
	work := w.in.WorkDir
	out, _ := w.run(t, fmt.Sprintf(`cd %[1]q
cat conf/legato.yaml
mv conf conf2 && cat conf2/legato.yaml
ln conf/legato.yaml linked && cat linked
cp -c conf/legato.yaml cloned && cat cloned
true`, work))
	if strings.Contains(out, "hidden-config-55") {
		t.Fatalf("the hidden file was read:\n%s", out)
	}
	if _, err := os.Stat(filepath.Join(work, "conf2")); err == nil {
		t.Fatal("the folder that holds the cover was renamed")
	}
}

// The network, DNS and TLS work as outside; so do git and the shells.
func TestWallsOnAMacTheNetworkAndToolsWork(t *testing.T) {
	w := wallFixture(t)
	out, err := w.run(t, `/usr/bin/curl -sS -o /dev/null -w "%{http_code}\n" https://github.com && /usr/bin/git --version && /bin/zsh -c 'echo zsh-ok' && /bin/bash -c 'echo bash-ok'`)
	if err != nil {
		t.Fatalf("%v:\n%s", err, out)
	}
	for _, want := range []string{"200", "git version", "zsh-ok", "bash-ok"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q:\n%s", want, out)
		}
	}
}

// runInTerminal runs the sandbox's line in a new terminal, as the session
// leader, as a tmux pane runs its shell.
func (w wall) runInTerminal(t *testing.T, command string, env ...string) string {
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
	master, slave, err := openDarwinPTY()
	if err != nil {
		t.Skipf("no pseudo-terminal: %v", err)
	}
	defer master.Close()
	cmd := exec.Command("/bin/sh", "-c", line+" "+command)
	cmd.Env = append(append(os.Environ(), "HOME="+in.Home, wallLauncherEnv+"=1"), env...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = slave, slave, slave
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true, Setctty: true}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	slave.Close()
	var buf bytes.Buffer
	var mu sync.Mutex
	go func() {
		b := make([]byte, 4096)
		for {
			n, err := master.Read(b)
			mu.Lock()
			buf.Write(b[:n])
			mu.Unlock()
			if err != nil {
				return
			}
		}
	}()
	waited := make(chan error, 1)
	go func() { waited <- cmd.Wait() }()
	select {
	case <-waited:
	case <-time.After(30 * time.Second):
		_ = cmd.Process.Kill()
		t.Fatal("the command in the terminal did not end")
	}
	time.Sleep(200 * time.Millisecond)
	mu.Lock()
	defer mu.Unlock()
	return buf.String()
}

func openDarwinPTY() (master, slave *os.File, err error) {
	fd, err := unix.Open("/dev/ptmx", unix.O_RDWR|unix.O_NOCTTY|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, nil, err
	}
	master = os.NewFile(uintptr(fd), "/dev/ptmx")
	if err := unix.IoctlSetInt(fd, unix.TIOCPTYGRANT, 0); err != nil {
		master.Close()
		return nil, nil, err
	}
	if err := unix.IoctlSetInt(fd, unix.TIOCPTYUNLK, 0); err != nil {
		master.Close()
		return nil, nil, err
	}
	var name [128]byte
	if _, _, errno := unix.Syscall(unix.SYS_IOCTL, uintptr(fd), uintptr(unix.TIOCPTYGNAME), uintptr(unsafe.Pointer(&name[0]))); errno != 0 {
		master.Close()
		return nil, nil, errno
	}
	end := bytes.IndexByte(name[:], 0)
	if end < 0 {
		return nil, nil, errors.New("no terminal name")
	}
	slave, err = os.OpenFile(string(name[:end]), os.O_RDWR|syscall.O_NOCTTY, 0)
	if err != nil {
		master.Close()
		return nil, nil, err
	}
	return master, slave, nil
}
