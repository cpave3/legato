package cli_test

import (
	"context"
	"strings"
	"testing"

	"github.com/cpave3/legato/internal/cli"
	"github.com/cpave3/legato/internal/service"
)

type fakeSpeech struct {
	on    bool
	lines []service.SpokenLine
}

func (f *fakeSpeech) Enabled(context.Context) bool { return f.on }

func (f *fakeSpeech) Say(_ context.Context, line service.SpokenLine) error {
	f.lines = append(f.lines, line)
	return nil
}

// speak is the Reeve's, and offered only while its voice is on.
func TestSpeakIsOfferedOnlyToTheReeveWhileItsVoiceIsOn(t *testing.T) {
	s := newTestStore(t)
	taskID, executionID := seedReeve(t, s)
	other := seedMCPAgent(t, s, "work-1")
	speech := &fakeSpeech{on: true}
	reeve := &cli.MCPReeve{Speech: speech}

	if names := reeveToolNames(t, connectMCP(t, cli.MCPOptions{Store: s, TaskID: taskID, ExecutionID: executionID, Reeve: reeve})); !names["speak"] {
		t.Fatalf("the Reeve lacks speak with its voice on: %v", names)
	}
	if names := reeveToolNames(t, connectMCP(t, cli.MCPOptions{Store: s, TaskID: "work-1", ExecutionID: other, Reeve: reeve})); names["speak"] {
		t.Fatal("another session is offered speak")
	}
	speech.on = false
	if names := reeveToolNames(t, connectMCP(t, cli.MCPOptions{Store: s, TaskID: taskID, ExecutionID: executionID, Reeve: reeve})); names["speak"] {
		t.Fatal("speak is offered with the voice off")
	}
}

// A call queues the line for the calling session and answers at once.
func TestSpeakQueuesTheLineAndAnswersAtOnce(t *testing.T) {
	s := newTestStore(t)
	taskID, executionID := seedReeve(t, s)
	speech := &fakeSpeech{on: true}
	session := connectMCP(t, cli.MCPOptions{Store: s, TaskID: taskID, ExecutionID: executionID, Reeve: &cli.MCPReeve{Speech: speech}})

	got := mcpText(callMCP(t, session, "speak", map[string]any{"text": "The board is clear."}))

	if !strings.Contains(got, "queued") {
		t.Fatalf("result %q", got)
	}
	want := service.SpokenLine{TaskID: taskID, ExecutionID: executionID, Text: "The board is clear."}
	if len(speech.lines) != 1 || speech.lines[0] != want {
		t.Fatalf("queued %+v", speech.lines)
	}
}

// Muted mid-session, speak refuses rather than queue.
func TestSpeakRefusesWhileTheVoiceIsOff(t *testing.T) {
	s := newTestStore(t)
	taskID, executionID := seedReeve(t, s)
	speech := &fakeSpeech{on: true}
	session := connectMCP(t, cli.MCPOptions{Store: s, TaskID: taskID, ExecutionID: executionID, Reeve: &cli.MCPReeve{Speech: speech}})
	speech.on = false

	result := callMCP(t, session, "speak", map[string]any{"text": "Hello."})

	if !result.IsError || len(speech.lines) != 0 {
		t.Fatalf("result %q (error %v), queued %+v", mcpText(result), result.IsError, speech.lines)
	}
}
