package broker

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// socketPath is short: a Unix socket path has a limit of about 100 bytes,
// which t.TempDir() can pass.
func socketPath(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "brk")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return filepath.Join(dir, "b.sock")
}

func echo(_ context.Context, req Request) (any, error) {
	switch req.Op {
	case "echo":
		var args map[string]any
		_ = json.Unmarshal(req.Args, &args)
		return map[string]any{"token": req.Token, "args": args}, nil
	case "refuse":
		return nil, &Error{Code: "forbidden", Message: "not yours"}
	case "boom":
		return nil, errors.New("database is locked")
	case "slow":
		time.Sleep(3 * time.Second)
		return nil, nil
	}
	return nil, &Error{Code: "unknown_op", Message: "no such operation: " + req.Op}
}

func TestACallGetsTheHandlersResult(t *testing.T) {
	path := socketPath(t)
	srv, err := Listen(path, echo)
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Close()
	var got struct {
		Token string         `json:"token"`
		Args  map[string]any `json:"args"`
	}
	if err := Call(context.Background(), path, "tok", "echo", map[string]any{"x": 1}, &got); err != nil {
		t.Fatal(err)
	}
	if got.Token != "tok" || got.Args["x"] != float64(1) {
		t.Fatalf("got %+v", got)
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("socket mode = %v, %v", info.Mode().Perm(), err)
	}
}

func TestErrorsComeBackWithTheirCode(t *testing.T) {
	path := socketPath(t)
	srv, _ := Listen(path, echo)
	defer srv.Close()
	err := Call(context.Background(), path, "tok", "refuse", nil, nil)
	var be *Error
	if !errors.As(err, &be) || be.Code != "forbidden" || be.Message != "not yours" {
		t.Fatalf("refuse: %v", err)
	}
	// An error that is not the broker's own says nothing of the host's insides.
	err = Call(context.Background(), path, "tok", "boom", nil, nil)
	if !errors.As(err, &be) || be.Code != "internal" || strings.Contains(be.Message, "database") {
		t.Fatalf("boom: %v", err)
	}
}

func TestACallGivesUpAfterItsTimeout(t *testing.T) {
	path := socketPath(t)
	srv, _ := Listen(path, echo)
	defer srv.Close()
	start := time.Now()
	err := Call(context.Background(), path, "tok", "slow", nil, nil)
	if err == nil || time.Since(start) > 2500*time.Millisecond {
		t.Fatalf("slow call: %v after %v", err, time.Since(start))
	}
	if !errors.Is(err, ErrUnreachable) {
		t.Fatalf("a timeout is not ErrUnreachable: %v", err)
	}
}

func TestAMissingSocketIsUnreachable(t *testing.T) {
	err := Call(context.Background(), filepath.Join(t.TempDir(), "none.sock"), "tok", "echo", nil, nil)
	if !errors.Is(err, ErrUnreachable) {
		t.Fatalf("missing socket: %v", err)
	}
}

func TestAStaleSocketIsReplacedButALiveOneIsNot(t *testing.T) {
	path := socketPath(t)
	// A socket file left by a process that ended.
	ln, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	ln.(*net.UnixListener).SetUnlinkOnClose(false)
	_ = ln.Close()
	srv, err := Listen(path, echo)
	if err != nil {
		t.Fatalf("stale socket: %v", err)
	}
	defer srv.Close()
	// A second broker on the same live socket is refused.
	if _, err := Listen(path, echo); err == nil {
		t.Fatal("a live socket was taken over")
	}
}
