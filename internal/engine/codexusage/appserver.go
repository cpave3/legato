package codexusage

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"syscall"
	"time"
)

// DefaultTimeout bounds one app-server read, start to stop.
const DefaultTimeout = 10 * time.Second

// lineLimit bounds one message from the app server.
const lineLimit = 4 << 20

// AppServer reads the limits from a `codex app-server` it starts for the
// read and stops after, speaking JSON-RPC over its standard input and output.
type AppServer struct {
	// Command is the codex command: a path, or a name looked up in the PATH
	// of Env (the process's PATH when Env is nil). "codex" when empty.
	Command string
	// Env is the app server's whole environment; the process's when nil.
	Env []string
	// Timeout is DefaultTimeout when zero.
	Timeout time.Duration
}

type rpcFrame struct {
	ID     *int64          `json:"id,omitempty"`
	Method string          `json:"method,omitempty"`
	Params any             `json:"params,omitempty"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

// Read starts the app server, introduces itself, asks for the rate limits
// and stops it.
func (a AppServer) Read(ctx context.Context) (Reading, error) {
	command := a.Command
	if command == "" {
		command = "codex"
	}
	path, err := lookPath(command, a.Env)
	if err != nil {
		return Reading{}, ErrCodexMissing
	}
	timeout := a.Timeout
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.Command(path, "app-server")
	cmd.Env = a.Env
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return Reading{}, fmt.Errorf("codex: starting the app server: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return Reading{}, fmt.Errorf("codex: starting the app server: %w", err)
	}
	if err := cmd.Start(); err != nil {
		return Reading{}, fmt.Errorf("codex: starting the app server: %w", err)
	}
	exited := make(chan struct{})
	go func() { _ = cmd.Wait(); close(exited) }()
	defer func() {
		_ = stdin.Close()
		select {
		case <-exited:
		case <-time.After(500 * time.Millisecond):
			_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
			<-exited
		}
	}()

	type outcome struct {
		reading Reading
		err     error
	}
	done := make(chan outcome, 1)
	go func() {
		reading, err := converse(stdin, stdout)
		done <- outcome{reading, err}
	}()
	select {
	case out := <-done:
		return out.reading, out.err
	case <-ctx.Done():
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		return Reading{}, errors.New("codex: the app server did not answer in time")
	}
}

// converse runs the handshake and the read. Notifications the app server
// sends meanwhile are skipped.
func converse(in io.Writer, out io.Reader) (Reading, error) {
	scanner := bufio.NewScanner(out)
	scanner.Buffer(make([]byte, 64<<10), lineLimit)
	send := func(f rpcFrame) error {
		data, err := json.Marshal(f)
		if err != nil {
			return err
		}
		_, err = in.Write(append(data, '\n'))
		return err
	}
	await := func(id int64) (rpcFrame, error) {
		for scanner.Scan() {
			var f rpcFrame
			if json.Unmarshal(scanner.Bytes(), &f) != nil || f.ID == nil || *f.ID != id || f.Method != "" {
				continue
			}
			return f, nil
		}
		return rpcFrame{}, errors.New("codex: the app server closed before answering")
	}
	one, two := int64(1), int64(2)

	initialize := map[string]any{"clientInfo": map[string]any{"name": "legato", "title": "Legato usage", "version": "1"}}
	if err := send(rpcFrame{ID: &one, Method: "initialize", Params: initialize}); err != nil {
		return Reading{}, errors.New("codex: writing to the app server failed")
	}
	reply, err := await(one)
	if err != nil {
		return Reading{}, err
	}
	if reply.Error != nil {
		return Reading{}, fmt.Errorf("codex: the app server refused to start: %s", reply.Error.Message)
	}
	if err := send(rpcFrame{Method: "initialized"}); err != nil {
		return Reading{}, errors.New("codex: writing to the app server failed")
	}
	if err := send(rpcFrame{ID: &two, Method: "account/rateLimits/read"}); err != nil {
		return Reading{}, errors.New("codex: writing to the app server failed")
	}
	reply, err = await(two)
	if err != nil {
		return Reading{}, err
	}
	if reply.Error != nil {
		return Reading{}, rpcError(reply.Error.Code, reply.Error.Message)
	}
	var result struct {
		RateLimits *wireLimits `json:"rateLimits"`
	}
	if err := json.Unmarshal(reply.Result, &result); err != nil {
		return Reading{}, errors.New("codex: the rate limits answer has an unknown shape")
	}
	return result.RateLimits.reading(SourceAppServer)
}

func rpcError(code int, message string) error {
	lower := strings.ToLower(message)
	switch {
	case code == -32601 || strings.Contains(lower, "method not found") || strings.Contains(lower, "unknown variant"):
		return ErrUnsupported
	case strings.Contains(lower, "auth") || strings.Contains(lower, "login") || strings.Contains(lower, "logged in") || strings.Contains(lower, "sign in"):
		return ErrNotSignedIn
	default:
		return fmt.Errorf("codex: reading the rate limits failed (code %d)", code)
	}
}

// lookPath finds a command in env's PATH, or the process's when env is nil.
func lookPath(command string, env []string) (string, error) {
	if strings.Contains(command, "/") || env == nil {
		return exec.LookPath(command)
	}
	for _, kv := range env {
		if dirs, ok := strings.CutPrefix(kv, "PATH="); ok {
			for _, dir := range strings.Split(dirs, ":") {
				if dir == "" {
					continue
				}
				if path, err := exec.LookPath(dir + "/" + command); err == nil {
					return path, nil
				}
			}
		}
	}
	return "", exec.ErrNotFound
}
