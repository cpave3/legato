package service

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base32"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"

	"github.com/cpave3/legato/internal/engine/credential"
	"github.com/cpave3/legato/internal/engine/store"
	"golang.org/x/crypto/ssh"
)

// SessionEnvProvider gives an agent session the environment its credential
// channels need: placeholder keys and the proxy's address, and the session's
// own SSH agent socket.
type SessionEnvProvider interface {
	SessionEnv(ctx context.Context, taskID, executionID string) (map[string]string, error)
}

// CredentialChannelsOptions configures the channels.
type CredentialChannelsOptions struct {
	// ProxyURL is the base address of the proxy a host Legato serves, ""
	// when none does. Proxied credentials get no variables without it.
	ProxyURL func() string
	// SSHSocketDir holds the agent socket of each execution.
	SSHSocketDir string
	// KnownHosts names hosts by their keys for ssh logins.
	KnownHosts string
	// OnSession is called for each session that was given an SSH socket,
	// so a host process can open it before the agent starts.
	OnSession func(taskID, executionID string)
}

// CredentialChannels are the channels that give agents credentials without
// giving them the secret: the http-proxy channel (a placeholder key and
// Legato's proxy) and the ssh-agent channel (a socket that signs logins).
// It is the backend of both: every use goes through the broker.
type CredentialChannels struct {
	broker *CredentialBroker
	opts   CredentialChannelsOptions

	mu      sync.Mutex
	publics map[string]ssh.PublicKey // public keys only; private keys are read for each signature
}

// NewCredentialChannels builds the channels of the broker's credentials.
func NewCredentialChannels(broker *CredentialBroker, opts CredentialChannelsOptions) *CredentialChannels {
	return &CredentialChannels{broker: broker, opts: opts, publics: map[string]ssh.PublicKey{}}
}

func (c *CredentialChannels) withChannel(channel string) []brokerCredential {
	var out []brokerCredential
	for _, name := range c.broker.names {
		bc := c.broker.credentials[name]
		if listHas(bc.profile.Channels, channel) {
			out = append(out, bc)
		}
	}
	return out
}

// HasProxy reports whether a credential uses the http-proxy channel.
func (c *CredentialChannels) HasProxy() bool { return len(c.withChannel(ChannelHTTPProxy)) > 0 }

// HasSSH reports whether a credential uses the ssh-agent channel.
func (c *CredentialChannels) HasSSH() bool { return len(c.withChannel(ChannelSSHAgent)) > 0 }

// SSHSocketPath is the agent socket of an execution.
func (c *CredentialChannels) SSHSocketPath(executionID string) string {
	return filepath.Join(c.opts.SSHSocketDir, executionID+".sock")
}

var envName = regexp.MustCompile(`[^A-Z0-9]+`)

// SessionEnv makes a placeholder for each proxied credential and names the
// session's SSH socket. An agent of a Legato with no such credential gets
// nothing, and keeps the SSH_AUTH_SOCK it inherits.
func (c *CredentialChannels) SessionEnv(ctx context.Context, taskID, executionID string) (map[string]string, error) {
	env := map[string]string{}
	for _, name := range c.HiddenEnv() {
		env[name] = ""
	}
	base := ""
	if c.opts.ProxyURL != nil {
		base = strings.TrimRight(c.opts.ProxyURL(), "/")
	}
	for _, bc := range c.withChannel(ChannelHTTPProxy) {
		if base == "" {
			continue
		}
		placeholder, err := newPlaceholder()
		if err != nil {
			return nil, err
		}
		if err := c.broker.db.CreateCredentialPlaceholder(ctx, hashPlaceholder(placeholder), executionID, taskID, bc.config.Name); err != nil {
			return nil, err
		}
		upper := strings.Trim(envName.ReplaceAllString(strings.ToUpper(bc.config.Name), "_"), "_")
		for key, value := range bc.profile.Env {
			key = strings.ReplaceAll(key, "{NAME}", upper)
			value = strings.ReplaceAll(value, "{placeholder}", placeholder)
			value = strings.ReplaceAll(value, "{base_url}", base+"/c/"+bc.config.Name)
			env[key] = value
		}
	}
	if c.HasSSH() && c.opts.SSHSocketDir != "" {
		env["SSH_AUTH_SOCK"] = c.SSHSocketPath(executionID)
		if c.opts.OnSession != nil {
			c.opts.OnSession(taskID, executionID)
		}
	}
	return env, nil
}

// alwaysHidden are the variables that unlock Legato's secrets: the
// 1Password service account token and the passphrase of Legato's store.
var alwaysHidden = []string{"OP_SERVICE_ACCOUNT_TOKEN", "LEGATO_SECRETS_PASSPHRASE"}

// HiddenEnv names the variables an agent session gets set empty, whatever
// Legato or the tmux server inherited: those that unlock Legato's secrets,
// and those env:// credentials of the proxy and SSH channels are read from.
// A helper credential's variable is kept, since legato credential get reads
// it in the agent's own session.
func (c *CredentialChannels) HiddenEnv() []string {
	names := append([]string{}, alwaysHidden...)
	for _, name := range c.broker.names {
		bc := c.broker.credentials[name]
		if bc.ref.Scheme != "env" {
			continue
		}
		if listHas(bc.profile.Channels, ChannelHTTPProxy) || listHas(bc.profile.Channels, ChannelSSHAgent) {
			names = append(names, bc.ref.Path)
		}
	}
	return names
}

// EndSession ends an execution's placeholders.
func (c *CredentialChannels) EndSession(ctx context.Context, executionID string) error {
	return c.broker.db.RevokeCredentialPlaceholders(ctx, executionID)
}

func newPlaceholder() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return "lgk_" + strings.ToLower(base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(b)), nil
}

func hashPlaceholder(placeholder string) string {
	sum := sha256.Sum256([]byte(placeholder))
	return hex.EncodeToString(sum[:])
}

// Proxy is the http-proxy channel's handler, nil with no proxied credential.
func (c *CredentialChannels) Proxy() *credential.HTTPProxy {
	proxied := c.withChannel(ChannelHTTPProxy)
	if len(proxied) == 0 {
		return nil
	}
	p := &credential.HTTPProxy{Credentials: map[string]credential.ProxyCredential{}, Backend: c}
	for _, bc := range proxied {
		upstream := *bc.upstream
		p.Credentials[bc.config.Name] = credential.ProxyCredential{
			Name: bc.config.Name, Upstream: &upstream, Key: bc.key,
			Inspector: credential.RestInspector{Host: bc.upstream.Host, Routes: bc.profile.Routes},
		}
	}
	return p
}

// ResolvePlaceholder says whose a placeholder is, while its session runs.
func (c *CredentialChannels) ResolvePlaceholder(ctx context.Context, placeholder string) (string, credential.Execution, bool, error) {
	if !strings.HasPrefix(placeholder, "lgk_") {
		return "", credential.Execution{}, false, nil
	}
	p, err := c.broker.db.ResolveCredentialPlaceholder(ctx, hashPlaceholder(placeholder))
	if errors.Is(err, store.ErrNotFound) {
		return "", credential.Execution{}, false, nil
	}
	if err != nil {
		return "", credential.Execution{}, false, err
	}
	return p.Credential, credential.Execution{TaskID: p.TaskID, ExecutionID: p.ExecutionID}, true, nil
}

// Authorize checks a proxied request with the broker. A use that needs the
// person waits as a request does.
func (c *CredentialChannels) Authorize(ctx context.Context, use credential.ProxyUse) (credential.Decision, error) {
	return c.authorize(ctx, CredentialUse{TaskID: use.TaskID, ExecutionID: use.ExecutionID, Credential: use.Credential,
		Operation: use.Operation, Resource: use.Resource, Amounts: use.Amounts, Call: use.Method + " " + use.Path})
}

func (c *CredentialChannels) authorize(ctx context.Context, use CredentialUse) (credential.Decision, error) {
	d, err := c.broker.Authorize(ctx, use)
	if err != nil {
		return credential.Decision{}, err
	}
	out := credential.Decision{Allowed: d.Allowed, ApprovalID: d.ApprovalID, Reason: d.Reason}
	if !d.Allowed && d.ApprovalID != "" {
		if approval, err := c.broker.approvals.Get(ctx, d.ApprovalID); err == nil && approval.Status == store.ApprovalPending {
			out.Pending = true
		}
	}
	return out, nil
}

// Key reads a proxied credential's key.
func (c *CredentialChannels) Key(ctx context.Context, name string) ([]byte, error) {
	secret, err := c.broker.readSecret(ctx, name)
	if err != nil {
		return nil, err
	}
	return []byte(strings.TrimSpace(string(secret.Value))), nil
}

// Measured adds what a proxied request measured after it ended.
func (c *CredentialChannels) Measured(ctx context.Context, use credential.ProxyUse, amounts map[string]int64) {
	c.broker.RecordMeasured(ctx, use.Credential, amounts)
}

// SSHServer is the ssh-agent channel's server, nil with no ssh credential.
func (c *CredentialChannels) SSHServer() *credential.SSHAgentServer {
	if !c.HasSSH() {
		return nil
	}
	return &credential.SSHAgentServer{Backend: sshBackend{c}, KnownHosts: credential.KnownHosts{Path: c.opts.KnownHosts}, Candidates: c.sshHosts}
}

// sshHosts are the host names the ssh rules name, to match hashed
// known_hosts entries.
func (c *CredentialChannels) sshHosts() []string {
	seen := map[string]bool{}
	var hosts []string
	for _, bc := range c.withChannel(ChannelSSHAgent) {
		for _, r := range bc.rules {
			_, rest, ok := strings.Cut(r.Resource, "@")
			if !ok || strings.ContainsAny(rest, "*?[{/") || rest == "" || seen[rest] {
				continue
			}
			seen[rest] = true
			hosts = append(hosts, rest)
		}
	}
	sort.Strings(hosts)
	return hosts
}

// Keys are the public keys of the ssh credentials.
func (c *CredentialChannels) Keys(ctx context.Context) ([]credential.SSHKey, error) {
	var keys []credential.SSHKey
	for _, bc := range c.withChannel(ChannelSSHAgent) {
		name := bc.config.Name
		c.mu.Lock()
		public := c.publics[name]
		c.mu.Unlock()
		if public == nil {
			signer, err := c.Signer(ctx, name)
			if err != nil {
				log.Printf("credentials: %s: %v", name, err)
				continue
			}
			public = signer.PublicKey()
			c.mu.Lock()
			c.publics[name] = public
			c.mu.Unlock()
		}
		keys = append(keys, credential.SSHKey{Credential: name, Public: public})
	}
	return keys, nil
}

// Signer reads an ssh credential's private key.
func (c *CredentialChannels) Signer(ctx context.Context, name string) (ssh.Signer, error) {
	secret, err := c.broker.readSecret(ctx, name)
	if err != nil {
		return nil, err
	}
	signer, err := ssh.ParsePrivateKey(secret.Value)
	if err != nil {
		var missing *ssh.PassphraseMissingError
		if errors.As(err, &missing) {
			return nil, fmt.Errorf("the key of %s has a passphrase; store it without one", name)
		}
		return nil, fmt.Errorf("the secret of %s is not an SSH private key", name)
	}
	return signer, nil
}

// Authorize implements the ssh-agent backend: a login waits for the person
// as a request does, up to the broker's wait.
func (c *CredentialChannels) authorizeSSH(ctx context.Context, use credential.SSHUse) (credential.Decision, error) {
	call := "sign " + use.Operation
	return c.authorize(ctx, CredentialUse{TaskID: use.TaskID, ExecutionID: use.ExecutionID, Credential: use.Credential,
		Operation: use.Operation, Resource: use.Resource, Amounts: use.Amounts, Call: call})
}

// Refused records a sign request turned down before the rules were asked.
func (c *CredentialChannels) Refused(ctx context.Context, execution credential.Execution, name, reason string) {
	c.broker.audit(ctx, execution.TaskID, execution.ExecutionID, store.CredentialAuditEntry{Event: "refuse", Credential: name, Operation: credential.OperationSSHLogin, Detail: clip(reason, 500)})
}

// sshBackend adapts the channels to the ssh-agent backend, whose Authorize
// takes an SSHUse.
type sshBackend struct{ *CredentialChannels }

func (b sshBackend) Authorize(ctx context.Context, use credential.SSHUse) (credential.Decision, error) {
	return b.authorizeSSH(ctx, use)
}
