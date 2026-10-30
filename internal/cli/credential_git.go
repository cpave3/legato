package cli

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// GitHubSession is what an agent session's environment says about its
// GitHub credential: the proxy's API address and the placeholder key.
type GitHubSession struct {
	APIURL      string // LEGATO_GITHUB_API_URL: http://127.0.0.1:<port>/c/<credential>/api
	Placeholder string // LEGATO_GITHUB_TOKEN
	Client      *http.Client
}

// GitHubSessionFromEnv reads the session's variables. ok is false outside a
// session with a GitHub credential.
func GitHubSessionFromEnv(getenv func(string) string) (GitHubSession, bool) {
	s := GitHubSession{APIURL: getenv("LEGATO_GITHUB_API_URL"), Placeholder: getenv("LEGATO_GITHUB_TOKEN")}
	return s, s.APIURL != "" && s.Placeholder != ""
}

func (s GitHubSession) base() string { return strings.TrimSuffix(s.APIURL, "/api") }

// Token asks Legato's proxy for a token for one repository and operation:
// a read token, or one that writes pull requests (pr.write).
func (s GitHubSession) Token(ctx context.Context, repo, operation string) (string, error) {
	body, _ := json.Marshal(map[string]string{"repo": repo, "operation": operation})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.base()+"/token", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+s.Placeholder)
	req.Header.Set("Content-Type", "application/json")
	client := s.Client
	if client == nil {
		client = &http.Client{Timeout: 90 * time.Second}
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("Legato's proxy did not answer: %w", err)
	}
	defer resp.Body.Close()
	var out map[string]string
	_ = json.NewDecoder(io.LimitReader(resp.Body, 1<<16)).Decode(&out)
	if resp.StatusCode != http.StatusOK || out["token"] == "" {
		reason := out["error"]
		if out["status"] == "pending" {
			reason += " (pending " + out["approval"] + ")"
		}
		return "", fmt.Errorf("Legato did not give a token for %s: %s", repo, firstNonEmptyString(reason, resp.Status))
	}
	return out["token"], nil
}

func firstNonEmptyString(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

var repoPath = regexp.MustCompile(`^([A-Za-z0-9_.-]+)/([A-Za-z0-9_.-]+?)(\.git)?/?$`)

// GitCredentialHelper answers git's credential helper protocol (action get,
// store or erase) for an agent session: for the proxy's own address, the
// placeholder (pushes); for the GitHub host, a read token for the one
// repository git names (fetches). Store and erase are ignored: Legato
// keeps nothing git hands back. Anything else gets no answer.
func GitCredentialHelper(ctx context.Context, action string, in io.Reader, out, errOut io.Writer, session GitHubSession) error {
	fields := map[string]string{}
	scanner := bufio.NewScanner(in)
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			break
		}
		if key, value, ok := strings.Cut(line, "="); ok {
			fields[key] = value
		}
	}
	if action != "get" {
		return nil
	}
	proxy, err := url.Parse(session.APIURL)
	if err != nil {
		return nil
	}
	if fields["host"] == proxy.Host {
		_, err := fmt.Fprintf(out, "username=x-access-token\npassword=%s\n", session.Placeholder)
		return err
	}
	m := repoPath.FindStringSubmatch(fields["path"])
	if m == nil {
		fmt.Fprintln(errOut, "legato: git did not name a repository; set credential.useHttpPath")
		return nil
	}
	token, err := session.Token(ctx, m[1]+"/"+m[2], "git.read")
	if err != nil {
		fmt.Fprintln(errOut, "legato: "+err.Error())
		return nil
	}
	_, err = fmt.Fprintf(out, "username=x-access-token\npassword=%s\n", token)
	return err
}

// prWrites are the gh pr commands that write to a pull request. Merging is
// not one: it writes code, which a pr.write token cannot. Nor is ready (see
// GHOperation).
var prWrites = map[string]bool{"create": true, "edit": true, "comment": true, "review": true, "close": true, "reopen": true}

// GHOperation is the operation a gh command needs: pr.write for a command
// that writes to a pull request; pr.ready for gh pr ready, which GitHub
// allows only a token that can also write code, so Legato does it itself;
// api.read for everything else.
func GHOperation(args []string) string {
	var words []string
	for i := 0; i < len(args) && len(words) < 2; i++ {
		a := args[i]
		if a == "-R" || a == "--repo" {
			i++
			continue
		}
		if strings.HasPrefix(a, "-") {
			continue
		}
		words = append(words, a)
	}
	if len(words) == 2 && words[0] == "pr" && words[1] == "ready" {
		return "pr.ready"
	}
	if len(words) == 2 && words[0] == "pr" && prWrites[words[1]] {
		return "pr.write"
	}
	return "api.read"
}

var pullNumber = regexp.MustCompile(`^#?(\d+)$|/pull/(\d+)/?$`)

// GHReadyTarget is the pull request gh pr ready names (0 for the current
// branch's) and whether --undo turns it back into a draft.
func GHReadyTarget(args []string) (number int, undo bool) {
	seenReady := false
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "-R" || a == "--repo":
			i++
		case a == "--undo":
			undo = true
		case a == "ready":
			seenReady = true
		case seenReady && number == 0 && !strings.HasPrefix(a, "-"):
			if m := pullNumber.FindStringSubmatch(a); m != nil {
				number, _ = strconv.Atoi(m[1] + m[2])
			}
		}
	}
	return number, undo
}

// SetPullRequestReady asks Legato to mark a pull request ready for review
// (or a draft, when undo): the one numbered, or the open one whose head is
// branch. It returns the pull request's number.
func (s GitHubSession) SetPullRequestReady(ctx context.Context, repo string, number int, branch string, undo bool) (int, error) {
	body, _ := json.Marshal(map[string]any{"repo": repo, "number": number, "branch": branch, "undo": undo})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.base()+"/pr-ready", bytes.NewReader(body))
	if err != nil {
		return 0, err
	}
	req.Header.Set("Authorization", "Bearer "+s.Placeholder)
	req.Header.Set("Content-Type", "application/json")
	client := s.Client
	if client == nil {
		client = &http.Client{Timeout: 90 * time.Second}
	}
	resp, err := client.Do(req)
	if err != nil {
		return 0, fmt.Errorf("Legato's proxy did not answer: %w", err)
	}
	defer resp.Body.Close()
	var out map[string]string
	_ = json.NewDecoder(io.LimitReader(resp.Body, 1<<16)).Decode(&out)
	if resp.StatusCode != http.StatusOK {
		reason := out["error"]
		if out["status"] == "pending" {
			reason += " (pending " + out["approval"] + ")"
		}
		return 0, fmt.Errorf("Legato did not change the pull request: %s", firstNonEmptyString(reason, resp.Status))
	}
	n, _ := strconv.Atoi(out["number"])
	return n, nil
}

var githubRemote = regexp.MustCompile(`github\.com[:/]([A-Za-z0-9_.-]+)/([A-Za-z0-9_.-]+?)(\.git)?/?$`)

// GHRepo finds the repository a gh command acts on: -R/--repo, then
// GH_REPO, then the origin remote of the current directory.
func GHRepo(args []string, getenv func(string) string, origin func() string) (string, error) {
	for i, a := range args {
		var value string
		switch {
		case a == "-R" || a == "--repo":
			if i+1 < len(args) {
				value = args[i+1]
			}
		case strings.HasPrefix(a, "--repo="):
			value = strings.TrimPrefix(a, "--repo=")
		case strings.HasPrefix(a, "-R") && len(a) > 2:
			value = a[2:]
		}
		if value != "" {
			return normalizeGHRepo(value)
		}
	}
	if v := getenv("GH_REPO"); v != "" {
		return normalizeGHRepo(v)
	}
	if o := origin(); o != "" {
		if m := githubRemote.FindStringSubmatch(strings.TrimSpace(o)); m != nil {
			return m[1] + "/" + m[2], nil
		}
	}
	return "", errors.New("no repository: give -R owner/name, set GH_REPO, or run inside a clone of a GitHub repository")
}

func normalizeGHRepo(value string) (string, error) {
	value = strings.TrimPrefix(strings.TrimPrefix(value, "https://"), "github.com/")
	if m := repoPath.FindStringSubmatch(value); m != nil {
		return m[1] + "/" + m[2], nil
	}
	return "", fmt.Errorf("%q is not owner/name on github.com", value)
}
