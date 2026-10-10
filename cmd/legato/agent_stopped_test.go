package main

import (
	"strings"
	"testing"

	"github.com/cpave3/legato/internal/engine/store"
)

func TestClaudeStopPayloadSaysWhetherBackgroundWorkWillWakeTheSession(t *testing.T) {
	tests := []struct {
		name    string
		payload string
		want    string
	}{
		{
			name:    "a shell is still running",
			payload: `{"hook_event_name":"Stop","background_tasks":[{"id":"bj9","type":"shell","status":"running","command":"sleep 25"}],"session_crons":[]}`,
			want:    store.ActivityBackground,
		},
		{name: "nothing in flight", payload: `{"hook_event_name":"Stop","background_tasks":[],"session_crons":[]}`, want: ""},
		{name: "a Claude Code that does not report background work", payload: `{"hook_event_name":"Stop"}`, want: ""},
		// A Stop hook that fails is shown to the person on every turn, so a
		// payload this cannot read clears the activity as the hook always did.
		{name: "no payload", payload: ``, want: ""},
		{name: "malformed JSON", payload: `{`, want: ""},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := claudeStopActivity(strings.NewReader(test.payload)); got != test.want {
				t.Fatalf("activity = %q, want %q", got, test.want)
			}
		})
	}
}
