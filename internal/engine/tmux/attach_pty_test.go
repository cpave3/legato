package tmux

import (
	"bytes"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func readUntil(t *testing.T, p *PTY, want string) string {
	t.Helper()
	var got bytes.Buffer
	done := make(chan struct{})
	go func() {
		defer close(done)
		buf := make([]byte, 4096)
		for !strings.Contains(got.String(), want) {
			n, err := p.Read(buf)
			got.Write(buf[:n])
			if err != nil {
				return
			}
		}
	}()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatalf("timed out waiting for %q, got %q", want, got.String())
	}
	if !strings.Contains(got.String(), want) {
		t.Fatalf("stream ended before %q, got %q", want, got.String())
	}
	return got.String()
}

func TestAttachPTYRoundTrip(t *testing.T) {
	skipWithoutTmux(t)
	t.Setenv("TMUX", "")
	t.Setenv("TMUX_TMPDIR", t.TempDir())

	m, err := New(Options{})
	if err != nil {
		t.Fatal(err)
	}
	name := "legato-pty-test"
	t.Cleanup(func() { m.Kill(name) })
	if err := m.Spawn(name, t.TempDir(), 80, 24); err != nil {
		t.Fatal(err)
	}

	p, err := m.AttachPTY(name, 100, 30)
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()

	// Keystrokes go to the session unparsed.
	if _, err := p.Write([]byte("echo pty-$((6*7))\r")); err != nil {
		t.Fatal(err)
	}
	readUntil(t, p, "pty-42")

	// The wheel scrolls tmux's own history, so mouse support is on.
	if out, _ := exec.Command(m.tmuxPath, "show-options", "-v", "-t", name, "mouse").Output(); strings.TrimSpace(string(out)) != "on" {
		t.Fatalf("mouse = %q, want on", out)
	}

	// A resize reaches the session's window.
	if err := p.Resize(70, 20); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		out, _ := exec.Command(m.tmuxPath, "display-message", "-p", "-t", name, "#{window_width}x#{window_height}").Output()
		_ = out
		if strings.TrimSpace(string(out)) == "70x20" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("window size = %q, want 70x20", out)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func TestAttachPTYCloseDetachesButKeepsSession(t *testing.T) {
	skipWithoutTmux(t)
	t.Setenv("TMUX", "")
	t.Setenv("TMUX_TMPDIR", t.TempDir())

	m, _ := New(Options{})
	name := "legato-pty-close"
	t.Cleanup(func() { m.Kill(name) })
	if err := m.Spawn(name, t.TempDir(), 80, 24); err != nil {
		t.Fatal(err)
	}
	p, err := m.AttachPTY(name, 80, 24)
	if err != nil {
		t.Fatal(err)
	}
	if err := p.Close(); err != nil {
		t.Fatal(err)
	}
	if alive, _ := m.IsAlive(name); !alive {
		t.Fatal("closing the terminal killed the session")
	}
}

func TestAttachPTYMissingSession(t *testing.T) {
	skipWithoutTmux(t)
	t.Setenv("TMUX", "")
	t.Setenv("TMUX_TMPDIR", t.TempDir())
	m, _ := New(Options{})
	p, err := m.AttachPTY("legato-nope", 80, 24)
	if err == nil {
		p.Close()
		t.Fatal("want an error for a missing session")
	}
}
