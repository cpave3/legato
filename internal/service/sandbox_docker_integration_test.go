package service

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cpave3/legato/internal/engine/dockerproxy"
)

// dockerFixture is a bwrap sandbox with its own Docker socket, served by
// the proxy in this test and passing to the person's real daemon. It skips
// without bwrap, without the docker command, or without a daemon.
func dockerFixture(t *testing.T) (SandboxPlanInput, dockerproxy.Cleaner, string) {
	t.Helper()
	if testing.Short() {
		t.Skip("needs a Docker daemon")
	}
	docker, err := exec.LookPath("docker")
	if err != nil {
		t.Skip("docker is not installed")
	}
	home, _ := os.UserHomeDir()
	upstream, err := dockerproxy.ParseHost(dockerproxy.FindHost(os.Getenv("LEGATO_TEST_DOCKER_HOST"), os.Getenv, home))
	if err != nil {
		t.Skip(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := upstream.Ping(ctx); err != nil {
		t.Skipf("no Docker daemon at %s: %v", upstream, err)
	}
	in := bwrapFixture(t)
	// The docker command and its plugins, where the host keeps them.
	if real, err := filepath.EvalSymlinks(docker); err == nil {
		in.Extra = append(in.Extra, SandboxPath{Path: filepath.Dir(real), Access: "read"})
	}
	execution := fmt.Sprintf("ex-it%d", time.Now().UnixNano()%1e9)
	sd := &SandboxDocker{RuntimeDir: in.RuntimeDir}
	in.DockerSocket = sd.SocketPath(in.ExecutionID)
	plan, err := BuildSandboxPlan(in)
	if err != nil {
		t.Fatal(err)
	}
	if err := sd.WriteView(in.ExecutionID, plan); err != nil {
		t.Fatal(err)
	}
	view, err := sd.View(in.ExecutionID)
	if err != nil {
		t.Fatal(err)
	}
	ln, err := net.Listen("unix", in.DockerSocket)
	if err != nil {
		t.Fatal(err)
	}
	proxy := dockerproxy.New(dockerproxy.Options{Execution: execution, View: view, Upstream: upstream, Logf: t.Logf})
	server := proxy.Server()
	go server.Serve(ln)
	cleaner := dockerproxy.Cleaner{Upstream: upstream}
	t.Cleanup(func() {
		server.Close()
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		_ = cleaner.Clean(ctx, execution)
	})
	return in, cleaner, execution
}

// Inside a sandbox, docker run, compose up --build, logs -f, exec -it and
// compose down work through the execution's socket; another agent's
// containers are not seen; a bind of ~/.ssh is refused; and everything the
// execution made is gone once it is cleaned.
func TestDockerWorksInsideTheSandboxThroughItsOwnSocket(t *testing.T) {
	in, cleaner, execution := dockerFixture(t)
	work := in.WorkDir
	files := map[string]string{
		"hello.txt":  "hello from the worktree\n",
		"Dockerfile": "FROM busybox:1.36\nCOPY hello.txt /hello.txt\nCMD [\"sh\", \"-c\", \"echo started; cat /hello.txt; sleep 300\"]\n",
		"compose.yaml": `services:
  app:
    build: .
    volumes:
      - ./:/w:z
      - data:/data
    ports:
      - "80"
volumes:
  data:
`,
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(work, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// Another execution's container, made outside the proxy.
	other := "legato-it-other-" + execution
	if out, err := exec.Command("docker", "run", "-d", "--name", other, "--label", dockerproxy.Label+"=ex-someone-else", "busybox:1.36", "sleep", "300").CombinedOutput(); err != nil {
		t.Fatalf("starting another execution's container: %v: %s", err, out)
	}
	t.Cleanup(func() { _ = exec.Command("docker", "rm", "-f", other).Run() })

	script := `
set -e
cd "` + work + `"
echo "host=$DOCKER_HOST"
docker run --rm -v "$PWD:/w:z" busybox:1.36 cat /w/hello.txt
docker compose -p lgit up -d --build
timeout 10 docker compose -p lgit logs -f app | grep -m1 started && echo logs-ok
script -qec "docker compose -p lgit exec app echo exec-ok" /dev/null
docker ps --format '{{.Names}}' > /tmp/ps.txt
cat /tmp/ps.txt
if docker run --rm -v "` + filepath.Join(in.Home, ".ssh") + `:/x" busybox:1.36 true 2>/tmp/refused.txt; then echo SSH-MOUNTED; fi
cat /tmp/refused.txt
docker compose -p lgit down -v && echo down-ok
docker run -d --name lgit-left busybox:1.36 sleep 300 >/dev/null && echo left-one
`
	out, err := runInSandbox(t, in, script)
	if err != nil {
		t.Fatalf("%v:\n%s", err, out)
	}
	for _, want := range []string{"host=unix://" + in.DockerSocket, "hello from the worktree", "logs-ok", "exec-ok", "legato: ", "down-ok", "left-one"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	if strings.Contains(out, "SSH-MOUNTED") || strings.Contains(out, other) {
		t.Errorf("the sandbox escaped its own containers:\n%s", out)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	if err := cleaner.Clean(ctx, execution); err != nil {
		t.Fatal(err)
	}
	left, err := cleaner.Executions(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range left {
		if id == execution {
			t.Fatalf("%s still has containers, networks or volumes after it was cleaned", execution)
		}
	}
}
