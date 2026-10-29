package credential

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
)

func newSigner(t *testing.T) ssh.Signer {
	t.Helper()
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := ssh.NewSignerFromKey(priv)
	if err != nil {
		t.Fatal(err)
	}
	return signer
}

// bindContents is a session-bind@openssh.com request as OpenSSH sends it.
func bindContents(t *testing.T, host ssh.Signer, sessionID []byte, forwarding bool, forge bool) []byte {
	t.Helper()
	sig, err := host.Sign(rand.Reader, sessionID)
	if err != nil {
		t.Fatal(err)
	}
	if forge {
		sig.Blob[0] ^= 0xff
	}
	return ssh.Marshal(struct {
		HostKey    []byte
		SessionID  []byte
		Signature  []byte
		Forwarding bool
	}{host.PublicKey().Marshal(), sessionID, ssh.Marshal(sig), forwarding})
}

// userauthData is what an SSH client asks the agent to sign to log in.
func userauthData(sessionID []byte, user, service, method string, key ssh.PublicKey, hostKey ssh.PublicKey) []byte {
	data := ssh.Marshal(struct {
		SessionID []byte
		Type      byte
		User      string
		Service   string
		Method    string
		Sign      bool
		Algorithm string
		Key       []byte
	}{sessionID, 50, user, service, method, true, key.Type(), key.Marshal()})
	if hostKey != nil {
		data = append(data, ssh.Marshal(struct{ HostKey []byte }{hostKey.Marshal()})...)
	}
	return data
}

func TestParseUserauthRequestReadsTheUserAndRefusesOtherData(t *testing.T) {
	key, host := newSigner(t), newSigner(t)
	sid := []byte("session-1")
	got, err := parseUserauth(userauthData(sid, "cameron", "ssh-connection", "publickey", key.PublicKey(), nil))
	if err != nil || got.user != "cameron" || string(got.sessionID) != "session-1" || got.hostKey != nil {
		t.Fatalf("got %+v, %v", got, err)
	}
	got, err = parseUserauth(userauthData(sid, "cameron", "ssh-connection", "publickey-hostbound-v00@openssh.com", key.PublicKey(), host.PublicKey()))
	if err != nil || got.hostKey == nil || string(got.hostKey.Marshal()) != string(host.PublicKey().Marshal()) {
		t.Fatalf("hostbound: got %+v, %v", got, err)
	}
	for name, data := range map[string][]byte{
		"other service": userauthData(sid, "cameron", "ssh-userauth", "publickey", key.PublicKey(), nil),
		"other method":  userauthData(sid, "cameron", "ssh-connection", "password", key.PublicKey(), nil),
		"not userauth":  []byte("a git commit to sign"),
		"empty":         nil,
	} {
		if _, err := parseUserauth(data); err == nil {
			t.Errorf("%s: want an error", name)
		}
	}
}

func TestKnownHostsNamesAHostKey(t *testing.T) {
	nas, git, hashed, unknown := newSigner(t), newSigner(t), newSigner(t), newSigner(t)
	path := filepath.Join(t.TempDir(), "known_hosts")
	hashedName := knownhostsHash("secret.lan")
	lines := []string{
		"nas.lan,192.168.1.10 " + string(ssh.MarshalAuthorizedKey(nas.PublicKey())),
		"[git.lan]:2222 " + string(ssh.MarshalAuthorizedKey(git.PublicKey())),
		hashedName + " " + string(ssh.MarshalAuthorizedKey(hashed.PublicKey())),
		"@revoked old.lan " + string(ssh.MarshalAuthorizedKey(unknown.PublicKey())),
	}
	if err := os.WriteFile(path, []byte(strings.Join(lines, "")), 0o600); err != nil {
		t.Fatal(err)
	}
	k := KnownHosts{Path: path}
	if got := k.Names(nas.PublicKey(), nil); len(got) != 2 || got[0] != "nas.lan" || got[1] != "192.168.1.10" {
		t.Fatalf("nas = %v", got)
	}
	if got := k.Names(git.PublicKey(), nil); len(got) != 1 || got[0] != "git.lan:2222" {
		t.Fatalf("git = %v", got)
	}
	if got := k.Names(hashed.PublicKey(), []string{"other.lan", "secret.lan"}); len(got) != 1 || got[0] != "secret.lan" {
		t.Fatalf("hashed = %v", got)
	}
	if got := k.Names(hashed.PublicKey(), nil); len(got) != 0 {
		t.Fatalf("hashed with no candidates = %v", got)
	}
	if got := k.Names(unknown.PublicKey(), nil); len(got) != 0 {
		t.Fatalf("revoked = %v", got)
	}
	if got := (KnownHosts{Path: filepath.Join(t.TempDir(), "none")}).Names(nas.PublicKey(), nil); len(got) != 0 {
		t.Fatalf("missing file = %v", got)
	}
}

// sshBackend is a fake: two keys, allow decides, and it records the uses.
type sshBackend struct {
	mu      sync.Mutex
	keys    map[string]ssh.Signer
	allow   func(SSHUse) Decision
	uses    []SSHUse
	refused []string
}

func (b *sshBackend) Keys(context.Context) ([]SSHKey, error) {
	var out []SSHKey
	for name, s := range b.keys {
		out = append(out, SSHKey{Credential: name, Public: s.PublicKey()})
	}
	return out, nil
}

func (b *sshBackend) Authorize(_ context.Context, use SSHUse) (Decision, error) {
	b.mu.Lock()
	b.uses = append(b.uses, use)
	b.mu.Unlock()
	if b.allow == nil {
		return Decision{Allowed: true}, nil
	}
	return b.allow(use), nil
}

func (b *sshBackend) Signer(_ context.Context, credential string) (ssh.Signer, error) {
	return b.keys[credential], nil
}

func serveAgent(t *testing.T, backend *sshBackend, hosts KnownHosts) (string, func() agent.ExtendedAgent) {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "ssh")
	path := filepath.Join(dir, "ex-1.sock")
	server := &SSHAgentServer{Backend: backend, KnownHosts: hosts}
	closer, err := server.Listen(path, Execution{TaskID: "t1", ExecutionID: "ex-1"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = closer.Close() })
	return path, func() agent.ExtendedAgent {
		conn, err := net.Dial("unix", path)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { conn.Close() })
		return agent.NewClient(conn)
	}
}

func TestTheSocketIsPrivateAndRemovedOnClose(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "ssh")
	path := filepath.Join(dir, "ex-1.sock")
	server := &SSHAgentServer{Backend: &sshBackend{}}
	closer, err := server.Listen(path, Execution{ExecutionID: "ex-1"})
	if err != nil {
		t.Fatal(err)
	}
	if info, _ := os.Stat(dir); info.Mode().Perm() != 0o700 {
		t.Fatalf("dir mode %v", info.Mode().Perm())
	}
	if info, _ := os.Stat(path); info.Mode().Perm() != 0o600 {
		t.Fatalf("socket mode %v", info.Mode().Perm())
	}
	if err := closer.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("socket still there: %v", err)
	}
}

func TestTheAgentListsOnlyTheCredentialsKeysAndRefusesChanges(t *testing.T) {
	backend := &sshBackend{keys: map[string]ssh.Signer{"homelab": newSigner(t)}}
	_, dial := serveAgent(t, backend, KnownHosts{})
	client := dial()
	keys, err := client.List()
	if err != nil || len(keys) != 1 || keys[0].Comment != "homelab" || string(keys[0].Blob) != string(backend.keys["homelab"].PublicKey().Marshal()) {
		t.Fatalf("keys = %v, %v", keys, err)
	}
	_, priv, _ := ed25519.GenerateKey(rand.Reader)
	if err := client.Add(agent.AddedKey{PrivateKey: priv}); err == nil {
		t.Error("Add worked")
	}
	if err := client.Lock([]byte("x")); err == nil {
		t.Error("Lock worked")
	}
	if err := client.RemoveAll(); err == nil {
		t.Error("RemoveAll worked")
	}
	if _, err := client.Extension("other@example.com", nil); err == nil {
		t.Error("an unknown extension worked")
	}
}

func TestASignatureNeedsASessionBind(t *testing.T) {
	key := newSigner(t)
	backend := &sshBackend{keys: map[string]ssh.Signer{"homelab": key}}
	_, dial := serveAgent(t, backend, KnownHosts{})
	client := dial()
	data := userauthData([]byte("sid"), "cameron", "ssh-connection", "publickey", key.PublicKey(), nil)
	if _, err := client.Sign(key.PublicKey(), data); err == nil {
		t.Fatal("signed without a bind")
	}
	if len(backend.uses) != 0 || len(backend.refused) != 1 || !strings.Contains(backend.refused[0], "8.9") {
		t.Fatalf("uses = %v, refused = %v", backend.uses, backend.refused)
	}
}

func TestABoundLoginIsAuthorizedAndSigned(t *testing.T) {
	key, host := newSigner(t), newSigner(t)
	path := filepath.Join(t.TempDir(), "known_hosts")
	_ = os.WriteFile(path, []byte("nas.lan "+string(ssh.MarshalAuthorizedKey(host.PublicKey()))), 0o600)
	backend := &sshBackend{keys: map[string]ssh.Signer{"homelab": key}}
	_, dial := serveAgent(t, backend, KnownHosts{Path: path})
	client := dial()
	sid := []byte("session-identifier")
	if _, err := client.Extension(SessionBindExtension, bindContents(t, host, sid, false, false)); err != nil {
		t.Fatal(err)
	}
	data := userauthData(sid, "cameron", "ssh-connection", "publickey", key.PublicKey(), nil)
	sig, err := client.Sign(key.PublicKey(), data)
	if err != nil {
		t.Fatal(err)
	}
	if err := key.PublicKey().Verify(data, sig); err != nil {
		t.Fatalf("signature does not verify: %v", err)
	}
	use := backend.uses[0]
	if use.Credential != "homelab" || use.Operation != "ssh.login" || use.Resource != "ssh://cameron@nas.lan" || use.ExecutionID != "ex-1" {
		t.Fatalf("use = %+v", use)
	}
}

func TestAnUnknownHostKeyIsNamedByItsFingerprint(t *testing.T) {
	key, host := newSigner(t), newSigner(t)
	backend := &sshBackend{keys: map[string]ssh.Signer{"homelab": key}}
	_, dial := serveAgent(t, backend, KnownHosts{})
	client := dial()
	sid := []byte("s")
	_, _ = client.Extension(SessionBindExtension, bindContents(t, host, sid, false, false))
	if _, err := client.Sign(key.PublicKey(), userauthData(sid, "cameron", "ssh-connection", "publickey", key.PublicKey(), nil)); err != nil {
		t.Fatal(err)
	}
	if want := "ssh://cameron@key:" + ssh.FingerprintSHA256(host.PublicKey()); backend.uses[0].Resource != want {
		t.Fatalf("resource %q, want %q", backend.uses[0].Resource, want)
	}
}

func TestAForgedBindRefusesLaterSigns(t *testing.T) {
	key, host := newSigner(t), newSigner(t)
	backend := &sshBackend{keys: map[string]ssh.Signer{"homelab": key}}
	_, dial := serveAgent(t, backend, KnownHosts{})
	client := dial()
	sid := []byte("s")
	if _, err := client.Extension(SessionBindExtension, bindContents(t, host, sid, false, true)); err == nil {
		t.Fatal("a forged bind was accepted")
	}
	if _, err := client.Sign(key.PublicKey(), userauthData(sid, "cameron", "ssh-connection", "publickey", key.PublicKey(), nil)); err == nil {
		t.Fatal("signed after a forged bind")
	}
}

func TestASignForAnotherSessionOrHostIsRefused(t *testing.T) {
	key, host, other := newSigner(t), newSigner(t), newSigner(t)
	backend := &sshBackend{keys: map[string]ssh.Signer{"homelab": key}}
	_, dial := serveAgent(t, backend, KnownHosts{})
	client := dial()
	_, _ = client.Extension(SessionBindExtension, bindContents(t, host, []byte("bound"), false, false))
	if _, err := client.Sign(key.PublicKey(), userauthData([]byte("other"), "cameron", "ssh-connection", "publickey", key.PublicKey(), nil)); err == nil {
		t.Error("signed for another session")
	}
	if _, err := client.Sign(key.PublicKey(), userauthData([]byte("bound"), "cameron", "ssh-connection", "publickey-hostbound-v00@openssh.com", key.PublicKey(), other.PublicKey())); err == nil {
		t.Error("signed for another host key")
	}
	if _, err := client.Sign(key.PublicKey(), []byte("not a login")); err == nil {
		t.Error("signed data that is not a login")
	}
}

func TestASecondBindIsRefusedUnlessTheFirstForwards(t *testing.T) {
	key, a, b := newSigner(t), newSigner(t), newSigner(t)
	backend := &sshBackend{keys: map[string]ssh.Signer{"homelab": key}}
	_, dial := serveAgent(t, backend, KnownHosts{})

	direct := dial()
	_, _ = direct.Extension(SessionBindExtension, bindContents(t, a, []byte("a"), false, false))
	if _, err := direct.Extension(SessionBindExtension, bindContents(t, b, []byte("b"), false, false)); err == nil {
		t.Fatal("a second bind on a direct connection was accepted")
	}

	forwarded := dial()
	if _, err := forwarded.Extension(SessionBindExtension, bindContents(t, a, []byte("a"), true, false)); err != nil {
		t.Fatal(err)
	}
	if _, err := forwarded.Extension(SessionBindExtension, bindContents(t, b, []byte("b"), false, false)); err != nil {
		t.Fatalf("the hop's bind: %v", err)
	}
	_, _ = forwarded.Sign(key.PublicKey(), userauthData([]byte("b"), "cameron", "ssh-connection", "publickey", key.PublicKey(), nil))
	if len(backend.uses) != 1 || backend.uses[0].Operation != "ssh.forwarded-login" {
		t.Fatalf("uses = %+v", backend.uses)
	}
}

func TestADeniedLoginIsNotSigned(t *testing.T) {
	key, host := newSigner(t), newSigner(t)
	backend := &sshBackend{keys: map[string]ssh.Signer{"homelab": key}, allow: func(SSHUse) Decision { return Decision{Reason: "no"} }}
	_, dial := serveAgent(t, backend, KnownHosts{})
	client := dial()
	_, _ = client.Extension(SessionBindExtension, bindContents(t, host, []byte("s"), false, false))
	if _, err := client.Sign(key.PublicKey(), userauthData([]byte("s"), "cameron", "ssh-connection", "publickey", key.PublicKey(), nil)); err == nil {
		t.Fatal("signed a denied login")
	}
}

func TestAKeyThatIsNotACredentialsIsNotSigned(t *testing.T) {
	key, stranger, host := newSigner(t), newSigner(t), newSigner(t)
	backend := &sshBackend{keys: map[string]ssh.Signer{"homelab": key}}
	_, dial := serveAgent(t, backend, KnownHosts{})
	client := dial()
	_, _ = client.Extension(SessionBindExtension, bindContents(t, host, []byte("s"), false, false))
	if _, err := client.Sign(stranger.PublicKey(), userauthData([]byte("s"), "cameron", "ssh-connection", "publickey", stranger.PublicKey(), nil)); err == nil {
		t.Fatal("signed with a key that is no credential's")
	}
}

// knownhostsHash writes a host name as ssh-keygen -H does.
func knownhostsHash(host string) string {
	salt := make([]byte, 20)
	_, _ = rand.Read(salt)
	return "|1|" + base64.StdEncoding.EncodeToString(salt) + "|" + base64.StdEncoding.EncodeToString(hostHMAC(salt, host))
}

func (b *sshBackend) Refused(_ context.Context, _ Execution, _ string, reason string) {
	b.mu.Lock()
	b.refused = append(b.refused, reason)
	b.mu.Unlock()
}
