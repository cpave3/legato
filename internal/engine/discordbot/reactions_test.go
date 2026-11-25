package discordbot

import (
	"context"
	"errors"
	"net/http"
	"testing"
)

func TestAddReaction(t *testing.T) {
	for _, tc := range []struct{ emoji, path string }{
		{"👀", "/channels/c1/messages/m1/reactions/%F0%9F%91%80/@me"},
		{"✅", "/channels/c1/messages/m1/reactions/%E2%9C%85/@me"},
		{"party:123", "/channels/c1/messages/m1/reactions/party:123/@me"},
	} {
		t.Run(tc.emoji, func(t *testing.T) {
			c, calls := restServer(t, func(_ int, w http.ResponseWriter) { w.WriteHeader(http.StatusNoContent) })
			if err := c.AddReaction(context.Background(), "c1", "m1", tc.emoji); err != nil {
				t.Fatal(err)
			}
			call := <-calls
			if call.method != http.MethodPut || call.escaped != tc.path || call.auth != "Bot "+testToken {
				t.Fatalf("call = %+v, want PUT %s", call, tc.path)
			}
		})
	}
}

// Discord answers 204 to a reaction the bot has already left; that is success,
// and no check is made first.
func TestAddReactionTwiceIsSuccess(t *testing.T) {
	c, calls := restServer(t, func(_ int, w http.ResponseWriter) { w.WriteHeader(http.StatusNoContent) })
	for range 2 {
		if err := c.AddReaction(context.Background(), "c1", "m1", "👀"); err != nil {
			t.Fatal(err)
		}
	}
	if len(calls) != 2 {
		t.Fatalf("%d calls, want 2", len(calls))
	}
}

func TestRemoveReaction(t *testing.T) {
	c, calls := restServer(t, func(_ int, w http.ResponseWriter) { w.WriteHeader(http.StatusNoContent) })
	if err := c.RemoveReaction(context.Background(), "c1", "m1", "👀"); err != nil {
		t.Fatal(err)
	}
	call := <-calls
	if call.method != http.MethodDelete || call.escaped != "/channels/c1/messages/m1/reactions/%F0%9F%91%80/@me" {
		t.Fatalf("call = %+v", call)
	}
}

func TestAddReactionMissingPermissions(t *testing.T) {
	c, _ := restServer(t, func(_ int, w http.ResponseWriter) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"message":"Missing Permissions","code":50013}`))
	})
	err := c.AddReaction(context.Background(), "c1", "m1", "👀")
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.Code != CodeMissingPermissions {
		t.Fatalf("err = %v", err)
	}
	noToken(t, err)
}

func TestAddReactionRateLimitedRetriesOnce(t *testing.T) {
	c, calls := restServer(t, func(n int, w http.ResponseWriter) {
		if n == 1 {
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte(`{"retry_after":0.01}`))
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
	if err := c.AddReaction(context.Background(), "c1", "m1", "👀"); err != nil {
		t.Fatal(err)
	}
	if len(calls) != 2 {
		t.Fatalf("%d calls, want 2", len(calls))
	}
}

func TestReactionChecks(t *testing.T) {
	c, calls := restServer(t, func(_ int, w http.ResponseWriter) {})
	for _, args := range [][3]string{{"", "m1", "👀"}, {"c1", "", "👀"}, {"c1", "m1", ""}} {
		if err := c.AddReaction(context.Background(), args[0], args[1], args[2]); err == nil {
			t.Errorf("AddReaction%v accepted", args)
		}
	}
	if len(calls) != 0 {
		t.Fatalf("%d HTTP calls made", len(calls))
	}
}
