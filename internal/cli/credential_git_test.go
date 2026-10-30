package cli_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/cpave3/legato/internal/cli"
)

// tokenProxy stands in for Legato's proxy: a read token for cpave3/legato
// to the right placeholder, a refusal for anything else.
func tokenProxy(t *testing.T) (*httptest.Server, *[]map[string]string) {
	t.Helper()
	asked := &[]map[string]string{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var in map[string]string
		_ = json.NewDecoder(r.Body).Decode(&in)
		*asked = append(*asked, in)
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path != "/c/gh/token" || r.Header.Get("Authorization") != "Bearer lgk_place" {
			w.WriteHeader(401)
			_, _ = w.Write([]byte(`{"error":"no key"}`))
			return
		}
		if in["repo"] != "cpave3/legato" {
			w.WriteHeader(403)
			_, _ = w.Write([]byte(`{"error":"someone/else is not one of the repositories of gh (settings.repositories in config.yaml)","status":"denied"}`))
			return
		}
		_, _ = w.Write([]byte(`{"token":"ghs_read"}`))
	}))
	t.Cleanup(srv.Close)
	return srv, asked
}

func helper(t *testing.T, session cli.GitHubSession, action, input string) (string, string) {
	t.Helper()
	var out, errOut bytes.Buffer
	if err := cli.GitCredentialHelper(context.Background(), action, strings.NewReader(input), &out, &errOut, session); err != nil {
		t.Fatal(err)
	}
	return out.String(), errOut.String()
}

func TestTheGitHelperGivesAReadTokenForTheRepositoryGitNames(t *testing.T) {
	srv, asked := tokenProxy(t)
	session := cli.GitHubSession{APIURL: srv.URL + "/c/gh/api", Placeholder: "lgk_place"}
	out, _ := helper(t, session, "get", "protocol=https\nhost=github.com\npath=cpave3/legato.git\n\n")
	if out != "username=x-access-token\npassword=ghs_read\n" {
		t.Fatalf("out = %q", out)
	}
	if (*asked)[0]["operation"] != "git.read" || (*asked)[0]["repo"] != "cpave3/legato" {
		t.Fatalf("asked = %v", *asked)
	}
	out, errOut := helper(t, session, "get", "protocol=https\nhost=github.com\npath=someone/else\n\n")
	if out != "" || !strings.Contains(errOut, "settings.repositories") {
		t.Fatalf("an unlisted repository: out %q err %q", out, errOut)
	}
}

func TestTheGitHelperGivesTheProxyThePlaceholder(t *testing.T) {
	srv, asked := tokenProxy(t)
	host := strings.TrimPrefix(srv.URL, "http://")
	session := cli.GitHubSession{APIURL: srv.URL + "/c/gh/api", Placeholder: "lgk_place"}
	out, _ := helper(t, session, "get", "protocol=http\nhost="+host+"\npath=c/gh/git/cpave3/legato.git\n\n")
	if out != "username=x-access-token\npassword=lgk_place\n" || len(*asked) != 0 {
		t.Fatalf("out = %q asked %v", out, *asked)
	}
	if out, _ := helper(t, session, "store", "protocol=https\nhost=github.com\npassword=x\n\n"); out != "" {
		t.Fatalf("store answered %q", out)
	}
	if out, _ := helper(t, session, "get", "protocol=https\nhost=github.com\n\n"); out != "" {
		t.Fatalf("no path answered %q", out)
	}
}

func TestGHFindsItsRepository(t *testing.T) {
	none := func(string) string { return "" }
	noOrigin := func() string { return "" }
	for _, tc := range []struct {
		args   []string
		env    func(string) string
		origin func() string
		want   string
	}{
		{[]string{"pr", "list", "-R", "cpave3/legato"}, none, noOrigin, "cpave3/legato"},
		{[]string{"pr", "view", "--repo=github.com/cpave3/legato"}, none, noOrigin, "cpave3/legato"},
		{[]string{"issue", "list", "-Rcpave3/legato"}, none, noOrigin, "cpave3/legato"},
		{[]string{"pr", "list"}, func(k string) string {
			if k == "GH_REPO" {
				return "cpave3/chimera"
			}
			return ""
		}, noOrigin, "cpave3/chimera"},
		{[]string{"pr", "list"}, none, func() string { return "git@github.com:cpave3/legato.git" }, "cpave3/legato"},
		{[]string{"pr", "list"}, none, func() string { return "https://github.com/cpave3/legato" }, "cpave3/legato"},
	} {
		got, err := cli.GHRepo(tc.args, tc.env, tc.origin)
		if err != nil || got != tc.want {
			t.Errorf("%v: %q, %v; want %q", tc.args, got, err, tc.want)
		}
	}
	if _, err := cli.GHRepo([]string{"pr", "list"}, none, noOrigin); err == nil {
		t.Error("a repository was found with nothing to find it from")
	}
}

func TestGHOperationSaysWhichCommandsWriteToAPullRequest(t *testing.T) {
	for args, want := range map[string]string{
		"pr edit 1529 -R autopilot-dev/autopilot --body-file b.md": "pr.write",
		"pr create --fill":           "pr.write",
		"pr comment 12 --body hi":    "pr.write",
		"pr review 12 --approve":     "pr.write",
		"pr close 12":                "pr.write",
		"pr reopen 12":               "pr.write",
		"pr ready 12":                "pr.ready",
		"pr ready 12 --undo":         "pr.ready",
		"-R cpave3/legato pr edit 3": "pr.write",
		"pr view 1529 --json body":   "api.read",
		"pr list":                    "api.read",
		"pr checks 12":               "api.read",
		"pr merge 12":                "api.read",
		"repo view":                  "api.read",
		"api /repos/a/b":             "api.read",
		"":                           "api.read",
	} {
		if got := cli.GHOperation(strings.Fields(args)); got != want {
			t.Errorf("gh %s: %s, want %s", args, got, want)
		}
	}
}

func TestGHReadyTargetFindsThePullRequestAndWhetherToUndo(t *testing.T) {
	for args, want := range map[string]struct {
		number int
		undo   bool
	}{
		"pr ready 1529 -R autopilot-dev/autopilot":                      {1529, false},
		"pr ready #12 --undo":                                           {12, true},
		"pr ready https://github.com/autopilot-dev/autopilot/pull/1531": {1531, false},
		"-R a/b pr ready":                                               {0, false},
		"pr ready --undo":                                               {0, true},
	} {
		number, undo := cli.GHReadyTarget(strings.Fields(args))
		if number != want.number || undo != want.undo {
			t.Errorf("gh %s: %d %v, want %d %v", args, number, undo, want.number, want.undo)
		}
	}
}
