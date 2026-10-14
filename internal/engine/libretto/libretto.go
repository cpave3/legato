// Package libretto runs the libretto command: the program that renders a
// storyboard into a narrated video, and serves the studio in which a person
// records a storyboard's scenes in their own voice. Legato does not need
// libretto to run. It asks what the installed one can do before it relies on
// it, and says plainly when there is none or it is too old.
package libretto

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// StudioLevel is the level of libretto's studio API that Legato is written
// for. libretto raises its level when an older client would be misled.
const StudioLevel = 5

// ErrNotInstalled says there is no libretto command on this machine.
var ErrNotInstalled = errors.New("libretto is not installed")

// TooOldError says the libretto that is installed has no studio, or an
// older one than Legato needs.
type TooOldError struct {
	// Version is libretto's own version; empty when it is too old to say.
	Version string
	// Studio is its studio level; 0 when it has none.
	Studio int
}

func (e *TooOldError) Error() string {
	if e.Version == "" {
		return "the libretto installed here is too old to have a studio"
	}
	return fmt.Sprintf("libretto %s has studio level %d, and Legato needs %d", e.Version, e.Studio, StudioLevel)
}

// Explain turns an error from this package into words for the person or the
// agent: what is wrong and what to do about it. Any other error is returned
// as its own text.
func Explain(err error) string {
	var old *TooOldError
	switch {
	case errors.Is(err, ErrNotInstalled):
		return "libretto is not installed on this machine, so the studio cannot open. To use it, install libretto and make sure the libretto command is on the PATH of the Legato server."
	case errors.As(err, &old):
		if old.Version == "" {
			return "The libretto installed on this machine is too old for the studio: it comes from before the studio was added. To use it, update libretto."
		}
		return fmt.Sprintf("The libretto installed on this machine is too old for the studio: it is version %s, with studio level %d, and Legato needs level %d. To use it, update libretto.", old.Version, old.Studio, StudioLevel)
	}
	return err.Error()
}

// Info is what an installed libretto says of itself.
type Info struct {
	Version  string `json:"version"`
	Manifest int    `json:"manifest"`
	Studio   int    `json:"studio"`
}

// Runner runs libretto. The zero value finds it on PATH.
type Runner struct {
	// LookPath finds the command; exec.LookPath when nil.
	LookPath func(name string) (string, error)
}

func (r Runner) find() (string, error) {
	look := r.LookPath
	if look == nil {
		look = exec.LookPath
	}
	path, err := look("libretto")
	if err != nil {
		return "", ErrNotInstalled
	}
	return path, nil
}

// probeTimeout bounds how long libretto may take to say what it is.
const probeTimeout = 10 * time.Second

// Probe asks the installed libretto what it can do. It returns
// ErrNotInstalled when there is none, and a *TooOldError when its studio is
// missing or older than StudioLevel.
func (r Runner) Probe(ctx context.Context) (Info, error) {
	path, err := r.find()
	if err != nil {
		return Info{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, path, "version", "--json").Output()
	var info Info
	// A libretto from before the studio has no version command.
	if err != nil || json.Unmarshal(out, &info) != nil {
		return Info{}, &TooOldError{}
	}
	if info.Studio < StudioLevel {
		return info, &TooOldError{Version: info.Version, Studio: info.Studio}
	}
	return info, nil
}

// Studio is a running libretto studio.
type Studio struct {
	// URL is where it listens, on this machine only.
	URL string
	// Stop ends it and waits for it to be gone.
	Stop func()
	// Exited is closed when the process has ended, for whatever reason.
	Exited <-chan struct{}
}

// studioStartTimeout bounds the wait for a studio to say where it listens.
// It starts a browser check first, which takes a second or two.
const studioStartTimeout = 30 * time.Second

// studioAddress is what the studio prints before the address it listens on.
const studioAddress = "studio: "

// StartStudio runs `libretto studio` on a storyboard, on a port of its own
// choosing, and returns once it says where it listens. The studio is told to
// end when its input closes, and this process holds that input: if Legato
// ends without calling Stop, the studio still does not outlive it.
func (r Runner) StartStudio(ctx context.Context, storyboard string) (*Studio, error) {
	path, err := r.find()
	if err != nil {
		return nil, err
	}
	cmd := exec.Command(path, "studio", storyboard, "--port", "0", "--until-stdin-closes")
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	var said tail
	cmd.Stderr = &said
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("starting libretto studio: %w", err)
	}

	found := make(chan string, 1)
	read := make(chan struct{})
	go func() {
		defer close(read)
		lines := bufio.NewScanner(stdout)
		for lines.Scan() {
			if _, address, ok := strings.Cut(lines.Text(), studioAddress); ok {
				found <- strings.TrimSpace(address)
				break
			}
		}
		// Keep the pipe drained so the studio never blocks on its own words.
		_, _ = io.Copy(io.Discard, stdout)
	}()
	exited := make(chan struct{})
	go func() {
		// Everything it printed is read before Wait closes the pipe.
		<-read
		_ = cmd.Wait()
		close(exited)
	}()
	var once sync.Once
	stop := func() {
		once.Do(func() {
			_ = stdin.Close()
			_ = cmd.Process.Kill()
		})
		<-exited
	}

	select {
	case address := <-found:
		return &Studio{URL: address, Stop: stop, Exited: exited}, nil
	case <-exited:
		return nil, fmt.Errorf("libretto studio did not start: %s", said.String())
	case <-ctx.Done():
		stop()
		return nil, ctx.Err()
	case <-time.After(studioStartTimeout):
		stop()
		return nil, fmt.Errorf("libretto studio did not say where it listens within %s", studioStartTimeout)
	}
}

// tail keeps the last of what a process printed, for saying why it failed.
type tail struct {
	mu   sync.Mutex
	text []byte
}

func (t *tail) Write(p []byte) (int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.text = append(t.text, p...)
	if over := len(t.text) - 2000; over > 0 {
		t.text = t.text[over:]
	}
	return len(p), nil
}

func (t *tail) String() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	text := strings.TrimSpace(string(t.text))
	if text == "" {
		return "it printed nothing"
	}
	return strings.TrimPrefix(text, "libretto: ")
}
