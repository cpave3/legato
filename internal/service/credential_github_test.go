package service

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"io"
	"net/http"
	"net/http/cgi"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cpave3/legato/config"
	"github.com/cpave3/legato/internal/engine/store"
)

// githubFake is GitHub for one App and one repository, cpave3/legato:
// the App endpoints, the REST API and smart-HTTP git.
type githubFake struct {
	mu      sync.Mutex
	srv     *httptest.Server
	bare    string
	tokens  map[string]map[string]any // token -> permissions it was made with
	revoked []string
	pulls   []map[string]any
	next    int
}

func newGitHubFake(t *testing.T) *githubFake {
	t.Helper()
	gitPath, err := exec.LookPath("git")
	if err != nil {
		t.Skip("no git")
	}
	root := t.TempDir()
	g := &githubFake{tokens: map[string]map[string]any{}, bare: filepath.Join(root, "cpave3", "legato.git")}
	ghGit(t, "", "init", "-q", "--bare", "-b", "main", g.bare)
	ghGit(t, g.bare, "config", "http.receivepack", "true")
	backend := &cgi.Handler{Path: gitPath, Args: []string{"http-backend"}, Env: []string{"GIT_PROJECT_ROOT=" + root, "GIT_HTTP_EXPORT_ALL=1"}}
	g.srv = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		g.mu.Lock()
		defer g.mu.Unlock()
		bearer := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		switch {
		case r.URL.Path == "/repos/cpave3/legato/installation":
			_, _ = io.WriteString(w, `{"id": 7}`)
		case r.URL.Path == "/app/installations/7/access_tokens":
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			g.next++
			token := "ghs_" + strings.Repeat("x", 4) + string(rune('a'+g.next))
			g.tokens[token], _ = body["permissions"].(map[string]any)
			w.WriteHeader(201)
			_ = json.NewEncoder(w).Encode(map[string]string{"token": token, "expires_at": time.Now().Add(time.Hour).UTC().Format(time.RFC3339)})
		case r.Method == "DELETE" && r.URL.Path == "/installation/token":
			g.revoked = append(g.revoked, bearer)
			delete(g.tokens, bearer)
			w.WriteHeader(204)
		case r.URL.Path == "/repos/cpave3/legato" && g.tokens[bearer] != nil:
			_, _ = io.WriteString(w, `{"default_branch": "main"}`)
		case r.Method == "POST" && r.URL.Path == "/repos/cpave3/legato/pulls" && g.tokens[bearer]["pull_requests"] == "write":
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			g.pulls = append(g.pulls, body)
			w.WriteHeader(201)
			_, _ = io.WriteString(w, `{"number": 1}`)
		case strings.HasPrefix(r.URL.Path, "/cpave3/legato.git/"):
			user, pass, _ := r.BasicAuth()
			if user != "x-access-token" || g.tokens[pass]["contents"] != "write" {
				w.WriteHeader(401)
				return
			}
			g.mu.Unlock()
			backend.ServeHTTP(w, r)
			g.mu.Lock()
		default:
			w.WriteHeader(404)
		}
	}))
	t.Cleanup(g.srv.Close)
	return g
}

func ghGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "HOME="+t.TempDir(), "GIT_TERMINAL_PROMPT=0")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return string(out)
}

// githubProfileFor writes the shipped github profile with its addresses
// moved to the fake.
func githubProfileFor(t *testing.T, g *githubFake) string {
	t.Helper()
	data, err := shippedProfiles.ReadFile("credential_profiles/github.yaml")
	if err != nil {
		t.Fatal(err)
	}
	text := strings.ReplaceAll(string(data), "https://api.github.com", g.srv.URL)
	text = strings.ReplaceAll(text, "https://github.com", g.srv.URL)
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "github.yaml"), []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
	return dir
}

type githubFixture struct {
	brokerFixture
	channels *CredentialChannels
	github   *githubFake
	proxy    *httptest.Server
	env      map[string]string
}

func newGitHubFixture(t *testing.T) githubFixture {
	t.Helper()
	g := newGitHubFake(t)
	key, _ := rsa.GenerateKey(rand.Reader, 2048)
	t.Setenv("GH_APP_KEY", string(pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})))
	approvals, db, _ := newApprovalFixture(t)
	approvals.opts.PollInterval = 5 * time.Millisecond
	createTask(t, db, "task-1")
	if err := db.CreateExecution(context.Background(), store.Execution{ID: "ex-1", Kind: store.ExecutionInteractive, Status: store.ExecutionRunning, TaskID: "task-1", ExecutionTaskID: "task-1", StartedAt: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}
	messenger := &recordingMessenger{}
	broker, err := NewCredentialBroker(db, approvals, CredentialBrokerOptions{
		Credentials: []config.CredentialConfig{{Name: "gh", Secret: "env://GH_APP_KEY", Profile: "github", Type: "keypair",
			Settings: config.CredentialSettings{ClientID: "Iv1.test", Repositories: []string{"cpave3/legato"}}}},
		ProfileDirs: []string{githubProfileFor(t, g)},
		Messenger:   messenger, WaitTimeout: 100 * time.Millisecond, HTTPClient: g.srv.Client(),
	})
	if err != nil {
		t.Fatal(err)
	}
	broker.RegisterResolver()
	var proxyURL string
	channels := NewCredentialChannels(broker, CredentialChannelsOptions{ProxyURL: func() string { return proxyURL }, LegatoBin: "/opt/legato"})
	p := channels.Proxy()
	p.Transport = g.srv.Client().Transport
	proxy := httptest.NewServer(p)
	t.Cleanup(proxy.Close)
	proxyURL = proxy.URL
	env, err := channels.SessionEnv(context.Background(), "task-1", "ex-1")
	if err != nil {
		t.Fatal(err)
	}
	return githubFixture{brokerFixture: brokerFixture{broker: broker, approvals: approvals, store: db, messenger: messenger}, channels: channels, github: g, proxy: proxy, env: env}
}

// push makes a commit and pushes it through the proxy, as an agent's git
// would with Legato's helper answering for the proxy.
func (f githubFixture) push(t *testing.T, refspec string) (string, error) {
	t.Helper()
	work := t.TempDir()
	ghGit(t, work, "init", "-q", "-b", "main")
	ghGit(t, work, "-c", "user.name=a", "-c", "user.email=a@b", "commit", "-q", "--allow-empty", "-m", "work")
	helper := "!f() { echo username=x-access-token; echo password=" + f.env["LEGATO_GITHUB_TOKEN"] + "; }; f"
	cmd := exec.Command("git", "-c", "credential.helper=", "-c", "credential.helper="+helper, "push", f.proxy.URL+"/c/gh/git/cpave3/legato.git", refspec)
	cmd.Dir = work
	cmd.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "HOME="+t.TempDir(), "GIT_TERMINAL_PROMPT=0")
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func (f githubFixture) pending(t *testing.T) Approval {
	t.Helper()
	list, _ := f.approvals.List(context.Background(), store.ApprovalFilter{Status: store.ApprovalPending})
	if len(list) != 1 {
		t.Fatalf("pending approvals = %+v", list)
	}
	return list[0]
}

func TestAListedRepositoryIsReadWithoutAsking(t *testing.T) {
	f := newGitHubFixture(t)
	token := func(repo string) (int, map[string]string) {
		req, _ := http.NewRequest("POST", f.proxy.URL+"/c/gh/token", strings.NewReader(`{"repo":"`+repo+`","operation":"git.read"}`))
		req.Header.Set("Authorization", "Bearer "+f.env["LEGATO_GITHUB_TOKEN"])
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		var body map[string]string
		_ = json.NewDecoder(resp.Body).Decode(&body)
		return resp.StatusCode, body
	}
	status, body := token("cpave3/legato")
	if status != 200 || !strings.HasPrefix(body["token"], "ghs_") || f.github.tokens[body["token"]]["contents"] != "read" {
		t.Fatalf("status %d body %v", status, body)
	}
	if second, again := token("cpave3/legato"); second != 200 || again["token"] != body["token"] {
		t.Fatal("a read token was not reused")
	}
	status, body = token("someone/else")
	if status != 403 || !strings.Contains(body["error"], "settings.repositories") {
		t.Fatalf("an unlisted repository: status %d body %v", status, body)
	}
}

func TestAPushToALegatoBranchIsAskedOnceASession(t *testing.T) {
	f := newGitHubFixture(t)
	out, err := f.push(t, "HEAD:refs/heads/legato/task-1-fix")
	if err == nil || !strings.Contains(out, "pending ap-") {
		t.Fatalf("the first push was not held for the person: %v\n%s", err, out)
	}
	approval := f.pending(t)
	if approval.Options[0].ID != "session" || !strings.Contains(approval.Summary, "github:cpave3/legato@refs/heads/legato/task-1-fix") {
		t.Fatalf("approval = %+v", approval)
	}
	if _, err := f.approvals.Decide(context.Background(), approval.ID, ApprovalDecision{Decision: ApprovalAllow, Option: "session", By: "web"}); err != nil {
		t.Fatal(err)
	}
	if out, err := f.push(t, "HEAD:refs/heads/legato/task-1-fix"); err != nil {
		t.Fatalf("the allowed push failed: %v\n%s", err, out)
	}
	if got := ghGit(t, f.github.bare, "branch", "--list"); !strings.Contains(got, "legato/task-1-fix") {
		t.Fatalf("branches = %q", got)
	}
	// Every write token was revoked; the agent never held one.
	for token, perms := range f.github.tokens {
		if perms["contents"] == "write" {
			t.Fatalf("write token %s is still live", token)
		}
	}
	if len(f.github.revoked) == 0 {
		t.Fatal("no token was revoked")
	}
	if strings.Contains(out, "ghs_") {
		t.Fatal("git's output holds a token")
	}
}

func TestAPushToTheDefaultBranchIsRefusedWithoutAsking(t *testing.T) {
	f := newGitHubFixture(t)
	out, err := f.push(t, "HEAD:refs/heads/main")
	if err == nil || !strings.Contains(out, "gh does not allow git.push-default") {
		t.Fatalf("err %v\n%s", err, out)
	}
	list, _ := f.approvals.List(context.Background(), store.ApprovalFilter{Status: store.ApprovalPending})
	if len(list) != 0 {
		t.Fatalf("the person was asked: %+v", list)
	}
}

func TestAPullRequestFromAGrantedBranch(t *testing.T) {
	f := newGitHubFixture(t)
	open := func() (int, string) {
		req, _ := http.NewRequest("POST", f.proxy.URL+"/c/gh/api/repos/cpave3/legato/pulls", strings.NewReader(`{"head":"legato/task-1-fix","base":"main","title":"Fix"}`))
		req.Header.Set("Authorization", "Bearer "+f.env["LEGATO_GITHUB_TOKEN"])
		resp, err := http.DefaultTransport.RoundTrip(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		data, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, string(data)
	}
	f.broker.wait = 50 * time.Millisecond
	if status, body := open(); status != 403 || !strings.Contains(body, "pending") {
		t.Fatalf("status %d %s", status, body)
	}
	approval := f.pending(t)
	_, _ = f.approvals.Decide(context.Background(), approval.ID, ApprovalDecision{Decision: ApprovalAllow, Option: "session", By: "web"})
	if status, body := open(); status != 201 {
		t.Fatalf("status %d %s", status, body)
	}
	if len(f.github.pulls) != 1 || f.github.pulls[0]["head"] != "legato/task-1-fix" {
		t.Fatalf("pulls = %v", f.github.pulls)
	}
	// From a branch outside the convention the person is asked each time.
	req, _ := http.NewRequest("POST", f.proxy.URL+"/c/gh/api/repos/cpave3/legato/pulls", strings.NewReader(`{"head":"feature","base":"main"}`))
	req.Header.Set("Authorization", "Bearer "+f.env["LEGATO_GITHUB_TOKEN"])
	resp, _ := http.DefaultTransport.RoundTrip(req)
	resp.Body.Close()
	list, _ := f.approvals.List(context.Background(), store.ApprovalFilter{Status: store.ApprovalPending})
	if len(list) != 1 || list[0].Options[0].ID != "once" {
		t.Fatalf("pending = %+v", list)
	}
}

func TestASessionsGitSendsPushesToTheProxyAndAsksLegatoForCredentials(t *testing.T) {
	f := newGitHubFixture(t)
	env := f.env
	if env["LEGATO_GITHUB_API_URL"] != f.proxy.URL+"/c/gh/api" || !strings.HasPrefix(env["LEGATO_GITHUB_TOKEN"], "lgk_") {
		t.Fatalf("env = %v", env)
	}
	config := map[string][]string{}
	for i := 0; i < 8; i++ {
		key := env["GIT_CONFIG_KEY_"+string(rune('0'+i))]
		config[key] = append(config[key], env["GIT_CONFIG_VALUE_"+string(rune('0'+i))])
	}
	host := f.github.srv.URL
	if env["GIT_CONFIG_COUNT"] != "8" || len(config["credential."+host+".helper"]) != 2 || config["credential."+host+".helper"][0] != "" ||
		config["credential."+host+".helper"][1] != "!'/opt/legato' credential git gh" {
		t.Fatalf("git config = %v", config)
	}
	if got := config["url."+f.proxy.URL+"/c/gh/git/.pushInsteadOf"]; len(got) != 3 || got[0] != host+"/" {
		t.Fatalf("pushInsteadOf = %v", got)
	}
	_ = base64.StdEncoding
}

func TestGHRunsWithAReadTokenThroughLegatosWrapper(t *testing.T) {
	bin := t.TempDir()
	// The real gh prints the token it was given; legato prints a token.
	_ = os.WriteFile(filepath.Join(bin, "gh"), []byte("#!/bin/sh\necho \"gh $* token=$GH_TOKEN\"\n"), 0o755)
	legato := filepath.Join(t.TempDir(), "legato")
	_ = os.WriteFile(legato, []byte("#!/bin/sh\n[ \"$1 $2 $3\" = \"credential gh-token --\" ] && echo ghs_read_for_gh\n"), 0o755)
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))

	f := newGitHubFixture(t)
	wrappers := filepath.Join(t.TempDir(), "bin")
	f.channels.opts.WrapperDir = wrappers
	f.channels.opts.LegatoBin = legato
	env, err := f.channels.SessionEnv(context.Background(), "task-1", "ex-1")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(env["PATH"], wrappers+string(os.PathListSeparator)) {
		t.Fatalf("PATH = %q", env["PATH"])
	}
	before := os.Getenv("GH_TOKEN")
	// As the agent's shell runs it: gh found on the session's PATH.
	cmd := exec.Command("/bin/sh", "-c", "gh pr list -R cpave3/legato")
	cmd.Env = append(os.Environ(), "PATH="+env["PATH"])
	out, err := cmd.CombinedOutput()
	if err != nil || strings.TrimSpace(string(out)) != "gh pr list -R cpave3/legato token=ghs_read_for_gh" {
		t.Fatalf("gh through the wrapper: %v %q", err, out)
	}
	if os.Getenv("GH_TOKEN") != before {
		t.Fatal("GH_TOKEN leaked into this process")
	}
}

func TestOnlyLegatosHelperAnswersForGitHub(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("no git")
	}
	home := t.TempDir()
	// The person's own helper for github.com, which must not be asked.
	_ = os.WriteFile(filepath.Join(home, ".gitconfig"), []byte("[credential \"https://github.com\"]\n\thelper = \"!f() { echo password=PERSONS_TOKEN; }; f\"\n[credential]\n\thelper = \"!f() { echo password=GLOBAL_TOKEN; }; f\"\n"), 0o600)
	legato := filepath.Join(t.TempDir(), "legato")
	_ = os.WriteFile(legato, []byte("#!/bin/sh\ncat >/dev/null\necho username=x-access-token\necho password=LEGATOS_TOKEN\n"), 0o755)
	env := map[string]string{}
	githubEnv(env, legato, "gh", "http://127.0.0.1:7788", "https://github.com")

	cmd := exec.Command("git", "credential", "fill")
	cmd.Stdin = strings.NewReader("protocol=https\nhost=github.com\npath=cpave3/legato.git\n\n")
	cmd.Env = []string{"HOME=" + home, "PATH=" + os.Getenv("PATH"), "GIT_TERMINAL_PROMPT=0", "GIT_CONFIG_NOSYSTEM=1"}
	for k, v := range env {
		cmd.Env = append(cmd.Env, k+"="+v)
	}
	out, err := cmd.CombinedOutput()
	if err != nil || !strings.Contains(string(out), "password=LEGATOS_TOKEN") {
		t.Fatalf("git credential fill: %v\n%s", err, out)
	}
}

func TestEveryRepositoryOfAnOwnerCanBeListedAtOnce(t *testing.T) {
	approvals, db, _ := newApprovalFixture(t)
	createTask(t, db, "task-1")
	_ = db.CreateExecution(context.Background(), store.Execution{ID: "ex-1", Kind: store.ExecutionInteractive, Status: store.ExecutionRunning, TaskID: "task-1", ExecutionTaskID: "task-1", StartedAt: time.Now().UTC()})
	broker, err := NewCredentialBroker(db, approvals, CredentialBrokerOptions{
		Credentials: []config.CredentialConfig{{Name: "gh", Secret: "env://GH_APP_KEY", Profile: "github", Type: "keypair",
			Settings: config.CredentialSettings{ClientID: "Iv1", Repositories: []string{"cpave3/*", "autopilot-dev/autopilot"}}}},
		WaitTimeout: 50 * time.Millisecond,
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	for repo, allowed := range map[string]bool{"cpave3/legato": true, "cpave3/new-thing": true, "autopilot-dev/autopilot": true, "autopilot-dev/other": false} {
		d, _ := broker.Authorize(ctx, CredentialUse{TaskID: "task-1", ExecutionID: "ex-1", Credential: "gh", Operation: "git.read", Resource: "github:" + repo})
		if d.Allowed != allowed {
			t.Errorf("%s: %+v, want allowed %v", repo, d, allowed)
		}
		if !allowed && !strings.Contains(d.Reason, "settings.repositories") {
			t.Errorf("%s: reason %q", repo, d.Reason)
		}
	}
	// A push to a legato/ branch of any cpave3 repository is asked for once.
	d, _ := broker.CheckUse(ctx, CredentialUse{TaskID: "task-1", ExecutionID: "ex-1", Credential: "gh", Operation: "git.push", Resource: "github:cpave3/new-thing@refs/heads/legato/x"})
	approval, _ := approvals.Get(ctx, d.ApprovalID)
	if approval == nil || approval.Options[0].ID != "session" {
		t.Fatalf("approval = %+v", approval)
	}
}

func TestASessionsGHHasAnEmptyConfigurationOfItsOwn(t *testing.T) {
	f := newGitHubFixture(t)
	if _, ok := f.env["GH_CONFIG_DIR"]; ok {
		t.Fatal("GH_CONFIG_DIR set with isolation off")
	}
	if _, ok := f.env["GH_TOKEN"]; ok {
		t.Fatal("GH_TOKEN set with isolation off")
	}
	dir := filepath.Join(t.TempDir(), "gh-config")
	f.channels.opts.GHConfigDir = dir
	env, err := f.channels.SessionEnv(context.Background(), "task-1", "ex-1")
	if err != nil {
		t.Fatal(err)
	}
	if env["GH_CONFIG_DIR"] != dir {
		t.Fatalf("GH_CONFIG_DIR = %q", env["GH_CONFIG_DIR"])
	}
	// gh takes GH_TOKEN before the keyring, where the person's login is.
	if env["GH_TOKEN"] != GHSignedOut {
		t.Fatalf("GH_TOKEN = %q, want the stand-in", env["GH_TOKEN"])
	}
	info, err := os.Stat(dir)
	if err != nil || info.Mode().Perm() != 0o700 {
		t.Fatalf("the directory: %v %v", info, err)
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 0 {
		t.Fatalf("the directory is not empty: %v", entries)
	}
}

func TestTheWrapperSaysWhyGHHasNoToken(t *testing.T) {
	bin := t.TempDir()
	_ = os.WriteFile(filepath.Join(bin, "gh"), []byte("#!/bin/sh\necho \"token=$GH_TOKEN\"\n"), 0o755)
	legato := filepath.Join(t.TempDir(), "legato")
	_ = os.WriteFile(legato, []byte("#!/bin/sh\necho 'legato: no installation of the App for autopilot-dev/autopilot' >&2\nexit 6\n"), 0o755)
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	wrappers := filepath.Join(t.TempDir(), "bin")
	if err := writeGHWrapper(wrappers, legato); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("/bin/sh", "-c", "gh api /user")
	cmd.Env = append(os.Environ(), "PATH="+wrappers+string(os.PathListSeparator)+os.Getenv("PATH"), "GH_TOKEN="+GHSignedOut)
	var stderr strings.Builder
	cmd.Stderr = &stderr
	out, _ := cmd.Output()
	// Legato's own reason is passed on, not thrown away.
	if strings.TrimSpace(string(out)) != "token="+GHSignedOut || !strings.Contains(stderr.String(), "no installation of the App for autopilot-dev/autopilot") {
		t.Fatalf("out %q stderr %q", out, stderr.String())
	}
}

func TestTheWrapperRunsTheRealGHItIsGiven(t *testing.T) {
	bin := t.TempDir()
	_ = os.WriteFile(filepath.Join(bin, "gh"), []byte("#!/bin/sh\necho first-on-path\n"), 0o755)
	moved := filepath.Join(t.TempDir(), "gh-real")
	_ = os.WriteFile(moved, []byte("#!/bin/sh\necho \"moved $* token=$GH_TOKEN\"\n"), 0o755)
	legato := filepath.Join(t.TempDir(), "legato")
	_ = os.WriteFile(legato, []byte("#!/bin/sh\necho ghs_read\n"), 0o755)
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	wrappers := filepath.Join(t.TempDir(), "bin")
	if err := writeGHWrapper(wrappers, legato); err != nil {
		t.Fatal(err)
	}
	// In a sandbox the real gh's own path is the wrapper, so the real one is
	// reached through LEGATO_GH_REAL.
	cmd := exec.Command(filepath.Join(wrappers, "gh"), "pr", "list")
	cmd.Env = append(os.Environ(), "LEGATO_GH_REAL="+moved)
	out, err := cmd.CombinedOutput()
	if err != nil || strings.TrimSpace(string(out)) != "moved pr list token=ghs_read" {
		t.Fatalf("%v %q", err, out)
	}
}

func TestInTheSandboxGitHubFetchesOverSSHUseHTTPS(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("no git")
	}
	env := map[string]string{}
	githubEnv(env, "/bin/legato", "gh", "http://127.0.0.1:7788", "https://github.com")
	for k, v := range sandboxGitFetches(env) {
		env[k] = v
	}
	repo := t.TempDir()
	git := func(args ...string) string {
		cmd := exec.Command("git", args...)
		cmd.Dir = repo
		cmd.Env = []string{"HOME=" + t.TempDir(), "PATH=" + os.Getenv("PATH"), "GIT_CONFIG_NOSYSTEM=1"}
		for k, v := range env {
			cmd.Env = append(cmd.Env, k+"="+v)
		}
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	git("init", "-q")
	git("remote", "add", "origin", "placeholder")
	for _, remote := range []string{"git@github.com:cpave3/chimera.git", "ssh://git@github.com/cpave3/chimera.git"} {
		git("remote", "set-url", "origin", remote)
		if got := git("remote", "get-url", "origin"); got != "https://github.com/cpave3/chimera.git" {
			t.Errorf("%s: fetch url = %q", remote, got)
		}
		if got := git("remote", "get-url", "--push", "origin"); got != "http://127.0.0.1:7788/c/gh/git/cpave3/chimera.git" {
			t.Errorf("%s: push url = %q", remote, got)
		}
	}
	if extra := sandboxGitFetches(map[string]string{}); len(extra) != 0 {
		t.Fatalf("without a GitHub credential: %v", extra)
	}
}
