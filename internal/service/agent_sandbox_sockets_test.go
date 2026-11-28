package service

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cpave3/legato/internal/engine/store"
)

// sshChanneledService is a sandboxed service whose sessions are given an SSH
// agent socket under the runtime folder, as the credential channels give
// one, and which waits at most wait for it.
func sshChanneledService(t *testing.T, wait time.Duration) (AgentService, *store.Store, *mockTmux, string) {
	t.Helper()
	var sock string
	svc, s, mt := sandboxedService(t, &fakeBackend{}, func(settings *SandboxSettings) {
		// A short folder: a Unix socket's path is limited to 108 bytes.
		dir, err := os.MkdirTemp("", "lg")
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { os.RemoveAll(dir) })
		settings.Base.RuntimeDir = dir
		settings.SocketWait = wait
		sock = filepath.Join(dir, "ssh", "ex.sock")
	})
	svc.(interface{ SetSessionEnv(SessionEnvProvider) }).SetSessionEnv(fixedEnv{"SSH_AUTH_SOCK": sock})
	return svc, s, mt, sock
}

func listenAt(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Error(err)
		return
	}
	ln, err := net.Listen("unix", path)
	if err != nil {
		t.Error(err)
		return
	}
	t.Cleanup(func() { ln.Close() })
}

// LEG-709: the channel host may open an execution's SSH socket a moment
// after the spawn (another process serves the channels); bwrap refuses to
// bind a path that is not there, so the launch waits for it.
func TestASandboxedLaunchWaitsForItsSSHSocket(t *testing.T) {
	svc, s, mt, sock := sshChanneledService(t, 5*time.Second)
	createTask(t, s, "task-1")
	var missingAtLaunch []string
	mt.onRunInPane = func(string) {
		if _, err := os.Lstat(sock); err != nil {
			missingAtLaunch = append(missingAtLaunch, sock)
		}
	}
	go func() {
		time.Sleep(150 * time.Millisecond)
		listenAt(t, sock)
	}()
	if err := svc.SpawnAgent(context.Background(), "task-1", 0, 0); err != nil {
		t.Fatal(err)
	}
	if len(mt.paneLaunchesFor("legato-task-1")) == 0 {
		t.Fatal("the agent was not launched")
	}
	if len(missingAtLaunch) > 0 {
		t.Fatalf("launched before %v existed", missingAtLaunch)
	}
}

func TestASandboxedLaunchFailsNamingASocketThatNeverAppears(t *testing.T) {
	svc, s, mt, sock := sshChanneledService(t, 200*time.Millisecond)
	createTask(t, s, "task-1")
	err := svc.SpawnAgent(context.Background(), "task-1", 0, 0)
	if err == nil || !strings.Contains(err.Error(), sock) {
		t.Fatalf("err = %v, want one naming %s", err, sock)
	}
	if launches := mt.paneLaunchesFor("legato-task-1"); len(launches) != 0 {
		t.Fatalf("launched anyway: %v", launches)
	}
	executions, _ := s.ListRunningExecutions(context.Background())
	if len(executions) != 0 {
		t.Fatalf("executions still running: %+v", executions)
	}
}

// With real bwrap: a socket the channel host opens after the launch began
// stops bwrap, and waiting for it lets the agent start with it bound.
func TestBwrapStartsWithAnSSHSocketOpenedLateOnceItIsWaitedFor(t *testing.T) {
	in := bwrapFixture(t)
	// A short folder: the fixture's may pass a Unix socket's 108 bytes.
	dir, err := os.MkdirTemp("", "lg")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	in.SSHSocket = filepath.Join(dir, "ssh", "ex-1.sock")
	if out, err := runInSandbox(t, in, "true"); err == nil || !strings.Contains(out, "Can't find source path "+in.SSHSocket) {
		t.Fatalf("bwrap started without the socket: %v %s", err, out)
	}
	go func() {
		time.Sleep(100 * time.Millisecond)
		listenAt(t, in.SSHSocket)
	}()
	settings := &SandboxSettings{SocketWait: 5 * time.Second}
	if err := settings.awaitSandboxSockets(context.Background(), sandboxSockets(in)); err != nil {
		t.Fatal(err)
	}
	if out, err := runInSandbox(t, in, "test -S "+in.SSHSocket); err != nil {
		t.Fatalf("%v: %s", err, out)
	}
}
