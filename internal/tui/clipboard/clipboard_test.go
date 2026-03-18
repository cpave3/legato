package clipboard

import (
	"testing"
)

func TestNewDarwin(t *testing.T) {
	c := detect("darwin", "", "", func(name string) (string, error) {
		if name == "pbcopy" {
			return "/usr/bin/pbcopy", nil
		}
		return "", &lookPathError{name}
	})
	if !c.Available() {
		t.Fatal("expected Available() on darwin")
	}
	if c.tool != "pbcopy" {
		t.Errorf("tool = %q, want pbcopy", c.tool)
	}
	if len(c.args) != 0 {
		t.Errorf("args = %v, want empty", c.args)
	}
}

func TestNewWayland(t *testing.T) {
	c := detect("linux", "wayland-0", "", func(name string) (string, error) {
		if name == "wl-copy" {
			return "/usr/bin/wl-copy", nil
		}
		return "", &lookPathError{name}
	})
	if !c.Available() {
		t.Fatal("expected Available() on wayland")
	}
	if c.tool != "wl-copy" {
		t.Errorf("tool = %q, want wl-copy", c.tool)
	}
}

func TestNewX11Xclip(t *testing.T) {
	c := detect("linux", "", ":0", func(name string) (string, error) {
		if name == "xclip" {
			return "/usr/bin/xclip", nil
		}
		return "", &lookPathError{name}
	})
	if !c.Available() {
		t.Fatal("expected Available() on X11 with xclip")
	}
	if c.tool != "xclip" {
		t.Errorf("tool = %q, want xclip", c.tool)
	}
	if len(c.args) != 2 || c.args[0] != "-selection" || c.args[1] != "clipboard" {
		t.Errorf("args = %v, want [-selection clipboard]", c.args)
	}
}

func TestNewX11XselFallback(t *testing.T) {
	c := detect("linux", "", ":0", func(name string) (string, error) {
		if name == "xsel" {
			return "/usr/bin/xsel", nil
		}
		return "", &lookPathError{name}
	})
	if !c.Available() {
		t.Fatal("expected Available() on X11 with xsel")
	}
	if c.tool != "xsel" {
		t.Errorf("tool = %q, want xsel", c.tool)
	}
	if len(c.args) != 2 || c.args[0] != "--clipboard" || c.args[1] != "--input" {
		t.Errorf("args = %v, want [--clipboard --input]", c.args)
	}
}

func TestNewNoToolFound(t *testing.T) {
	c := detect("linux", "", ":0", func(name string) (string, error) {
		return "", &lookPathError{name}
	})
	if c.Available() {
		t.Fatal("expected Available() = false when no tool found")
	}
}

func TestNewNoDisplay(t *testing.T) {
	c := detect("linux", "", "", func(name string) (string, error) {
		return "/usr/bin/" + name, nil
	})
	if c.Available() {
		t.Fatal("expected Available() = false on linux with no display")
	}
}

func TestCopyNoTool(t *testing.T) {
	c := &Clipboard{}
	err := c.Copy("hello")
	if err == nil {
		t.Fatal("expected error when no tool available")
	}
	if err.Error() != "clipboard: no tool available" {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestCopyIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	c := New()
	if !c.Available() {
		t.Skip("no clipboard tool available")
	}
	err := c.Copy("legato-test-clipboard")
	if err != nil {
		t.Fatalf("Copy() error: %v", err)
	}
}

func TestOpenURLCommandDarwin(t *testing.T) {
	cmd := openURLCommand("darwin")
	if cmd != "open" {
		t.Errorf("darwin command = %q, want open", cmd)
	}
}

func TestOpenURLCommandLinux(t *testing.T) {
	cmd := openURLCommand("linux")
	if cmd != "xdg-open" {
		t.Errorf("linux command = %q, want xdg-open", cmd)
	}
}

func TestOpenURLCommandUnknown(t *testing.T) {
	cmd := openURLCommand("freebsd")
	if cmd != "" {
		t.Errorf("unknown OS command = %q, want empty", cmd)
	}
}

// lookPathError is a simple error for test lookup failures.
type lookPathError struct {
	name string
}

func (e *lookPathError) Error() string {
	return "executable file not found: " + e.name
}
