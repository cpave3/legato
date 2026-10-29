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

// ReadToken asks Legato's proxy for a read token for one repository.
func (s GitHubSession) ReadToken(ctx context.Context, repo, operation string) (string, error) {
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
	token, err := session.ReadToken(ctx, m[1]+"/"+m[2], "git.read")
	if err != nil {
		fmt.Fprintln(errOut, "legato: "+err.Error())
		return nil
	}
	_, err = fmt.Fprintf(out, "username=x-access-token\npassword=%s\n", token)
	return err
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
