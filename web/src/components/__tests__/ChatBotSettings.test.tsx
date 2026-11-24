import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import { cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react"
import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import type { ReactNode } from "react"
import type { ConfigSetting } from "../../lib/configSettings"
import type { ChatBotStatus } from "../../lib/chatBots"

const mockFetchBots = vi.fn()
const mockReconnect = vi.fn()
vi.mock("../../lib/chatBots", async (importActual) => ({
  ...(await importActual<typeof import("../../lib/chatBots")>()),
  fetchChatBots: (...a: unknown[]) => mockFetchBots(...a),
  reconnectChatBot: (...a: unknown[]) => mockReconnect(...a),
}))

import { SlackSettings } from "../settings/SlackSettings"
import { DiscordSettings } from "../settings/DiscordSettings"

const setting = (over: Partial<ConfigSetting>): ConfigSetting => ({
  key: "x", title: "X", kind: "string", class: "risky", mode: "restart", value: "", source: "default", pinned: false, section: "integrations", ...over,
})

// The registry's rows as the server sends them: secret settings hold a
// secret's name, never the token.
const settings: ConfigSetting[] = [
  setting({ key: "integrations.slack.enabled", title: "Connect", kind: "bool", value: true, group: "Slack" }),
  setting({ key: "integrations.slack.app_token", title: "App-level token", secret: true, value: "legato://slack-app-token", secret_status: "stored", source: "database", group: "Slack" }),
  setting({ key: "integrations.slack.bot_token", title: "Bot token", secret: true, value: "legato://slack-bot-token", secret_status: "missing", source: "database", group: "Slack" }),
  setting({ key: "integrations.slack.default_channel", title: "Default channel", value: "C123", group: "Slack" }),
  setting({ key: "integrations.discord.enabled", title: "Connect", kind: "bool", value: true, group: "Discord" }),
  setting({ key: "integrations.discord.token", title: "Bot token", secret: true, value: "legato://discord-token", secret_status: "stored", group: "Discord" }),
  setting({ key: "integrations.discord.intents", title: "Gateway intents", kind: "strings", value: ["guilds", "guild_messages", "direct_messages", "message_content"], group: "Discord" }),
  setting({ key: "integrations.discord.default_channel", title: "Default channel", value: "", group: "Discord" }),
  setting({ key: "integrations.mqtt.broker", title: "MQTT broker", value: "", group: "Home Assistant" }),
]

const bot = (over: Partial<ChatBotStatus>): ChatBotStatus => ({ name: "slack", platform: "slack", enabled: true, configured: true, state: "connected", since: "2026-11-24T09:00:00Z", ...over })

function wrap(children: ReactNode) {
  return <QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>{children}</QueryClientProvider>
}

describe("the Slack and Discord settings cards", () => {
  beforeEach(() => { mockFetchBots.mockReset(); mockReconnect.mockReset() })
  afterEach(() => cleanup())

  it("shows Slack's settings, its status and its secrets' state, never a token", async () => {
    // A status with fields that could hold a token, were the server to send one.
    mockFetchBots.mockResolvedValue([
      { ...bot({}), token: "xoxb-1234-secret", app_token: "xapp-1-secret" },
      bot({ name: "discord", platform: "discord", state: "auth_failed" }),
    ])
    const { container } = render(wrap(<SlackSettings settings={settings} baseUrl="https://legato" />))
    const card = within(screen.getByRole("region", { name: "Slack bot" }))
    const status = within(await card.findByRole("group", { name: "Slack connection" }))
    expect(status.getByText("Connected")).toBeTruthy()
    expect(card.queryByRole("button", { name: "Reconnect Slack" })).toBeNull()
    expect(card.getByTestId("setting-integrations.slack.app_token")).toBeTruthy()
    expect(within(card.getByTestId("setting-integrations.slack.app_token")).getByText("Stored")).toBeTruthy()
    expect(within(card.getByTestId("setting-integrations.slack.bot_token")).getByText("Not found")).toBeTruthy()
    expect(card.queryByTestId("setting-integrations.discord.token")).toBeNull()
    expect(card.queryByTestId("setting-integrations.mqtt.broker")).toBeNull()
    expect(card.getByText(/applies when Legato restarts|apply when Legato restarts/)).toBeTruthy()
    expect(card.getByText(/connections:write/)).toBeTruthy()
    expect(card.getByText(/app_mentions:read/)).toBeTruthy()
    expect(container.textContent).not.toMatch(/xoxb-|xapp-|secret"/)
    expect(mockFetchBots).toHaveBeenCalledWith("https://legato")
  })

  it("shows the last error and reconnects a bot that backs off", async () => {
    mockFetchBots.mockResolvedValue([bot({ state: "backing_off", last_error: "socket mode: dial tcp: i/o timeout", next_attempt: "2026-11-24T09:01:00Z" })])
    mockReconnect.mockResolvedValue([bot({ state: "connecting" })])
    render(wrap(<SlackSettings settings={settings} baseUrl="" />))
    const status = within(await screen.findByRole("group", { name: "Slack connection" }))
    expect(status.getByText("Backing off")).toBeTruthy()
    expect(status.getByText(/i\/o timeout/)).toBeTruthy()
    fireEvent.click(screen.getByRole("button", { name: "Reconnect Slack" }))
    await waitFor(() => expect(mockReconnect).toHaveBeenCalledWith("", "slack"))
    expect(await status.findByText("Connecting")).toBeTruthy()
  })

  it("shows Discord's status and its refusal to reconnect", async () => {
    mockFetchBots.mockResolvedValue([bot({}), bot({ name: "discord", platform: "discord", state: "auth_failed", last_error: "4004: Authentication failed" })])
    mockReconnect.mockRejectedValue(new Error("discord is switched off"))
    render(wrap(<DiscordSettings settings={settings} baseUrl="" />))
    const card = within(screen.getByRole("region", { name: "Discord bot" }))
    const status = within(await card.findByRole("group", { name: "Discord connection" }))
    expect(status.getByText("Auth failed")).toBeTruthy()
    expect(status.getByText(/4004: Authentication failed/)).toBeTruthy()
    expect(card.getByText(/Message Content intent/)).toBeTruthy()
    expect(within(card.getByTestId("setting-integrations.discord.token")).getByText("Stored")).toBeTruthy()
    expect(card.queryByTestId("setting-integrations.slack.bot_token")).toBeNull()
    fireEvent.click(card.getByRole("button", { name: "Reconnect Discord" }))
    expect(await card.findByText("discord is switched off")).toBeTruthy()
  })

  it("says when another Legato process holds the connection", async () => {
    mockFetchBots.mockResolvedValue([bot({ state: "elsewhere" })])
    render(wrap(<SlackSettings settings={settings} baseUrl="" />))
    const status = within(await screen.findByRole("group", { name: "Slack connection" }))
    expect(status.getByText("Connected in another Legato process")).toBeTruthy()
  })
})
