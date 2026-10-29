package credential

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"regexp"
	"strings"
	"time"
)

// GitBackend is what the proxy asks of Legato for pushes.
type GitBackend interface {
	// DefaultBranch is a repository's default branch ("main").
	DefaultBranch(ctx context.Context, credential, repo string) (string, error)
	// AuthorizePush decides every ref update of a push together: a push is
	// all or nothing, so nothing is spent unless every update is allowed.
	// A use that needs the person is pending at once; git cannot wait.
	AuthorizePush(ctx context.Context, execution Execution, credential, repo string, uses []Classified) ([]Decision, error)
	// PushToken makes a write token for one repository, and revokes it.
	PushToken(ctx context.Context, execution Execution, credential, repo string) ([]byte, func(), error)
}

// pushTokenLife is how long a write token made for a push's advertisement
// waits for the push itself.
const pushTokenLife = 2 * time.Minute

type pushToken struct {
	token   []byte
	release func()
	timer   *time.Timer
}

var repoPart = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)

// serveGit takes a push for owner/repo: the receive-pack advertisement and
// the push itself. Fetches do not come here (git fetches from GitHub with a
// read token); anything else is refused.
func (p *HTTPProxy) serveGit(w http.ResponseWriter, r *http.Request, c ProxyCredential, rest string) {
	ctx := r.Context()
	gb, ok := p.Backend.(GitBackend)
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "pushes are not served here"})
		return
	}
	parts := strings.SplitN(rest, "/", 3)
	if len(parts) != 3 {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "want /git/<owner>/<repo>.git/…"})
		return
	}
	owner, name, tail := parts[0], strings.TrimSuffix(parts[1], ".git"), parts[2]
	if !repoPart.MatchString(owner) || !repoPart.MatchString(name) || owner == "." || owner == ".." || name == "." || name == ".." {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "not a repository name"})
		return
	}
	repo := owner + "/" + name
	push := r.Method == http.MethodPost && tail == "git-receive-pack"
	advertise := r.Method == http.MethodGet && tail == "info/refs" && r.URL.Query().Get("service") == "git-receive-pack"
	if !push && !advertise {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "only pushes go through Legato; fetch from GitHub"})
		return
	}

	_, placeholder, hasAuth := r.BasicAuth()
	owner2, execution, found, err := p.Backend.ResolvePlaceholder(ctx, placeholder)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Legato could not check the key: " + err.Error()})
		return
	}
	if !hasAuth || !found || owner2 != c.Name {
		w.Header().Set("WWW-Authenticate", `Basic realm="Legato"`)
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "this push carries no key Legato gave this agent for " + c.Name})
		return
	}
	key := execution.ExecutionID + "\x00" + repo

	if advertise {
		token, err := p.pushTokenFor(ctx, gb, execution, c.Name, repo, key, true)
		if err != nil {
			log.Printf("credentials: proxy %s: a write token for %s: %v", c.Name, repo, err)
			writeJSON(w, http.StatusBadGateway, map[string]string{"error": "Legato could not get a token to push to " + repo})
			return
		}
		p.forwardGit(w, r, c, repo, tail, nil, token)
		return
	}

	pushed, head, err := ReadReceivePack(r.Body, r.Header.Get("Content-Encoding"))
	if err != nil {
		p.dropPushToken(key)
		_, _ = io.Copy(io.Discard, r.Body)
		log.Printf("credentials: proxy %s: reading a push (encoding %q, length %d): %v", c.Name, r.Header.Get("Content-Encoding"), r.ContentLength, err)
		http.Error(w, "Legato: "+err.Error(), http.StatusBadRequest)
		return
	}
	defaultBranch, err := gb.DefaultBranch(ctx, c.Name, repo)
	if err != nil {
		log.Printf("credentials: proxy %s: the default branch of %s: %v", c.Name, repo, err)
	}
	reasons := map[string]string{}
	var messages []string
	var uses []Classified
	var useRefs []string
	for _, cmd := range pushed.Commands {
		use, err := ClassifyRefUpdate(repo, defaultBranch, cmd)
		if err != nil {
			reasons[cmd.Ref] = "not a branch or tag"
			messages = append(messages, "Legato: "+err.Error()+"; only branches can be pushed.")
			continue
		}
		uses = append(uses, use)
		useRefs = append(useRefs, cmd.Ref)
	}
	if len(reasons) == 0 {
		decisions, err := gb.AuthorizePush(ctx, execution, c.Name, repo, uses)
		if err != nil {
			p.dropPushToken(key)
			_, _ = io.Copy(io.Discard, r.Body)
			http.Error(w, "Legato could not check this push: "+err.Error(), http.StatusInternalServerError)
			return
		}
		for i, d := range decisions {
			if d.Allowed {
				continue
			}
			ref, use := useRefs[i], uses[i]
			if d.Pending {
				reasons[ref] = "pending " + d.ApprovalID
				messages = append(messages,
					fmt.Sprintf("Legato: %s waits for the person (pending %s).", ref, d.ApprovalID),
					"Legato: push again after the [legato approvals] message that tells you they allowed it.")
				continue
			}
			reasons[ref] = "not allowed"
			messages = append(messages, fmt.Sprintf("Legato: %s is outside your grant (%s).", ref, firstOr(d.Reason, use.Operation+" on "+use.Resource)))
			if use.Operation == OperationGitPush {
				messages = append(messages, fmt.Sprintf("Legato: to push it, ask with request_access (credential %s, operation %s, resource %s), then push again.", c.Name, use.Operation, use.Resource))
			}
		}
	}
	if len(reasons) > 0 {
		p.dropPushToken(key)
		_, _ = io.Copy(io.Discard, r.Body)
		w.Header().Set("Content-Type", "application/x-git-receive-pack-result")
		w.Header().Set("Cache-Control", "no-cache")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(RefusePush(pushed, reasons, messages))
		return
	}

	token, err := p.pushTokenFor(ctx, gb, execution, c.Name, repo, key, false)
	if err != nil {
		_, _ = io.Copy(io.Discard, r.Body)
		http.Error(w, "Legato could not get a token to push to "+repo, http.StatusBadGateway)
		return
	}
	defer p.dropPushToken(key)
	p.forwardGit(w, r, c, repo, tail, io.MultiReader(bytes.NewReader(head), r.Body), token)
}

// pushTokenFor makes a write token for a push, or takes the one made for
// its advertisement. keep holds it for the push that follows.
func (p *HTTPProxy) pushTokenFor(ctx context.Context, gb GitBackend, execution Execution, credential, repo, key string, keep bool) ([]byte, error) {
	p.mu.Lock()
	if held, ok := p.pushes[key]; ok {
		p.mu.Unlock()
		return held.token, nil
	}
	p.mu.Unlock()
	token, release, err := gb.PushToken(ctx, execution, credential, repo)
	if err != nil {
		return nil, err
	}
	held := &pushToken{token: token, release: release}
	held.timer = time.AfterFunc(pushTokenLife, func() { p.dropPushToken(key) })
	p.mu.Lock()
	if p.pushes == nil {
		p.pushes = map[string]*pushToken{}
	}
	if old, ok := p.pushes[key]; ok { // another request made one meanwhile
		p.mu.Unlock()
		held.timer.Stop()
		if release != nil {
			release()
		}
		return old.token, nil
	}
	p.pushes[key] = held
	p.mu.Unlock()
	return token, nil
}

// dropPushToken revokes a push's write token.
func (p *HTTPProxy) dropPushToken(key string) {
	p.mu.Lock()
	held, ok := p.pushes[key]
	delete(p.pushes, key)
	p.mu.Unlock()
	if !ok {
		return
	}
	held.timer.Stop()
	if held.release != nil {
		held.release()
	}
}

// forwardGit sends a push request to the git host with the write token and
// streams the answer back, without the token.
func (p *HTTPProxy) forwardGit(w http.ResponseWriter, r *http.Request, c ProxyCredential, repo, tail string, body io.Reader, token []byte) {
	target := *c.Git
	target.Path = strings.TrimSuffix(c.Git.Path, "/") + "/" + repo + ".git/" + tail
	target.RawQuery = r.URL.RawQuery
	out, err := http.NewRequestWithContext(r.Context(), r.Method, target.String(), body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if body != nil && r.ContentLength > 0 {
		out.ContentLength = r.ContentLength // the same bytes as they came
	}
	for _, h := range []string{"Content-Type", "Content-Encoding", "Accept", "Accept-Encoding", "Git-Protocol", "User-Agent"} {
		if v := r.Header.Get(h); v != "" {
			out.Header.Set(h, v)
		}
	}
	out.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte("x-access-token:"+string(token))))
	transport := p.Transport
	if transport == nil {
		transport = http.DefaultTransport
	}
	resp, err := transport.RoundTrip(out)
	if err != nil {
		log.Printf("credentials: proxy %s: %v", c.Name, err)
		http.Error(w, "the git host of "+c.Name+" could not be reached", http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()
	tokenText := string(token)
	for header, values := range resp.Header {
		if isHop(header) || strings.EqualFold(header, "WWW-Authenticate") {
			continue
		}
		for _, v := range values {
			if !strings.Contains(v, tokenText) {
				w.Header().Add(header, v)
			}
		}
	}
	w.WriteHeader(resp.StatusCode)
	_, _ = copyFlushing(w, resp.Body)
}

// serveToken gives the git and gh helpers a read token for one repository,
// after the broker allows the read. Writes never come this way.
func (p *HTTPProxy) serveToken(w http.ResponseWriter, r *http.Request, c ProxyCredential) {
	ctx := r.Context()
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "POST {repo, operation}"})
		return
	}
	placeholder := c.placeholderIn(r)
	owner, execution, found, err := p.Backend.ResolvePlaceholder(ctx, placeholder)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Legato could not check the key: " + err.Error()})
		return
	}
	if placeholder == "" || !found || owner != c.Name {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "this request carries no key Legato gave this agent for " + c.Name})
		return
	}
	var in struct {
		Repo      string `json:"repo"`
		Operation string `json:"operation"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 4096)).Decode(&in); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "want {repo, operation}"})
		return
	}
	repoOwner, repoName, ok := strings.Cut(in.Repo, "/")
	if !ok || !repoPart.MatchString(repoOwner) || !repoPart.MatchString(repoName) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "repo: want owner/name"})
		return
	}
	if in.Operation != OperationGitRead && in.Operation != "api.read" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "only read tokens are given out; pushes go through Legato's proxy"})
		return
	}
	use := ProxyUse{Execution: execution, Credential: c.Name, Method: "TOKEN", Path: in.Repo,
		Classified: Classified{Operation: in.Operation, Resource: "github:" + in.Repo, Amounts: map[string]int64{"requests": 1}}}
	decision, err := p.Backend.Authorize(ctx, use)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Legato could not check this use: " + err.Error()})
		return
	}
	if !decision.Allowed {
		out := map[string]string{"error": firstOr(decision.Reason, "not allowed"), "operation": use.Operation, "resource": use.Resource, "status": "denied"}
		if decision.Pending {
			out["status"] = "pending"
		}
		if decision.ApprovalID != "" {
			out["approval"] = decision.ApprovalID
		}
		writeJSON(w, http.StatusForbidden, out)
		return
	}
	token, _, err := p.Backend.Key(ctx, use)
	if err != nil {
		log.Printf("credentials: proxy %s: a read token for %s: %v", c.Name, in.Repo, err)
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": "Legato could not get a token for " + in.Repo})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"token": string(token)})
}
