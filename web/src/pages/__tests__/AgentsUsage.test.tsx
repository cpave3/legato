import { act, cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react"
import { useState } from "react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import { MemoryRouter } from "react-router-dom"
import { QueryClientProvider } from "@tanstack/react-query"
import { AgentsPage } from "../Agents"
import { createQueryClient } from "../../lib/query"
import { mockReactFlow } from "../../test/reactFlow"

// The usage card on the agents page, with the real UsageStrip: a card at the
// head of the sidebar on a desktop, and on a phone only a button in the
// picker bar, which opens the same details as a sheet.

// A tap focuses what it taps; fireEvent.click alone does not.
function tap(element: HTMLElement) {
  element.focus()
  fireEvent.click(element)
}

mockReactFlow()
vi.mock("../../components/PreparingAgents", () => ({ PreparingAgents: () => null }))
vi.mock("../../components/InterruptedAgents", () => ({ InterruptedAgents: () => null }))

const { mockFetch, socket } = vi.hoisted(() => ({
  mockFetch: vi.fn(),
  socket: { send: vi.fn(), subscribe: () => () => {}, connected: true },
}))

vi.stubGlobal("fetch", mockFetch)
vi.mock("../../hooks/useWebSocket", () => ({ useWebSocket: () => socket }))
vi.mock("../../hooks/useServer", () => ({ useServer: () => ({ baseUrl: "" }) }))
vi.mock("../../hooks/useApprovals", () => ({ useApprovals: () => ({ data: [], error: null, refresh: async () => {} }) }))
vi.mock("../../hooks/useAgentSelection", () => ({
  useAgentSelection: () => {
    const [selectedAgentId, setSelectedAgentId] = useState<string | null>("TASK-1")
    return { selectedAgentId, setSelectedAgentId }
  },
  useAgentReselected: () => {},
}))
vi.mock("../../hooks/useSettings", () => ({ useSettings: () => ({ data: { ntfy_configured: false, voice_enabled: false } }) }))
vi.mock("../../hooks/useSwarmEvents", () => ({ useSwarmEvents: () => ({ fetchState: vi.fn(), states: {} }) }))
vi.mock("../../hooks/useTaskActivity", () => ({ useTaskActivity: () => ({ items: [] }) }))
vi.mock("../../hooks/useAgentUI", () => ({
  useAgentUIItems: () => ({ items: [], loading: false, error: null }),
  useAgentUIItem: () => ({ item: null, loading: false, error: null }),
}))
vi.mock("../../hooks/useReviewBriefing", () => ({
  useReviewBriefing: () => ({ pages: [] }),
  useReviewBriefingPage: () => ({ page: null, loading: false, error: null }),
  useUnseenBriefingPages: () => 0,
}))
vi.mock("../../hooks/useToast", () => ({ useToast: () => ({ addToast: vi.fn() }) }))
vi.mock("../../hooks/useTaskCard", () => ({ useTaskCard: () => null }))
vi.mock("../../components/TerminalPanel", () => ({ TerminalPanel: ({ agentId }: { agentId: string }) => <div>Terminal of {agentId}</div> }))
vi.mock("../../components/LiveTerminal", () => ({ LiveTerminal: ({ agentId }: { agentId: string }) => <div>Terminal of {agentId}</div> }))
vi.mock("../../components/PromptBar", () => ({ PromptBar: () => <textarea aria-label="Message" /> }))
vi.mock("../../components/StartSwarmModal", () => ({ StartSwarmModal: () => null }))
vi.mock("../../components/SpawnEphemeralModal", () => ({ SpawnEphemeralModal: () => null }))
vi.mock("../../components/SwarmEventLog", () => ({ SwarmEventLog: () => null }))
vi.mock("../../components/AgentArtifactPanel", () => ({ AgentArtifactPanel: () => null }))

const NOW = new Date("2026-11-26T08:00:00Z")

const agent = {
  id: 1, task_id: "TASK-1", task_title: "TASK-1 title", tmux_session: "legato-TASK-1",
  command: "codex", agent_kind: "codex", status: "running", activity: "", role: "",
  started_at: "2026-08-13T00:00:00Z", working_seconds: 1, waiting_seconds: 0, error_seconds: 0,
}

const catalog = [
  { provider: "claude", name: "Claude", meters: [
    { window: "five_hour", qualified: "claude.five_hour", short: "5h", long: "5-hour" },
    { window: "weekly", qualified: "claude.weekly", short: "wk", long: "weekly" },
  ] },
  { provider: "codex", name: "Codex", meters: [{ window: "weekly", qualified: "codex.weekly", short: "wk", long: "weekly" }] },
]

let usage: Record<string, unknown>
let posted: string[]

function json(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } })
}

function setWidth(width: number) {
  Object.defineProperty(window, "innerWidth", { value: width, configurable: true })
}

function open(address = "/agents") {
  return render(
    <QueryClientProvider client={createQueryClient()}>
      <MemoryRouter initialEntries={[address]}><AgentsPage /></MemoryRouter>
    </QueryClientProvider>,
  )
}

const usageButton = () => screen.findByRole("button", { name: /^Subscription usage/ })
const meterRows = () => document.querySelectorAll("[data-provider]")
const sheet = () => screen.getByRole("dialog", { name: "Subscription usage" })

beforeEach(() => {
  vi.useFakeTimers({ toFake: ["Date"] })
  vi.setSystemTime(NOW)
  usage = {
    poll_interval_seconds: 300,
    catalog,
    providers: [
      { provider: "claude", state: "ok", stale: false, as_of: "2026-11-26T07:58:00Z", windows: [
        { meter: "five_hour", used_percent: 42, resets_at: "2026-11-26T10:00:00Z" },
        { meter: "weekly", used_percent: 61, resets_at: "2026-12-01T09:00:00Z" },
      ] },
      { provider: "codex", state: "ok", stale: false, as_of: "2026-11-26T07:58:00Z", windows: [
        { meter: "weekly", used_percent: 5, resets_at: "2026-11-29T00:00:00Z" },
      ] },
    ],
    policy: {
      active: { profile: { name: "Economy", agent: "codex" }, reason: { text: "Fallback" }, since: NOW.toISOString() },
      profiles: [{ name: "Economy", agent: "codex" }], switches: [],
    },
  }
  posted = []
  mockFetch.mockReset()
  mockFetch.mockImplementation(async (input: RequestInfo | URL, init?: RequestInit) => {
    const path = String(input)
    if (path === "/api/usage/refresh" && init?.method === "POST") { posted.push(path); return json({ status: "refreshing" }, 202) }
    if (path === "/api/usage") return json(usage)
    if (path === "/api/agents") return json([agent])
    return json({})
  })
  localStorage.clear()
  setWidth(1440)
})

afterEach(() => {
  cleanup()
  vi.useRealTimers()
  vi.restoreAllMocks()
})

describe("on a phone", () => {
  beforeEach(() => setWidth(390))

  it("shows the agents and a usage button, not the usage card", async () => {
    open()
    const button = await usageButton()
    await screen.findByText("Terminal of TASK-1")

    expect(meterRows()).toHaveLength(0)
    expect(screen.queryByText("Economy")).toBeNull()
    // The button sits in the picker bar, beside the ensemble.
    expect(button.parentElement).toBe(screen.getByRole("button", { name: "Ensemble" }).parentElement)
    expect(button.getAttribute("aria-haspopup")).toBe("dialog")
    expect(button.getAttribute("aria-expanded")).toBe("false")
  })

  it("does not show the card while the agents are still loading", async () => {
    // Until the agents arrive the page shows its empty state; the card must
    // not appear there first and vanish when they do.
    let agentsArrive: (response: Response) => void = () => {}
    mockFetch.mockImplementation(async (input: RequestInfo | URL) => {
      const path = String(input)
      if (path === "/api/usage") return json(usage)
      if (path === "/api/agents") return new Promise<Response>((resolve) => { agentsArrive = resolve })
      return json({})
    })
    open()
    const button = await usageButton()
    expect(screen.getByText("No active agents")).toBeTruthy()
    expect(meterRows()).toHaveLength(0)

    fireEvent.click(button)
    expect(sheet().getAttribute("aria-modal")).toBe("true")
    fireEvent.keyDown(sheet(), { key: "Escape" })
    await act(async () => { agentsArrive(json([agent])) })
    await screen.findByText("Terminal of TASK-1")
    expect(meterRows()).toHaveLength(0)
  })

  it("opens the usage details as a sheet, with the accounts, Read now, the pin and the settings", async () => {
    open()
    fireEvent.click(await usageButton())

    const dialog = sheet()
    expect(dialog.getAttribute("aria-modal")).toBe("true")
    expect(within(dialog).getByRole("group", { name: "Claude" })).toBeTruthy()
    expect(within(dialog).getByRole("group", { name: "Codex" })).toBeTruthy()
    expect(within(dialog).getByRole("button", { name: "Pin a profile" })).toBeTruthy()
    expect(within(dialog).getByRole("link", { name: "Usage settings" })).toBeTruthy()
    fireEvent.click(within(dialog).getByRole("button", { name: "Read now" }))
    await waitFor(() => expect(posted).toEqual(["/api/usage/refresh"]))
  })

  it("closes the sheet on Escape and gives focus back to the button", async () => {
    open()
    const button = await usageButton()
    tap(button)
    fireEvent.keyDown(sheet(), { key: "Escape" })

    await waitFor(() => expect(screen.queryByRole("dialog", { name: "Subscription usage" })).toBeNull())
    expect(document.activeElement).toBe(button)
    expect(button.getAttribute("aria-expanded")).toBe("false")
    expect(meterRows()).toHaveLength(0)
  })

  it("closes the sheet from its Close button and from the backdrop", async () => {
    open()
    const button = await usageButton()
    tap(button)
    fireEvent.click(within(sheet()).getByRole("button", { name: "Close" }))
    await waitFor(() => expect(screen.queryByRole("dialog", { name: "Subscription usage" })).toBeNull())
    expect(document.activeElement).toBe(button)

    tap(button)
    fireEvent.mouseDown(screen.getByTestId("dialog-backdrop"))
    await waitFor(() => expect(screen.queryByRole("dialog", { name: "Subscription usage" })).toBeNull())
  })

  it("opens the sheet at the pin from an Attention link", async () => {
    open("/agents?usage=open&pin=Economy")
    expect(await screen.findByRole("dialog", { name: "Subscription usage" })).toBeTruthy()
    expect(await screen.findByRole("combobox", { name: "Pin profile" })).toBeTruthy()
  })

  it("still says when an account is at its limit", async () => {
    const providers = usage.providers as { windows: { used_percent: number }[] }[]
    providers[0].windows[1].used_percent = 92
    open()
    const button = await usageButton()
    expect(button.getAttribute("aria-label")).toMatch(/at limit/)
    expect(button.querySelector("[data-tone='over']")).toBeTruthy()
  })

  it("still says when the profile switch failed", async () => {
    (usage.policy as { active: Record<string, unknown> }).active.error = "codex is not installed"
    open()
    const button = await usageButton()
    expect(button.getAttribute("aria-label")).toMatch(/switch failed/)
    expect(button.querySelector("[data-tone='error']")).toBeTruthy()
  })
})

describe("on a desktop", () => {
  it("keeps the card at the head of the sidebar, opening a popout", async () => {
    open()
    const card = await usageButton()

    await waitFor(() => expect(meterRows()).toHaveLength(2))
    expect(within(card).getByText("Economy")).toBeTruthy()
    expect(within(card).getByText("Auto")).toBeTruthy()
    expect(screen.getAllByRole("button", { name: /^Subscription usage/ })).toHaveLength(1)

    fireEvent.click(card)
    expect(sheet().getAttribute("aria-modal")).toBeNull()
  })
})

it("swaps the card and the button as the window crosses the phone's width", async () => {
  open()
  await waitFor(() => expect(meterRows()).toHaveLength(2))

  act(() => { setWidth(390); window.dispatchEvent(new Event("resize")) })
  await waitFor(() => expect(meterRows()).toHaveLength(0))
  expect(await usageButton()).toBeTruthy()
  expect(screen.getAllByRole("button", { name: /^Subscription usage/ })).toHaveLength(1)

  act(() => { setWidth(1280); window.dispatchEvent(new Event("resize")) })
  await waitFor(() => expect(meterRows()).toHaveLength(2))
  expect(screen.getAllByRole("button", { name: /^Subscription usage/ })).toHaveLength(1)
})
