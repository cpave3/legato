package discordbot

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"
)

func TestAForwardCarriesItsMessageSnapshot(t *testing.T) {
	gw := newFakeGateway(t)
	rec := newRecorder()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	errc := startRun(ctx, newTestClient(gw), rec)
	fc := identifyAndReady(t, gw, rec, 1)

	// A forward, in the shape Discord sends it: an empty content, a
	// FORWARD reference to the original and its snapshot, which names no
	// author.
	fc.dispatch(2, "MESSAGE_CREATE", json.RawMessage(`{
	  "id":"m9","channel_id":"dm1","type":0,"content":"","mentions":[],
	  "author":{"id":"cameron"},"timestamp":"2026-11-25T01:00:00+00:00",
	  "message_reference":{"type":1,"message_id":"orig1","channel_id":"alerts","guild_id":"g7"},
	  "message_snapshots":[{"message":{"type":0,"content":"checkout is returning 500s <@bot-1>",
	    "embeds":[],"attachments":[],"timestamp":"2026-11-25T00:59:00+00:00","edited_timestamp":null,
	    "flags":0,"mentions":[{"id":"bot-1"}],"mention_roles":[],"components":[]}}]}`))
	m := rec.message(t)
	if m.Content != "" || m.ReferenceID != "" || len(m.Mentions) != 0 {
		t.Fatalf("message = %+v", m)
	}
	want := []Forward{{Content: "checkout is returning 500s <@bot-1>", ChannelID: "alerts", GuildID: "g7", MessageID: "orig1"}}
	if !reflect.DeepEqual(m.Forwards, want) {
		t.Fatalf("forwards = %+v, want %+v", m.Forwards, want)
	}

	// A reply is not a forward.
	fc.dispatch(3, "MESSAGE_CREATE", json.RawMessage(`{"id":"m10","channel_id":"c1","guild_id":"g7","content":"yes",
	  "author":{"id":"cameron"},"message_reference":{"type":0,"message_id":"m9","channel_id":"c1"}}`))
	if m := rec.message(t); m.Forwards != nil || m.ReferenceID != "m9" {
		t.Fatalf("reply = %+v", m)
	}
	cancel()
	if err := waitRun(t, errc); !errors.Is(err, context.Canceled) {
		t.Fatalf("Run = %v", err)
	}
}
