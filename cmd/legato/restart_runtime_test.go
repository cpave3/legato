package main

import (
	"bufio"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/cpave3/legato/internal/engine/ipc"
)

func TestARestartReachesOnlyTheProcessesOfItsDataFolder(t *testing.T) {
	msg := restartMessage("/srv/legato/data/")
	if !isRestartFor(msg, "/srv/legato/data") {
		t.Fatal("the instance's own processes did not take the restart")
	}
	if isRestartFor(msg, "/tmp/scratch-legato") {
		t.Fatal("a scratch instance sharing the runtime folder took the restart")
	}
	if isRestartFor(ipc.Message{Type: ipcRestart}, "/srv/legato/data") {
		t.Fatal("a restart naming no data folder was taken")
	}
}

func TestUnderTheSupervisorARestartIsTheSupervisors(t *testing.T) {
	env := map[string]string{hotReloadChildEnv: "1"}
	if got := restartHow(func(k string) string { return env[k] }); got != "supervisor" {
		t.Fatalf("restartHow = %q", got)
	}
	if got := restartHow(func(string) string { return "" }); got != "exec" {
		t.Fatalf("restartHow = %q", got)
	}
}

func TestLegatoRestartAsksEveryProcessOfTheInstance(t *testing.T) {
	dataDir := t.TempDir()
	cfgPath := filepath.Join(t.TempDir(), "config.yaml")
	os.WriteFile(cfgPath, []byte("data_dir: "+dataDir+"\n"), 0o600)
	t.Setenv("LEGATO_CONFIG", cfgPath)
	got := make(chan ipc.Message, 1)
	srv, err := ipc.NewServer(filepath.Join(ipc.SocketDir(), "legato-test-restart.sock"), func(m ipc.Message) { got <- m })
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Close()

	if code := runRestartCmd(nil); code != 0 {
		t.Fatalf("legato restart = %d", code)
	}
	select {
	case msg := <-got:
		if !isRestartFor(msg, dataDir) {
			t.Fatalf("message = %+v", msg)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no restart message")
	}
}

// A scratch `legato serve`, with its own configuration, data, runtime and
// tmux folders and port, restarts by exec and keeps its PID.
func TestServeRestartsInPlaceAndKeepsItsPID(t *testing.T) {
	if testing.Short() {
		t.Skip("builds and runs the binary")
	}
	bin := filepath.Join(t.TempDir(), "legato")
	if out, err := exec.Command("go", "build", "-o", bin, ".").CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}
	root := t.TempDir()
	port := freePort(t)
	cfgPath := filepath.Join(root, "config.yaml")
	os.WriteFile(cfgPath, []byte("data_dir: "+filepath.Join(root, "data")+"\nweb:\n  port: \""+port+"\"\n"), 0o600)
	env := scratchEnv(root, cfgPath)

	serve := exec.Command(bin, "serve")
	serve.Env = env
	stdout, _ := serve.StdoutPipe()
	serve.Stderr = io.Discard
	if err := serve.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = serve.Process.Kill(); _ = serve.Wait() })
	lines := make(chan string, 64)
	go func() {
		s := bufio.NewScanner(stdout)
		for s.Scan() {
			lines <- s.Text()
		}
		close(lines)
	}()
	waitForLine(t, lines, "Legato web UI")
	waitHealthy(t, port)

	restart := exec.Command(bin, "restart")
	restart.Env = env
	if out, err := restart.CombinedOutput(); err != nil {
		t.Fatalf("legato restart: %v\n%s", err, out)
	}
	// The same process prints its address again: it started again by exec.
	waitForLine(t, lines, "Legato web UI")
	waitHealthy(t, port)
	if err := serve.Process.Signal(syscallZero); err != nil {
		t.Fatalf("the process with the first PID is gone: %v", err)
	}
}

func scratchEnv(root, cfgPath string) []string {
	env := []string{}
	for _, kv := range os.Environ() {
		if strings.HasPrefix(kv, "LEGATO_") || strings.HasPrefix(kv, "TMUX") || strings.HasPrefix(kv, "XDG_") || strings.HasPrefix(kv, "HOME=") || strings.HasPrefix(kv, "SYSTEMD_") {
			continue
		}
		env = append(env, kv)
	}
	for _, d := range []string{"home", "run", "tmux", "state", "data"} {
		os.MkdirAll(filepath.Join(root, d), 0o700)
	}
	return append(env, "LEGATO_CONFIG="+cfgPath, "HOME="+filepath.Join(root, "home"),
		"XDG_RUNTIME_DIR="+filepath.Join(root, "run"), "TMUX_TMPDIR="+filepath.Join(root, "tmux"),
		"XDG_STATE_HOME="+filepath.Join(root, "state"), "XDG_CONFIG_HOME="+filepath.Join(root, "home", ".config"))
}

func freePort(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	return strconv.Itoa(ln.Addr().(*net.TCPAddr).Port)
}

func waitForLine(t *testing.T, lines <-chan string, prefix string) {
	t.Helper()
	deadline := time.After(30 * time.Second)
	for {
		select {
		case line, ok := <-lines:
			if !ok {
				t.Fatalf("output ended before %q", prefix)
			}
			if strings.HasPrefix(line, prefix) {
				return
			}
		case <-deadline:
			t.Fatalf("no %q line", prefix)
		}
	}
}

func waitHealthy(t *testing.T, port string) {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		resp, err := http.Get("http://127.0.0.1:" + port + "/health")
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatal("never healthy")
}
