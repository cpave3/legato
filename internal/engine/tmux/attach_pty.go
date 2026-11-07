package tmux

import (
	"fmt"
	"os"
	"os/exec"
	"sync"
	"syscall"

	"golang.org/x/sys/unix"
)

// PTY is a real tmux client attached to a session over a pseudo-terminal.
// Reads give the client's terminal output, writes are keystrokes, and
// Resize changes the client's size. It is the same thing `tmux attach`
// runs in a terminal, so what it shows, including scrollback and
// copy-mode, is tmux's own.
type PTY struct {
	master *os.File
	cmd    *exec.Cmd

	closeOnce sync.Once
	closeErr  error
}

// AttachPTY attaches a new tmux client to the named session on a pty of
// the given size. The session's status line is turned off, so the pty's
// rows are the window's rows, and its window-size follows the most
// recently active client, as in native tmux, and mouse support is on so the wheel scrolls tmux's history. Close detaches the client and
// leaves the session running.
func (m *Manager) AttachPTY(name string, cols, rows int) (*PTY, error) {
	if cols <= 0 || rows <= 0 {
		return nil, fmt.Errorf("attach %s: bad size %dx%d", name, cols, rows)
	}
	if err := exec.Command(m.tmuxPath, "has-session", "-t", "="+name).Run(); err != nil {
		return nil, fmt.Errorf("attach %s: no such session", name)
	}
	for _, kv := range [][2]string{{"status", "off"}, {"window-size", "latest"}, {"mouse", "on"}} {
		_ = exec.Command(m.tmuxPath, "set-option", "-t", name, kv[0], kv[1]).Run()
	}

	master, slave, err := openPTY()
	if err != nil {
		return nil, fmt.Errorf("attach %s: %w", name, err)
	}
	defer slave.Close()
	if err := setWinsize(master, cols, rows); err != nil {
		master.Close()
		return nil, fmt.Errorf("attach %s: %w", name, err)
	}

	cmd := exec.Command(m.tmuxPath, "attach-session", "-t", "="+name)
	cmd.Env = append(os.Environ(), "TERM=xterm-256color")
	cmd.Stdin, cmd.Stdout, cmd.Stderr = slave, slave, slave
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true, Setctty: true}
	if err := cmd.Start(); err != nil {
		master.Close()
		return nil, fmt.Errorf("attach %s: %w", name, err)
	}
	return &PTY{master: master, cmd: cmd}, nil
}

func (p *PTY) Read(b []byte) (int, error)  { return p.master.Read(b) }
func (p *PTY) Write(b []byte) (int, error) { return p.master.Write(b) }

// Resize sets the size of the client's terminal.
func (p *PTY) Resize(cols, rows int) error { return setWinsize(p.master, cols, rows) }

// Close detaches the client. The session keeps running.
func (p *PTY) Close() error {
	p.closeOnce.Do(func() {
		p.closeErr = p.master.Close()
		if p.cmd.Process != nil {
			_ = p.cmd.Process.Kill()
		}
		_ = p.cmd.Wait()
	})
	return p.closeErr
}

func openPTY() (master, slave *os.File, err error) {
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

func setWinsize(f *os.File, cols, rows int) error {
	return unix.IoctlSetWinsize(int(f.Fd()), unix.TIOCSWINSZ, &unix.Winsize{Col: uint16(cols), Row: uint16(rows)})
}
