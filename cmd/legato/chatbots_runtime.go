package main

import (
	"context"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/cpave3/legato/config"
	"github.com/cpave3/legato/internal/engine/store"
	"github.com/cpave3/legato/internal/service"
)

// chatBotRuntimes holds each database's chat bots in this process: the
// supervisor, the chat.message.send step and the web server share them.
var chatBotRuntimes = struct {
	sync.Mutex
	by map[*store.Store]*service.ChatBots
}{by: map[*store.Store]*service.ChatBots{}}

// chatBotsFor is the process's chat bots for a database, made from the
// effective configuration. Their connections run only once startChatBots
// claims them; until then (and in short-lived commands) they can send.
func chatBotsFor(cfg *config.Config, db *store.Store) *service.ChatBots {
	chatBotRuntimes.Lock()
	defer chatBotRuntimes.Unlock()
	if b, ok := chatBotRuntimes.by[db]; ok {
		return b
	}
	b := service.NewChatBots(service.ChatBotSpecs(chatBotSettings(cfg, db)), nil)
	chatBotRuntimes.by[db] = b
	return b
}

// chatBotSettings reads the bots' settings. The settings applied from the
// database already hold the tokens themselves; a token config.yaml names
// by its secret (legato://, op://) is read here, in memory only.
func chatBotSettings(cfg *config.Config, db *store.Store) service.ChatBotSettings {
	in := cfg.Integrations
	resolve := func(key, value string) string {
		if !strings.HasPrefix(value, "legato://") && !strings.HasPrefix(value, "op://") {
			return value
		}
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		plain, err := settingSecrets(cfg, db)(ctx, value)
		if err != nil {
			log.Printf("chat bots: %s names %s, which cannot be read: %v", key, value, err)
			return ""
		}
		return plain
	}
	return service.ChatBotSettings{
		Slack: service.SlackBotSettings{Enabled: in.Slack.Enabled, DefaultChannel: in.Slack.DefaultChannel,
			AppToken: resolve("integrations.slack.app_token", in.Slack.AppToken), BotToken: resolve("integrations.slack.bot_token", in.Slack.BotToken)},
		Discord: service.DiscordBotSettings{Enabled: in.Discord.Enabled, DefaultChannel: in.Discord.DefaultChannel, Intents: in.Discord.Intents,
			Token: resolve("integrations.discord.token", in.Discord.Token)},
	}
}

// claimChatBots is the per-data-directory lock of the process that keeps
// the bots connected: a TUI and a legato serve on one data directory would
// otherwise split Slack's events between them and receive Discord's twice.
func claimChatBots(dataDir string) (func(), error) {
	return claimDataDirLock(dataDir, "chatbots.lock")
}

// startChatBots connects the enabled bots, handing their messages to the
// workflow triggers, when this process claims them; another process that
// holds them leaves them "elsewhere" here. Process-owned, like MQTT: the
// TUI and legato serve call it, short-lived commands never do.
func startChatBots(cfg *config.Config, db *store.Store, triggers *service.WorkflowTriggerCoordinator) func() {
	bots := chatBotsFor(cfg, db)
	in := cfg.Integrations
	if !in.Slack.Enabled && !in.Discord.Enabled {
		return func() {}
	}
	release, err := claimChatBots(config.ResolveDataDir(cfg))
	if err != nil {
		log.Print("chat bots: another Legato process on this data directory keeps them connected")
		bots.SetElsewhere()
		return func() {}
	}
	bots.SetEventHandler(func(ctx context.Context, e service.ChatEvent) {
		if triggers == nil {
			return
		}
		if e.Click != nil {
			chatClick(ctx, bots, triggers, e)
			return
		}
		if err := triggers.ChatEvent(ctx, e); err != nil {
			log.Printf("chat bots: %s message %s: %v", e.Bot, e.MessageID, err)
		}
	})
	bots.Start(context.Background())
	return func() {
		bots.Stop()
		release()
	}
}

// chatClick fires the chat.button triggers for a click, and when one takes
// it, rewrites the message to say who chose what, so no one clicks again.
// A click no trigger takes, or one by someone a trigger does not allow,
// leaves the message as it is (the platform was answered already).
func chatClick(ctx context.Context, bots *service.ChatBots, triggers *service.WorkflowTriggerCoordinator, e service.ChatEvent) {
	outcome, err := triggers.ChatButton(ctx, e)
	if err != nil {
		log.Printf("chat bots: %s click on message %s: %v", e.Bot, e.MessageID, err)
	}
	if outcome.Status != service.ChatClickAccepted {
		return
	}
	if err := bots.SettleClick(ctx, e, outcome.Line); err != nil {
		log.Printf("chat bots: %v", err)
	}
}
