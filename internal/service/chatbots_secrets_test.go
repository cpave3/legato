package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cpave3/legato/config"
	"github.com/cpave3/legato/internal/engine/credential"
	"github.com/cpave3/legato/internal/engine/store"
	workflowengine "github.com/cpave3/legato/internal/engine/workflow"
)

// The chat bots' tokens live in Legato's secret store: config.yaml, logs,
// API responses and workflow records never hold one (LEG-624).
func TestChatBotTokensNeverLeaveTheSecretStore(t *testing.T) {
	const appToken, botToken = "xapp-1-A111-very-secret-app", "xoxb-222-very-secret-bot"
	settings, db := newSettingsFixture(t, "")
	ctx := context.Background()
	secrets := NewSecretStoreService(db, credential.MasterKeyOptions{DataDir: t.TempDir(), Source: "file"})
	if err := secrets.Add(ctx, "slack-app", credential.TypeValue, []byte(appToken)); err != nil {
		t.Fatal(err)
	}
	if err := secrets.Add(ctx, "slack-bot", credential.TypeValue, []byte(botToken)); err != nil {
		t.Fatal(err)
	}
	if err := settings.Set(ctx, "integrations.slack.bot_token", json.RawMessage(`"`+botToken+`"`), "web"); err == nil {
		t.Fatal("a token itself was saved as a setting")
	}
	for key, value := range map[string]string{
		"integrations.slack.enabled": `true`, "integrations.slack.app_token": `"legato://slack-app"`,
		"integrations.slack.bot_token": `"legato://slack-bot"`, "integrations.slack.default_channel": `"C1"`,
	} {
		if err := settings.Set(ctx, key, json.RawMessage(value), "web"); err != nil {
			t.Fatalf("%s: %v", key, err)
		}
	}
	resolve := func(ctx context.Context, raw string) (string, error) {
		ref, err := credential.ParseRef(raw)
		if err != nil {
			return "", err
		}
		got, err := secrets.Get(ctx, ref)
		return string(got.Value), err
	}
	settings.SetSecretResolver(resolve)
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	if err := ApplySettings(ctx, db, cfg, resolve); err != nil {
		t.Fatal(err)
	}
	if cfg.Integrations.Slack.AppToken != appToken || cfg.Integrations.Slack.BotToken != botToken {
		t.Fatalf("the tokens were not read into memory: %+v", cfg.Integrations.Slack)
	}

	var logs bytes.Buffer
	log.SetOutput(&logs)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })

	specs := ChatBotSpecs(ChatBotSettings{Slack: SlackBotSettings{Enabled: cfg.Integrations.Slack.Enabled,
		AppToken: cfg.Integrations.Slack.AppToken, BotToken: cfg.Integrations.Slack.BotToken, DefaultChannel: cfg.Integrations.Slack.DefaultChannel}})
	leaky := &fakeChatConnection{name: "slack", platform: "slack",
		runs: []func(context.Context, func(ChatEvent), func()) error{failing(errors.New("refused Bearer " + botToken + " at wss://x.slack.com/link?ticket=" + appToken))},
		send: func(context.Context, string, string, string) (SentMessage, error) {
			return SentMessage{}, errors.New("invalid_auth for " + botToken)
		},
	}
	for i := range specs {
		if specs[i].Name == "slack" {
			specs[i].Conn = leaky
		}
	}
	bots, _ := testChatBots(t, specs, nil)
	bots.after = func(time.Duration) <-chan time.Time { return make(chan time.Time) }
	bots.Start(ctx)
	waitForChat(t, "backing off", func() bool { return chatStatusOf(bots, "slack").State == ChatBotBackingOff })

	// A triggered run that sends, and fails to.
	dir := t.TempDir()
	definition := "schema_version: 1\nname: ops\ntitle: Ops\non:\n  chat.message:\n    bot: slack\n    inputs: {thread: \"${{ event.thread_id }}\"}\nsteps:\n  - {id: say, title: Say, type: chat.message.send, with: {bot: slack, text: hi, thread_id: \"${{ workflow.inputs.thread }}\"}}\ninputs:\n  thread: {type: string}\n"
	if err := os.WriteFile(filepath.Join(dir, "ops.yaml"), []byte(definition), 0o600); err != nil {
		t.Fatal(err)
	}
	workflows := NewWorkflowService(db, []WorkflowExecutor{NewChatSendExecutor(bots)})
	coordinator := NewWorkflowTriggerCoordinator(db, workflows, func() workflowengine.DiscoveryResult { return workflowengine.DiscoverPaths(dir) })
	if err := coordinator.ChatEvent(ctx, chatMessage("1.1", "C1", "deploy")); err != nil {
		t.Fatal(err)
	}
	started := requestsWith(t, db, "started")
	if len(started) != 1 {
		t.Fatalf("started = %+v", started)
	}
	var run *store.WorkflowRunView
	waitForChat(t, "the send to fail", func() bool {
		found, err := db.FindWorkflowRunByTriggerRequest(ctx, started[0].ID)
		if err != nil {
			return false
		}
		run, _ = db.GetWorkflowRun(ctx, found.Run.ID)
		return run != nil && run.Run.Status == "failed"
	})

	overview := NewIntegrationOverview(db, nil)
	overview.SetChatBots(bots)
	integrations, err := overview.Overview(ctx)
	if err != nil {
		t.Fatal(err)
	}
	listed, err := settings.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	exported, _ := settings.Export(ctx, false)
	exportedPinned, _ := settings.Export(ctx, true)
	requests := requestsWith(t, db, "started")
	places := map[string]any{
		"settings": listed, "integrations": integrations, "statuses": bots.Statuses(),
		"export": string(exported), "export --pinned": string(exportedPinned),
		"trigger requests": requests, "run": run, "logs": logs.String(),
	}
	for name, value := range places {
		text, _ := json.Marshal(value)
		for _, secret := range []string{appToken, botToken} {
			if strings.Contains(string(text), secret) {
				t.Errorf("%s holds a token: %s", name, text)
			}
		}
	}
	if !strings.Contains(chatStatusOf(bots, "slack").LastError, "[redacted]") {
		t.Fatalf("status = %+v", chatStatusOf(bots, "slack"))
	}
}
