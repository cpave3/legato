package service

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/cpave3/legato/internal/engine/credential"
)

// defaultBranchLife is how long a repository's default branch is kept.
const defaultBranchLife = 10 * time.Minute

type knownBranch struct {
	name string
	at   time.Time
}

// githubState is what the channels keep about GitHub repositories.
type githubState struct {
	mu       sync.Mutex
	branches map[string]knownBranch
}

// repoOf reads owner/name from a resource: github:owner/name[@ref].
func repoOf(resource string) (string, error) {
	rest, ok := strings.CutPrefix(resource, "github:")
	if !ok {
		return "", fmt.Errorf("%q is not a GitHub resource", resource)
	}
	repo, _, _ := strings.Cut(rest, "@")
	if owner, name, ok := strings.Cut(repo, "/"); !ok || owner == "" || name == "" {
		return "", fmt.Errorf("%q names no repository", resource)
	}
	return repo, nil
}

// Key puts a credential's key in a proxied request: the secret itself for
// a static profile, or a token made for the use's repository and operation
// (revoked after the request when it can write).
func (c *CredentialChannels) Key(ctx context.Context, use credential.ProxyUse) ([]byte, func(), error) {
	bc, ok := c.broker.lookup(ctx, use.Credential)
	if !ok {
		return nil, nil, fmt.Errorf("no credential named %q", use.Credential)
	}
	if bc.jwt == nil {
		secret, err := c.broker.readSecret(ctx, use.Credential)
		if err != nil {
			return nil, nil, err
		}
		return []byte(strings.TrimSpace(string(secret.Value))), nil, nil
	}
	repo, err := repoOf(use.Resource)
	if err != nil {
		return nil, nil, err
	}
	return c.githubToken(ctx, bc, repo, use.Operation)
}

// githubToken makes a token for one repository with the permissions the
// profile gives an operation. A read token is cached and shared; a token
// that can write is made for the caller alone and revoked by release.
func (c *CredentialChannels) githubToken(ctx context.Context, bc brokerCredential, repo, operation string) ([]byte, func(), error) {
	permissions := bc.profile.Permissions[operation]
	if len(permissions) == 0 {
		return nil, nil, fmt.Errorf("profile %s gives %s no permissions", bc.profile.Name, operation)
	}
	write := false
	for _, level := range permissions {
		write = write || level == "write"
	}
	owner, name, _ := strings.Cut(repo, "/")
	values := map[string]any{"owner": owner, "repo": name, "permissions": permissions}
	if write {
		nonce := make([]byte, 6)
		_, _ = rand.Read(nonce)
		values["use"] = hex.EncodeToString(nonce) // never shared: its own cache entry
	}
	scope := credential.Scope{Operations: []string{operation}, Resources: []string{"github:" + repo}, Values: values}
	if issued, ok := bc.jwt.Cached(scope); ok {
		return issued.Value, nil, nil
	}
	secret, err := c.broker.readSecret(ctx, bc.config.Name)
	if err != nil {
		return nil, nil, err
	}
	issued, err := bc.jwt.Issue(ctx, secret, scope)
	if err != nil {
		return nil, nil, err
	}
	if !write {
		return issued.Value, nil, nil
	}
	return issued.Value, func() {
		if err := bc.jwt.Revoke(context.Background(), issued); err != nil {
			log.Printf("credentials: %s: a write token for %s was not revoked: %v", bc.config.Name, repo, err)
		}
	}, nil
}

// DefaultBranch reads a repository's default branch with a read token, and
// keeps it for ten minutes.
func (c *CredentialChannels) DefaultBranch(ctx context.Context, name, repo string) (string, error) {
	c.github.mu.Lock()
	if known, ok := c.github.branches[name+"\x00"+repo]; ok && c.broker.now().Sub(known.at) < defaultBranchLife {
		c.github.mu.Unlock()
		return known.name, nil
	}
	c.github.mu.Unlock()
	bc, ok := c.broker.lookup(ctx, name)
	if !ok || bc.upstream == nil {
		return "", fmt.Errorf("no GitHub credential named %q", name)
	}
	token, _, err := c.githubToken(ctx, bc, repo, "api.read")
	if err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimSuffix(bc.upstream.String(), "/")+"/repos/"+repo, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("Authorization", "Bearer "+string(token))
	client := c.broker.httpClient
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("reading %s: %s", repo, resp.Status)
	}
	var body struct {
		DefaultBranch string `json:"default_branch"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&body); err != nil || body.DefaultBranch == "" {
		return "", fmt.Errorf("reading %s: no default branch", repo)
	}
	c.github.mu.Lock()
	if c.github.branches == nil {
		c.github.branches = map[string]knownBranch{}
	}
	c.github.branches[name+"\x00"+repo] = knownBranch{name: body.DefaultBranch, at: c.broker.now()}
	c.github.mu.Unlock()
	return body.DefaultBranch, nil
}

// AuthorizePush decides a push's ref updates together. Each is checked
// without spending anything; only when every one is allowed are they used
// (a once-grant spent, each recorded). One that needs the person is asked
// for at once and the push refused: git cannot wait.
func (c *CredentialChannels) AuthorizePush(ctx context.Context, execution credential.Execution, name, repo string, uses []credential.Classified) ([]credential.Decision, error) {
	out := make([]credential.Decision, len(uses))
	all := true
	for i, u := range uses {
		d, err := c.broker.CheckUse(ctx, c.pushUse(execution, name, u))
		if err != nil {
			return nil, err
		}
		out[i] = c.decision(ctx, d)
		all = all && d.Allowed
	}
	if !all {
		for i := range out {
			if out[i].Allowed {
				out[i] = credential.Decision{Reason: "refused with the rest of the push"}
			}
		}
		return out, nil
	}
	for i, u := range uses {
		d, err := c.broker.Authorize(ctx, c.pushUse(execution, name, u))
		if err != nil {
			return nil, err
		}
		out[i] = c.decision(ctx, d)
	}
	return out, nil
}

func (c *CredentialChannels) pushUse(execution credential.Execution, name string, u credential.Classified) CredentialUse {
	return CredentialUse{TaskID: execution.TaskID, ExecutionID: execution.ExecutionID, Credential: name,
		Operation: u.Operation, Resource: u.Resource, Amounts: u.Amounts, Call: "git push " + strings.TrimPrefix(u.Resource, "github:")}
}

func (c *CredentialChannels) decision(ctx context.Context, d UseDecision) credential.Decision {
	out := credential.Decision{Allowed: d.Allowed, ApprovalID: d.ApprovalID, Reason: d.Reason}
	if !d.Allowed && d.ApprovalID != "" {
		if approval, err := c.broker.approvals.Get(ctx, d.ApprovalID); err == nil && approval.Status == "pending" {
			out.Pending = true
		}
	}
	return out
}

// PushToken makes a write token for one push, revoked by its release.
func (c *CredentialChannels) PushToken(ctx context.Context, execution credential.Execution, name, repo string) ([]byte, func(), error) {
	bc, ok := c.broker.lookup(ctx, name)
	if !ok || bc.jwt == nil {
		return nil, nil, fmt.Errorf("no GitHub credential named %q", name)
	}
	return c.githubToken(ctx, bc, repo, credential.OperationGitPush)
}

// githubEnv is the git configuration an agent session gets for a GitHub
// credential, through GIT_CONFIG_COUNT: Legato's credential helper for
// github.com (and no other helper there), and pushes sent to the proxy.
func githubEnv(env map[string]string, legato, name, base, gitHost string) {
	proxyGit := base + "/c/" + name + "/git/"
	proxyOrigin := base
	helper := "!" + shellQuote(legato) + " credential git " + name
	entries := [][2]string{
		{"credential." + gitHost + ".helper", ""},
		{"credential." + gitHost + ".helper", helper},
		{"credential." + gitHost + ".useHttpPath", "true"},
		{"credential." + proxyOrigin + ".helper", ""},
		{"credential." + proxyOrigin + ".helper", helper},
		{"url." + proxyGit + ".pushInsteadOf", gitHost + "/"},
		{"url." + proxyGit + ".pushInsteadOf", "git@" + strings.TrimPrefix(gitHost, "https://") + ":"},
		{"url." + proxyGit + ".pushInsteadOf", "ssh://git@" + strings.TrimPrefix(gitHost, "https://") + "/"},
	}
	env["GIT_CONFIG_COUNT"] = strconv.Itoa(len(entries))
	for i, e := range entries {
		env["GIT_CONFIG_KEY_"+strconv.Itoa(i)] = e[0]
		env["GIT_CONFIG_VALUE_"+strconv.Itoa(i)] = e[1]
	}
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
