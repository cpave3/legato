package service

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
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
	// lists are the comment lists' pages, by path; listHits counts the
	// requests and notModified the 304s; limitLists answers lists 403.
	lists       map[string][]string
	listHits    int
	notModified int
	limitLists  bool
	threadReads int
	// checkRuns are each head's check runs; noAdministration is an App
	// without the permission to read branch protection.
	checkRuns        map[string]string
	noAdministration bool
	// pulls are the open pull requests the list answers with; methods
	// the methods of every request.
	pulls   []map[string]any
	methods []string
	// headReads are the permissions of each token that read #12 itself.
	headReads []map[string]any
	// bodies are the bodies of every POST, in order.
	bodies []string
	// replyStall records a reply or a conversation comment, then answers
	// too late; replyFail records one, then answers with this status.
	replyStall time.Duration
	replyFail  int
	// replyRefuse answers a reply or a comment with this status without
	// recording it.
	replyRefuse int
	// replyPosts counts the POSTs of replies and conversation comments.
	replyPosts int
	// apps are the logins of the Apps each token was made for (the
	// author App is legato-author, the reviewer App legato-reviews), and
	// replyBy, replyTo, commentBy and reviewBy who posted each reply (in
	// which thread), comment and review.
	apps      map[string]string
	replyBy   []string
	replyTo   []int64
	commentBy []string
	reviewBy  []string
	// others are comments and reviews already on the pull request that
	// Legato did not post: review comments, conversation comments, reviews.
	otherReplies  []map[string]any
	otherComments []map[string]any
	otherReviews  []map[string]any
	// limit answers the API requests (not the token endpoints) with a
	// rate limit while set; apiHits counts the API requests that reached
	// GitHub, by repository (owner/name, or graphql).
	limit   *fakeLimit
	apiHits map[string]int
	// graphqlLimit, while set, answers GraphQL with a primary rate limit
	// in a 200, as GitHub does, that resets then.
	graphqlLimit time.Time
}

// fakeAppSlugs are the Apps of the fixture, by the client ID their JWTs
// are issued by.
var fakeAppSlugs = map[string]string{"Iv1.author": "legato-author", "Iv1.reviewer": "legato-reviews"}

// jwtApp is the App whose JWT r carries: its slug.
func jwtApp(r *http.Request) string {
	parts := strings.Split(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "), ".")
	if len(parts) != 3 {
		return ""
	}
	data, _ := base64.RawURLEncoding.DecodeString(parts[1])
	var claims struct {
		Iss string `json:"iss"`
	}
	_ = json.Unmarshal(data, &claims)
	return fakeAppSlugs[claims.Iss]
}

// answerAfterAccept answers a reply or a comment GitHub has recorded:
// late, with an error, or as created.
func (g *reviewGitHub) answerAfterAccept(w http.ResponseWriter, created string) {
	if g.replyStall > 0 {
		stall := g.replyStall
		g.mu.Unlock()
		time.Sleep(stall)
		g.mu.Lock()
	}
	if g.replyFail != 0 {
		w.WriteHeader(g.replyFail)
		_, _ = io.WriteString(w, `{"message":"Server Error"}`)
		return
	}
	w.WriteHeader(201)
	_, _ = io.WriteString(w, created)
}

// fakeLimit is a rate-limit answer: status, headers and message, for one
// owner's repositories (any when empty).
type fakeLimit struct {
	owner   string
	status  int
	headers map[string]string
	message string
}

const reviewPatch = "@@ -1,3 +1,4 @@\n package main\n-import \"fmt\"\n+import \"os\"\n+\n func main() {\n@@ -20,2 +21,3 @@ func helper() {\n \treturn\n+\t// done\n }"

func newReviewGitHub(t *testing.T) *reviewGitHub {
	t.Helper()
	g := &reviewGitHub{tokens: map[string]map[string]any{}, apps: map[string]string{}}
	g.srv = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		g.mu.Lock()
		bearer := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		perms := g.tokens[bearer]
		g.methods = append(g.methods, r.Method)
		if r.Method == "POST" && r.Body != nil {
			data, _ := io.ReadAll(r.Body)
			g.bodies = append(g.bodies, string(data))
			r.Body = io.NopCloser(strings.NewReader(string(data)))
		}
		switch {
		case r.URL.Path == "/repos/cpave3/legato/installation":
			_ = json.NewEncoder(w).Encode(map[string]any{"id": 7, "app_slug": jwtApp(r)})
		case r.URL.Path == "/repos/acme/widgets/installation":
			_ = json.NewEncoder(w).Encode(map[string]any{"id": 8, "app_slug": jwtApp(r)})
		case strings.HasPrefix(r.URL.Path, "/app/installations/") && strings.HasSuffix(r.URL.Path, "/access_tokens"):
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			if asked, _ := body["permissions"].(map[string]any); g.noAdministration && asked["administration"] != nil {
				w.WriteHeader(422)
				_, _ = io.WriteString(w, `{"message":"The permissions requested are not granted to this installation."}`)
				break
			}
			token := fmt.Sprintf("ghs_%d", len(g.tokens)+len(g.revoked)+1)
			g.tokens[token], _ = body["permissions"].(map[string]any)
			g.apps[token] = jwtApp(r) + "[bot]"
			w.WriteHeader(201)
			_ = json.NewEncoder(w).Encode(map[string]string{"token": token, "expires_at": time.Now().Add(time.Hour).UTC().Format(time.RFC3339)})
		case r.Method == "DELETE" && r.URL.Path == "/installation/token":
			g.revoked = append(g.revoked, bearer)
			delete(g.tokens, bearer)
			w.WriteHeader(204)
		case perms == nil:
			w.WriteHeader(401)
		case g.hit(r) && g.limit != nil && (g.limit.owner == "" || strings.HasPrefix(r.URL.Path, "/repos/"+g.limit.owner+"/")):
			for k, v := range g.limit.headers {
				w.Header().Set(k, v)
			}
			w.WriteHeader(g.limit.status)
			_, _ = io.WriteString(w, `{"message":"`+g.limit.message+`"}`)
		case r.URL.Path == "/graphql" && !g.graphqlLimit.IsZero():
			w.Header().Set("x-ratelimit-remaining", "0")
			w.Header().Set("x-ratelimit-reset", fmt.Sprint(g.graphqlLimit.Unix()))
			_, _ = io.WriteString(w, `{"errors":[{"type":"RATE_LIMITED","message":"API rate limit exceeded for installation ID 7."}]}`)
		case r.Method == "GET" && r.URL.Path == "/repos/cpave3/legato/pulls":
			out := []map[string]any{}
			head, base := r.URL.Query().Get("head"), r.URL.Query().Get("base")
			for _, pr := range g.pulls {
				h := pr["head"].(map[string]any)
				b := pr["base"].(map[string]any)
				owner := strings.Split(h["repo"].(map[string]any)["full_name"].(string), "/")[0]
				if (head != "" && owner+":"+h["ref"].(string) == head) || (base != "" && b["ref"] == base) {
					out = append(out, pr)
				}
			}
			_ = json.NewEncoder(w).Encode(out)
		case r.Method == "GET" && r.URL.Path == "/repos/cpave3/legato/pulls/12":
			g.headReads = append(g.headReads, perms)
			pull := githubPull(12, "feature", "main", "legato-author[bot]")
			pull["state"] = "open"
			_ = json.NewEncoder(w).Encode(pull)
		case r.Method == "GET" && r.URL.Path == "/repos/cpave3/legato/pulls/12/files":
			_ = json.NewEncoder(w).Encode([]map[string]any{{"filename": "main.go", "patch": reviewPatch}, {"filename": "big.bin"}})
		case r.Method == "GET" && g.lists != nil && g.lists[r.URL.Path] != nil:
			g.listHits++
			if g.limitLists {
				w.Header().Set("x-ratelimit-remaining", "0")
				w.Header().Set("x-ratelimit-reset", fmt.Sprint(time.Now().Add(time.Hour).Unix()))
				w.WriteHeader(403)
				_, _ = io.WriteString(w, `{"message":"API rate limit exceeded"}`)
				break
			}
			pages := g.lists[r.URL.Path]
			page := 1
			fmt.Sscan(r.URL.Query().Get("page"), &page)
			body := "[]"
			if page <= len(pages) {
				body = pages[page-1]
			}
			etag := fmt.Sprintf(`"%x"`, len(body)*31+page)
			if r.Header.Get("If-None-Match") == etag {
				g.notModified++
				w.WriteHeader(304)
				break
			}
			w.Header().Set("ETag", etag)
			_, _ = io.WriteString(w, body)
		case r.Method == "GET" && r.URL.Path == "/repos/cpave3/legato/branches/main/protection/required_status_checks":
			if perms["administration"] != "read" {
				w.WriteHeader(403)
				_, _ = io.WriteString(w, `{"message":"Resource not accessible by integration"}`)
				break
			}
			_, _ = io.WriteString(w, `{"contexts":["build"],"checks":[{"context":"build"},{"context":"lint"}]}`)
		case r.Method == "GET" && strings.HasPrefix(r.URL.Path, "/repos/cpave3/legato/commits/") && strings.HasSuffix(r.URL.Path, "/check-runs"):
			head := strings.Split(r.URL.Path, "/")[5]
			_, _ = io.WriteString(w, g.checkRuns[head])
		case r.Method == "GET" && strings.HasPrefix(r.URL.Path, "/repos/cpave3/legato/commits/") && strings.HasSuffix(r.URL.Path, "/status"):
			_, _ = io.WriteString(w, `{"statuses":[{"context":"ci/legacy","state":"success","description":"ok","target_url":"s"}]}`)
		case r.Method == "GET" && r.URL.Path == "/repos/cpave3/legato/check-runs/77/annotations":
			_, _ = io.WriteString(w, `[{"path":"login_test.go","start_line":40,"message":"expected 3, got 4"}]`)
		case r.Method == "GET" && r.URL.Path == "/repos/cpave3/legato/pulls/12/reviews":
			out := append([]map[string]any{}, g.otherReviews...)
			for i, review := range g.reviews {
				out = append(out, map[string]any{"id": 100 + i, "body": review["body"], "commit_id": review["commit_id"], "state": review["event"], "html_url": fmt.Sprintf("https://github.com/r/%d", 100+i),
					"user": map[string]any{"login": g.reviewBy[i]}})
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
			g.reviewBy = append(g.reviewBy, g.apps[bearer])
			if g.acceptThenStall > 0 {
				stall := g.acceptThenStall
				g.mu.Unlock()
				time.Sleep(stall)
				g.mu.Lock()
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"id": 100 + len(g.reviews) - 1, "html_url": "https://github.com/r/new"})
		case r.Method == "POST" && (r.URL.Path == "/repos/cpave3/legato/pulls/12/comments/555/replies" || r.URL.Path == "/repos/cpave3/legato/pulls/12/comments/600/replies"):
			g.replyPosts++
			if g.replyRefuse != 0 {
				w.WriteHeader(g.replyRefuse)
				_, _ = io.WriteString(w, `{"message":"no"}`)
				break
			}
			var body map[string]string
			_ = json.NewDecoder(r.Body).Decode(&body)
			g.replies = append(g.replies, body["body"])
			g.replyBy = append(g.replyBy, g.apps[bearer])
			g.replyTo = append(g.replyTo, map[bool]int64{true: 555, false: 600}[strings.Contains(r.URL.Path, "/555/")])
			g.answerAfterAccept(w, fmt.Sprintf(`{"id": %d}`, 900+len(g.replies)))
		case r.Method == "GET" && r.URL.Path == "/repos/cpave3/legato/pulls/12/comments":
			// The review comments: the threads' first comments, then the
			// replies posted.
			page := 1
			fmt.Sscan(r.URL.Query().Get("page"), &page)
			out := []map[string]any{}
			if page == 1 {
				reviewer := map[string]any{"login": "legato-reviews[bot]"}
				out = append(out, map[string]any{"id": 555, "body": "Name the import.", "user": reviewer}, map[string]any{"id": 600, "body": "Bound the loop.", "user": reviewer})
				out = append(out, g.otherReplies...)
				for i, reply := range g.replies {
					out = append(out, map[string]any{"id": 901 + i, "in_reply_to_id": g.replyTo[i], "body": reply, "user": map[string]any{"login": g.replyBy[i]}})
				}
			}
			_ = json.NewEncoder(w).Encode(out)
		case r.Method == "POST" && r.URL.Path == "/repos/cpave3/legato/issues/12/comments":
			g.replyPosts++
			if g.replyRefuse != 0 {
				w.WriteHeader(g.replyRefuse)
				_, _ = io.WriteString(w, `{"message":"no"}`)
				break
			}
			var body map[string]string
			_ = json.NewDecoder(r.Body).Decode(&body)
			g.comments = append(g.comments, fmt.Sprintf("%s|%v", body["body"], perms["contents"]))
			g.commentBy = append(g.commentBy, g.apps[bearer])
			g.answerAfterAccept(w, fmt.Sprintf(`{"id": %d, "html_url": "https://github.com/c/%d"}`, 776+len(g.comments), 776+len(g.comments)))
		case r.Method == "GET" && r.URL.Path == "/repos/cpave3/legato/issues/12/comments":
			page := 1
			fmt.Sscan(r.URL.Query().Get("page"), &page)
			out := []map[string]any{}
			if page == 1 {
				out = append(out, g.otherComments...)
				for i, c := range g.comments {
					body, _, _ := strings.Cut(c, "|")
					out = append(out, map[string]any{"id": 777 + i, "body": body, "user": map[string]any{"login": g.commentBy[i]}})
				}
			}
			_ = json.NewEncoder(w).Encode(out)
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
			case strings.Contains(body.Query, "isOutdated"):
				g.threadReads++
				_, _ = io.WriteString(w, `{"data":{"repository":{"pullRequest":{"reviewThreads":{"pageInfo":{"hasNextPage":false},"nodes":[
					{"isResolved":true,"isOutdated":false,"comments":{"nodes":[{"fullDatabaseId":"501"}]}},
					{"isResolved":false,"isOutdated":true,"comments":{"nodes":[{"fullDatabaseId":"502"}]}}]}}}}}`)
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

// hit counts an API request, always true.
func (g *reviewGitHub) hit(r *http.Request) bool {
	if g.apiHits == nil {
		g.apiHits = map[string]int{}
	}
	key := "graphql"
	if parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/"), "/"); len(parts) >= 3 && parts[0] == "repos" {
		key = parts[1] + "/" + parts[2]
	}
	g.apiHits[key]++
	return true
}

// hits are the API requests that reached GitHub for key.
func (g *reviewGitHub) hits(key string) int {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.apiHits[key]
}

func (g *reviewGitHub) setLimit(l *fakeLimit) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.limit = l
}

// reviewProfiles writes the shipped GitHub profiles with their addresses
// moved to the fake.
// edits, by file name, change a profile's text further.
func reviewProfiles(t *testing.T, url string, edits map[string]func(string) string) string {
	t.Helper()
	dir := t.TempDir()
	for _, name := range []string{"github.yaml", "github-reviewer.yaml"} {
		data, err := shippedProfiles.ReadFile("credential_profiles/" + name)
		if err != nil {
			t.Fatal(err)
		}
		text := strings.ReplaceAll(strings.ReplaceAll(string(data), "https://api.github.com", url), "https://github.com", url)
		if edit := edits[name]; edit != nil {
			text = edit(text)
		}
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
	clock     *offsetClock
}

// offsetClock is the time now, moved on by Advance.
type offsetClock struct {
	mu     sync.Mutex
	offset time.Duration
}

func (c *offsetClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return time.Now().Add(c.offset)
}

func (c *offsetClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.offset += d
}

func newPublisherFixture(t *testing.T) publisherFixture {
	t.Helper()
	return newPublisherFixtureWith(t, nil)
}

// newPublisherFixtureWith is newPublisherFixture with its profiles' text
// edited, by file name.
func newPublisherFixtureWith(t *testing.T, edits map[string]func(string) string) publisherFixture {
	t.Helper()
	g := newReviewGitHub(t)
	key, _ := rsa.GenerateKey(rand.Reader, 2048)
	t.Setenv("GH_APP_KEY", string(pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})))
	approvals, db, _ := newApprovalFixture(t)
	createTask(t, db, "task-1")
	if err := db.CreateExecution(context.Background(), store.Execution{ID: "ex-1", Kind: store.ExecutionInteractive, Status: store.ExecutionRunning, TaskID: "task-1", ExecutionTaskID: "task-1", StartedAt: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}
	author := config.CredentialSettings{ClientID: "Iv1.author", Repositories: []string{"cpave3/legato", "acme/widgets"}}
	reviewer := config.CredentialSettings{ClientID: "Iv1.reviewer", Repositories: []string{"cpave3/legato", "acme/widgets"}}
	clock := &offsetClock{}
	broker, err := NewCredentialBroker(db, approvals, CredentialBrokerOptions{
		Credentials: []config.CredentialConfig{
			{Name: "gh", Secret: "env://GH_APP_KEY", Profile: "github", Type: "keypair", Settings: author},
			{Name: "reviewer-app", Secret: "env://GH_APP_KEY", Profile: "github-reviewer", Type: "keypair", Settings: reviewer},
		},
		ProfileDirs: []string{reviewProfiles(t, g.srv.URL, edits)},
		Messenger:   &recordingMessenger{}, WaitTimeout: 100 * time.Millisecond, HTTPClient: g.srv.Client(), Now: clock.Now,
	})
	if err != nil {
		t.Fatal(err)
	}
	channels := NewCredentialChannels(broker, CredentialChannelsOptions{ProxyURL: func() string { return "" }, LegatoBin: "/opt/legato"})
	publisher := NewReviewPublisher(channels, "reviewer-app")
	publisher.Timeout = time.Second
	return publisherFixture{brokerFixture: brokerFixture{broker: broker, approvals: approvals, store: db}, publisher: publisher, github: g, clock: clock}
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
			var limit *GitHubRateLimit
			if !strings.Contains(fmt.Sprint(err), tc.want) {
				t.Fatalf("err = %#v", err)
			}
			if tc.rateLimited {
				if !errors.As(err, &limit) || limit.Status != tc.status || limit.RetryAt.IsZero() || limit.Installation != "reviewer-app@cpave3" {
					t.Fatalf("err = %#v; a rate limit says when to try again", err)
				}
			} else if !errors.As(err, &gh) || gh.Status != tc.status || errors.As(err, &limit) {
				t.Fatalf("err = %#v", err)
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
	if err := f.publisher.Reply(ctx, AsReviewer, "cpave3/legato", 12, 555, "Resolved in pass 2: the import is named.", CommentMarker{Kind: MarkerRuling, Key: "t1/pass-2/f-1"}); err != nil {
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
	if _, err := f.publisher.PostIssueComment(context.Background(), AsAuthor, "cpave3/legato", 12, "Fixed in abc124.", CommentMarker{Kind: MarkerAnswer, Key: "t1/b-1/n1"}); err != nil {
		t.Fatal(err)
	}
	// The author App's token writes pull requests and reads code only.
	if len(f.github.comments) != 1 || !strings.HasPrefix(f.github.comments[0], "Fixed in abc124.") || !strings.HasSuffix(f.github.comments[0], "|read") {
		t.Fatalf("comments = %v", f.github.comments)
	}
}

// An override of a GitHub profile copied before Legato needed the App's
// name fails closed, and the error says which file needs which line.
func TestAProfileThatNamesNoAppCannotPostAndTheErrorNamesItsFileAndTheLine(t *testing.T) {
	f := newPublisherFixtureWith(t, map[string]func(string) string{"github.yaml": withoutActor})
	_, err := f.publisher.PostIssueComment(context.Background(), AsAuthor, "cpave3/legato", 12, "Fixed in abc124.", answerKey("t1/b-1/n1"))
	if err == nil {
		t.Fatal("posted without knowing the App")
	}
	path := f.broker.credentials["gh"].profile.Source
	for _, want := range []string{"github", path, `actor: "$.app_slug"`, "jwt_exchange.lookup"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error lacks %q: %v", want, err)
		}
	}
	if len(f.github.comments) != 0 {
		t.Fatalf("comments = %v", f.github.comments)
	}
}

func answerKey(key string) CommentMarker { return CommentMarker{Kind: MarkerAnswer, Key: key} }

// An answer GitHub took before Legato heard back (a timeout, a 5xx, or a
// crash before the answer was marked posted) is found by its marker, not
// posted again.
func TestAnAnswerGitHubTookIsNotPostedAgain(t *testing.T) {
	for _, tc := range []struct {
		name  string
		setup func(g *reviewGitHub, p *ReviewPublisher)
	}{
		{"timeout", func(g *reviewGitHub, p *ReviewPublisher) {
			g.replyStall, p.Timeout = 300*time.Millisecond, 100*time.Millisecond
		}},
		{"server error", func(g *reviewGitHub, _ *ReviewPublisher) { g.replyFail = 502 }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newPublisherFixture(t)
			ctx := context.Background()
			tc.setup(f.github, f.publisher)

			if err := f.publisher.Reply(ctx, AsAuthor, "cpave3/legato", 12, 555, "Fixed in abc124.", answerKey("t1/b-1/n1")); err != nil {
				t.Fatalf("a reply GitHub took: %v", err)
			}
			if _, err := f.publisher.PostIssueComment(ctx, AsAuthor, "cpave3/legato", 12, "Skipped: this was optional.", answerKey("t1/n-1/n2")); err != nil {
				t.Fatalf("a comment GitHub took: %v", err)
			}

			// Asked again, as after a restart: found, not posted.
			f.github.replyStall, f.github.replyFail = 0, 0
			posts := f.github.replyPosts
			if err := f.publisher.Reply(ctx, AsAuthor, "cpave3/legato", 12, 555, "Fixed in abc124.", answerKey("t1/b-1/n1")); err != nil {
				t.Fatal(err)
			}
			if _, err := f.publisher.PostIssueComment(ctx, AsAuthor, "cpave3/legato", 12, "Skipped: this was optional.", answerKey("t1/n-1/n2")); err != nil {
				t.Fatal(err)
			}
			if len(f.github.replies) != 1 || len(f.github.comments) != 1 || f.github.replyPosts != posts {
				t.Fatalf("replies %v, comments %v, posts %d then %d", f.github.replies, f.github.comments, posts, f.github.replyPosts)
			}
			if !strings.Contains(f.github.replies[0], "<!-- legato-answer t1/b-1/n1 -->") {
				t.Fatalf("reply %q has no hidden marker", f.github.replies[0])
			}
		})
	}
}

// A server error before GitHub took the answer: it is not there, so the
// error is returned, and the next try posts it once. A refusal (4xx) is
// returned as it is.
func TestAnAnswerGitHubDidNotTakeIsPostedOnceOnTheNextTry(t *testing.T) {
	f := newPublisherFixture(t)
	ctx := context.Background()
	f.github.replyRefuse = 502
	var gh *GitHubError
	if err := f.publisher.Reply(ctx, AsAuthor, "cpave3/legato", 12, 600, "Fixed.", answerKey("t1/b-2/n1")); !errors.As(err, &gh) || gh.Status != 502 {
		t.Fatalf("err = %v", err)
	}
	f.github.replyRefuse = 0
	if err := f.publisher.Reply(ctx, AsAuthor, "cpave3/legato", 12, 600, "Fixed.", answerKey("t1/b-2/n1")); err != nil {
		t.Fatal(err)
	}
	if len(f.github.replies) != 1 || f.github.replyPosts != 2 {
		t.Fatalf("replies %v after %d posts", f.github.replies, f.github.replyPosts)
	}

	f.github.replyRefuse = 422
	if _, err := f.publisher.PostIssueComment(ctx, AsAuthor, "cpave3/legato", 12, "Fixed.", answerKey("t1/b-3/n1")); !errors.As(err, &gh) || gh.Status != 422 {
		t.Fatalf("err = %v", err)
	}
}

// The search matches an answer's marker, not its text: the same words
// answering two findings are two answers.
func TestIdenticalAnswersToTwoFindingsArePostedEach(t *testing.T) {
	f := newPublisherFixture(t)
	ctx := context.Background()
	for _, key := range []string{"t1/b-1/n1", "t1/b-2/n2"} {
		if err := f.publisher.Reply(ctx, AsAuthor, "cpave3/legato", 12, 555, "Fixed.", answerKey(key)); err != nil {
			t.Fatal(err)
		}
		if _, err := f.publisher.PostIssueComment(ctx, AsAuthor, "cpave3/legato", 12, "Fixed.", answerKey(key)); err != nil {
			t.Fatal(err)
		}
	}
	if len(f.github.replies) != 2 || len(f.github.comments) != 2 {
		t.Fatalf("replies %v, comments %v", f.github.replies, f.github.comments)
	}
	// A key that is the start of another's is not taken for it.
	if err := f.publisher.Reply(ctx, AsAuthor, "cpave3/legato", 12, 555, "Fixed.", answerKey("t1/b-1/n")); err != nil || len(f.github.replies) != 3 {
		t.Fatalf("replies %v, %v", f.github.replies, err)
	}
	// An answer and a ruling with the same key are different comments.
	if err := f.publisher.Reply(ctx, AsReviewer, "cpave3/legato", 12, 555, "Fixed.", CommentMarker{Kind: MarkerRuling, Key: "t1/b-1/n1"}); err != nil || len(f.github.replies) != 4 {
		t.Fatalf("replies %v, %v", f.github.replies, err)
	}
	// A comment needs a marker.
	if err := f.publisher.Reply(ctx, AsAuthor, "cpave3/legato", 12, 555, "Fixed.", CommentMarker{}); err == nil {
		t.Fatal("a reply with no marker was posted")
	}
}

// A comment or a review is taken for one Legato posted only when the App
// it was posted as posted it, and a reply only in its own thread: a marker
// anyone can copy is not enough.
func TestOnlyTheAppsOwnPostWithTheMarkerIsTakenForIt(t *testing.T) {
	f := newPublisherFixture(t)
	ctx := context.Background()
	g := f.github
	user := func(login string) map[string]any { return map[string]any{"login": login} }
	ruling := CommentMarker{Kind: MarkerRuling, Key: "t1/pass-2/f-1"}
	g.otherReplies = []map[string]any{
		// An outsider, and a person named as the App is without [bot].
		{"id": 701, "in_reply_to_id": 555, "body": "ok " + ruling.hidden(), "user": user("mallory")},
		{"id": 702, "in_reply_to_id": 555, "body": "ok " + ruling.hidden(), "user": user("legato-reviews")},
		// The author App, which never posts rulings.
		{"id": 703, "in_reply_to_id": 555, "body": "ok " + ruling.hidden(), "user": user("legato-author[bot]")},
		// The reviewer App itself, but in another thread.
		{"id": 704, "in_reply_to_id": 600, "body": "ok " + ruling.hidden(), "user": user("legato-reviews[bot]")},
	}
	answer := answerKey("t1/n-1/n1")
	g.otherComments = []map[string]any{{"id": 705, "body": "ok " + answer.hidden(), "user": user("mallory")}}
	g.otherReviews = []map[string]any{{"id": 706, "body": "ok " + reviewMarker("pass-1/pr-12"), "user": user("mallory"), "html_url": "https://github.com/r/706"}}

	if err := f.publisher.Reply(ctx, AsReviewer, "cpave3/legato", 12, 555, "Still open in pass 2.", ruling); err != nil {
		t.Fatal(err)
	}
	if _, err := f.publisher.PostIssueComment(ctx, AsAuthor, "cpave3/legato", 12, "Skipped.", answer); err != nil {
		t.Fatal(err)
	}
	review, err := f.publisher.PostReview(ctx, cleanReview())
	if err != nil {
		t.Fatal(err)
	}
	if len(g.replies) != 1 || len(g.comments) != 1 || g.posts != 1 || review.Existed || review.ID == 706 {
		t.Fatalf("replies %v, comments %v, %d review posts, review %+v", g.replies, g.comments, g.posts, review)
	}

	// The App's own, in its thread, is found: nothing is posted again.
	if err := f.publisher.Reply(ctx, AsReviewer, "cpave3/legato", 12, 555, "Still open in pass 2.", ruling); err != nil {
		t.Fatal(err)
	}
	if _, err := f.publisher.PostIssueComment(ctx, AsAuthor, "cpave3/legato", 12, "Skipped.", answer); err != nil {
		t.Fatal(err)
	}
	if again, err := f.publisher.PostReview(ctx, cleanReview()); err != nil || !again.Existed || again.ID != review.ID {
		t.Fatalf("review again %+v, %v", again, err)
	}
	if len(g.replies) != 1 || len(g.comments) != 1 || g.posts != 1 || g.replyBy[0] != "legato-reviews[bot]" || g.commentBy[0] != "legato-author[bot]" {
		t.Fatalf("replies %v by %v, comments %v by %v, %d review posts", g.replies, g.replyBy, g.comments, g.commentBy, g.posts)
	}
}

// Nothing Legato posts asks for changes: a review is APPROVE or COMMENT,
// and answers and replies are comments.
func TestNoAnswerReplyOrReviewUsesRequestChanges(t *testing.T) {
	f := newPublisherFixture(t)
	ctx := context.Background()
	refused := cleanReview()
	refused.Event = "REQUEST_CHANGES"
	if _, err := f.publisher.PostReview(ctx, refused); err == nil || f.github.posts != 0 {
		t.Fatalf("a REQUEST_CHANGES review: %v, %d posts", err, f.github.posts)
	}
	changes := cleanReview()
	changes.Event, changes.Marker = ReviewComment, "pass-2/pr-12"
	if _, err := f.publisher.PostReview(ctx, changes); err != nil {
		t.Fatal(err)
	}
	if err := f.publisher.Reply(ctx, AsReviewer, "cpave3/legato", 12, 555, "Still open in pass 2.", CommentMarker{Kind: MarkerRuling, Key: "t1/pass-2/f-1"}); err != nil {
		t.Fatal(err)
	}
	if err := f.publisher.Reply(ctx, AsAuthor, "cpave3/legato", 12, 555, "Fixed.", answerKey("t1/f-1/n1")); err != nil {
		t.Fatal(err)
	}
	if _, err := f.publisher.PostIssueComment(ctx, AsAuthor, "cpave3/legato", 12, "Skipped.", answerKey("t1/n-1/n1")); err != nil {
		t.Fatal(err)
	}
	if len(f.github.bodies) == 0 {
		t.Fatal("nothing was posted")
	}
	for _, body := range f.github.bodies {
		if strings.Contains(body, "REQUEST_CHANGES") {
			t.Fatalf("a post asks for changes: %s", body)
		}
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

// memoryPollPages keeps polled pages in memory.
type memoryPollPages map[string][2]string

func (m memoryPollPages) PollPage(_ context.Context, list string, page int) (string, string, bool) {
	v, ok := m[fmt.Sprint(list, page)]
	return v[0], v[1], ok
}

func (m memoryPollPages) SavePollPage(_ context.Context, list string, page int, etag, body string) error {
	m[fmt.Sprint(list, page)] = [2]string{etag, body}
	return nil
}

func TestCommentsArePolledPageByPageWithETagsAndThreadsOnlyOnAChange(t *testing.T) {
	f := newPublisherFixture(t)
	var full []string
	for i := 0; i < 100; i++ {
		full = append(full, fmt.Sprintf(`{"id":%d,"user":{"login":"amy","type":"User"},"author_association":"MEMBER","body":"c%d","html_url":"u"}`, 1000+i, i))
	}
	f.github.lists = map[string][]string{
		"/repos/cpave3/legato/pulls/12/reviews":   {`[{"id":1,"user":{"login":"bob","type":"User"},"author_association":"OWNER","body":"Please split this.","state":"COMMENTED","html_url":"r1"}]`},
		"/repos/cpave3/legato/pulls/12/comments":  {`[{"id":501,"user":{"login":"cubic-dev-ai[bot]","type":"Bot"},"author_association":"NONE","body":"Nil check.","path":"a.go","line":4,"html_url":"c501"},{"id":502,"user":{"login":"amy","type":"User"},"author_association":"MEMBER","body":"Old line.","path":"a.go","line":null,"original_line":9,"html_url":"c502"},{"id":503,"in_reply_to_id":502,"user":{"login":"amy","type":"User"},"author_association":"MEMBER","body":"Still?","path":"a.go","html_url":"c503"}]`},
		"/repos/cpave3/legato/issues/12/comments": {"[" + strings.Join(full, ",") + "]", `[{"id":2000,"user":{"login":"stranger","type":"User"},"author_association":"NONE","body":"+1","html_url":"i2000"}]`},
	}
	pages := memoryPollPages{}
	ctx := context.Background()

	poll, err := f.publisher.PollComments(ctx, "cpave3/legato", 12, pages)
	if err != nil {
		t.Fatal(err)
	}
	if !poll.Changed || len(poll.Comments) != 1+3+101 || f.github.threadReads != 1 {
		t.Fatalf("first poll: changed %v, %d comments, %d thread reads", poll.Changed, len(poll.Comments), f.github.threadReads)
	}
	byID := map[int64]PRComment{}
	for _, c := range poll.Comments {
		byID[c.ID] = c
	}
	if !byID[501].Resolved || !byID[502].Outdated || byID[503].ThreadRoot != 502 || byID[1].Kind != "review" || byID[2000].Kind != "issue" || byID[501].Line != 4 {
		t.Fatalf("comments %+v", byID)
	}

	// Nothing changed: every page is a 304, and threads are not read.
	before := f.github.notModified
	poll, err = f.publisher.PollComments(ctx, "cpave3/legato", 12, pages)
	if err != nil || poll.Changed || len(poll.Comments) != 105 || f.github.notModified-before != 4 || f.github.threadReads != 1 {
		t.Fatalf("second poll: changed %v, %d comments, 304s %d, thread reads %d, %v", poll.Changed, len(poll.Comments), f.github.notModified-before, f.github.threadReads, err)
	}

	// Rate limited: reported, never taken for "unchanged".
	f.github.limitLists = true
	_, err = f.publisher.PollComments(ctx, "cpave3/legato", 12, pages)
	if !errors.As(err, new(*GitHubRateLimit)) {
		t.Fatalf("rate limited poll: %v", err)
	}
}

func TestRequiredChecksAreReadFromTheBaseBranchsProtection(t *testing.T) {
	f := newPublisherFixture(t)
	f.github.checkRuns = map[string]string{
		"aaa": `{"check_runs":[{"id":77,"name":"build","status":"completed","conclusion":"failure","html_url":"b77","output":{"title":"2 tests failed","summary":"TestLogin failed"}},{"id":78,"name":"lint","status":"in_progress","html_url":"l78"},{"id":79,"name":"docs","status":"completed","conclusion":"failure"}]}`,
		"bbb": `{"check_runs":[{"id":80,"name":"build","status":"completed","conclusion":"success"},{"id":81,"name":"lint","status":"completed","conclusion":"success"}]}`,
	}
	ctx := context.Background()

	checks, err := f.publisher.Checks(ctx, "cpave3/legato", "main", "aaa", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(checks) != 2 {
		t.Fatalf("checks %+v; want the two required ones only", checks)
	}
	build, lint := checks[0], checks[1]
	if build.Name != "build" || build.State != CheckFailing || build.Conclusion != "failure" || build.URL != "b77" ||
		!strings.Contains(build.Summary, "2 tests failed") || !strings.Contains(build.Excerpt, "login_test.go:40: expected 3, got 4") {
		t.Fatalf("build %+v", build)
	}
	if lint.Name != "lint" || lint.State != CheckPending {
		t.Fatalf("lint %+v", lint)
	}
	if checks, _ := f.publisher.Checks(ctx, "cpave3/legato", "main", "bbb", nil); checks[0].State != CheckPassing || checks[1].State != CheckPassing {
		t.Fatalf("on a new head: %+v", checks)
	}

	// An App that may not read branch protection counts the configured
	// list.
	runs := f.github.checkRuns
	f = newPublisherFixture(t)
	f.github.checkRuns, f.github.noAdministration = runs, true
	checks, err = f.publisher.Checks(ctx, "cpave3/legato", "main", "aaa", []string{"docs", "ci/legacy", "e2e"})
	if err != nil || len(checks) != 3 || checks[0].State != CheckFailing || checks[1].State != CheckPassing || checks[2].State != CheckPending {
		t.Fatalf("configured checks %+v, %v", checks, err)
	}
}

func githubPull(number int, head, base, author string) map[string]any {
	return map[string]any{"number": number, "title": fmt.Sprintf("PR %d", number), "html_url": fmt.Sprintf("https://github.com/cpave3/legato/pull/%d", number),
		"user": map[string]any{"login": author},
		"head": map[string]any{"ref": head, "sha": fmt.Sprintf("sha%d", number), "repo": map[string]any{"full_name": "cpave3/legato"}},
		"base": map[string]any{"ref": base}}
}

func TestAStackIsFollowedOnGitHubWithGETsOnly(t *testing.T) {
	f := newPublisherFixture(t)
	f.github.pulls = []map[string]any{
		githubPull(137, "s1", "main", "legato-author[bot]"),
		githubPull(138, "s2", "s1", "legato-author[bot]"),
		githubPull(139, "s3", "s2", "legato-author[bot]"),
		githubPull(150, "other", "s3", "stranger"),
	}

	stack, err := FindReviewStack(context.Background(), f.publisher, "cpave3/legato", []string{"s2"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := numbers(stack.PRs); !slices.Equal(got, []int{137, 138, 139}) || stack.PRs[1].Base != "s1" || stack.PRs[1].HeadRepo != "cpave3/legato" {
		t.Fatalf("stack %v: %+v", got, stack.PRs)
	}
	for _, m := range f.github.methods {
		if m != "GET" && m != "POST" && m != "DELETE" {
			t.Fatalf("methods %v", f.github.methods)
		}
	}
}

// A pass's heads are read from GitHub as the reviewer App, with a read
// token: no write permission is asked for.
func TestAPullRequestsHeadIsReadFromGitHubAsTheReviewerApp(t *testing.T) {
	f := newPublisherFixture(t)

	pr, state, err := f.publisher.PullRequest(context.Background(), "cpave3/legato", 12)
	if err != nil {
		t.Fatal(err)
	}

	if pr.HeadSHA != "sha12" || pr.Number != 12 || state != "open" {
		t.Fatalf("pull request %+v, state %q", pr, state)
	}
	// The reviewer App's read permissions (the author App's token has no
	// checks read), with no write.
	if len(f.github.headReads) != 1 || f.github.headReads[0]["checks"] != "read" || f.github.headReads[0]["pull_requests"] != "read" ||
		f.github.headReads[0]["contents"] != "read" {
		t.Fatalf("read with %v", f.github.headReads)
	}
}
