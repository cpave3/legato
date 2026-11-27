package jira

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type recordedRequest struct {
	method, path, auth  string
	contentType, accept string
	body                map[string]any
}

func jiraServer(t *testing.T, status int, answer string) (*httptest.Server, *[]recordedRequest) {
	t.Helper()
	var seen []recordedRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		req := recordedRequest{method: r.Method, path: r.URL.RequestURI(), auth: r.Header.Get("Authorization"),
			contentType: r.Header.Get("Content-Type"), accept: r.Header.Get("Accept")}
		_ = json.Unmarshal(raw, &req.body)
		seen = append(seen, req)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(answer))
	}))
	t.Cleanup(srv.Close)
	return srv, &seen
}

// path walks decoded JSON: a string is a key, an int an index.
func path(v any, steps ...any) any {
	for _, s := range steps {
		switch s := s.(type) {
		case string:
			m, _ := v.(map[string]any)
			v = m[s]
		case int:
			l, _ := v.([]any)
			if s >= len(l) {
				return nil
			}
			v = l[s]
		}
	}
	return v
}

func TestCreateIssueSendsTheFieldsWithAnADFDescription(t *testing.T) {
	srv, seen := jiraServer(t, http.StatusCreated, `{"id":"10042","key":"AG-12","self":"https://x/rest/api/3/issue/10042"}`)
	c := NewClient(srv.URL+"/", "alice@example.com", "secret-token", 5*time.Second)

	got, err := c.CreateIssue(context.Background(), IssueDraft{
		Project: "AG", Type: "Bug", Summary: "Login fails in prod",
		Description: "Seen today:\n\n- step one\n- step two\n\n```sh\ncurl -s /login\n```",
		Labels:      []string{"prod", "slack"}, Priority: "High",
		Fields: map[string]any{"customfield_10020": 7},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got != (CreatedIssue{ID: "10042", Key: "AG-12", URL: srv.URL + "/browse/AG-12"}) {
		t.Fatalf("created = %+v", got)
	}
	if len(*seen) != 1 {
		t.Fatalf("requests = %d", len(*seen))
	}
	req := (*seen)[0]
	if req.method != http.MethodPost || req.path != "/rest/api/3/issue" || req.auth != "Basic YWxpY2VAZXhhbXBsZS5jb206c2VjcmV0LXRva2Vu" {
		t.Fatalf("request = %s %s %q", req.method, req.path, req.auth)
	}
	f := req.body["fields"]
	for _, check := range []struct {
		got, want any
	}{
		{path(f, "project", "key"), "AG"},
		{path(f, "issuetype", "name"), "Bug"},
		{path(f, "summary"), "Login fails in prod"},
		{path(f, "priority", "name"), "High"},
		{path(f, "labels", 1), "slack"},
		{path(f, "customfield_10020"), float64(7)},
		{path(f, "description", "type"), "doc"},
		{path(f, "description", "content", 1, "type"), "bulletList"},
		{path(f, "description", "content", 2, "type"), "codeBlock"},
		{path(f, "description", "content", 2, "content", 0, "text"), "curl -s /login"},
	} {
		if check.got != check.want {
			t.Errorf("got %v, want %v in %v", check.got, check.want, f)
		}
	}
}

// Fields Legato does not set are left out: Jira refuses a field a project's
// screen lacks, even an empty one.
func TestCreateIssueLeavesOutWhatIsNotGiven(t *testing.T) {
	srv, seen := jiraServer(t, http.StatusCreated, `{"id":"1","key":"AG-1"}`)
	c := NewClient(srv.URL, "a@b.com", "tok", 5*time.Second)
	if _, err := c.CreateIssue(context.Background(), IssueDraft{Project: "AG", Type: "Task", Summary: "S"}); err != nil {
		t.Fatal(err)
	}
	fields, _ := (*seen)[0].body["fields"].(map[string]any)
	for _, name := range []string{"labels", "priority", "description"} {
		if _, ok := fields[name]; ok {
			t.Errorf("%s sent: %v", name, fields)
		}
	}
}

func TestCreateIssueRefusalNamesTheField(t *testing.T) {
	srv, _ := jiraServer(t, http.StatusBadRequest, `{"errorMessages":[],"errors":{"issuetype":"Specify a valid issue type"}}`)
	c := NewClient(srv.URL, "a@b.com", "secret-token", 5*time.Second)
	_, err := c.CreateIssue(context.Background(), IssueDraft{Project: "AG", Type: "Bgu", Summary: "S"})
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.Status != http.StatusBadRequest {
		t.Fatalf("err = %#v", err)
	}
	if !strings.Contains(err.Error(), "issuetype: Specify a valid issue type") {
		t.Fatalf("err = %v", err)
	}
	if strings.Contains(err.Error(), "secret-token") {
		t.Fatalf("the token is in the error: %v", err)
	}
}

func TestAddCommentPostsAnADFBody(t *testing.T) {
	srv, seen := jiraServer(t, http.StatusCreated, `{"id":"20001","body":{}}`)
	c := NewClient(srv.URL, "a@b.com", "tok", 5*time.Second)
	got, err := c.AddComment(context.Background(), "AG-12", "Seen again in **prod** today")
	if err != nil {
		t.Fatal(err)
	}
	if got != (CreatedComment{ID: "20001", URL: srv.URL + "/browse/AG-12?focusedCommentId=20001"}) {
		t.Fatalf("comment = %+v", got)
	}
	req := (*seen)[0]
	if req.method != http.MethodPost || req.path != "/rest/api/3/issue/AG-12/comment" {
		t.Fatalf("request = %s %s", req.method, req.path)
	}
	if path(req.body, "body", "type") != "doc" || path(req.body, "body", "content", 0, "content", 1, "marks", 0, "type") != "strong" {
		t.Fatalf("body = %v", req.body)
	}
}

func TestAddCommentOnAMissingIssueIsANotFoundAPIError(t *testing.T) {
	srv, _ := jiraServer(t, http.StatusNotFound, `{"errorMessages":["Issue does not exist or you do not have permission to see it."],"errors":{}}`)
	c := NewClient(srv.URL, "a@b.com", "tok", 5*time.Second)
	_, err := c.AddComment(context.Background(), "AG-999", "x")
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.Status != http.StatusNotFound {
		t.Fatalf("err = %#v", err)
	}
	if !strings.Contains(err.Error(), "Issue does not exist") {
		t.Fatalf("err = %v", err)
	}
}
