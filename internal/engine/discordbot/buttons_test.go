package discordbot

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestCreateMessageWithButtonsSendsAnActionRow(t *testing.T) {
	c, calls := restServer(t, func(_ int, w http.ResponseWriter) {
		_, _ = w.Write([]byte(`{"id":"new-1"}`))
	})
	_, err := c.CreateMessage(context.Background(), "c1", "Findings", "m0",
		Button{Label: "🛠 Fix it", Value: "fix:T1", Style: "primary"},
		Button{Label: "Ticket", Value: "ticket:T1"},
		Button{Label: "Dismiss", Value: "dismiss:T1", Style: "danger"})
	if err != nil {
		t.Fatal(err)
	}
	call := <-calls
	want := `[{"type":1,"components":[` +
		`{"type":2,"style":1,"label":"🛠 Fix it","custom_id":"fix:T1"},` +
		`{"type":2,"style":2,"label":"Ticket","custom_id":"ticket:T1"},` +
		`{"type":2,"style":4,"label":"Dismiss","custom_id":"dismiss:T1"}]}]`
	var w any
	_ = json.Unmarshal([]byte(want), &w)
	if !reflect.DeepEqual(call.body["components"], w) {
		t.Errorf("components = %v\nwant %s", call.body["components"], want)
	}
	if am := call.body["allowed_mentions"].(map[string]any); len(am["parse"].([]any)) != 0 {
		t.Errorf("allowed_mentions = %v", am)
	}
}

func TestCreateMessageWithoutButtonsSendsNoComponents(t *testing.T) {
	c, calls := restServer(t, func(_ int, w http.ResponseWriter) {
		_, _ = w.Write([]byte(`{"id":"new-1"}`))
	})
	if _, err := c.CreateMessage(context.Background(), "c1", "hi", ""); err != nil {
		t.Fatal(err)
	}
	if _, ok := (<-calls).body["components"]; ok {
		t.Error("a message without buttons has components")
	}
}

func buttonInteraction(user map[string]any) map[string]any {
	d := map[string]any{
		"id": "int-1", "application_id": "app-1", "type": 3, "token": "interaction-token-abc",
		"channel_id": "c1", "guild_id": "g1", "channel": map[string]any{"id": "c1", "type": 0},
		"message": map[string]any{
			"id": "m9", "channel_id": "c1", "content": "Findings",
			"components": []any{map[string]any{"type": 1, "components": []any{
				map[string]any{"type": 2, "style": 1, "label": "🛠 Fix it", "custom_id": "fix:T1"},
				map[string]any{"type": 2, "style": 4, "label": "Dismiss", "custom_id": "dismiss:T1"},
			}}},
		},
		"data": map[string]any{"custom_id": "fix:T1", "component_type": 2},
	}
	for k, v := range user {
		d[k] = v
	}
	return d
}

type callbackRecorder struct {
	calls chan restCall
}

// interactionsREST answers the interaction callback and records it.
func interactionsREST(gw *fakeGateway) *callbackRecorder {
	rec := &callbackRecorder{calls: make(chan restCall, 8)}
	gw.rest = func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		var body map[string]any
		_ = json.Unmarshal(b, &body)
		rec.calls <- restCall{method: r.Method, path: r.URL.Path, auth: r.Header.Get("Authorization"), escaped: r.URL.EscapedPath(), body: body}
		w.WriteHeader(http.StatusNoContent)
	}
	return rec
}

func TestAButtonClickIsAnsweredThenHandedOn(t *testing.T) {
	gw := newFakeGateway(t)
	callbacks := interactionsREST(gw)
	c := newTestClient(gw)
	rec := newRecorder()
	clicks := make(chan Interaction, 4)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	errc := make(chan error, 1)
	go func() {
		errc <- c.Serve(ctx, Handlers{Message: rec.onMessage, Ready: rec.onReady, Interaction: func(i Interaction) {
			// The deferred response is already on its way.
			select {
			case call := <-callbacks.calls:
				if call.method != http.MethodPost || call.path != "/interactions/"+i.ID+"/interaction-token-abc/callback" {
					t.Errorf("callback = %+v", call)
				}
				if call.body["type"] != float64(6) {
					t.Errorf("callback body = %v", call.body)
				}
			case <-time.After(2 * time.Second):
				t.Error("no deferred response before the click was handed on")
			}
			clicks <- i
		}})
	}()
	fc := identifyAndReady(t, gw, rec, 1)

	fc.dispatch(2, "INTERACTION_CREATE", buttonInteraction(map[string]any{"member": map[string]any{"user": map[string]any{"id": "u7"}}}))
	var got Interaction
	select {
	case got = <-clicks:
	case <-time.After(3 * time.Second):
		t.Fatal("no click")
	}
	want := Interaction{
		ID: "int-1", Token: "interaction-token-abc", ApplicationID: "app-1",
		ChannelID: "c1", GuildID: "g1", UserID: "u7", MessageID: "m9", Content: "Findings",
		Value: "fix:T1", Label: "🛠 Fix it",
		Buttons: []Button{{Label: "🛠 Fix it", Value: "fix:T1", Style: "primary"}, {Label: "Dismiss", Value: "dismiss:T1", Style: "danger"}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("click = %+v\nwant  %+v", got, want)
	}

	// In a DM the user is at the top, and a click in a thread says so.
	dm := buttonInteraction(map[string]any{"user": map[string]any{"id": "u8"}, "guild_id": nil,
		"channel": map[string]any{"id": "c1", "type": 11}, "id": "int-2"})
	fc.dispatch(3, "INTERACTION_CREATE", dm)
	select {
	case got = <-clicks:
	case <-time.After(3 * time.Second):
		t.Fatal("no second click")
	}
	if got.UserID != "u8" || got.GuildID != "" || !got.InThread || got.ID != "int-2" {
		t.Errorf("dm click = %+v", got)
	}

	// Other interactions (a slash command, a select menu) are not clicks.
	cmd := buttonInteraction(nil)
	cmd["type"] = 2
	fc.dispatch(4, "INTERACTION_CREATE", cmd)
	menu := buttonInteraction(nil)
	menu["data"] = map[string]any{"custom_id": "x", "component_type": 3}
	fc.dispatch(5, "INTERACTION_CREATE", menu)
	fc.dispatch(6, "MESSAGE_CREATE", map[string]any{"id": "m2", "channel_id": "c1", "author": map[string]any{"id": "u1"}, "content": "after"})
	rec.message(t)
	select {
	case i := <-clicks:
		t.Errorf("a click from %+v", i)
	default:
	}
	cancel()
	_ = waitRun(t, errc)
}

func TestSettleDisablesTheButtonsAndAddsTheLine(t *testing.T) {
	c, calls := restServer(t, func(_ int, w http.ResponseWriter) {
		_, _ = w.Write([]byte(`{"id":"m9"}`))
	})
	i := Interaction{ChannelID: "c1", MessageID: "m9", Content: "Findings", Token: "interaction-token-abc",
		Buttons: []Button{{Label: "Fix", Value: "fix:T1", Style: "primary"}, {Label: "No", Value: "no"}}}
	if err := c.Settle(context.Background(), i, "Fix — chosen by <@u7>"); err != nil {
		t.Fatal(err)
	}
	call := <-calls
	if call.method != http.MethodPatch || call.path != "/channels/c1/messages/m9" || call.auth != "Bot "+testToken {
		t.Fatalf("call = %+v", call)
	}
	if call.body["content"] != "Findings\n-# Fix — chosen by <@u7>" {
		t.Errorf("content = %q", call.body["content"])
	}
	want := `[{"type":1,"components":[` +
		`{"type":2,"style":1,"label":"Fix","custom_id":"fix:T1","disabled":true},` +
		`{"type":2,"style":2,"label":"No","custom_id":"no","disabled":true}]}]`
	var w any
	_ = json.Unmarshal([]byte(want), &w)
	if !reflect.DeepEqual(call.body["components"], w) {
		t.Errorf("components = %v", call.body["components"])
	}
	if am := call.body["allowed_mentions"].(map[string]any); len(am["parse"].([]any)) != 0 {
		t.Errorf("allowed_mentions = %v", am)
	}
}

func TestSettleKeepsTheContentWithinTheLimit(t *testing.T) {
	c, calls := restServer(t, func(_ int, w http.ResponseWriter) {
		_, _ = w.Write([]byte(`{"id":"m9"}`))
	})
	i := Interaction{ChannelID: "c1", MessageID: "m9", Content: strings.Repeat("x", MaxContentLength)}
	if err := c.Settle(context.Background(), i, "chosen"); err != nil {
		t.Fatal(err)
	}
	content := (<-calls).body["content"].(string)
	if n := len([]rune(content)); n > MaxContentLength {
		t.Errorf("content of %d characters", n)
	}
	if !strings.HasSuffix(content, "…\n-# chosen") {
		t.Errorf("content ends %q", content[len(content)-20:])
	}
}

func TestRespondDeferredUpdateNeverShowsTheInteractionToken(t *testing.T) {
	c := &Client{Token: testToken, APIBase: "http://127.0.0.1:1"}
	err := c.RespondDeferredUpdate(context.Background(), "int-1", "interaction-token-abc")
	if err == nil {
		t.Fatal("no error from an unreachable server")
	}
	if strings.Contains(err.Error(), "interaction-token-abc") {
		t.Errorf("error shows the interaction token: %v", err)
	}
	noToken(t, err)
	if errors.Is(err, ErrAuth) {
		t.Errorf("err = %v", err)
	}
}
