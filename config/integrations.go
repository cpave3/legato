package config

import (
	"fmt"
	"net"
	"net/url"
	"slices"
	"strings"
)

// IntegrationsConfig holds optional connections to services outside Legato.
type IntegrationsConfig struct {
	MQTT MQTTConfig `yaml:"mqtt"`
	// Argus delegates cases to agents and receives their replies. Unset,
	// its delegation routes answer 503 and agents get no reply_case tool.
	Argus *ArgusConfig `yaml:"argus"`
	// Slack and Discord are chat bots workflows start on and post as.
	Slack   SlackBotConfig   `yaml:"slack"`
	Discord DiscordBotConfig `yaml:"discord"`
}

// SlackBotConfig is the Slack bot: a Slack app in Socket Mode. The tokens
// are names of secrets (legato://, op://) until the settings are applied,
// which puts the secrets themselves in the in-memory configuration only.
type SlackBotConfig struct {
	Enabled bool `yaml:"enabled"`
	// AppToken is the app-level token (xapp-…) Socket Mode connects with.
	AppToken string `yaml:"app_token"`
	// BotToken is the bot token (xoxb-…) messages are posted with.
	BotToken string `yaml:"bot_token"`
	// DefaultChannel is the channel ID chat.message.send posts to when a
	// step names none.
	DefaultChannel string `yaml:"default_channel"`
}

// DiscordBotConfig is the Discord bot, over the Gateway.
type DiscordBotConfig struct {
	Enabled bool `yaml:"enabled"`
	// Token is the bot's token, a secret's name like the Slack tokens.
	Token string `yaml:"token"`
	// Intents are the gateway intents the bot asks for, of
	// DiscordIntentNames; DefaultDiscordIntents when empty.
	Intents        []string `yaml:"intents"`
	DefaultChannel string   `yaml:"default_channel"`
}

// DiscordIntentNames are the gateway intents a Discord bot may ask for.
var DiscordIntentNames = []string{"guilds", "guild_messages", "direct_messages", "message_content"}

// DefaultDiscordIntents is all of them: without message_content, Discord
// sends a guild message's text only when it mentions the bot.
var DefaultDiscordIntents = []string{"guilds", "guild_messages", "direct_messages", "message_content"}

// ArgusConfig is the Argus integration.
type ArgusConfig struct {
	// URL is Argus's address: where replies go and where `legato integration
	// connect argus` registers. https, or http to a loopback address.
	URL string `yaml:"url"`
	// Delivery is auto (deliver at once) or ask (wait for the person's
	// approval). Default auto.
	Delivery string `yaml:"delivery"`
	// Agent is the tool an agent started for a delegation runs. Default
	// claude-code.
	Agent string `yaml:"agent"`
}

func normalizeIntegrations(cfg *Config) error {
	if err := normalizeMQTT(&cfg.Integrations.MQTT); err != nil {
		return err
	}
	if err := normalizeChatBots(&cfg.Integrations); err != nil {
		return err
	}
	argus := cfg.Integrations.Argus
	if argus == nil {
		return nil
	}
	argus.URL = strings.TrimRight(strings.TrimSpace(argus.URL), "/")
	if argus.URL == "" {
		return fmt.Errorf("integrations.argus.url is required")
	}
	u, err := url.Parse(argus.URL)
	if err != nil || u.Host == "" || (u.Scheme != "https" && !(u.Scheme == "http" && isLoopbackHost(u.Hostname()))) {
		return fmt.Errorf("integrations.argus.url %q: want an https address, or http to a loopback address", argus.URL)
	}
	switch argus.Delivery = strings.TrimSpace(argus.Delivery); argus.Delivery {
	case "":
		argus.Delivery = "auto"
	case "auto", "ask":
	default:
		return fmt.Errorf("integrations.argus.delivery %q: want auto or ask", argus.Delivery)
	}
	argus.Agent = strings.TrimSpace(argus.Agent)
	if argus.Agent == "" {
		argus.Agent = "claude-code"
	}
	if _, ok := supportedSwarmAgents[argus.Agent]; !ok {
		return fmt.Errorf("integrations.argus.agent %q: want claude-code, codex or chimera", argus.Agent)
	}
	return nil
}

func isLoopbackHost(host string) bool {
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func normalizeChatBots(in *IntegrationsConfig) error {
	in.Slack.DefaultChannel = strings.TrimSpace(in.Slack.DefaultChannel)
	in.Discord.DefaultChannel = strings.TrimSpace(in.Discord.DefaultChannel)
	if len(in.Discord.Intents) == 0 {
		in.Discord.Intents = append([]string(nil), DefaultDiscordIntents...)
	}
	for _, name := range in.Discord.Intents {
		if !slices.Contains(DiscordIntentNames, name) {
			return fmt.Errorf("integrations.discord.intents %q: want %s", name, strings.Join(DiscordIntentNames, ", "))
		}
	}
	return nil
}
