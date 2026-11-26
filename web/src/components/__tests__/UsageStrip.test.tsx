import { MemoryRouter } from "react-router-dom"
import { QueryClientProvider } from "@tanstack/react-query"
import { cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import { createQueryClient } from "../../lib/query"
import { UsageStrip } from "../UsageStrip"

vi.mock("../../hooks/useServer", () => ({ useServer: () => ({ baseUrl: "" }) }))

const NOW = new Date("2026-11-26T08:00:00Z")

function json(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } })
}

let usage: unknown
let posted: string[]

beforeEach(() => {
  vi.useFakeTimers({ toFake: ["Date"] })
  vi.setSystemTime(NOW)
  usage = {
    poll_interval_seconds: 300,
    providers: [
      {
        provider: "claude", state: "ok", stale: false, plan: "max", source: "endpoint",
        as_of: "2026-11-26T07:58:00Z",
        windows: [
          { meter: "five_hour", used_percent: 42, resets_at: "2026-11-26T10:00:00Z" },
          { meter: "weekly", used_percent: 91.4, resets_at: "2026-12-01T09:00:00Z" },
        ],
      },
      {
        provider: "codex", state: "stale", stale: true, reason: "unreachable", source: "logs",
        as_of: "2026-11-26T05:00:00Z",
        windows: [{ meter: "weekly", used_percent: 5, resets_at: "2026-11-29T00:00:00Z", window_minutes: 10080 }],
      },
    ],
  }
  posted = []
  vi.stubGlobal("fetch", vi.fn(async (url: string, init?: RequestInit) => {
    if (init?.method === "POST") {
      posted.push(url)
      return json({ status: "refreshing" }, 202)
    }
    if (url === "/api/usage") return json(usage)
    return json({}, 404)
  }))
})

afterEach(() => {
  cleanup()
  vi.useRealTimers()
  vi.unstubAllGlobals()
})

function renderIt() {
  render(<QueryClientProvider client={createQueryClient()}><MemoryRouter><UsageStrip /></MemoryRouter></QueryClientProvider>)
}

describe("UsageStrip", () => {
  it("shows each provider's windows in brief", async () => {
    renderIt()
    const strip = await screen.findByRole("button", { name: /subscription usage/i })
    expect(strip.getAttribute("aria-expanded")).toBe("false")
    expect(within(strip).getByText("Claude")).toBeTruthy()
    expect(within(strip).getByText("5h 42%")).toBeTruthy()
    expect(within(strip).getByText("wk 91%")).toBeTruthy()
    expect(within(strip).getByText("Codex")).toBeTruthy()
    expect(within(strip).getByText("wk 5%")).toBeTruthy()
    expect(within(strip).getByText("stale")).toBeTruthy()
  })

  it("opens to bars with when each resets and how fresh the reading is", async () => {
    renderIt()
    fireEvent.click(await screen.findByRole("button", { name: /subscription usage/i }))
    const card = screen.getByRole("region", { name: "Subscription usage" })
    const claude = within(card).getByRole("group", { name: "Claude" })
    const weekly = within(claude).getByRole("progressbar", { name: "Claude weekly" })
    expect(weekly.getAttribute("aria-valuenow")).toBe("91")
    expect(within(claude).getByText("resets in 5d 1h")).toBeTruthy()
    expect(within(claude).getByText("resets in 2h")).toBeTruthy()
    expect(within(claude).getByText(/as of 2m ago/)).toBeTruthy()
    expect(within(claude).getByText(/max/)).toBeTruthy()

    const codex = within(card).getByRole("group", { name: "Codex" })
    expect(within(codex).getByText(/stale · as of 3h ago · could not reach it/)).toBeTruthy()
    expect(within(codex).getByText(/from Codex's session logs/)).toBeTruthy()
  })

  it("says why a provider is unavailable", async () => {
    usage = {
      poll_interval_seconds: 300,
      providers: [
        { provider: "claude", state: "unavailable", stale: false, reason: "not_signed_in", windows: [] },
        { provider: "codex", state: "unavailable", stale: false, reason: "codex_missing", windows: [] },
      ],
    }
    renderIt()
    const strip = await screen.findByRole("button", { name: /subscription usage/i })
    expect(within(strip).getAllByText("unavailable")).toHaveLength(2)
    fireEvent.click(strip)
    const card = screen.getByRole("region", { name: "Subscription usage" })
    expect(within(card).getByText("Sign in to Claude Code on this machine.")).toBeTruthy()
    expect(within(card).getByText("The codex command is not installed on this machine.")).toBeTruthy()
    expect(within(card).queryByRole("progressbar")).toBeNull()
  })

  it("asks for a reading now", async () => {
    renderIt()
    fireEvent.click(await screen.findByRole("button", { name: /subscription usage/i }))
    fireEvent.click(screen.getByRole("button", { name: "Read now" }))
    await waitFor(() => expect(posted).toEqual(["/api/usage/refresh"]))
  })

  it("shows nothing when no provider is read", async () => {
    usage = { poll_interval_seconds: 0, providers: [] }
    renderIt()
    await waitFor(() => expect(vi.mocked(fetch)).toHaveBeenCalled())
    expect(screen.queryByRole("button", { name: /subscription usage/i })).toBeNull()
  })

  it("shows nothing on a server without usage", async () => {
    vi.stubGlobal("fetch", vi.fn(async () => json({ error: "not found" }, 404)))
    renderIt()
    await waitFor(() => expect(vi.mocked(fetch)).toHaveBeenCalled())
    expect(screen.queryByRole("button", { name: /subscription usage/i })).toBeNull()
  })
})
