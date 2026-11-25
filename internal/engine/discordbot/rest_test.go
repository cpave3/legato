package discordbot

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

type restCall struct {
	method, path, auth string
	escaped            string // the path as sent, percent-encoding kept
	body               map[string]any
}

func restServer(t *testing.T, handle func(n int, w http.ResponseWriter)) (*Client, chan restCall) {
	t.Helper()
	calls := make(chan restCall, 8)
	var n atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		var body map[string]any
		_ = json.Unmarshal(b, &body)
		calls <- restCall{method: r.Method, path: r.URL.Path, auth: r.Header.Get("Authorization"), escaped: r.URL.EscapedPath(), body: body}
		handle(int(n.Add(1)), w)
	}))
	t.Cleanup(srv.Close)
	return &Client{Token: testToken, APIBase: srv.URL}, calls
}

func noToken(t *testing.T, err error) {
	t.Helper()
	if err != nil && strings.Contains(err.Error(), testToken) {
		t.Fatalf("error leaks the token: %v", err)
	}
}

func TestCreateMessage(t *testing.T) {
	c, calls := restServer(t, func(_ int, w http.ResponseWriter) {
		_, _ = w.Write([]byte(`{"id":"new-1","channel_id":"c1"}`))
	})
	id, err := c.CreateMessage(context.Background(), "c1", "hello", "")
	if err != nil || id != "new-1" {
		t.Fatalf("CreateMessage = %q, %v", id, err)
	}
	call := <-calls
	if call.method != http.MethodPost || call.path != "/channels/c1/messages" || call.auth != "Bot "+testToken {
		t.Fatalf("call = %+v", call)
	}
	if call.body["content"] != "hello" {
		t.Fatalf("body = %v", call.body)
	}
	if _, ok := call.body["message_reference"]; ok {
		t.Fatalf("unexpected message_reference: %v", call.body)
	}
	am := call.body["allowed_mentions"].(map[string]any)
	if parse := am["parse"].([]any); len(parse) != 0 {
		t.Fatalf("allowed_mentions = %v", am)
	}

	if _, err := c.CreateMessage(context.Background(), "c1", "re", "m9"); err != nil {
		t.Fatal(err)
	}
	ref := (<-calls).body["message_reference"].(map[string]any)
	if ref["message_id"] != "m9" || ref["fail_if_not_exists"] != false {
		t.Fatalf("message_reference = %v", ref)
	}
}

func TestCreateMessageRateLimitedRetriesOnce(t *testing.T) {
	c, calls := restServer(t, func(n int, w http.ResponseWriter) {
		if n == 1 {
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte(`{"message":"You are being rate limited.","retry_after":0.01,"global":false}`))
			return
		}
		_, _ = w.Write([]byte(`{"id":"new-2"}`))
	})
	id, err := c.CreateMessage(context.Background(), "c1", "hi", "")
	if err != nil || id != "new-2" {
		t.Fatalf("CreateMessage = %q, %v", id, err)
	}
	if len(calls) != 2 {
		t.Fatalf("%d calls, want 2", len(calls))
	}
}

func TestCreateMessageRateLimitedTwice(t *testing.T) {
	c, calls := restServer(t, func(_ int, w http.ResponseWriter) {
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"message":"You are being rate limited.","retry_after":0.01}`))
	})
	_, err := c.CreateMessage(context.Background(), "c1", "hi", "")
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.Status != 429 || apiErr.RetryAfter <= 0 {
		t.Fatalf("err = %v", err)
	}
	noToken(t, err)
	if len(calls) != 2 {
		t.Fatalf("%d calls, want 2", len(calls))
	}
}

func TestCreateMessageLongRetryAfterNotRetried(t *testing.T) {
	c, calls := restServer(t, func(_ int, w http.ResponseWriter) {
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"retry_after":30}`))
	})
	_, err := c.CreateMessage(context.Background(), "c1", "hi", "")
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.Status != 429 {
		t.Fatalf("err = %v", err)
	}
	if len(calls) != 1 {
		t.Fatalf("%d calls, want 1", len(calls))
	}
}

func TestCreateMessageErrors(t *testing.T) {
	c, _ := restServer(t, func(_ int, w http.ResponseWriter) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"message":"401: Unauthorized","code":0}`))
	})
	_, err := c.CreateMessage(context.Background(), "c1", "hi", "")
	if !errors.Is(err, ErrAuth) {
		t.Fatalf("err = %v, want ErrAuth", err)
	}
	noToken(t, err)

	c, _ = restServer(t, func(_ int, w http.ResponseWriter) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"message":"Missing Permissions","code":50013}`))
	})
	_, err = c.CreateMessage(context.Background(), "c1", "hi", "")
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.Status != 403 || apiErr.Code != 50013 || apiErr.Message != "Missing Permissions" {
		t.Fatalf("err = %v", err)
	}
	if !strings.Contains(err.Error(), "Missing Permissions") {
		t.Fatalf("error text = %q", err.Error())
	}
	noToken(t, err)
}

func TestCreateMessageContentChecks(t *testing.T) {
	c, calls := restServer(t, func(_ int, w http.ResponseWriter) {})
	_, err := c.CreateMessage(context.Background(), "c1", strings.Repeat("é", MaxContentLength+1), "")
	if !errors.Is(err, ErrTooLong) {
		t.Fatalf("err = %v, want ErrTooLong", err)
	}
	if _, err := c.CreateMessage(context.Background(), "c1", "", ""); err == nil {
		t.Fatal("empty content accepted")
	}
	if len(calls) != 0 {
		t.Fatalf("%d HTTP calls made", len(calls))
	}
	// Exactly the limit, counted in characters, is fine to send.
	c2, _ := restServer(t, func(_ int, w http.ResponseWriter) { _, _ = w.Write([]byte(`{"id":"x"}`)) })
	if _, err := c2.CreateMessage(context.Background(), "c1", strings.Repeat("é", MaxContentLength), ""); err != nil {
		t.Fatal(err)
	}
}
