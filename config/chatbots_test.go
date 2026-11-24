package config

import (
	"slices"
	"strings"
	"testing"
)

func TestChatBotsLoadAndDefaultTheirIntents(t *testing.T) {
	writeTestConfig(t, `integrations:
  slack:
    enabled: true
    app_token: legato://slack-app
    bot_token: legato://slack-bot
    default_channel: " C123 "
  discord:
    enabled: true
    token: legato://discord-bot
`)
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	slack, discord := cfg.Integrations.Slack, cfg.Integrations.Discord
	if !slack.Enabled || slack.AppToken != "legato://slack-app" || slack.BotToken != "legato://slack-bot" || slack.DefaultChannel != "C123" {
		t.Fatalf("slack = %+v", slack)
	}
	if !discord.Enabled || discord.Token != "legato://discord-bot" || !slices.Equal(discord.Intents, DefaultDiscordIntents) {
		t.Fatalf("discord = %+v", discord)
	}
}

func TestChatBotsRefuseUnknownIntents(t *testing.T) {
	writeTestConfig(t, "integrations:\n  discord:\n    intents: [guilds, presence]\n")
	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "integrations.discord.intents") {
		t.Fatalf("err = %v", err)
	}
}

func TestChatBotTokensAreSecretSettings(t *testing.T) {
	for _, key := range []string{"integrations.slack.app_token", "integrations.slack.bot_token", "integrations.discord.token"} {
		s, ok := LookupSetting(key)
		if !ok || !s.Secret {
			t.Fatalf("%s: registered %v, secret %v", key, ok, s.Secret)
		}
		if _, err := DecodeSetting(s, []byte(`"xoxb-1234-abcd"`)); err == nil {
			t.Errorf("%s took a token itself", key)
		}
	}
	for _, key := range []string{"integrations.slack.enabled", "integrations.slack.default_channel", "integrations.discord.enabled", "integrations.discord.intents", "integrations.discord.default_channel"} {
		if _, ok := LookupSetting(key); !ok {
			t.Errorf("%s is not registered", key)
		}
	}
}
