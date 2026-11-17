// Package mcpstdio runs a local MCP server: a command that speaks MCP on its
// stdin and stdout. It starts the command with exactly the environment it is
// given, finds a bare command in that environment's PATH and nowhere else,
// hands each line the server writes to stderr to the caller, and stops the
// server with every process it started, helpers it detached included. It
// knows nothing of Legato or of the MCP messages themselves: Transport is an
// mcp.Transport over the process's pipes for any MCP client.
package mcpstdio

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// ErrNotFound is a command that cannot be run: not in PATH, or not there.
var ErrNotFound = errors.New("command not found")

// MaxLine is the most of one stderr line handed on; the rest is cut.
const MaxLine = 1024

// tailLines is how many of the last stderr lines StderrTail keeps.
const tailLines = 40

// WatchEvery is how often the processes a server started are noted, so a
// server that exits by itself does not leave them behind.
const WatchEvery = 500 * time.Millisecond

// DefaultMaxMessage bounds one message the server sends.
const DefaultMaxMessage = 16 << 20

// Spec is a server to start.
type Spec struct {
	// Command is an absolute path, or a name looked up in Env's PATH.
	Command string
	Args    []string
	// Dir is the working directory.
	Dir string
	// Env is the server's whole environment; nothing else is inherited.
	Env []string
	// Stderr is given each line the server writes to stderr, cut at MaxLine.
	Stderr func(line string)
	// Grace is how long Stop waits after closing stdin, and again after
	// SIGTERM, before it kills; 3 seconds when zero.
	Grace time.Duration
	// MaxMessage bounds one message from the server; DefaultMaxMessage when zero.
	MaxMessage int
}

// Resolve is the path of the command a spec would run: an absolute path as
// it is, a bare name found in env's PATH. A relative path is refused, since
// it would depend on the working directory.
func Resolve(command string, env []string) (string, error) {
	command = strings.TrimSpace(command)
	if command == "" {
		return "", errors.New("no command")
	}
	if strings.ContainsRune(command, '/') {
		if !filepath.IsAbs(command) {
			return "", fmt.Errorf("%q: give the command as an absolute path, or as a bare name found in PATH", command)
		}
		if err := runnable(command); err != nil {
			return "", err
		}
		return command, nil
	}
	path := lookupEnv(env, "PATH")
	for _, dir := range filepath.SplitList(path) {
		if dir == "" || !filepath.IsAbs(dir) {
			continue
		}
		candidate := filepath.Join(dir, command)
		if runnable(candidate) == nil {
			return candidate, nil
		}
	}
	if path == "" {
		path = "(empty)"
	}
	return "", fmt.Errorf("%w: %q is not in PATH %s. Give the command's absolute path, or set PATH in the environment to include its folder", ErrNotFound, command, path)
}

func runnable(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("%w: %s does not exist", ErrNotFound, path)
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0o111 == 0 {
		return fmt.Errorf("%s cannot be run: it is not an executable file", path)
	}
	return nil
}

func lookupEnv(env []string, key string) string {
	value := ""
	for _, kv := range env {
		if k, v, ok := strings.Cut(kv, "="); ok && k == key {
			value = v
		}
	}
	return value
}

// Process is a running server.
type Process struct {
	spec   Spec
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout *os.File
	done   chan struct{}

	stopOnce sync.Once
	mu       sync.Mutex
	tail     []string
	// family is the processes the server started, as last seen.
	family []proc
}

// Start starts the server. The process is its own process group's leader,
// and on Linux it is killed if Legato dies.
func Start(spec Spec) (*Process, error) {
	path, err := Resolve(spec.Command, spec.Env)
	if err != nil {
		return nil, err
	}
	if spec.Grace <= 0 {
		spec.Grace = 3 * time.Second
	}
	if spec.MaxMessage <= 0 {
		spec.MaxMessage = DefaultMaxMessage
	}
	cmd := exec.Command(path, spec.Args...)
	cmd.Dir = spec.Dir
	cmd.Env = spec.Env
	if cmd.Env == nil {
		cmd.Env = []string{}
	}
	cmd.SysProcAttr = sysProcAttr()
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	// The read ends are Legato's own, not Wait's: a server's last answer
	// is still read after it exits, and a helper that kept stderr open
	// cannot hold Done open after it is killed.
	stdoutR, stdoutW, err := os.Pipe()
	if err != nil {
		return nil, err
	}
	stderrR, stderrW, err := os.Pipe()
	if err != nil {
		stdoutR.Close()
		stdoutW.Close()
		return nil, err
	}
	cmd.Stdout, cmd.Stderr = stdoutW, stderrW
	err = cmd.Start()
	stdoutW.Close()
	stderrW.Close()
	if err != nil {
		stdoutR.Close()
		stderrR.Close()
		return nil, fmt.Errorf("starting %s: %w", path, err)
	}
	p := &Process{spec: spec, cmd: cmd, stdin: stdin, stdout: stdoutR, done: make(chan struct{})}
	read := make(chan struct{})
	go func() {
		defer close(read)
		p.readStderr(stderrR)
	}()
	watching := make(chan struct{})
	go p.watch(watching)
	go func() {
		_ = cmd.Wait()
		close(watching)
		p.mu.Lock()
		family := p.family
		p.mu.Unlock()
		// Whatever the server left running goes with it.
		killGroup(cmd.Process.Pid, syscall.SIGKILL)
		killFamily(family)
		select {
		case <-read:
		case <-time.After(time.Second):
		}
		stderrR.Close()
		<-read
		close(p.done)
	}()
	return p, nil
}

func (p *Process) readStderr(r io.Reader) {
	br := bufio.NewReaderSize(r, MaxLine)
	for {
		line, err := readLine(br)
		if line != "" || err == nil {
			p.mu.Lock()
			p.tail = append(p.tail, line)
			if len(p.tail) > tailLines {
				p.tail = p.tail[len(p.tail)-tailLines:]
			}
			p.mu.Unlock()
			if p.spec.Stderr != nil {
				p.spec.Stderr(line)
			}
		}
		if err != nil {
			return
		}
	}
}

// readLine reads one line, keeping at most MaxLine bytes of it.
func readLine(br *bufio.Reader) (string, error) {
	var b []byte
	for {
		chunk, isPrefix, err := br.ReadLine()
		if len(b) < MaxLine {
			b = append(b, chunk[:min(len(chunk), MaxLine-len(b))]...)
		}
		if err != nil {
			return strings.TrimRight(string(b), "\r"), err
		}
		if !isPrefix {
			return strings.TrimRight(string(b), "\r"), nil
		}
	}
}

func (p *Process) watch(stop <-chan struct{}) {
	t := time.NewTicker(WatchEvery)
	defer t.Stop()
	for {
		if family := descendants(p.cmd.Process.Pid); family != nil {
			p.mu.Lock()
			p.family = mergeFamily(p.family, family)
			p.mu.Unlock()
		}
		select {
		case <-stop:
			return
		case <-t.C:
		}
	}
}

// mergeFamily keeps processes seen before that are still the same process
// (a helper reparented away from the server is no longer its descendant),
// and adds the new ones.
func mergeFamily(old, now []proc) []proc {
	seen := map[proc]bool{}
	var out []proc
	for _, list := range [][]proc{now, old} {
		for _, p := range list {
			if !seen[p] && p.same() {
				seen[p] = true
				out = append(out, p)
			}
		}
	}
	return out
}

// Transport is an MCP transport over the server's pipes. Closing its
// connection stops the server. Either end stops it before it is closed: a
// client closes the server's stdout first, and a server whose next write
// then fails would exit and orphan its helpers before Stop could find them.
func (p *Process) Transport() mcp.Transport {
	return &mcp.IOTransport{Reader: stdoutEnd{p}, Writer: stdinEnd{p}, MaxLineLength: p.spec.MaxMessage}
}

type stdoutEnd struct{ p *Process }

func (s stdoutEnd) Read(b []byte) (int, error) { return s.p.stdout.Read(b) }
func (s stdoutEnd) Close() error {
	s.p.Stop()
	return s.p.stdout.Close()
}

type stdinEnd struct{ p *Process }

func (s stdinEnd) Write(b []byte) (int, error) { return s.p.stdin.Write(b) }
func (s stdinEnd) Close() error {
	s.p.Stop()
	return nil
}

// Done is closed when the server and what it started have ended.
func (p *Process) Done() <-chan struct{} { return p.done }

// PID is the server's process ID.
func (p *Process) PID() int { return p.cmd.Process.Pid }

// StderrTail is the last lines the server wrote to stderr.
func (p *Process) StderrTail() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return strings.Join(p.tail, "\n")
}

// Stop ends the server: stdin is closed, then the server's process group is
// sent SIGTERM (so it can close what it opened), then every process it
// started is killed. It returns when they have ended.
func (p *Process) Stop() {
	p.stopOnce.Do(func() {
		if family := descendants(p.cmd.Process.Pid); family != nil {
			p.mu.Lock()
			p.family = mergeFamily(p.family, family)
			p.mu.Unlock()
		}
		_ = p.stdin.Close()
		if p.wait(p.spec.Grace) {
			return
		}
		killGroup(p.cmd.Process.Pid, syscall.SIGTERM)
		if p.wait(p.spec.Grace) {
			return
		}
		killGroup(p.cmd.Process.Pid, syscall.SIGKILL)
		p.mu.Lock()
		family := p.family
		p.mu.Unlock()
		killFamily(family)
		<-p.done
	})
	<-p.done
}

func (p *Process) wait(d time.Duration) bool {
	select {
	case <-p.done:
		return true
	case <-time.After(d):
		return false
	}
}

func killGroup(pid int, sig syscall.Signal) {
	_ = syscall.Kill(-pid, sig)
	_ = syscall.Kill(pid, sig)
}

func killFamily(family []proc) {
	for _, f := range family {
		if f.same() {
			_ = syscall.Kill(f.pid, syscall.SIGKILL)
		}
	}
}
