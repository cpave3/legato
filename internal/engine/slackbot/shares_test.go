package slackbot

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"nhooyr.io/websocket"
)

// runEnvelopes sends each raw Socket Mode envelope to a client and returns
// the events it handed on.
func runEnvelopes(t *testing.T, envelopes ...string) []Event {
	t.Helper()
	f := newFake(t)
	f.socket = func(ctx context.Context, c *websocket.Conn) {
		send(ctx, t, c, map[string]any{"type": "hello"})
		for _, raw := range envelopes {
			send(ctx, t, c, json.RawMessage(raw))
			readAck(ctx, t, c)
		}
		_ = c.Close(websocket.StatusNormalClosure, "")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	var events []Event
	_ = f.client().Run(ctx, func(e Event) { events = append(events, e) }, nil)
	return events
}

// A forward to the bot's DM, in the shape Slack sends it: no subtype, an
// empty text, the forwarded message in a share attachment.
const forwardToDM = `{"type":"events_api","envelope_id":"e1","payload":{"event":{
  "type":"message","channel":"D0BOTDM","channel_type":"im","user":"UCAMERON",
  "text":"","ts":"1724898300.000100","event_ts":"1724898300.000100",
  "attachments":[{"is_share":true,"is_msg_unfurl":true,"ts":"1724898240.000200",
    "author_id":"UALICE","author_name":"alice","author_link":"https://example.slack.com/team/UALICE",
    "channel_id":"C0ALERTS","channel_team":"T1","channel_name":"alerts","footer":"Posted in #alerts",
    "from_url":"https://example.slack.com/archives/C0ALERTS/p1724898240000200",
    "text":"checkout is returning 500s","fallback":"[August 29th, 2024 10:24 AM] alice: checkout is returning 500s",
    "mrkdwn_in":["text"],"color":"D0D0D0",
    "message_blocks":[{"team":"T1","channel":"C0ALERTS","ts":"1724898240.000200","message":{"blocks":[
      {"type":"rich_text","block_id":"b1","elements":[{"type":"rich_text_section","elements":[{"type":"text","text":"checkout is returning 500s"}]}]}]}}]}]
}}}`

func TestAForwardToADMCarriesTheSharedMessage(t *testing.T) {
	events := runEnvelopes(t, forwardToDM)
	want := []Event{{
		Type: "message", Channel: "D0BOTDM", ChannelType: "im", User: "UCAMERON",
		TS: "1724898300.000100", EventTS: "1724898300.000100",
		Shares: []Share{{
			AuthorID: "UALICE", AuthorName: "alice", ChannelID: "C0ALERTS", ChannelName: "alerts",
			FromURL: "https://example.slack.com/archives/C0ALERTS/p1724898240000200", TS: "1724898240.000200",
			Text: "checkout is returning 500s",
		}},
	}}
	if !reflect.DeepEqual(events, want) {
		t.Fatalf("events = %+v\nwant %+v", events, want)
	}
}

func TestAForwardWithANoteKeepsTheNoteAsItsText(t *testing.T) {
	events := runEnvelopes(t, `{"type":"events_api","envelope_id":"e1","payload":{"event":{
	  "type":"message","channel":"C1","channel_type":"channel","user":"UCAMERON",
	  "text":"<@UBOT> can you look at this?","ts":"2.1",
	  "blocks":[{"type":"rich_text","block_id":"n","elements":[{"type":"rich_text_section","elements":[
	    {"type":"user","user_id":"UBOT"},{"type":"text","text":" can you look at this?"}]}]}],
	  "attachments":[{"is_share":true,"is_msg_unfurl":true,"author_name":"lev","channel_id":"C9",
	    "from_url":"https://example.slack.com/archives/C9/p1","ts":"1.5","text":"123"}]}}}`)
	if len(events) != 1 {
		t.Fatalf("events = %+v", events)
	}
	e := events[0]
	if e.Text != "<@UBOT> can you look at this?" || len(e.Shares) != 1 || e.Shares[0].Text != "123" ||
		e.Shares[0].AuthorName != "lev" || e.Shares[0].ChannelName != "" || e.Shares[0].FromURL != "https://example.slack.com/archives/C9/p1" {
		t.Fatalf("event = %+v", e)
	}
}

func TestAShareWithNoTextIsReadFromItsBlocks(t *testing.T) {
	events := runEnvelopes(t, `{"type":"events_api","envelope_id":"e1","payload":{"event":{
	  "type":"message","channel":"D1","channel_type":"im","user":"U1","text":"","ts":"3.1",
	  "attachments":[{"is_share":true,"from_url":"https://x/p","fallback":"[then] bob: the fallback",
	    "message_blocks":[{"message":{"blocks":[
	      {"type":"rich_text","elements":[
	        {"type":"rich_text_section","elements":[
	          {"type":"text","text":"deploy "},{"type":"link","url":"https://ci/1","text":"build 1"},
	          {"type":"text","text":" failed for "},{"type":"user","user_id":"U7"},
	          {"type":"text","text":" in "},{"type":"channel","channel_id":"C3"},{"type":"text","text":" "},
	          {"type":"emoji","name":"fire"},{"type":"link","url":"https://bare"},{"type":"mystery","text":"x"}]},
	        {"type":"rich_text_list","style":"bullet","elements":[
	          {"type":"rich_text_section","elements":[{"type":"text","text":"one"}]},
	          {"type":"rich_text_section","elements":[{"type":"text","text":"two"}]}]},
	        {"type":"rich_text_preformatted","elements":[{"type":"text","text":"panic: nil map"}]},
	        {"type":"rich_text_quote","elements":[{"type":"text","text":"quoted"}]}]}]}}]},
	    {"is_share":true,"from_url":"https://x/q","fallback":"[then] bob: only the fallback"}]}}}`)
	if len(events) != 1 || len(events[0].Shares) != 2 {
		t.Fatalf("events = %+v", events)
	}
	want := "deploy build 1 failed for <@U7> in <#C3> :fire:https://bare\n- one\n- two\npanic: nil map\nquoted"
	if got := events[0].Shares[0].Text; got != want {
		t.Errorf("rendered blocks = %q\nwant %q", got, want)
	}
	if got := events[0].Shares[1].Text; got != "[then] bob: only the fallback" {
		t.Errorf("fallback = %q", got)
	}
}

func TestAnAttachmentThatSharesNothingIsNotAShare(t *testing.T) {
	events := runEnvelopes(t, `{"type":"events_api","envelope_id":"e1","payload":{"event":{
	  "type":"message","subtype":"bot_message","bot_id":"B9","channel":"C1","text":"build","ts":"4.1",
	  "attachments":[{"color":"good","text":"passed","fallback":"passed"}]}}}`)
	if len(events) != 1 || events[0].Text != "build" || events[0].Shares != nil {
		t.Fatalf("events = %+v", events)
	}
}

func TestAMentionCarriesItsShares(t *testing.T) {
	events := runEnvelopes(t, `{"type":"events_api","envelope_id":"e1","payload":{"event":{
	  "type":"app_mention","channel":"C1","user":"U1","text":"<@UBOT> look","ts":"5.1",
	  "attachments":[{"is_msg_unfurl":true,"author_name":"al","text":"down"}]}}}`)
	if len(events) != 1 || events[0].Type != "mention" || len(events[0].Shares) != 1 || events[0].Shares[0].Text != "down" {
		t.Fatalf("events = %+v", events)
	}
}
