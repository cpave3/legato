package credential

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"fmt"
	"net"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
)

// startSSHD runs an sshd on loopback that lets the current user in with
// key. It returns the port and the server's host key.
func startSSHD(t *testing.T, key ssh.PublicKey) (int, ssh.PublicKey) {
	t.Helper()
	sshd, err := exec.LookPath("sshd")
	if err != nil {
		if _, statErr := os.Stat("/usr/sbin/sshd"); statErr != nil {
			t.Skip("no sshd")
		}
		sshd = "/usr/sbin/sshd"
	}
	if _, err := exec.LookPath("ssh"); err != nil {
		t.Skip("no ssh")
	}
	dir := t.TempDir()
	_, hostPriv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	host, err := ssh.NewSignerFromKey(hostPriv)
	if err != nil {
		t.Fatal(err)
	}
	hostPEM, err := ssh.MarshalPrivateKey(hostPriv, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "host_key"), pem.EncodeToMemory(hostPEM), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "authorized_keys"), ssh.MarshalAuthorizedKey(key), 0o600); err != nil {
		t.Fatal(err)
	}
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := l.Addr().(*net.TCPAddr).Port
	l.Close()
	config := fmt.Sprintf(`Port %d
ListenAddress 127.0.0.1
HostKey %s/host_key
AuthorizedKeysFile %s/authorized_keys
PasswordAuthentication no
KbdInteractiveAuthentication no
UsePAM no
StrictModes no
PidFile %s/sshd.pid
`, port, dir, dir, dir)
	if err := os.WriteFile(filepath.Join(dir, "sshd_config"), []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(sshd, "-D", "-e", "-f", filepath.Join(dir, "sshd_config"))
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		t.Skipf("sshd does not start here: %v", err)
	}
	t.Cleanup(func() { _ = cmd.Process.Kill(); _ = cmd.Wait() })
	for i := 0; i < 50; i++ {
		if conn, err := net.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", port)); err == nil {
			conn.Close()
			return port, host.PublicKey()
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Skipf("sshd did not listen: %s", stderr.String())
	return 0, nil
}

func TestARealSSHLoginGoesThroughTheExecutionsSocket(t *testing.T) {
	if testing.Short() {
		t.Skip("starts sshd")
	}
	me, err := user.Current()
	if err != nil {
		t.Skip(err)
	}
	key := newSigner(t)
	port, hostKey := startSSHD(t, key.PublicKey())
	knownHosts := filepath.Join(t.TempDir(), "known_hosts")
	line := fmt.Sprintf("[127.0.0.1]:%d %s", port, ssh.MarshalAuthorizedKey(hostKey))
	if err := os.WriteFile(knownHosts, []byte(line), 0o600); err != nil {
		t.Fatal(err)
	}

	allowed := true
	backend := &sshBackend{keys: map[string]ssh.Signer{"homelab": key}, allow: func(SSHUse) Decision { return Decision{Allowed: allowed, Reason: "no"} }}
	path, _ := serveAgent(t, backend, KnownHosts{Path: knownHosts})

	login := func() error {
		cmd := exec.Command("ssh", "-F", "/dev/null",
			"-o", "IdentityAgent="+path, "-o", "BatchMode=yes", "-o", "PasswordAuthentication=no",
			"-o", "UserKnownHostsFile="+knownHosts, "-o", "StrictHostKeyChecking=yes",
			"-p", fmt.Sprint(port), me.Username+"@127.0.0.1", "echo", "logged-in")
		cmd.Env = append(os.Environ(), "SSH_AUTH_SOCK="+path)
		out, err := cmd.CombinedOutput()
		if err == nil && !strings.Contains(string(out), "logged-in") {
			return fmt.Errorf("output %q", out)
		}
		if err != nil {
			return fmt.Errorf("%v: %s", err, out)
		}
		return nil
	}

	if err := login(); err != nil {
		t.Fatalf("allowed login failed: %v", err)
	}
	want := fmt.Sprintf("ssh://%s@127.0.0.1:%d", me.Username, port)
	if len(backend.uses) == 0 || backend.uses[0].Resource != want || backend.uses[0].Operation != OperationSSHLogin {
		t.Fatalf("uses = %+v, want %s", backend.uses, want)
	}

	allowed = false
	if err := login(); err == nil {
		t.Fatal("a denied login worked")
	}
}
