package service

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cpave3/legato/config"
	"github.com/cpave3/legato/internal/engine/store"
)

// reviewGitHub is GitHub for review posting: two Apps (installation 7),
// one repository, cpave3/legato, and one pull request, #12.
type reviewGitHub struct {
	mu       sync.Mutex
	srv      *httptest.Server
	tokens   map[string]map[string]any
	revoked  []string
	reviews  []map[string]any
	replies  []string
	comments []string
	resolved []string
	// fail answers the next review post with this status and body.
	fail   int
	failBy string
	// acceptThenStall records a review, then answers too late.
	acceptThenStall time.Duration
	resolveError    string
	posts           int
}

const reviewPatch = "@@ -1,3 +1,4 @@\n package main\n-import \"fmt\"\n+import \"os\"\n+\n func main() {\n@@ -20,2 +21,3 @@ func helper() {\n \treturn\n+\t// done\n }"

func newReviewGitHub(t *testing.T) *reviewGitHub {
	t.Helper()
	g := &reviewGitHub{tokens: map[string]map[string]any{}}
	g.srv = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		g.mu.Lock()
		bearer := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		perms := g.tokens[bearer]
		switch {
		case r.URL.Path == "/repos/cpave3/legato/installation":
			_, _ = io.WriteString(w, `{"id": 7}`)
		case r.URL.Path == "/app/installations/7/access_tokens":
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			token := fmt.Sprintf("ghs_%d", len(g.tokens)+len(g.revoked)+1)
			g.tokens[token], _ = body["permissions"].(map[string]any)
			w.WriteHeader(201)
			_ = json.NewEncoder(w).Encode(map[string]string{"token": token, "expires_at": time.Now().Add(time.Hour).UTC().Format(time.RFC3339)})
		case r.Method == "DELETE" && r.URL.Path == "/installation/token":
			g.revoked = append(g.revoked, bearer)
			delete(g.tokens, bearer)
			w.WriteHeader(204)
		case perms == nil:
			w.WriteHeader(401)
		case r.Method == "GET" && r.URL.Path == "/repos/cpave3/legato/pulls/12/files":
			_ = json.NewEncoder(w).Encode([]map[string]any{{"filename": "main.go", "patch": reviewPatch}, {"filename": "big.bin"}})
		case r.Method == "GET" && r.URL.Path == "/repos/cpave3/legato/pulls/12/reviews":
			out := []map[string]any{}
			for i, review := range g.reviews {
				out = append(out, map[string]any{"id": 100 + i, "body": review["body"], "commit_id": review["commit_id"], "state": review["event"], "html_url": fmt.Sprintf("https://github.com/r/%d", 100+i)})
			}
			_ = json.NewEncoder(w).Encode(out)
		case r.Method == "POST" && r.URL.Path == "/repos/cpave3/legato/pulls/12/reviews":
			g.posts++
			if perms["pull_requests"] != "write" {
				w.WriteHeader(403)
				_, _ = io.WriteString(w, `{"message":"Resource not accessible by integration"}`)
				break
			}
			if g.fail != 0 {
				w.Header().Set("x-ratelimit-remaining", "0")
				w.Header().Set("x-ratelimit-reset", fmt.Sprint(time.Now().Add(time.Hour).Unix()))
				w.WriteHeader(g.fail)
				_, _ = io.WriteString(w, g.failBy)
				break
			}
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			g.reviews = append(g.reviews, body)
			if g.acceptThenStall > 0 {
				stall := g.acceptThenStall
				g.mu.Unlock()
				time.Sleep(stall)
				g.mu.Lock()
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"id": 100 + len(g.reviews) - 1, "html_url": "https://github.com/r/new"})
		case r.Method == "POST" && r.URL.Path == "/repos/cpave3/legato/pulls/12/comments/555/replies":
			var body map[string]string
			_ = json.NewDecoder(r.Body).Decode(&body)
			g.replies = append(g.replies, body["body"])
			w.WriteHeader(201)
			_, _ = io.WriteString(w, `{"id": 556}`)
		case r.Method == "POST" && r.URL.Path == "/repos/cpave3/legato/issues/12/comments":
			var body map[string]string
			_ = json.NewDecoder(r.Body).Decode(&body)
			g.comments = append(g.comments, fmt.Sprintf("%s|%v", body["body"], perms["contents"]))
			w.WriteHeader(201)
			_, _ = io.WriteString(w, `{"id": 777, "html_url": "https://github.com/c/777"}`)
		case r.Method == "POST" && r.URL.Path == "/graphql":
			var body struct {
				Query     string         `json:"query"`
				Variables map[string]any `json:"variables"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			switch {
			case strings.Contains(body.Query, "resolveReviewThread"):
				if g.resolveError != "" {
					_, _ = io.WriteString(w, `{"errors":[{"message":"`+g.resolveError+`"}]}`)
					break
				}
				g.resolved = append(g.resolved, fmt.Sprint(body.Variables["id"]))
				_, _ = io.WriteString(w, `{"data":{"resolveReviewThread":{"thread":{"isResolved":true}}}}`)
			default:
				_, _ = io.WriteString(w, `{"data":{"repository":{"pullRequest":{"reviewThreads":{"pageInfo":{"hasNextPage":false},"nodes":[
					{"id":"T_other","comments":{"nodes":[{"fullDatabaseId":"400"}]}},
					{"id":"T_555","comments":{"nodes":[{"fullDatabaseId":"555"}]}}]}}}}}`)
			}
		default:
			w.WriteHeader(404)
		}
		g.mu.Unlock()
	}))
	t.Cleanup(g.srv.Close)
	return g
}

// reviewProfiles writes the shipped GitHub profiles with their addresses
// moved to the fake.
func reviewProfiles(t *testing.T, url string) string {
	t.Helper()
	dir := t.TempDir()
	for _, name := range []string{"github.yaml", "github-reviewer.yaml"} {
		data, err := shippedProfiles.ReadFile("credential_profiles/" + name)
		if err != nil {
			t.Fatal(err)
		}
		text := strings.ReplaceAll(strings.ReplaceAll(string(data), "https://api.github.com", url), "https://github.com", url)
		if err := os.WriteFile(filepath.Join(dir, name), []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

type publisherFixture struct {
	brokerFixture
	publisher *ReviewPublisher
	github    *reviewGitHub
}

func newPublisherFixture(t *testing.T) publisherFixture {
	t.Helper()
	g := newReviewGitHub(t)
	key, _ := rsa.GenerateKey(rand.Reader, 2048)
	t.Setenv("GH_APP_KEY", string(pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})))
	approvals, db, _ := newApprovalFixture(t)
	createTask(t, db, "task-1")
	if err := db.CreateExecution(context.Background(), store.Execution{ID: "ex-1", Kind: store.ExecutionInteractive, Status: store.ExecutionRunning, TaskID: "task-1", ExecutionTaskID: "task-1", StartedAt: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}
	settings := config.CredentialSettings{ClientID: "Iv1.test", Repositories: []string{"cpave3/legato"}}
	broker, err := NewCredentialBroker(db, approvals, CredentialBrokerOptions{
		Credentials: []config.CredentialConfig{
			{Name: "gh", Secret: "env://GH_APP_KEY", Profile: "github", Type: "keypair", Settings: settings},
			{Name: "reviewer-app", Secret: "env://GH_APP_KEY", Profile: "github-reviewer", Type: "keypair", Settings: settings},
		},
		ProfileDirs: []string{reviewProfiles(t, g.srv.URL)},
		Messenger:   &recordingMessenger{}, WaitTimeout: 100 * time.Millisecond, HTTPClient: g.srv.Client(),
	})
	if err != nil {
		t.Fatal(err)
	}
	channels := NewCredentialChannels(broker, CredentialChannelsOptions{ProxyURL: func() string { return "" }, LegatoBin: "/opt/legato"})
	publisher := NewReviewPublisher(channels, "reviewer-app")
	publisher.Timeout = time.Second
	return publisherFixture{brokerFixture: brokerFixture{broker: broker, approvals: approvals, store: db}, publisher: publisher, github: g}
}

func cleanReview() ReviewPost {
	return ReviewPost{
		Repo: "cpave3/legato", Number: 12, CommitID: "abc123", Event: ReviewApprove, Marker: "pass-1/pr-12",
		Summary: "Looks right.", Footer: "Reviewed by codex (gpt-5.5) with 3-tier-review.",
		Findings: []ReviewPostFinding{{Title: "Name the import", Body: "Prefer a named import.", Place: &ReviewPlace{Path: "main.go", Line: 2, Side: "RIGHT"}}},
	}
}

func TestACleanPassApprovesWithItsNitsInline(t *testing.T) {
	f := newPublisherFixture(t)

	posted, err := f.publisher.PostReview(context.Background(), cleanReview())
	if err != nil {
		t.Fatal(err)
	}

	if len(f.github.reviews) != 1 || posted.ID != 100 {
		t.Fatalf("reviews = %+v, posted %+v", f.github.reviews, posted)
	}
	review := f.github.reviews[0]
	body, _ := review["body"].(string)
	if review["event"] != "APPROVE" || review["commit_id"] != "abc123" || !strings.Contains(body, "Looks right.") ||
		!strings.Contains(body, "3-tier-review") || !strings.Contains(body, "<!-- legato-review-pass pass-1/pr-12 -->") {
		t.Fatalf("review = %+v", review)
	}
	comments, _ := review["comments"].([]any)
	if len(comments) != 1 {
		t.Fatalf("inline comments = %v", comments)
	}
	inline := comments[0].(map[string]any)
	if inline["path"] != "main.go" || inline["line"] != float64(2) || inline["side"] != "RIGHT" || !strings.Contains(inline["body"].(string), "Name the import") {
		t.Fatalf("inline comment = %v", inline)
	}
	// The token that posted could write code (an App's approval counts
	// with it), and it is gone; only a read token is kept.
	if len(f.github.revoked) != 1 {
		t.Fatalf("revoked %v", f.github.revoked)
	}
	for token, perms := range f.github.tokens {
		if perms["contents"] == "write" || perms["pull_requests"] == "write" {
			t.Fatalf("a write token is still live: %s %v", token, perms)
		}
	}
}

func TestFindingsOffTheDiffGoInTheReviewBody(t *testing.T) {
	f := newPublisherFixture(t)
	post := ReviewPost{
		Repo: "cpave3/legato", Number: 12, CommitID: "abc123", Event: ReviewComment, Marker: "pass-2/pr-12", Summary: "Two problems.",
		Findings: []ReviewPostFinding{
			{Title: "Range in the hunk", Body: "b1", Place: &ReviewPlace{Path: "main.go", StartLine: 2, Line: 3, Side: "RIGHT"}},
			{Title: "Outside the diff", Body: "b2", Place: &ReviewPlace{Path: "main.go", Line: 10, Side: "RIGHT"}},
			{Title: "Across two hunks", Body: "b3", Place: &ReviewPlace{Path: "main.go", StartLine: 3, Line: 22, Side: "RIGHT"}},
			{Title: "No place", Body: "b4"},
			{Title: "Deleted line", Body: "b5", Place: &ReviewPlace{Path: "main.go", Line: 2, Side: "LEFT"}},
		},
	}

	posted, err := f.publisher.PostReview(context.Background(), post)
	if err != nil {
		t.Fatal(err)
	}

	review := f.github.reviews[0]
	if review["event"] != "COMMENT" {
		t.Fatalf("event = %v", review["event"])
	}
	comments, _ := review["comments"].([]any)
	if len(comments) != 2 || len(posted.Inline) != 2 || posted.Inline[0] != 0 || posted.Inline[1] != 4 {
		t.Fatalf("inline = %v (findings %v)", comments, posted.Inline)
	}
	body := review["body"].(string)
	for _, want := range []string{"`main.go:10` Outside the diff", "`main.go:3-22` Across two hunks", "No place", "b4"} {
		if !strings.Contains(body, want) {
			t.Errorf("body lacks %q:\n%s", want, body)
		}
	}
	if strings.Contains(body, "Range in the hunk") {
		t.Errorf("an anchored finding is also in the body:\n%s", body)
	}
}

func TestAReviewAcceptedBeforeATimeoutIsNotPostedTwice(t *testing.T) {
	f := newPublisherFixture(t)
	f.github.acceptThenStall = 300 * time.Millisecond
	f.publisher.Timeout = 100 * time.Millisecond

	posted, err := f.publisher.PostReview(context.Background(), cleanReview())
	if err != nil {
		t.Fatalf("a review GitHub accepted: %v", err)
	}
	if len(f.github.reviews) != 1 || posted.ID != 100 || !posted.Existed {
		t.Fatalf("reviews = %d, posted %+v", len(f.github.reviews), posted)
	}

	// Asked again (a retry after Legato restarted): found, not posted.
	f.github.acceptThenStall = 0
	again, err := f.publisher.PostReview(context.Background(), cleanReview())
	if err != nil || !again.Existed || len(f.github.reviews) != 1 {
		t.Fatalf("again = %+v, %v; reviews %d", again, err, len(f.github.reviews))
	}
}

func TestGitHubRefusalsAreReportedNotTakenForSuccess(t *testing.T) {
	for _, tc := range []struct {
		name        string
		status      int
		body        string
		rateLimited bool
		want        string
	}{
		{"validation", 422, `{"message":"Validation Failed","errors":["Pull request review thread line must be part of the diff"]}`, false, "line must be part of the diff"},
		{"rate limit", 403, `{"message":"API rate limit exceeded"}`, true, "rate limit"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newPublisherFixture(t)
			f.github.fail, f.github.failBy = tc.status, tc.body

			_, err := f.publisher.PostReview(context.Background(), cleanReview())

			var gh *GitHubError
			if !errors.As(err, &gh) || gh.Status != tc.status || gh.RateLimited != tc.rateLimited || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %#v", err)
			}
			if tc.rateLimited && gh.RetryAt.IsZero() {
				t.Fatal("a rate limit says when to try again")
			}
			if f.github.posts != 1 {
				t.Fatalf("posts = %d; a refusal is not retried", f.github.posts)
			}
		})
	}
}

func TestARulingIsAThreadReplyAndAResolvedOneResolvesTheThread(t *testing.T) {
	f := newPublisherFixture(t)
	ctx := context.Background()
	if err := f.publisher.Reply(ctx, AsReviewer, "cpave3/legato", 12, 555, "Resolved in pass 2: the import is named."); err != nil {
		t.Fatal(err)
	}
	if err := f.publisher.ResolveThread(ctx, "cpave3/legato", 12, 555); err != nil {
		t.Fatal(err)
	}
	if len(f.github.replies) != 1 || len(f.github.resolved) != 1 || f.github.resolved[0] != "T_555" {
		t.Fatalf("replies %v, resolved %v", f.github.replies, f.github.resolved)
	}

	// Resolving can fail (the App's permissions): the reply stands, and the
	// failure is reported.
	f.github.resolveError = "Resource not accessible by integration"
	if err := f.publisher.ResolveThread(ctx, "cpave3/legato", 12, 555); err == nil || !strings.Contains(err.Error(), "not accessible") {
		t.Fatalf("err = %v", err)
	}
}

func TestTheAuthorsAnswersArePostedAsTheAuthorApp(t *testing.T) {
	f := newPublisherFixture(t)
	if _, err := f.publisher.PostIssueComment(context.Background(), AsAuthor, "cpave3/legato", 12, "Fixed in abc124."); err != nil {
		t.Fatal(err)
	}
	// The author App's token writes pull requests and reads code only.
	if len(f.github.comments) != 1 || f.github.comments[0] != "Fixed in abc124.|read" {
		t.Fatalf("comments = %v", f.github.comments)
	}
}

func TestNoAgentCanUseTheReviewerCredential(t *testing.T) {
	f := newPublisherFixture(t)
	ctx := context.Background()
	result, err := f.broker.Request(ctx, AccessRequest{TaskID: "task-1", ExecutionID: "ex-1", Credential: "reviewer-app", Operations: []string{"pr.review"}, Resources: []string{"github:cpave3/legato"}})
	if err != nil || result.Status == "granted" || !strings.Contains(result.Reason, "Legato's own") {
		t.Fatalf("request = %+v, %v", result, err)
	}
	decision, err := f.broker.Authorize(ctx, CredentialUse{TaskID: "task-1", ExecutionID: "ex-1", Credential: "reviewer-app", Operation: "api.read", Resource: "github:cpave3/legato"})
	if err != nil || decision.Allowed {
		t.Fatalf("use = %+v, %v", decision, err)
	}
	// And a make-a-change handoff's grants never name it.
	granted, err := f.broker.GrantBranches(ctx, BranchGrant{TaskID: "task-1", ExecutionID: "ex-1", Repo: "cpave3/legato", Branches: []string{"legato/x"}})
	if err != nil {
		t.Fatal(err)
	}
	grants, _ := f.broker.Grants(ctx)
	for _, g := range grants {
		if g.Credential == "reviewer-app" {
			t.Fatalf("a grant names the reviewer credential: %+v (granted %d)", g, granted)
		}
	}
}
