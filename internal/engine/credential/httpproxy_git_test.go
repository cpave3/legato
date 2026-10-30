package credential

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/cgi"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// gitBackend is a fake Legato for pushes: it allows legato/ branches, says
// main is the default branch, and hands out one write token per push.
type gitBackend struct {
	fakeBackend
	mu       sync.Mutex
	pushes   [][]Classified
	issued   int
	revoked  int
	readUses []ProxyUse
	pending  bool
	ready    []readyCall
}

func (g *gitBackend) DefaultBranch(_ context.Context, credential, repo string) (string, error) {
	return "main", nil
}

func (g *gitBackend) AuthorizePush(_ context.Context, _ Execution, _ string, repo string, uses []Classified) ([]Decision, error) {
	g.mu.Lock()
	g.pushes = append(g.pushes, uses)
	g.mu.Unlock()
	out := make([]Decision, len(uses))
	for i, u := range uses {
		switch {
		case u.Operation == OperationGitPush && strings.Contains(u.Resource, "@refs/heads/legato/") && !g.pending:
			out[i] = Decision{Allowed: true}
		case g.pending:
			out[i] = Decision{Pending: true, ApprovalID: "ap-12345678"}
		default:
			out[i] = Decision{Reason: "no rule of github allows " + u.Operation + " on " + u.Resource}
		}
	}
	return out, nil
}

func (g *gitBackend) PushToken(_ context.Context, _ Execution, _ string, repo string) ([]byte, func(), error) {
	g.mu.Lock()
	g.issued++
	g.mu.Unlock()
	return []byte("ghs_WRITE_TOKEN"), func() {
		g.mu.Lock()
		g.revoked++
		g.mu.Unlock()
	}, nil
}

func (g *gitBackend) Key(_ context.Context, use ProxyUse) ([]byte, func(), error) {
	g.mu.Lock()
	g.readUses = append(g.readUses, use)
	g.mu.Unlock()
	return []byte("ghs_READ_TOKEN"), func() {}, nil
}

// fakeGitHubGit serves bare repositories over smart HTTP, as github.com
// would, and records the credentials each request carried.
func fakeGitHubGit(t *testing.T) (*httptest.Server, string, *[]string) {
	t.Helper()
	root := t.TempDir()
	repo := filepath.Join(root, "cpave3", "legato.git")
	run(t, "", "git", "init", "-q", "--bare", "-b", "main", repo)
	run(t, repo, "git", "config", "http.receivepack", "true")
	var mu sync.Mutex
	seen := &[]string{}
	gitPath, err := exec.LookPath("git")
	if err != nil {
		t.Skip("no git")
	}
	backend := &cgi.Handler{Path: gitPath, Args: []string{"http-backend"}, Env: []string{"GIT_PROJECT_ROOT=" + root, "GIT_HTTP_EXPORT_ALL=1"}}
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		*seen = append(*seen, r.Method+" "+r.URL.Path+" "+r.Header.Get("Authorization"))
		mu.Unlock()
		if r.Header.Get("Authorization") != "Basic "+base64.StdEncoding.EncodeToString([]byte("x-access-token:ghs_WRITE_TOKEN")) {
			w.Header().Set("WWW-Authenticate", `Basic realm="GitHub"`)
			w.WriteHeader(401)
			return
		}
		backend.ServeHTTP(w, r)
	}))
	t.Cleanup(srv.Close)
	return srv, repo, seen
}

func run(t *testing.T, dir string, name string, args ...string) string {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "HOME="+t.TempDir(), "GIT_TERMINAL_PROMPT=0")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%s %v: %v\n%s", name, args, err, out)
	}
	return string(out)
}

func gitProxy(t *testing.T, backend *gitBackend, github *httptest.Server) *httptest.Server {
	t.Helper()
	u, _ := url.Parse(github.URL)
	p := &HTTPProxy{
		Credentials: map[string]ProxyCredential{"github": {
			Name: "github", Upstream: u, Key: KeyPlace{Header: "Authorization", Format: "Bearer {secret}"},
			APIPrefix: "/api", Git: u, Tokens: true,
			Inspector: RestInspector{Host: "api.github.com", Routes: []Route{{Method: "GET", Path: "/repos/{owner}/{repo}/**", Operation: "api.read", Resource: "github:{owner}/{repo}"}}},
		}},
		Backend:   backend,
		Transport: github.Client().Transport,
	}
	srv := httptest.NewServer(p)
	t.Cleanup(srv.Close)
	return srv
}

// pushFrom makes a commit in a fresh clone of nothing and pushes refspecs
// through the proxy, as an agent's git would, answering for credentials
// with the placeholder.
func pushFrom(t *testing.T, proxyURL string, refspecs ...string) (string, error) {
	t.Helper()
	work := t.TempDir()
	run(t, work, "git", "init", "-q", "-b", "main")
	run(t, work, "git", "-c", "user.name=a", "-c", "user.email=a@b", "commit", "-q", "--allow-empty", "-m", "work")
	args := []string{"-c", "credential.helper=", "-c", `credential.helper=!f() { echo username=x-access-token; echo password=lgk_one; }; f`,
		"push", proxyURL + "/c/github/git/cpave3/legato.git"}
	cmd := exec.Command("git", append(args, refspecs...)...)
	cmd.Dir = work
	cmd.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "HOME="+t.TempDir(), "GIT_TERMINAL_PROMPT=0")
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func gitTestBackend() *gitBackend {
	return &gitBackend{fakeBackend: fakeBackend{placeholders: map[string]string{"lgk_one": "github"}}}
}

func TestAPushToAGrantedBranchReachesGitHubWithAWriteTokenTheAgentNeverSees(t *testing.T) {
	github, repo, seen := fakeGitHubGit(t)
	backend := gitTestBackend()
	proxy := gitProxy(t, backend, github)

	out, err := pushFrom(t, proxy.URL, "HEAD:refs/heads/legato/abc-fix")
	if err != nil {
		t.Fatalf("push failed: %v\n%s", err, out)
	}
	if got := run(t, repo, "git", "branch", "--list"); !strings.Contains(got, "legato/abc-fix") {
		t.Fatalf("the branch did not arrive: %q", got)
	}
	if strings.Contains(out, "ghs_WRITE_TOKEN") {
		t.Fatal("the agent's git saw the write token")
	}
	if len(backend.pushes) != 1 || backend.pushes[0][0].Operation != OperationGitPush || backend.pushes[0][0].Resource != "github:cpave3/legato@refs/heads/legato/abc-fix" {
		t.Fatalf("pushes = %+v", backend.pushes)
	}
	if backend.issued != 1 || backend.revoked != 1 {
		t.Fatalf("issued %d revoked %d: want one token for the push, revoked after it", backend.issued, backend.revoked)
	}
	for _, line := range *seen {
		if strings.Contains(line, "lgk_one") {
			t.Fatalf("GitHub saw the placeholder: %s", line)
		}
	}
}

func TestAPushOutsideTheGrantIsRefusedInGitsOwnWords(t *testing.T) {
	github, repo, seen := fakeGitHubGit(t)
	backend := gitTestBackend()
	proxy := gitProxy(t, backend, github)

	out, err := pushFrom(t, proxy.URL, "HEAD:refs/heads/main", "HEAD:refs/heads/legato/ok")
	if err == nil {
		t.Fatalf("a push to main worked:\n%s", out)
	}
	for _, want := range []string{"remote: Legato:", "refs/heads/main", "[remote rejected]"} {
		if !strings.Contains(out, want) {
			t.Errorf("git's output does not say %q:\n%s", want, out)
		}
	}
	// All or nothing: the granted branch did not go either.
	if got := run(t, repo, "git", "branch", "--list"); got != "" {
		t.Fatalf("branches arrived: %q", got)
	}
	for _, line := range *seen {
		if strings.HasPrefix(line, "POST") {
			t.Fatalf("a refused push reached GitHub: %s", line)
		}
	}
	if backend.revoked != backend.issued {
		t.Fatalf("issued %d revoked %d", backend.issued, backend.revoked)
	}
}

func TestARefusedBranchSaysHowToAskForIt(t *testing.T) {
	github, _, _ := fakeGitHubGit(t)
	proxy := gitProxy(t, gitTestBackend(), github)
	out, err := pushFrom(t, proxy.URL, "HEAD:refs/heads/feature")
	if err == nil || !strings.Contains(out, "request_access (credential github, operation git.push, resource github:cpave3/legato@refs/heads/feature)") {
		t.Fatalf("err %v\n%s", err, out)
	}
}

func TestAPushThatNeedsThePersonIsRefusedAtOnceWithTheApproval(t *testing.T) {
	github, _, _ := fakeGitHubGit(t)
	backend := gitTestBackend()
	backend.pending = true
	proxy := gitProxy(t, backend, github)
	out, err := pushFrom(t, proxy.URL, "HEAD:refs/heads/legato/wait")
	if err == nil || !strings.Contains(out, "pending ap-12345678") || !strings.Contains(out, "[legato approvals]") {
		t.Fatalf("err %v\n%s", err, out)
	}
}

func TestDeletingAndTaggingAreRefused(t *testing.T) {
	github, _, _ := fakeGitHubGit(t)
	proxy := gitProxy(t, gitTestBackend(), github)
	if out, err := pushFrom(t, proxy.URL, "HEAD:refs/tags/v1"); err == nil {
		t.Fatalf("a tag was pushed:\n%s", out)
	}
	if out, err := pushFrom(t, proxy.URL, ":refs/heads/legato/gone"); err == nil {
		t.Fatalf("a branch was deleted:\n%s", out)
	}
}

func TestOnlyPushesGoThroughTheGitRoute(t *testing.T) {
	github, _, _ := fakeGitHubGit(t)
	proxy := gitProxy(t, gitTestBackend(), github)
	auth := "Basic " + base64.StdEncoding.EncodeToString([]byte("x-access-token:lgk_one"))
	for _, path := range []string{"/c/github/git/cpave3/legato.git/info/refs?service=git-upload-pack", "/c/github/git/cpave3/legato.git/git-upload-pack", "/c/github/git/../x/info/refs?service=git-receive-pack"} {
		req, _ := http.NewRequest("GET", proxy.URL+path, nil)
		req.Header.Set("Authorization", auth)
		resp, err := http.DefaultTransport.RoundTrip(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != 403 && resp.StatusCode != 404 && resp.StatusCode != 400 {
			t.Errorf("%s: status %d", path, resp.StatusCode)
		}
	}
	// Without a key, git is asked for one.
	resp, _ := http.Get(proxy.URL + "/c/github/git/cpave3/legato.git/info/refs?service=git-receive-pack")
	resp.Body.Close()
	if resp.StatusCode != 401 || !strings.HasPrefix(resp.Header.Get("WWW-Authenticate"), "Basic") {
		t.Fatalf("status %d %v", resp.StatusCode, resp.Header)
	}
}

func TestTheHelperGetsAReadTokenForOneRepository(t *testing.T) {
	github, _, _ := fakeGitHubGit(t)
	backend := gitTestBackend()
	proxy := gitProxy(t, backend, github)
	req, _ := http.NewRequest("POST", proxy.URL+"/c/github/token", strings.NewReader(`{"repo":"cpave3/legato","operation":"git.read"}`))
	req.Header.Set("Authorization", "Bearer lgk_one")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var got map[string]string
	_ = json.NewDecoder(resp.Body).Decode(&got)
	if resp.StatusCode != 200 || got["token"] != "ghs_READ_TOKEN" {
		t.Fatalf("status %d body %v", resp.StatusCode, got)
	}
	if len(backend.uses) != 1 || backend.uses[0].Operation != "git.read" || backend.uses[0].Resource != "github:cpave3/legato" {
		t.Fatalf("uses = %+v", backend.uses)
	}
	// Only reads come this way.
	req, _ = http.NewRequest("POST", proxy.URL+"/c/github/token", strings.NewReader(`{"repo":"cpave3/legato","operation":"git.push"}`))
	req.Header.Set("Authorization", "Bearer lgk_one")
	resp2, _ := http.DefaultClient.Do(req)
	resp2.Body.Close()
	if resp2.StatusCode != 400 {
		t.Fatalf("a write token was asked for: %d", resp2.StatusCode)
	}
}

// gh's pull request commands (gh pr edit, comment, ...) get a token that can
// write pull requests, after the broker allows pr.write on the repository.
func TestGHGetsAPullRequestWriteTokenWhenTheBrokerAllowsIt(t *testing.T) {
	github, _, _ := fakeGitHubGit(t)
	backend := gitTestBackend()
	proxy := gitProxy(t, backend, github)
	req, _ := http.NewRequest("POST", proxy.URL+"/c/github/token", strings.NewReader(`{"repo":"cpave3/legato","operation":"pr.write"}`))
	req.Header.Set("Authorization", "Bearer lgk_one")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var got map[string]string
	_ = json.NewDecoder(resp.Body).Decode(&got)
	if resp.StatusCode != 200 || got["token"] == "" {
		t.Fatalf("status %d body %v", resp.StatusCode, got)
	}
	if len(backend.uses) != 1 || backend.uses[0].Operation != "pr.write" || backend.uses[0].Resource != "github:cpave3/legato" {
		t.Fatalf("uses = %+v", backend.uses)
	}
}

func TestTheAPIIsServedUnderItsPrefix(t *testing.T) {
	api := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/cpave3/legato/pulls" || r.Header.Get("Authorization") != "Bearer ghs_READ_TOKEN" {
			w.WriteHeader(404)
			return
		}
		_, _ = w.Write([]byte("[]"))
	}))
	defer api.Close()
	backend := gitTestBackend()
	proxy := gitProxy(t, backend, api)
	req, _ := http.NewRequest("GET", proxy.URL+"/c/github/api/repos/cpave3/legato/pulls", nil)
	req.Header.Set("Authorization", "Bearer lgk_one")
	resp, err := http.DefaultTransport.RoundTrip(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("status %d", resp.StatusCode)
	}
}

type readyCall struct {
	repo, branch string
	number       int
	undo         bool
}

func (g *gitBackend) SetPullRequestReady(_ context.Context, _ Execution, _ string, repo string, number int, branch string, undo bool) (int, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.ready = append(g.ready, readyCall{repo: repo, number: number, branch: branch, undo: undo})
	if number == 0 {
		number = 42
	}
	return number, nil
}

func postReady(t *testing.T, url, body string) (int, map[string]any) {
	t.Helper()
	req, _ := http.NewRequest("POST", url+"/c/github/pr-ready", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer lgk_one")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var got map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&got)
	return resp.StatusCode, got
}

// gh pr ready needs a token that can also write code, so Legato marks the
// pull request itself, after the broker allows pr.ready, and the agent never
// holds that token.
func TestLegatoMarksAPullRequestReadyItselfWhenTheBrokerAllowsIt(t *testing.T) {
	github, _, _ := fakeGitHubGit(t)
	backend := gitTestBackend()
	proxy := gitProxy(t, backend, github)

	status, got := postReady(t, proxy.URL, `{"repo":"cpave3/legato","number":1529}`)
	if status != 200 || got["number"] != "1529" {
		t.Fatalf("status %d body %v", status, got)
	}
	if len(backend.uses) != 1 || backend.uses[0].Operation != "pr.ready" || backend.uses[0].Resource != "github:cpave3/legato" {
		t.Fatalf("uses = %+v", backend.uses)
	}
	status, got = postReady(t, proxy.URL, `{"repo":"cpave3/legato","branch":"legato/x","undo":true}`)
	if status != 200 || got["number"] != "42" {
		t.Fatalf("by branch: status %d body %v", status, got)
	}
	if len(backend.ready) != 2 || backend.ready[0] != (readyCall{repo: "cpave3/legato", number: 1529}) || backend.ready[1] != (readyCall{repo: "cpave3/legato", branch: "legato/x", undo: true}) {
		t.Fatalf("ready = %+v", backend.ready)
	}

	backend.decide = func(ProxyUse) Decision { return Decision{Pending: true, ApprovalID: "ap-1"} }
	status, got = postReady(t, proxy.URL, `{"repo":"cpave3/legato","number":7}`)
	if status != 403 || got["status"] != "pending" || got["approval"] != "ap-1" || len(backend.ready) != 2 {
		t.Fatalf("pending: status %d body %v", status, got)
	}
}
