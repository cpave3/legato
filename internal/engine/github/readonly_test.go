package github

import (
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
)

type recordingRoundTripper struct {
	methods   []string
	responder func(*http.Request) string
}

func (r *recordingRoundTripper) RoundTrip(request *http.Request) (*http.Response, error) {
	r.methods = append(r.methods, request.Method)
	body := `{}`
	if r.responder != nil {
		body = r.responder(request)
	}
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(body)),
		Request:    request,
	}, nil
}

func TestReadOnlyTransportRejectsMutationsBeforeDispatch(t *testing.T) {
	underlying := &recordingRoundTripper{}
	transport := NewReadOnlyTransport(underlying)
	for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete} {
		request, err := http.NewRequest(method, "https://api.github.com/user", strings.NewReader("{}"))
		if err != nil {
			t.Fatal(err)
		}
		_, err = transport.RoundTrip(request)
		if !errors.Is(err, ErrReadOnlyViolation) {
			t.Fatalf("%s error = %v", method, err)
		}
	}
	if len(underlying.methods) != 0 {
		t.Fatalf("mutation requests reached underlying transport: %v", underlying.methods)
	}
}

func TestAllGitHubClientEndpointsDispatchOnlyGET(t *testing.T) {
	underlying := &recordingRoundTripper{responder: func(request *http.Request) string {
		switch request.URL.Path {
		case "/user":
			return `{"login":"me"}`
		case "/repos/acme/widget/pulls":
			return `[]`
		case "/repos/acme/widget/pulls/42":
			return `{"number":42,"state":"open","head":{"sha":"abc","repo":{"full_name":"acme/widget"}}}`
		case "/repos/acme/widget/pulls/42/reviews", "/repos/acme/widget/commits/abc/pulls":
			return `[]`
		case "/repos/acme/widget/commits/abc/check-runs":
			return `{"check_runs":[]}`
		case "/repos/acme/widget/commits/abc/status":
			return `{"statuses":[]}`
		default:
			t.Fatalf("unexpected path: %s", request.URL.Path)
			return `{}`
		}
	}}
	client, err := New(Options{Token: "token", HTTPClient: &http.Client{Transport: underlying}})
	if err != nil {
		t.Fatal(err)
	}
	if err := client.CheckAvailable(); err != nil {
		t.Fatal(err)
	}
	if _, err := client.ListRepositoryPRs("acme", "widget"); err != nil {
		t.Fatal(err)
	}
	if _, err := client.FetchPRByNumber("acme", "widget", 42); err != nil {
		t.Fatal(err)
	}
	if _, err := client.FetchPRsForCommit("acme", "widget", "abc"); err != nil {
		t.Fatal(err)
	}
	if len(underlying.methods) != 7 {
		t.Fatalf("methods = %v", underlying.methods)
	}
	for _, method := range underlying.methods {
		if method != http.MethodGet {
			t.Fatalf("non-GET request dispatched: %v", underlying.methods)
		}
	}
}

func TestClientWrapsCustomTransportWithReadOnlyGuard(t *testing.T) {
	underlying := &recordingRoundTripper{}
	original := &http.Client{Transport: underlying}
	client, err := New(Options{Token: "token", HTTPClient: original})
	if err != nil {
		t.Fatal(err)
	}
	request, _ := http.NewRequest(http.MethodPost, "https://api.github.com/user", nil)
	_, err = client.httpClient.Do(request)
	if !errors.Is(err, ErrReadOnlyViolation) || len(underlying.methods) != 0 {
		t.Fatalf("error=%v underlying=%v", err, underlying.methods)
	}
	if original.Transport != underlying {
		t.Fatal("New mutated caller-owned HTTP client")
	}
}

func TestReadOnlyTransportAllowsGET(t *testing.T) {
	underlying := &recordingRoundTripper{}
	transport := NewReadOnlyTransport(underlying)
	request, _ := http.NewRequest(http.MethodGet, "https://api.github.com/user", nil)
	response, err := transport.RoundTrip(request)
	if err != nil {
		t.Fatal(err)
	}
	_ = response.Body.Close()
	if len(underlying.methods) != 1 || underlying.methods[0] != http.MethodGet {
		t.Fatalf("methods = %v", underlying.methods)
	}
}
