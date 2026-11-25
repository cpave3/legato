package slackbot

import (
	"errors"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

// formOf is the form of the call the fake is answering: it has read the
// body already, so the form comes from what it recorded.
func formOf(t *testing.T, f *fakeSlack, r *http.Request) url.Values {
	t.Helper()
	if ct := r.Header.Get("Content-Type"); !strings.HasPrefix(ct, "application/x-www-form-urlencoded") {
		t.Errorf("content type %q, want a form", ct)
	}
	method := strings.TrimPrefix(r.URL.Path, "/")
	f.mu.Lock()
	bodies := f.bodies[method]
	f.mu.Unlock()
	form, err := url.ParseQuery(bodies[len(bodies)-1])
	if err != nil {
		t.Fatalf("parse form: %v", err)
	}
	return form
}

func TestRepliesReadsTheThreadWithAFormAndPagesOnTheCursor(t *testing.T) {
	f := newFake(t)
	var forms []url.Values
	f.handle("conversations.replies", func(w http.ResponseWriter, r *http.Request) {
		form := formOf(t, f, r)
		forms = append(forms, form)
		if form.Get("cursor") == "" {
			writeJSON(w, map[string]any{"ok": true, "has_more": true,
				"response_metadata": map[string]any{"next_cursor": "page2"},
				"messages": []any{
					map[string]any{"type": "message", "user": "U1", "text": "", "ts": "100.000001", "thread_ts": "100.000001",
						"attachments": []any{map[string]any{"is_share": true, "author_id": "U9", "author_name": "alice",
							"channel_name": "alerts", "from_url": "https://x.slack.com/archives/C9/p1", "text": "prod is down"}}},
					map[string]any{"type": "message", "subtype": "channel_join", "user": "U3", "text": "joined", "ts": "100.5", "thread_ts": "100.000001"},
					map[string]any{"type": "message", "user": "U2", "text": "first reply", "ts": "101.0", "thread_ts": "100.000001"},
				}})
			return
		}
		writeJSON(w, map[string]any{"ok": true, "messages": []any{
			map[string]any{"type": "message", "subtype": "bot_message", "bot_id": "B1", "username": "deploybot", "text": "deployed", "ts": "102.0", "thread_ts": "100.000001"},
		}})
	})
	msgs, err := f.client().Replies(t.Context(), "C1", "100.000001", 1000)
	if err != nil {
		t.Fatal(err)
	}
	if len(forms) != 2 || forms[0].Get("channel") != "C1" || forms[0].Get("ts") != "100.000001" ||
		forms[0].Get("limit") == "" || forms[1].Get("cursor") != "page2" {
		t.Fatalf("forms = %v", forms)
	}
	if got := f.auths["conversations.replies"]; got != "Bearer "+botToken {
		t.Fatalf("auth = %q", got)
	}
	if len(msgs) != 3 {
		t.Fatalf("messages = %+v", msgs)
	}
	parent := msgs[0]
	if parent.TS != "100.000001" || parent.User != "U1" || len(parent.Shares) != 1 || parent.Shares[0].Text != "prod is down" {
		t.Fatalf("parent = %+v", parent)
	}
	if msgs[1].Text != "first reply" || msgs[1].User != "U2" {
		t.Fatalf("reply = %+v", msgs[1])
	}
	if msgs[2].BotID != "B1" || msgs[2].Username != "deploybot" {
		t.Fatalf("bot reply = %+v", msgs[2])
	}
}

func TestRepliesKeepsTheParentWhateverItsSubtype(t *testing.T) {
	f := newFake(t)
	f.handle("conversations.replies", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{"ok": true, "messages": []any{
			map[string]any{"type": "message", "subtype": "channel_topic", "user": "U1", "text": "topic", "ts": "1.0", "thread_ts": "1.0"},
		}})
	})
	msgs, err := f.client().Replies(t.Context(), "C1", "1.0", 1000)
	if err != nil || len(msgs) != 1 || msgs[0].Text != "topic" {
		t.Fatalf("Replies = %+v, %v", msgs, err)
	}
}

func TestRepliesStopsAtMax(t *testing.T) {
	f := newFake(t)
	f.handle("conversations.replies", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{"ok": true, "has_more": true,
			"response_metadata": map[string]any{"next_cursor": "more"},
			"messages": []any{
				map[string]any{"type": "message", "user": "U1", "text": "a", "ts": "1.0"},
				map[string]any{"type": "message", "user": "U1", "text": "b", "ts": "2.0"},
				map[string]any{"type": "message", "user": "U1", "text": "c", "ts": "3.0"},
			}})
	})
	msgs, err := f.client().Replies(t.Context(), "C1", "1.0", 2)
	if err != nil || len(msgs) != 2 || msgs[1].Text != "b" {
		t.Fatalf("Replies = %+v, %v", msgs, err)
	}
	if n := f.callCount("conversations.replies"); n != 1 {
		t.Fatalf("calls = %d, want 1", n)
	}
}

func TestRepliesRetriesOnceWhenRateLimited(t *testing.T) {
	f := newFake(t)
	f.handle("conversations.replies", func(w http.ResponseWriter, r *http.Request) {
		if f.callCount("conversations.replies") == 1 {
			w.Header().Set("Retry-After", "0")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		writeJSON(w, map[string]any{"ok": true, "messages": []any{map[string]any{"type": "message", "user": "U1", "text": "a", "ts": "1.0"}}})
	})
	msgs, err := f.client().Replies(t.Context(), "C1", "1.0", 10)
	if err != nil || len(msgs) != 1 {
		t.Fatalf("Replies = %+v, %v", msgs, err)
	}
}

func TestUserNamePrefersTheDisplayName(t *testing.T) {
	f := newFake(t)
	f.handle("users.info", func(w http.ResponseWriter, r *http.Request) {
		switch formOf(t, f, r).Get("user") {
		case "U1":
			writeJSON(w, map[string]any{"ok": true, "user": map[string]any{"name": "al", "real_name": "Alice A",
				"profile": map[string]any{"display_name": "ally", "real_name": "Alice A"}}})
		default:
			writeJSON(w, map[string]any{"ok": true, "user": map[string]any{"name": "bo", "real_name": "Bob B",
				"profile": map[string]any{"display_name": ""}}})
		}
	})
	c := f.client()
	if name, err := c.UserName(t.Context(), "U1"); err != nil || name != "ally" {
		t.Fatalf("UserName(U1) = %q, %v", name, err)
	}
	if name, err := c.UserName(t.Context(), "U2"); err != nil || name != "Bob B" {
		t.Fatalf("UserName(U2) = %q, %v", name, err)
	}
}

func TestUserNameWithoutTheScopeIsMissingScope(t *testing.T) {
	f := newFake(t)
	f.handle("users.info", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{"ok": false, "error": "missing_scope", "needed": "users:read", "provided": "chat:write"})
	})
	_, err := f.client().UserName(t.Context(), "U1")
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.Code != "missing_scope" || apiErr.Needed != "users:read" {
		t.Fatalf("err = %v", err)
	}
	assertNoSecrets(t, err)
}
