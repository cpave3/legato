import { afterEach, describe, expect, it, vi } from "vitest"

vi.mock("../api", () => ({ apiFetch: vi.fn() }))

import { apiFetch } from "../api"
import { builderBots, chatBotStateLabel, fetchChatBots, reconnectChatBot, type ChatBotStatus } from "../chatBots"

const mocked = vi.mocked(apiFetch)
const json = (body: unknown, status = 200) => new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } })
const bot = (over: Partial<ChatBotStatus>): ChatBotStatus => ({ name: "slack", platform: "slack", enabled: true, configured: true, state: "connected", since: "2026-11-24T09:00:00Z", ...over })

afterEach(() => mocked.mockReset())

describe("the chat bots' API", () => {
  it("reads the bots' status", async () => {
    mocked.mockResolvedValue(json({ bots: [bot({})] }))
    expect(await fetchChatBots("https://legato")).toEqual([bot({})])
    expect(mocked).toHaveBeenCalledWith("https://legato", "/api/settings/integrations/chat-bots")
  })

  it("reconnects a bot and returns every bot's status", async () => {
    mocked.mockResolvedValue(json({ bots: [bot({ state: "connecting" })] }))
    expect(await reconnectChatBot("", "slack")).toEqual([bot({ state: "connecting" })])
    expect(mocked).toHaveBeenCalledWith("", "/api/settings/integrations/chat-bots/slack/reconnect", { method: "POST" })
  })

  it("says the server's refusal", async () => {
    mocked.mockResolvedValue(json({ error: "discord is switched off" }, 409))
    await expect(reconnectChatBot("", "discord")).rejects.toThrow("discord is switched off")
  })
})

describe("chatBotStateLabel", () => {
  it("names every state in plain words", () => {
    expect(chatBotStateLabel("connected")).toBe("Connected")
    expect(chatBotStateLabel("connecting")).toBe("Connecting")
    expect(chatBotStateLabel("backing_off")).toBe("Backing off")
    expect(chatBotStateLabel("auth_failed")).toBe("Auth failed")
    expect(chatBotStateLabel("disabled")).toBe("Disabled")
    expect(chatBotStateLabel("elsewhere")).toBe("Connected in another Legato process")
  })
})

describe("builderBots", () => {
  it("offers the bots that are set up", () => {
    expect(builderBots([bot({ name: "discord", platform: "discord" }), bot({ name: "slack", configured: false })])).toEqual(["discord"])
  })

  it("offers slack and discord when none is set up, so a workflow can still be written", () => {
    expect(builderBots(undefined)).toEqual(["slack", "discord"])
    expect(builderBots([bot({ configured: false })])).toEqual(["slack", "discord"])
  })
})
