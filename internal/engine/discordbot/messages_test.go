package discordbot

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// readServer answers GETs from a map of path (with its query) to body; any
// other path is Discord's Unknown Message.
func readServer(t *testing.T, bodies map[string]string) (*Client, *[]string) {
	t.Helper()
	var seen []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := r.URL.Path
		if r.URL.RawQuery != "" {
			key += "?" + r.URL.RawQuery
		}
		seen = append(seen, r.Method+" "+key)
		if r.Header.Get("Authorization") != "Bot "+testToken {
			t.Errorf("auth = %q", r.Header.Get("Authorization"))
		}
		body, ok := bodies[key]
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"message":"Unknown Message","code":10008}`))
			return
		}
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return &Client{Token: testToken, APIBase: srv.URL}, &seen
}

func TestGetMessageReadsAForwardAndTheAuthorsName(t *testing.T) {
	c, seen := readServer(t, map[string]string{
		"/channels/c1/messages/m1": `{"id":"m1","channel_id":"c1","type":0,"content":"",
			"author":{"id":"u1","username":"alice","global_name":"Alice"},
			"timestamp":"2026-11-25T04:12:00.000000+00:00",
			"message_reference":{"type":1,"message_id":"m0","channel_id":"c0","guild_id":"g1"},
			"message_snapshots":[{"message":{"content":"prod is down"}}]}`,
		"/channels/c1/messages/m2": `{"id":"m2","channel_id":"c1","type":19,"content":"see above",
			"author":{"id":"u2","username":"bob"},"timestamp":"2026-11-25T04:13:00Z",
			"message_reference":{"type":0,"message_id":"m1","channel_id":"c1"}}`,
	})
	m, err := c.GetMessage(t.Context(), "c1", "m1")
	if err != nil {
		t.Fatal(err)
	}
	if m.ID != "m1" || m.AuthorID != "u1" || m.AuthorName != "Alice" || m.Type != 0 ||
		!m.Timestamp.Equal(time.Date(2026, 11, 25, 4, 12, 0, 0, time.UTC)) {
		t.Fatalf("message = %+v", m)
	}
	if len(m.Forwards) != 1 || m.Forwards[0] != (Forward{Content: "prod is down", ChannelID: "c0", GuildID: "g1", MessageID: "m0"}) {
		t.Fatalf("forwards = %+v", m.Forwards)
	}
	reply, err := c.GetMessage(t.Context(), "c1", "m2")
	if err != nil || reply.AuthorName != "bob" || reply.ReferenceID != "m1" || reply.Type != 19 {
		t.Fatalf("reply = %+v, %v", reply, err)
	}
	if (*seen)[0] != "GET /channels/c1/messages/m1" {
		t.Fatalf("calls = %v", *seen)
	}
}

func TestGetMessageThatIsGoneIsErrNotFound(t *testing.T) {
	c, _ := readServer(t, nil)
	_, err := c.GetMessage(t.Context(), "c1", "nope")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
	noToken(t, err)
}

func TestListMessagesPagesBackwards(t *testing.T) {
	c, seen := readServer(t, map[string]string{
		"/channels/t1/messages?limit=100": `[{"id":"3","type":0,"content":"c","author":{"id":"u1","username":"a"}},
			{"id":"2","type":0,"content":"b","author":{"id":"u1","username":"a"}}]`,
		"/channels/t1/messages?before=2&limit=50": `[]`,
	})
	msgs, err := c.ListMessages(t.Context(), "t1", "", 100)
	if err != nil || len(msgs) != 2 || msgs[0].ID != "3" || msgs[1].Content != "b" || msgs[0].ChannelID != "t1" {
		t.Fatalf("ListMessages = %+v, %v", msgs, err)
	}
	if msgs, err := c.ListMessages(t.Context(), "t1", "2", 50); err != nil || len(msgs) != 0 {
		t.Fatalf("ListMessages before = %+v, %v", msgs, err)
	}
	if len(*seen) != 2 {
		t.Fatalf("calls = %v", *seen)
	}
}

func TestGetChannelGivesItsParent(t *testing.T) {
	c, _ := readServer(t, map[string]string{
		"/channels/t1": `{"id":"t1","type":11,"parent_id":"c1","guild_id":"g1"}`,
	})
	ch, err := c.GetChannel(t.Context(), "t1")
	if err != nil || ch != (Channel{ID: "t1", Type: 11, ParentID: "c1", GuildID: "g1"}) {
		t.Fatalf("GetChannel = %+v, %v", ch, err)
	}
}
