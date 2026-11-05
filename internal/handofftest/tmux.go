package handofftest

import (
	"io"
	"os/exec"
	"strings"
	"sync"
)

// FakeTmux stands in for tmux on a worker: agents "start" in it and stay
// alive until killed, and what is typed to them is recorded. It never runs
// tmux.
type FakeTmux struct {
	mu       sync.Mutex
	sessions map[string]bool
	spawned  []Spawn
	typed    map[string][]string
}

// Spawn is one session an agent was started in.
type Spawn struct {
	Name    string
	WorkDir string
	Env     []string
}

// NewFakeTmux makes a FakeTmux with no sessions.
func NewFakeTmux() *FakeTmux {
	return &FakeTmux{sessions: map[string]bool{}, typed: map[string][]string{}}
}

// Spawned lists the sessions started, oldest first.
func (f *FakeTmux) Spawned() []Spawn {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]Spawn(nil), f.spawned...)
}

// Typed is what was typed into a session, in order.
func (f *FakeTmux) Typed(name string) []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.typed[name]...)
}

func (f *FakeTmux) Spawn(name, workDir string, _, _ int, env ...string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sessions[name] = true
	f.spawned = append(f.spawned, Spawn{Name: name, WorkDir: workDir, Env: append([]string(nil), env...)})
	return nil
}

func (f *FakeTmux) Kill(name string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.sessions, name)
	return nil
}

func (f *FakeTmux) Capture(string) (string, error)            { return "", nil }
func (f *FakeTmux) CaptureWithEscapes(string) (string, error) { return "", nil }
func (f *FakeTmux) Attach(string) *exec.Cmd                   { return exec.Command("true") }

func (f *FakeTmux) ListSessions() ([]string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []string
	for name := range f.sessions {
		out = append(out, name)
	}
	return out, nil
}

func (f *FakeTmux) IsAlive(name string) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.sessions[name], nil
}

func (f *FakeTmux) record(name, text string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.typed[name] = append(f.typed[name], text)
	return nil
}

func (f *FakeTmux) SendKeys(name, keys string) error                { return f.record(name, keys) }
func (f *FakeTmux) SendKey(name, key string) error                  { return f.record(name, key) }
func (f *FakeTmux) SendKeysLine(name, line string) error            { return f.record(name, line) }
func (f *FakeTmux) SendKeysMultiline(name, payload string) error    { return f.record(name, payload) }
func (f *FakeTmux) SendKeysShellCommand(name, command string) error { return f.record(name, command) }

func (f *FakeTmux) PipeOutput(string) (io.Reader, func(), error) {
	return strings.NewReader(""), func() {}, nil
}

func (f *FakeTmux) SetEnv(string, string, string) error    { return nil }
func (f *FakeTmux) SetOption(string, string, string) error { return nil }

func (f *FakeTmux) PaneCommands() (map[string]string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := map[string]string{}
	for name := range f.sessions {
		out[name] = "claude"
	}
	return out, nil
}
