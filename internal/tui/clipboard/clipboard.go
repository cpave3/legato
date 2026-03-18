package clipboard

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
)

// lookPathFunc is the signature for exec.LookPath, injectable for testing.
type lookPathFunc func(name string) (string, error)

// Clipboard provides system clipboard access.
type Clipboard struct {
	tool string
	args []string
}

// New creates a Clipboard by detecting the platform clipboard tool.
func New() *Clipboard {
	return detect(
		runtime.GOOS,
		envGet("WAYLAND_DISPLAY"),
		envGet("DISPLAY"),
		exec.LookPath,
	)
}

// detect selects the clipboard tool based on OS and environment.
func detect(goos, waylandDisplay, display string, lookPath lookPathFunc) *Clipboard {
	c := &Clipboard{}

	switch goos {
	case "darwin":
		if path, err := lookPath("pbcopy"); err == nil && path != "" {
			c.tool = "pbcopy"
		}
	case "linux":
		if waylandDisplay != "" {
			if path, err := lookPath("wl-copy"); err == nil && path != "" {
				c.tool = "wl-copy"
				return c
			}
		}
		if display != "" {
			if path, err := lookPath("xclip"); err == nil && path != "" {
				c.tool = "xclip"
				c.args = []string{"-selection", "clipboard"}
				return c
			}
			if path, err := lookPath("xsel"); err == nil && path != "" {
				c.tool = "xsel"
				c.args = []string{"--clipboard", "--input"}
				return c
			}
		}
	}

	return c
}

// Available returns true if a clipboard tool was detected.
func (c *Clipboard) Available() bool {
	return c.tool != ""
}

// Copy writes text to the system clipboard.
func (c *Clipboard) Copy(text string) error {
	if !c.Available() {
		return fmt.Errorf("clipboard: no tool available")
	}
	cmd := exec.Command(c.tool, c.args...)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return fmt.Errorf("clipboard: stdin pipe: %w", err)
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("clipboard: start %s: %w", c.tool, err)
	}
	if _, err := io.WriteString(stdin, text); err != nil {
		return fmt.Errorf("clipboard: write: %w", err)
	}
	stdin.Close()
	// Don't wait — wl-copy stays alive to serve paste requests on Wayland.
	// The process will exit on its own when the clipboard is overwritten.
	return nil
}

// OpenURL opens a URL in the default browser.
func OpenURL(url string) error {
	cmd := openURLCommand(runtime.GOOS)
	if cmd == "" {
		return fmt.Errorf("open-url: unsupported platform %s", runtime.GOOS)
	}
	return exec.Command(cmd, url).Start()
}

func openURLCommand(goos string) string {
	switch goos {
	case "darwin":
		return "open"
	case "linux":
		return "xdg-open"
	default:
		return ""
	}
}

func envGet(key string) string {
	return os.Getenv(key)
}
