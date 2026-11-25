package service

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cpave3/legato/internal/engine/store"
	workflowengine "github.com/cpave3/legato/internal/engine/workflow"
)

// chatWorkflowsFixture writes one workflow file per name, each with the
// given on: block (and the given top-level keys before it), into one
// folder the coordinator discovers.
func chatWorkflowsFixture(t *testing.T, workflows map[string]string) (*store.Store, *WorkflowTriggerCoordinator, *fakeWorkflowExecutor) {
	t.Helper()
	db, err := store.New(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	dir := t.TempDir()
	for name, body := range workflows {
		definition := "schema_version: 1\nname: " + name + "\ntitle: " + strings.ToUpper(name[:1]) + name[1:] + "\n" + body +
			"steps:\n  - {id: say, title: Say, type: chat.message.send, with: {bot: slack, text: hi}}\n"
		if err := os.WriteFile(filepath.Join(dir, name+".yaml"), []byte(definition), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	executor := &fakeWorkflowExecutor{typeName: ChatSendStepType, result: WorkflowExecutionResult{Status: "succeeded"}}
	coordinator := NewWorkflowTriggerCoordinator(db, NewWorkflowService(db, []WorkflowExecutor{executor}), func() workflowengine.DiscoveryResult { return workflowengine.DiscoverPaths(dir) })
	return db, coordinator, executor
}

func chatMention(id, user, text string) ChatEvent {
	e := chatMessage(id, "C1", text)
	e.User, e.Mention = user, true
	return e
}

// requestsFor are a workflow's trigger requests, of any status.
func requestsFor(t *testing.T, db *store.Store, workflow string) []store.WorkflowTriggerRequest {
	t.Helper()
	var out []store.WorkflowTriggerRequest
	for _, request := range requestsWith(t, db, "") {
		if request.WorkflowName == workflow {
			out = append(out, request)
		}
	}
	return out
}

func sendChat(t *testing.T, coordinator *WorkflowTriggerCoordinator, events ...ChatEvent) {
	t.Helper()
	for _, e := range events {
		if err := coordinator.ChatEvent(context.Background(), e); err != nil {
			t.Fatal(err)
		}
	}
}

const (
	investigateOn = "on:\n  chat.mention:\n    bot: slack\n    text: {contains: investigat}\n"
	helpOn        = "on:\n  chat.mention:\n    bot: slack\n    fallback: true\n"
)

func TestAMatchedChatTriggerSkipsTheFallbackAndSaysWhy(t *testing.T) {
	db, coordinator, _ := chatWorkflowsFixture(t, map[string]string{"investigate": investigateOn, "help": helpOn})
	sendChat(t, coordinator, chatMention("1.1", "U1", "<@UBOT> investigate the outage"))
	if started := requestsFor(t, db, "investigate"); len(started) != 1 || started[0].Status != "started" {
		t.Fatalf("investigate requests = %+v", started)
	}
	help := requestsFor(t, db, "help")
	if len(help) != 1 || help[0].Status != "skipped" || help[0].RunID != "" {
		t.Fatalf("help requests = %+v", help)
	}
	if !strings.Contains(help[0].Error, "investigate (chat.mention)") {
		t.Fatalf("the skipped fallback says %q, which does not name investigate", help[0].Error)
	}
}

func TestAMessageNothingMatchedStartsTheFallback(t *testing.T) {
	db, coordinator, _ := chatWorkflowsFixture(t, map[string]string{"investigate": investigateOn, "help": helpOn})
	sendChat(t, coordinator, chatMention("1.1", "U1", "<@UBOT> hello"))
	if requests := requestsFor(t, db, "investigate"); len(requests) != 0 {
		t.Fatalf("investigate requests = %+v", requests)
	}
	help := requestsFor(t, db, "help")
	if len(help) != 1 || help[0].Status != "started" {
		t.Fatalf("help requests = %+v", help)
	}
	if !strings.Contains(help[0].EventJSON, `"fallback":"no other enabled chat trigger on slack matched"`) {
		t.Fatalf("the fallback's event %s does not say why it fired", help[0].EventJSON)
	}
}

func TestPausedChatTriggersDoNotStopTheFallback(t *testing.T) {
	db, coordinator, _ := chatWorkflowsFixture(t, map[string]string{
		"investigate": investigateOn,
		"fix":         "on:\n  chat.mention:\n    bot: slack\n    enabled: false\n    text: {contains: investigat}\n",
		"help":        helpOn,
	})
	if err := coordinator.SetTriggerEnabled(context.Background(), "investigate", workflowengine.TriggerChatMention, false); err != nil {
		t.Fatal(err)
	}
	sendChat(t, coordinator, chatMention("1.1", "U1", "<@UBOT> investigate the outage"))
	if help := requestsFor(t, db, "help"); len(help) != 1 || help[0].Status != "started" {
		t.Fatalf("help requests = %+v", help)
	}
}

func TestAPausedFallbackMakesNoRequest(t *testing.T) {
	db, coordinator, _ := chatWorkflowsFixture(t, map[string]string{"investigate": investigateOn, "help": helpOn})
	if err := coordinator.SetTriggerEnabled(context.Background(), "help", workflowengine.TriggerChatMention, false); err != nil {
		t.Fatal(err)
	}
	sendChat(t, coordinator, chatMention("1.1", "U1", "<@UBOT> hello"), chatMention("1.2", "U1", "<@UBOT> investigate"))
	if help := requestsFor(t, db, "help"); len(help) != 0 {
		t.Fatalf("a paused fallback made requests %+v", help)
	}
}

func TestTheUserFilterAppliesToFallbacks(t *testing.T) {
	db, coordinator, _ := chatWorkflowsFixture(t, map[string]string{
		"investigate": investigateOn,
		"help":        "on:\n  chat.mention:\n    bot: slack\n    fallback: true\n    user: U2\n",
	})
	sendChat(t, coordinator, chatMention("1.1", "U1", "<@UBOT> hello"))
	if help := requestsFor(t, db, "help"); len(help) != 0 {
		t.Fatalf("a fallback for U2 answered U1: %+v", help)
	}
	sendChat(t, coordinator, chatMention("1.2", "U2", "<@UBOT> hello"))
	if help := requestsFor(t, db, "help"); len(help) != 1 || help[0].Status != "started" {
		t.Fatalf("help requests = %+v", help)
	}
}

func TestAChatMessageTriggerStopsAMentionFallback(t *testing.T) {
	db, coordinator, _ := chatWorkflowsFixture(t, map[string]string{
		"investigate": "on:\n  chat.message:\n    bot: slack\n    text: {contains: investigat}\n",
		"help":        helpOn,
	})
	sendChat(t, coordinator, chatMention("1.1", "U1", "<@UBOT> investigate the outage"))
	help := requestsFor(t, db, "help")
	if len(help) != 1 || help[0].Status != "skipped" || !strings.Contains(help[0].Error, "investigate (chat.message)") {
		t.Fatalf("help requests = %+v", help)
	}
}

func TestATriggerOnAnotherBotDoesNotStopTheFallback(t *testing.T) {
	db, coordinator, _ := chatWorkflowsFixture(t, map[string]string{
		"investigate": "on:\n  chat.mention:\n    bot: discord\n",
		"help":        helpOn,
	})
	sendChat(t, coordinator, chatMention("1.1", "U1", "<@UBOT> investigate"))
	if help := requestsFor(t, db, "help"); len(help) != 1 || help[0].Status != "started" {
		t.Fatalf("help requests = %+v", help)
	}
}

func TestEveryFallbackFiresWhenNothingMatched(t *testing.T) {
	db, coordinator, _ := chatWorkflowsFixture(t, map[string]string{"investigate": investigateOn, "help": helpOn, "log": helpOn})
	sendChat(t, coordinator, chatMention("1.1", "U1", "<@UBOT> hello"))
	if n := len(requestsWith(t, db, "started")); n != 2 {
		t.Fatalf("started = %d, want both fallbacks", n)
	}
}

func TestAHeldChatTriggerStillStopsTheFallback(t *testing.T) {
	db, coordinator, _ := chatWorkflowsFixture(t, map[string]string{
		"investigate": "on:\n  chat.mention:\n    bot: slack\n    max_runs_per_day: 1\n    text: {contains: investigat}\n",
		"help":        helpOn,
	})
	sendChat(t, coordinator, chatMention("1.1", "U1", "<@UBOT> investigate"), chatMention("1.2", "U1", "<@UBOT> investigate again"))
	if held := requestsWith(t, db, "held"); len(held) != 1 || held[0].WorkflowName != "investigate" {
		t.Fatalf("held = %+v", held)
	}
	if skipped := requestsWith(t, db, "skipped"); len(skipped) != 2 {
		t.Fatalf("skipped = %+v, want help skipped for both messages", skipped)
	}
}

func TestARedeliveredMessageMakesNoSecondFallbackRequest(t *testing.T) {
	db, coordinator, _ := chatWorkflowsFixture(t, map[string]string{"investigate": investigateOn, "help": helpOn})
	sendChat(t, coordinator,
		chatMention("1.1", "U1", "<@UBOT> hello"), chatMention("1.1", "U1", "<@UBOT> hello"),
		chatMention("1.2", "U1", "<@UBOT> investigate"), chatMention("1.2", "U1", "<@UBOT> investigate"))
	if n := len(requestsWith(t, db, "")); n != 3 {
		t.Fatalf("requests = %d, want 3 (help started, investigate started, help skipped)", n)
	}
}

func TestEventCommandsListsTheChatHelpWorkflowsOnTheBot(t *testing.T) {
	long := strings.Repeat("x", 300)
	db, coordinator, _ := chatWorkflowsFixture(t, map[string]string{
		"investigate": "description: |\n  Investigates an outage\n  and writes it up\nchat_help: true\n" + investigateOn,
		"fix":         "description: " + long + "\nchat_help: true\non:\n  chat.message:\n    bot: slack\n    text: {contains: fix}\n",
		"deploy":      "chat_help: true\non:\n  chat.mention:\n    bot: slack\n    text: {contains: deploy}\n",
		"paused":      "chat_help: true\non:\n  chat.mention:\n    bot: slack\n    enabled: false\n",
		"elsewhere":   "chat_help: true\non:\n  chat.mention:\n    bot: discord\n",
		"unlisted":    "on:\n  chat.mention:\n    bot: slack\n    text: {contains: unlisted}\n",
		"help": "chat_help: true\ninputs:\n  commands: {type: string}\n" +
			"on:\n  chat.mention:\n    bot: slack\n    fallback: true\n    inputs: {commands: \"${{ event.commands }}\"}\n",
	})
	sendChat(t, coordinator, chatMention("1.1", "U1", "<@UBOT> hello"))
	help := requestsFor(t, db, "help")
	if len(help) != 1 || help[0].Status != "started" {
		t.Fatalf("help requests = %+v", help)
	}
	run, err := db.FindWorkflowRunByTriggerRequest(context.Background(), help[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	var inputs struct{ Commands string }
	if err := json.Unmarshal([]byte(run.Run.InputsJSON), &inputs); err != nil {
		t.Fatal(err)
	}
	want := "• Deploy\n• Fix — " + strings.Repeat("x", 199) + "…\n• Investigate — Investigates an outage"
	if inputs.Commands != want {
		t.Fatalf("commands =\n%s\nwant\n%s", inputs.Commands, want)
	}
}

func TestEventCommandsIsEmptyWithNoChatHelpWorkflows(t *testing.T) {
	db, coordinator, _ := chatWorkflowsFixture(t, map[string]string{
		"help": "inputs:\n  commands: {type: string}\n" +
			"on:\n  chat.mention:\n    bot: slack\n    inputs: {commands: \"${{ event.commands }}\"}\n",
	})
	sendChat(t, coordinator, chatMention("1.1", "U1", "<@UBOT> hello"))
	if help := requestsFor(t, db, "help"); len(help) != 1 || help[0].Status != "started" || !strings.Contains(help[0].InputsJSON, `"commands":""`) {
		t.Fatalf("help requests = %+v", help)
	}
}

// commandsFor is ${{ event.commands }} as a help fallback on bot received
// it, with count chat_help workflows of 200-character descriptions on the
// bot.
func commandsFor(t *testing.T, bot string, count int) string {
	t.Helper()
	workflows := map[string]string{
		"help": "inputs:\n  commands: {type: string}\n" +
			"on:\n  chat.mention:\n    bot: " + bot + "\n    fallback: true\n    inputs: {commands: \"${{ event.commands }}\"}\n",
	}
	for i := 0; i < count; i++ {
		workflows[fmt.Sprintf("cmd%02d", i)] = "description: " + strings.Repeat("x", 200) + "\nchat_help: true\non:\n  chat.message:\n    bot: " + bot + "\n    text: {contains: never-said}\n"
	}
	db, coordinator, _ := chatWorkflowsFixture(t, workflows)
	e := chatMention("1.1", "U1", "<@UBOT> hello")
	e.Bot, e.Platform = bot, bot
	sendChat(t, coordinator, e)
	help := requestsFor(t, db, "help")
	if len(help) != 1 {
		t.Fatalf("help requests = %+v", help)
	}
	var inputs struct{ Commands string }
	if err := json.Unmarshal([]byte(help[0].InputsJSON), &inputs); err != nil {
		t.Fatal(err)
	}
	return inputs.Commands
}

func TestEventCommandsStaysWithinItsBoundWithTheSuffix(t *testing.T) {
	// 21 lines of 213 bytes: 19 fit in 4 KiB, and the suffix must too.
	commands := commandsFor(t, "slack", 21)
	if len(commands) > 4<<10 || !strings.HasSuffix(commands, " more") {
		t.Fatalf("commands are %d bytes, ending %q", len(commands), commands[len(commands)-20:])
	}
}

func TestEventCommandsFitsADiscordMessage(t *testing.T) {
	// Discord refuses a message over 2000 characters; the list leaves room
	// for the reply's own words.
	commands := commandsFor(t, "discord", 21)
	if len(commands) > 1500 || !strings.HasSuffix(commands, " more") {
		t.Fatalf("commands are %d bytes, ending %q", len(commands), commands[len(commands)-20:])
	}
}
