package credential

import (
	"bytes"
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
)

// SessionBindExtension is OpenSSH's agent extension that binds an agent
// connection to the host key of the session it serves (OpenSSH 8.9 on).
const SessionBindExtension = "session-bind@openssh.com"

// The operations of an SSH login.
const (
	OperationSSHLogin          = "ssh.login"
	OperationSSHForwardedLogin = "ssh.forwarded-login"
)

// maxBinds is how many session binds one connection may carry, as ssh-agent.
const maxBinds = 16

// SSHKey is the public key of a credential.
type SSHKey struct {
	Credential string
	Public     ssh.PublicKey
}

// SSHUse is one sign request, classified.
type SSHUse struct {
	Execution
	Credential string
	Classified
	Forwarded bool
}

// SSHBackend is what the agent asks of Legato.
type SSHBackend interface {
	Keys(ctx context.Context) ([]SSHKey, error)
	Authorize(ctx context.Context, use SSHUse) (Decision, error)
	Signer(ctx context.Context, credential string) (ssh.Signer, error)
	// Refused records a sign request turned down before any rule was asked.
	Refused(ctx context.Context, execution Execution, credential, reason string)
}

// SSHAgentServer serves the ssh-agent channel: an agent socket for each
// execution that lists the credentials' public keys and signs logins the
// rules allow. It holds no key of the person's other agents.
type SSHAgentServer struct {
	Backend    SSHBackend
	KnownHosts KnownHosts
	// Candidates are host names to try against hashed known_hosts entries.
	Candidates func() []string
}

// Listen opens the socket at path for an execution. The directory is made
// 0700 and the socket 0600. Close stops it and removes the file.
func (s *SSHAgentServer) Listen(path string, execution Execution) (io.Closer, error) {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		return nil, err
	}
	if _, err := os.Stat(path); err == nil {
		if conn, err := net.Dial("unix", path); err == nil {
			conn.Close()
			return nil, fmt.Errorf("%s is served already", path)
		}
		_ = os.Remove(path)
	}
	listener, err := net.Listen("unix", path)
	if err != nil {
		return nil, err
	}
	if err := os.Chmod(path, 0o600); err != nil {
		listener.Close()
		return nil, err
	}
	l := &agentListener{listener: listener, path: path, conns: map[net.Conn]bool{}}
	go l.accept(s, execution)
	return l, nil
}

type agentListener struct {
	listener net.Listener
	path     string
	mu       sync.Mutex
	conns    map[net.Conn]bool
	closed   bool
}

func (l *agentListener) accept(s *SSHAgentServer, execution Execution) {
	for {
		conn, err := l.listener.Accept()
		if err != nil {
			return
		}
		l.mu.Lock()
		if l.closed {
			l.mu.Unlock()
			conn.Close()
			return
		}
		l.conns[conn] = true
		l.mu.Unlock()
		go func() {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			_ = agent.ServeAgent(&connAgent{ctx: ctx, server: s, execution: execution}, conn)
			conn.Close()
			l.mu.Lock()
			delete(l.conns, conn)
			l.mu.Unlock()
		}()
	}
}

func (l *agentListener) Close() error {
	l.mu.Lock()
	l.closed = true
	for conn := range l.conns {
		conn.Close()
	}
	l.mu.Unlock()
	err := l.listener.Close()
	if rmErr := os.Remove(l.path); rmErr != nil && !errors.Is(rmErr, os.ErrNotExist) && err == nil {
		err = rmErr
	}
	return err
}

// sessionBind is one session-bind@openssh.com a connection carried.
type sessionBind struct {
	hostKey    ssh.PublicKey
	sessionID  []byte
	forwarding bool
}

// connAgent is the agent of one connection: its binds are its own.
type connAgent struct {
	ctx       context.Context
	server    *SSHAgentServer
	execution Execution
	binds     []sessionBind
	broken    bool // a bind failed: nothing more is signed
}

var errRefused = errors.New("refused by Legato")

func (a *connAgent) List() ([]*agent.Key, error) {
	keys, err := a.server.Backend.Keys(a.ctx)
	if err != nil {
		return nil, err
	}
	out := make([]*agent.Key, 0, len(keys))
	for _, k := range keys {
		out = append(out, &agent.Key{Format: k.Public.Type(), Blob: k.Public.Marshal(), Comment: k.Credential})
	}
	return out, nil
}

func (a *connAgent) Sign(key ssh.PublicKey, data []byte) (*ssh.Signature, error) {
	return a.SignWithFlags(key, data, 0)
}

func (a *connAgent) SignWithFlags(key ssh.PublicKey, data []byte, flags agent.SignatureFlags) (*ssh.Signature, error) {
	keys, err := a.server.Backend.Keys(a.ctx)
	if err != nil {
		return nil, err
	}
	name := ""
	for _, k := range keys {
		if bytes.Equal(k.Public.Marshal(), key.Marshal()) {
			name = k.Credential
		}
	}
	if name == "" {
		return nil, errRefused
	}
	use, reason := a.classify(data)
	if reason != "" {
		a.server.Backend.Refused(a.ctx, a.execution, name, reason)
		return nil, errRefused
	}
	use.Credential = name
	decision, err := a.server.Backend.Authorize(a.ctx, use)
	if err != nil {
		return nil, err
	}
	if !decision.Allowed {
		return nil, errRefused
	}
	signer, err := a.server.Backend.Signer(a.ctx, name)
	if err != nil {
		return nil, err
	}
	if algorithm := rsaAlgorithm(flags); algorithm != "" {
		if as, ok := signer.(ssh.AlgorithmSigner); ok {
			return as.SignWithAlgorithm(rand.Reader, data, algorithm)
		}
	}
	return signer.Sign(rand.Reader, data)
}

func rsaAlgorithm(flags agent.SignatureFlags) string {
	switch {
	case flags&agent.SignatureFlagRsaSha512 != 0:
		return ssh.KeyAlgoRSASHA512
	case flags&agent.SignatureFlagRsaSha256 != 0:
		return ssh.KeyAlgoRSASHA256
	}
	return ""
}

// classify checks sign data against the connection's binds and says what
// the login is. A non-empty reason is a refusal.
func (a *connAgent) classify(data []byte) (SSHUse, string) {
	if a.broken {
		return SSHUse{}, "a session bind on this connection did not verify"
	}
	if len(a.binds) == 0 {
		return SSHUse{}, "the SSH client did not bind the agent connection to a host (OpenSSH 8.9 or later is needed)"
	}
	login, err := parseUserauth(data)
	if err != nil {
		return SSHUse{}, "the data to sign is not an SSH login: " + err.Error()
	}
	bind := a.binds[len(a.binds)-1]
	if !bytes.Equal(login.sessionID, bind.sessionID) {
		return SSHUse{}, "the login is for another session than the one the connection is bound to"
	}
	if login.hostKey != nil && !bytes.Equal(login.hostKey.Marshal(), bind.hostKey.Marshal()) {
		return SSHUse{}, "the login names another host key than the bound one"
	}
	forwarded := false
	for _, b := range a.binds {
		forwarded = forwarded || b.forwarding
	}
	host := "key:" + ssh.FingerprintSHA256(bind.hostKey)
	var candidates []string
	if a.server.Candidates != nil {
		candidates = a.server.Candidates()
	}
	if names := a.server.KnownHosts.Names(bind.hostKey, candidates); len(names) > 0 {
		host = names[0]
	}
	operation := OperationSSHLogin
	if forwarded {
		operation = OperationSSHForwardedLogin
	}
	return SSHUse{Execution: a.execution, Forwarded: forwarded, Classified: Classified{
		Operation: operation, Resource: "ssh://" + login.user + "@" + host, Amounts: map[string]int64{"requests": 1},
	}}, ""
}

func (a *connAgent) Extension(extensionType string, contents []byte) ([]byte, error) {
	if extensionType != SessionBindExtension {
		return nil, agent.ErrExtensionUnsupported
	}
	bind, err := parseSessionBind(contents)
	if err != nil {
		a.broken = true
		return nil, err
	}
	if len(a.binds) >= maxBinds {
		return nil, errors.New("too many session binds on one connection")
	}
	if len(a.binds) > 0 && !a.binds[len(a.binds)-1].forwarding {
		return nil, errors.New("this connection is bound already")
	}
	a.binds = append(a.binds, bind)
	return nil, nil
}

func parseSessionBind(contents []byte) (sessionBind, error) {
	var msg struct {
		HostKey    []byte
		SessionID  []byte
		Signature  []byte
		Forwarding bool
	}
	if err := ssh.Unmarshal(contents, &msg); err != nil {
		return sessionBind{}, fmt.Errorf("reading the session bind: %w", err)
	}
	hostKey, err := ssh.ParsePublicKey(msg.HostKey)
	if err != nil {
		return sessionBind{}, fmt.Errorf("reading the bound host key: %w", err)
	}
	var sig ssh.Signature
	if err := ssh.Unmarshal(msg.Signature, &sig); err != nil {
		return sessionBind{}, fmt.Errorf("reading the host's signature: %w", err)
	}
	if err := hostKey.Verify(msg.SessionID, &sig); err != nil {
		return sessionBind{}, fmt.Errorf("the host's signature over the session does not verify: %w", err)
	}
	return sessionBind{hostKey: hostKey, sessionID: msg.SessionID, forwarding: msg.Forwarding}, nil
}

// userauth is the part of an SSH_MSG_USERAUTH_REQUEST a decision needs.
type userauth struct {
	sessionID []byte
	user      string
	hostKey   ssh.PublicKey // publickey-hostbound-v00@openssh.com only
}

const (
	methodPublicKey = "publickey"
	methodHostBound = "publickey-hostbound-v00@openssh.com"
)

// parseUserauth reads sign data as a publickey login for ssh-connection.
func parseUserauth(data []byte) (userauth, error) {
	var msg struct {
		SessionID []byte
		Type      byte
		User      string
		Service   string
		Method    string
		Sign      bool
		Algorithm string
		Key       []byte
		Rest      []byte `ssh:"rest"`
	}
	if err := ssh.Unmarshal(data, &msg); err != nil {
		return userauth{}, errors.New("it does not read as a login request")
	}
	if msg.Type != 50 {
		return userauth{}, errors.New("it is not a login request")
	}
	if msg.Service != "ssh-connection" {
		return userauth{}, fmt.Errorf("service %q, not ssh-connection", msg.Service)
	}
	if !msg.Sign {
		return userauth{}, errors.New("it does not ask for a signature")
	}
	out := userauth{sessionID: msg.SessionID, user: msg.User}
	switch msg.Method {
	case methodPublicKey:
		if len(msg.Rest) != 0 {
			return userauth{}, errors.New("it has data after the key")
		}
	case methodHostBound:
		var hb struct {
			HostKey []byte
		}
		if err := ssh.Unmarshal(msg.Rest, &hb); err != nil {
			return userauth{}, errors.New("its host key does not read")
		}
		key, err := ssh.ParsePublicKey(hb.HostKey)
		if err != nil {
			return userauth{}, errors.New("its host key does not read")
		}
		out.hostKey = key
	default:
		return userauth{}, fmt.Errorf("method %q, not publickey", msg.Method)
	}
	if strings.ContainsAny(out.user, "@/ \x00") || out.user == "" {
		return userauth{}, fmt.Errorf("user %q is not a user name Legato takes", out.user)
	}
	return out, nil
}

// The agent never changes: keys are Legato's, through its credentials.
var errNoChanges = errors.New("this agent's keys are Legato's credentials; it takes no changes")

func (a *connAgent) Add(agent.AddedKey) error       { return errNoChanges }
func (a *connAgent) Remove(ssh.PublicKey) error     { return errNoChanges }
func (a *connAgent) RemoveAll() error               { return errNoChanges }
func (a *connAgent) Lock([]byte) error              { return errNoChanges }
func (a *connAgent) Unlock([]byte) error            { return errNoChanges }
func (a *connAgent) Signers() ([]ssh.Signer, error) { return nil, errNoChanges }
