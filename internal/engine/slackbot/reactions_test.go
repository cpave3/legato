package slackbot

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
)

func TestAddReaction(t *testing.T) {
	f := newFake(t)
	f.handle("reactions.add", func(w http.ResponseWriter, r *http.Request) {
		if ct := r.Header.Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
			t.Errorf("content type %q", ct)
		}
		writeJSON(w, map[string]any{"ok": true})
	})
	if err := f.client().AddReaction(t.Context(), "C1", "1.5", "eyes"); err != nil {
		t.Fatal(err)
	}
	if got := f.auths["reactions.add"]; got != "Bearer "+botToken {
		t.Fatalf("auth header = %q", got)
	}
	var body map[string]any
	if err := json.Unmarshal([]byte(f.bodies["reactions.add"][0]), &body); err != nil {
		t.Fatal(err)
	}
	want := map[string]any{"channel": "C1", "timestamp": "1.5", "name": "eyes"}
	for k, v := range want {
		if body[k] != v {
			t.Errorf("body[%s] = %v, want %v (body %v)", k, body[k], v, body)
		}
	}
}

func TestRemoveReaction(t *testing.T) {
	f := newFake(t)
	f.handle("reactions.remove", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{"ok": true})
	})
	if err := f.client().RemoveReaction(t.Context(), "C1", "1.5", "white_check_mark"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(f.bodies["reactions.remove"][0], `"name":"white_check_mark"`) {
		t.Fatalf("body = %s", f.bodies["reactions.remove"][0])
	}
}

func TestAddReactionAlreadyReactedIsSuccess(t *testing.T) {
	f := newFake(t)
	f.handle("reactions.add", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{"ok": false, "error": "already_reacted"})
	})
	if err := f.client().AddReaction(t.Context(), "C1", "1.5", "eyes"); err != nil {
		t.Fatalf("err = %v, want nil", err)
	}
}

func TestRemoveReactionNoReactionIsSuccess(t *testing.T) {
	f := newFake(t)
	f.handle("reactions.remove", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{"ok": false, "error": "no_reaction"})
	})
	if err := f.client().RemoveReaction(t.Context(), "C1", "1.5", "eyes"); err != nil {
		t.Fatalf("err = %v, want nil", err)
	}
}

func TestAddReactionMissingScopeNamesTheScope(t *testing.T) {
	f := newFake(t)
	f.handle("reactions.add", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{"ok": false, "error": "missing_scope", "needed": "reactions:write", "provided": "chat:write,app_mentions:read"})
	})
	err := f.client().AddReaction(t.Context(), "C1", "1.5", "eyes")
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.Code != "missing_scope" || apiErr.Needed != "reactions:write" {
		t.Fatalf("err = %#v", err)
	}
	if !strings.Contains(err.Error(), "reactions:write") {
		t.Fatalf("error %q does not name the scope", err)
	}
	if errors.Is(err, ErrAuth) {
		t.Fatal("missing_scope must not be ErrAuth")
	}
	assertNoSecrets(t, err)
}

func TestAddReactionOtherErrorsAreAPIErrors(t *testing.T) {
	f := newFake(t)
	f.handle("reactions.add", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{"ok": false, "error": "message_not_found"})
	})
	err := f.client().AddReaction(t.Context(), "C1", "1.5", "eyes")
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.Code != "message_not_found" {
		t.Fatalf("err = %#v", err)
	}
}

func TestAddReactionRetriesOnceWhenRateLimited(t *testing.T) {
	f := newFake(t)
	var n atomic.Int32
	f.handle("reactions.add", func(w http.ResponseWriter, r *http.Request) {
		if n.Add(1) == 1 {
			w.Header().Set("Retry-After", "0")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		writeJSON(w, map[string]any{"ok": true})
	})
	if err := f.client().AddReaction(t.Context(), "C1", "1.5", "eyes"); err != nil {
		t.Fatal(err)
	}
	if n.Load() != 2 {
		t.Fatalf("calls = %d, want 2", n.Load())
	}
}
