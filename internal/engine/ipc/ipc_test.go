package ipc_test

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/cpave3/legato/internal/engine/ipc"
)

func TestSocketDir_UsesXDGRuntimeDir(t *testing.T) {
	t.Setenv("XDG_RUNTIME_DIR", "/run/user/1000")

	got := ipc.SocketDir()

	if got != "/run/user/1000/legato" {
		t.Errorf("SocketDir() = %q, want %q", got, "/run/user/1000/legato")
	}
}

func TestSocketDir_FallsBackToTmp(t *testing.T) {
	t.Setenv("XDG_RUNTIME_DIR", "")

	got := ipc.SocketDir()

	want := "/tmp/legato-" + itoa(os.Getuid())
	if got != want {
		t.Errorf("SocketDir() = %q, want %q", got, want)
	}
}

func TestSocketPath_IncludesPID(t *testing.T) {
	t.Setenv("XDG_RUNTIME_DIR", "/run/user/1000")

	got := ipc.SocketPath()

	pid := itoa(os.Getpid())
	want := "/run/user/1000/legato/legato-" + pid + ".sock"
	if got != want {
		t.Errorf("SocketPath() = %q, want %q", got, want)
	}
}

func TestServerReceivesClientMessage(t *testing.T) {
	sockPath := filepath.Join(t.TempDir(), "legato.sock")

	received := make(chan ipc.Message, 1)
	srv, err := ipc.NewServer(sockPath, func(msg ipc.Message) {
		received <- msg
	})
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	defer srv.Close()

	msg := ipc.Message{Type: "task_update", TaskID: "abc123", Status: "done"}
	if err := ipc.Send(sockPath, msg); err != nil {
		t.Fatalf("Send: %v", err)
	}

	select {
	case got := <-received:
		if got.Type != "task_update" || got.TaskID != "abc123" || got.Status != "done" {
			t.Errorf("received %+v, want %+v", got, msg)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for message")
	}
}

func TestServerCleansUpStaleSocket(t *testing.T) {
	sockPath := filepath.Join(t.TempDir(), "legato.sock")

	// Create a stale socket file (no one listening).
	if err := os.WriteFile(sockPath, []byte{}, 0o600); err != nil {
		t.Fatalf("creating stale file: %v", err)
	}

	received := make(chan ipc.Message, 1)
	srv, err := ipc.NewServer(sockPath, func(msg ipc.Message) {
		received <- msg
	})
	if err != nil {
		t.Fatalf("NewServer with stale socket: %v", err)
	}
	defer srv.Close()

	// Prove it works by doing a round-trip.
	msg := ipc.Message{Type: "task_note", TaskID: "def456", Content: "hello"}
	if err := ipc.Send(sockPath, msg); err != nil {
		t.Fatalf("Send: %v", err)
	}

	select {
	case got := <-received:
		if got.TaskID != "def456" || got.Content != "hello" {
			t.Errorf("received %+v, want %+v", got, msg)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for message")
	}
}

func TestConcurrentConnections(t *testing.T) {
	sockPath := filepath.Join(t.TempDir(), "legato.sock")

	var mu sync.Mutex
	var received []ipc.Message
	srv, err := ipc.NewServer(sockPath, func(msg ipc.Message) {
		mu.Lock()
		received = append(received, msg)
		mu.Unlock()
	})
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	defer srv.Close()

	const n = 10
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func(i int) {
			defer wg.Done()
			msg := ipc.Message{Type: "task_update", TaskID: itoa(i), Status: "done"}
			if err := ipc.Send(sockPath, msg); err != nil {
				t.Errorf("Send(%d): %v", i, err)
			}
		}(i)
	}
	wg.Wait()

	// Give the server a moment to process all messages.
	time.Sleep(100 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()
	if len(received) != n {
		t.Errorf("received %d messages, want %d", len(received), n)
	}
}

func TestServerIgnoresMalformedMessages(t *testing.T) {
	sockPath := filepath.Join(t.TempDir(), "legato.sock")

	received := make(chan ipc.Message, 2)
	srv, err := ipc.NewServer(sockPath, func(msg ipc.Message) {
		received <- msg
	})
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	defer srv.Close()

	// Send garbage followed by a valid message over raw socket.
	conn, err := net.Dial("unix", sockPath)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	conn.Write([]byte("not json\n"))
	conn.Write([]byte(`{"type":"task_update","task_id":"ok"}` + "\n"))
	conn.Close()

	select {
	case got := <-received:
		if got.TaskID != "ok" {
			t.Errorf("got TaskID=%q, want %q", got.TaskID, "ok")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out — server may have crashed on malformed input")
	}
}

func TestSendReturnsNilWhenNoSocket(t *testing.T) {
	sockPath := filepath.Join(t.TempDir(), "nonexistent.sock")

	err := ipc.Send(sockPath, ipc.Message{Type: "task_update", TaskID: "x"})

	if err != nil {
		t.Errorf("Send to missing socket returned error: %v", err)
	}
}

func itoa(i int) string {
	return fmt.Sprintf("%d", i)
}
