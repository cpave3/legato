package service

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
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

// SetPullRequestReady marks a pull request ready for review, or turns it
// back into a draft (undo), for gh pr ready. GitHub wants a token that can
// also write code for it, so Legato makes one, uses it here and revokes it:
// the agent never holds it. The pull request is the one numbered, or the
// open one whose head is branch.
func (c *CredentialChannels) SetPullRequestReady(ctx context.Context, _ credential.Execution, name, repo string, number int, branch string, undo bool) (int, error) {
	bc, ok := c.broker.lookup(ctx, name)
	if !ok || bc.jwt == nil || bc.upstream == nil {
		return 0, fmt.Errorf("no GitHub credential named %q", name)
	}
	token, release, err := c.githubToken(ctx, bc, repo, credential.OperationPRReady)
	if err != nil {
		return 0, err
	}
	if release != nil {
		defer release()
	}
	owner, repoName, _ := strings.Cut(repo, "/")
	type pull struct {
		ID      string `json:"id"`
		Number  int    `json:"number"`
		IsDraft bool   `json:"isDraft"`
	}
	var found struct {
		Repository struct {
			PullRequest  *pull `json:"pullRequest"`
			PullRequests struct {
				Nodes []pull `json:"nodes"`
			} `json:"pullRequests"`
		} `json:"repository"`
	}
	vars := map[string]any{"owner": owner, "name": repoName}
	query := `query($owner: String!, $name: String!, $number: Int!) { repository(owner: $owner, name: $name) { pullRequest(number: $number) { id number isDraft } } }`
	if number > 0 {
		vars["number"] = number
	} else {
		vars["head"] = branch
		query = `query($owner: String!, $name: String!, $head: String!) { repository(owner: $owner, name: $name) { pullRequests(headRefName: $head, states: OPEN, first: 1) { nodes { id number isDraft } } } }`
	}
	if err := c.githubGraphQL(ctx, bc, token, query, vars, &found); err != nil {
		return 0, err
	}
	target := found.Repository.PullRequest
	if target == nil && len(found.Repository.PullRequests.Nodes) > 0 {
		target = &found.Repository.PullRequests.Nodes[0]
	}
	if target == nil {
		if number > 0 {
			return 0, fmt.Errorf("no pull request #%d in %s", number, repo)
		}
		return 0, fmt.Errorf("no open pull request in %s has %s as its head", repo, branch)
	}
	if target.IsDraft == undo {
		return target.Number, nil // already as asked
	}
	mutation := `mutation($id: ID!) { markPullRequestReadyForReview(input: {pullRequestId: $id}) { pullRequest { number } } }`
	if undo {
		mutation = `mutation($id: ID!) { convertPullRequestToDraft(input: {pullRequestId: $id}) { pullRequest { number } } }`
	}
	if err := c.githubGraphQL(ctx, bc, token, mutation, map[string]any{"id": target.ID}, nil); err != nil {
		return 0, err
	}
	return target.Number, nil
}

// githubGraphQL runs one GraphQL request against the credential's API and
// decodes its data into out. GitHub reports most failures as errors in a
// 200 response.
func (c *CredentialChannels) githubGraphQL(ctx context.Context, bc brokerCredential, token []byte, query string, vars map[string]any, out any) error {
	body, _ := json.Marshal(map[string]any{"query": query, "variables": vars})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimSuffix(bc.upstream.String(), "/")+"/graphql", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+string(token))
	req.Header.Set("Content-Type", "application/json")
	client := c.broker.httpClient
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	var answer struct {
		Data   json.RawMessage `json:"data"`
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&answer); err != nil {
		return fmt.Errorf("GitHub answered %s", resp.Status)
	}
	if len(answer.Errors) > 0 {
		return fmt.Errorf("GitHub: %s", answer.Errors[0].Message)
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("GitHub answered %s", resp.Status)
	}
	if out != nil {
		return json.Unmarshal(answer.Data, out)
	}
	return nil
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

// sandboxGitFetches is what a sandboxed session adds to its GitHub git
// settings: fetches from the ssh forms of the host go to its https address,
// where Legato's credential helper gives read tokens. The sandbox has no ssh
// key for them. Pushes from those forms already go to the proxy.
func sandboxGitFetches(env map[string]string) map[string]string {
	count, err := strconv.Atoi(env["GIT_CONFIG_COUNT"])
	if err != nil {
		return nil
	}
	extra := map[string]string{}
	next := count
	for i := 0; i < count; i++ {
		key, value := env["GIT_CONFIG_KEY_"+strconv.Itoa(i)], env["GIT_CONFIG_VALUE_"+strconv.Itoa(i)]
		if !strings.HasPrefix(key, "url.") || !strings.HasSuffix(key, ".pushInsteadOf") {
			continue
		}
		var host string
		switch {
		case strings.HasPrefix(value, "ssh://git@"):
			host = strings.TrimSuffix(strings.TrimPrefix(value, "ssh://git@"), "/")
		case strings.HasPrefix(value, "git@"):
			host = strings.TrimSuffix(strings.TrimPrefix(value, "git@"), ":")
		default:
			continue
		}
		extra["GIT_CONFIG_KEY_"+strconv.Itoa(next)] = "url.https://" + host + "/.insteadOf"
		extra["GIT_CONFIG_VALUE_"+strconv.Itoa(next)] = value
		next++
	}
	if next == count {
		return nil
	}
	extra["GIT_CONFIG_COUNT"] = strconv.Itoa(next)
	return extra
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// GHSignedOut is the GH_TOKEN an agent session's gh starts with when the
// person's login is kept from agents: gh takes GH_TOKEN before its keyring,
// so it never reaches the login. GitHub refuses it as bad credentials.
const GHSignedOut = "legato-signed-out"

// writeGHWrapper writes dir/gh: the real gh, run with a read token for the
// repository the command acts on when Legato gives one. Without a real gh
// there is no wrapper.
func writeGHWrapper(dir, legato string) error {
	real := ""
	for _, d := range filepath.SplitList(os.Getenv("PATH")) {
		if d == "" || filepath.Clean(d) == filepath.Clean(dir) {
			continue
		}
		candidate := filepath.Join(d, "gh")
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() && info.Mode()&0o111 != 0 {
			real = candidate
			break
		}
	}
	if real == "" {
		return errors.New("no gh on PATH")
	}
	// In a sandbox the real gh's own path is this wrapper (see
	// sandboxGH), and the real one is at LEGATO_GH_REAL. Outside the sandbox
	// gh keeps the person's own login and the wrapper stays out of the way.
	// A token that can write (gh-token prints "<token> revoke", for a pull
	// request command) is revoked when gh exits. A command Legato does
	// itself (gh pr ready; gh-token prints "done <message>") is not run.
	script := "#!/bin/sh\n" +
		"# Legato: in Legato's sandbox, gh with a token for one repository from\n" +
		"# Legato: a read token, or one that writes pull requests once allowed.\n" +
		"real=${LEGATO_GH_REAL:-" + shellQuote(real) + "}\n" +
		"[ \"$LEGATO_SANDBOXED\" = 1 ] || exec \"$real\" \"$@\"\n" +
		"if out=$(" + shellQuote(legato) + " credential gh-token -- \"$@\" 2>&1); then\n" +
		"  case $out in \"done \"*) echo \"${out#done }\"; exit 0 ;; esac\n" +
		"  token=${out%% *}; GH_TOKEN=$token; export GH_TOKEN\n" +
		"  if [ \"$token\" != \"$out\" ]; then\n" +
		"    \"$real\" \"$@\"; status=$?\n" +
		"    \"$real\" api -X DELETE installation/token >/dev/null 2>&1\n" +
		"    exit $status\n" +
		"  fi\n" +
		"elif [ \"$GH_TOKEN\" = " + GHSignedOut + " ]; then\n" +
		"  echo \"legato: gh has no GitHub token for this command, so GitHub will refuse it.\" >&2\n" +
		"  [ -n \"$out\" ] && echo \"$out\" >&2\n" +
		"  case $out in *pending*) echo \"legato: the person has been asked. When they decide you get a [legato approvals] message; run this gh command again then.\" >&2 ;;\n" +
		"  *) echo \"legato: name a listed repository with -R owner/name, or run gh inside a clone. In Legato's sandbox gh never uses the person's own login.\" >&2 ;; esac\n" +
		"fi\n" +
		"exec \"$real\" \"$@\"\n"
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	path := filepath.Join(dir, "gh")
	if current, err := os.ReadFile(path); err == nil && string(current) == script {
		return nil
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(script), 0o700); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
