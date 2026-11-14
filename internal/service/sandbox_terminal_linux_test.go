package service

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"golang.org/x/sys/unix"

	"github.com/cpave3/legato/internal/engine/sandbox"
)

// An agent cannot push input into the pane's terminal (TIOCSTI), which the
// pane's host shell would read after the agent exits: a way out of the
// sandbox on a kernel that still allows it. The refusal is the sandbox's
// own (EPERM), whatever the kernel's dev.tty.legacy_tiocsti says (EIO),
// also for a request with high bits set, which ioctl truncates to TIOCSTI.
func TestInsideTheSandboxNothingCanBeTypedIntoThePane(t *testing.T) {
	in := bwrapFixture(t)
	// The system's python, which the sandbox shows (a toolchain's is not
	// mounted in this fixture).
	python := "/usr/bin/python3"
	if _, err := os.Stat(python); err != nil {
		t.Skip("no /usr/bin/python3")
	}
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
	script := `import fcntl, termios
for request in (termios.TIOCSTI, termios.TIOCSTI | (1 << 32), getattr(termios, "TIOCLINUX", 0x541C)):
    try:
        fcntl.ioctl(0, request, b"x")
        print("pushed", hex(request))
    except OSError as e:
        print("refused", hex(request), e.errno)
fcntl.ioctl(0, termios.TIOCGWINSZ, b"\0" * 8)
print("window size read")
`
	out := runInPTY(t, line+" "+shellWord(python)+" -I -c "+shellWord(script), "HOME="+in.Home)
	if !strings.Contains(out, "window size read") {
		t.Errorf("the agent lost its terminal's other ioctls:\n%s", out)
	}
	for _, request := range []string{"0x5412", "0x100005412", "0x541c"} {
		if !strings.Contains(out, "refused "+request+" 1\n") {
			t.Errorf("TIOCSTI-like request %s was not refused by the sandbox (EPERM):\n%s", request, out)
		}
	}
}

// runInPTY runs a shell line as the session leader of a new terminal, as a
// tmux pane runs its shell, and returns what it wrote there.
func runInPTY(t *testing.T, line string, env ...string) string {
	t.Helper()
	master, slave, err := openTestPTY()
	if err != nil {
		t.Skipf("no pseudo-terminal: %v", err)
	}
	defer master.Close()
	cmd := exec.Command("sh", "-c", line)
	cmd.Env = append(os.Environ(), env...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = slave, slave, slave
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true, Setctty: true}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	slave.Close()
	var out bytes.Buffer
	read := make(chan struct{})
	go func() {
		defer close(read)
		_, _ = io.Copy(&out, master)
	}()
	waited := make(chan error, 1)
	go func() { waited <- cmd.Wait() }()
	select {
	case <-waited:
	case <-time.After(30 * time.Second):
		_ = cmd.Process.Kill()
		t.Fatal("the command in the terminal did not end")
	}
	select {
	case <-read:
	case <-time.After(2 * time.Second):
	}
	return strings.ReplaceAll(out.String(), "\r", "")
}

func openTestPTY() (master, slave *os.File, err error) {
	fd, err := unix.Open("/dev/ptmx", unix.O_RDWR|unix.O_NOCTTY|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, nil, err
	}
	master = os.NewFile(uintptr(fd), "/dev/ptmx")
	n, err := unix.IoctlGetInt(fd, unix.TIOCGPTN)
	if err != nil {
		master.Close()
		return nil, nil, err
	}
	if err := unix.IoctlSetPointerInt(fd, unix.TIOCSPTLCK, 0); err != nil {
		master.Close()
		return nil, nil, err
	}
	slave, err = os.OpenFile(fmt.Sprintf("/dev/pts/%d", n), os.O_RDWR|syscall.O_NOCTTY, 0)
	if err != nil {
		master.Close()
		return nil, nil, err
	}
	return master, slave, nil
}
