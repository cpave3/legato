import { describe, it, expect, vi, beforeEach, afterEach } from "vitest"
import { render, screen, waitFor, fireEvent, cleanup, within } from "@testing-library/react"
import { MemoryRouter } from "react-router-dom"
import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import type { AttentionItem } from "../../lib/attention"

const mockFetchRail = vi.fn()
const mockExecute = vi.fn()
const mockDismiss = vi.fn()
let mockSelected: string | null = null

vi.mock("../../hooks/useServer", () => ({ useServer: () => ({ baseUrl: "http://legato" }) }))
vi.mock("../../hooks/useAgentSelection", () => ({ useAgentSelection: () => ({ selectedAgentId: mockSelected }) }))
vi.mock("../../lib/attention", async (importOriginal) => ({
  ...(await importOriginal<typeof import("../../lib/attention")>()),
  executeAttentionAction: (...a: unknown[]) => mockExecute(...a),
  dismissAttentionItem: (...a: unknown[]) => mockDismiss(...a),
}))
vi.mock("../../lib/needsYou", async (importOriginal) => ({
  ...(await importOriginal<typeof import("../../lib/needsYou")>()),
  fetchRailAttention: (...a: unknown[]) => mockFetchRail(...a),
}))

import { NeedsYouRail } from "../needsYou/NeedsYouRail"

function item(overrides: Partial<AttentionItem> = {}): AttentionItem {
  return {
    id: "att-1", producer: "operator_items", signal_kind: "operator_action", source_key: "op-1", source_revision: "2",
    task_id: "LEG-363", title: "Type /to-tickets in LEG-363's session", reason: "Blocks 7 LEG-365 build tickets",
    category: "agent", urgency: "normal", navigation: { kind: "agent", target: "LEG-363" },
    actions: [
      { id: "open", label: "Open agent", kind: "navigate", intent: "primary" },
      { id: "copy", label: "Copy", kind: "copy", text: "/to-tickets" },
      { id: "done", label: "Done", kind: "execute" },
    ],
    status: "open", created_at: "2026-11-20T08:00:00Z", updated_at: "2026-11-20T08:00:00Z",
    ...overrides,
  } as AttentionItem
}

const question = () => item({
  id: "att-2", signal_kind: "operator_question", source_key: "op-2", source_revision: "1", task_id: "LEG-358",
  title: "Should pr.review_requested fire for draft PRs?", reason: "Blocks the trigger's filter",
  navigation: { kind: "agent", target: "LEG-358" },
  actions: [
    { id: "open", label: "Open agent", kind: "navigate", intent: "primary" },
    { id: "option_1", label: "Yes", kind: "execute" },
    { id: "reply", label: "Reply", kind: "execute", requires_text: true },
    { id: "done", label: "Done", kind: "execute" },
  ],
})

let client: QueryClient
function renderRail(path = "/board") {
  client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return render(<QueryClientProvider client={client}><MemoryRouter initialEntries={[path]}><NeedsYouRail /></MemoryRouter></QueryClientProvider>)
}

describe("NeedsYouRail", () => {
  afterEach(() => cleanup())
  beforeEach(() => {
    vi.clearAllMocks()
    mockFetchRail.mockResolvedValue({ items: [item(), question()], hidden: 0 })
    mockExecute.mockResolvedValue(undefined)
    mockDismiss.mockResolvedValue(undefined)
    mockSelected = null
    window.localStorage.clear()
  })

  it("shows each item with its kind, who posted it, what is needed and what it blocks", async () => {
    renderRail()
    const rail = screen.getByRole("complementary", { name: "Needs you" })
    const action = await within(rail).findByRole("article", { name: "Type /to-tickets in LEG-363's session" })
    expect(within(action).getByText("Action")).toBeTruthy()
    expect(within(action).getByText("LEG-363")).toBeTruthy()
    expect(within(action).getByText("Blocks 7 LEG-365 build tickets")).toBeTruthy()
    expect(within(action).getByText("/to-tickets")).toBeTruthy()
    expect(within(action).getByRole("link", { name: "Open agent" }).getAttribute("href")).toBe("/agents?task=LEG-363")
    expect(within(rail).getByText("2")).toBeTruthy()
    const q = within(rail).getByRole("article", { name: /draft PRs/ })
    expect(within(q).getByText("Question")).toBeTruthy()
  })

  it("copies without calling, and runs Done, an option and a reply", async () => {
    const writeText = vi.fn().mockResolvedValue(undefined)
    Object.defineProperty(navigator, "clipboard", { value: { writeText }, configurable: true })
    renderRail()
    const action = await screen.findByRole("article", { name: "Type /to-tickets in LEG-363's session" })
    fireEvent.click(within(action).getByRole("button", { name: "Copy" }))
    await waitFor(() => expect(writeText).toHaveBeenCalledWith("/to-tickets"))
    expect(mockExecute).not.toHaveBeenCalled()

    fireEvent.click(within(action).getByRole("button", { name: "Done" }))
    await waitFor(() => expect(mockExecute).toHaveBeenCalledWith("http://legato", "att-1", "done", "2", { confirmed: false }))

    const q = screen.getByRole("article", { name: /draft PRs/ })
    fireEvent.click(within(q).getByRole("button", { name: "Yes" }))
    await waitFor(() => expect(mockExecute).toHaveBeenCalledWith("http://legato", "att-2", "option_1", "1", { confirmed: false }))
    fireEvent.click(within(q).getByRole("button", { name: "Reply…" }))
    fireEvent.change(within(q).getByRole("textbox", { name: "Reply" }), { target: { value: "Ready PRs only" } })
    fireEvent.click(within(q).getByRole("button", { name: "Send" }))
    await waitFor(() => expect(mockExecute).toHaveBeenCalledWith("http://legato", "att-2", "reply", "1", { confirmed: false, text: "Ready PRs only" }))

    fireEvent.click(within(q).getByRole("button", { name: "Dismiss" }))
    await waitFor(() => expect(mockDismiss).toHaveBeenCalledWith("http://legato", "att-2", "1"))
  })

  it("folds to a strip with the count and an icon per item, and stays folded after a reload", async () => {
    renderRail()
    await screen.findByRole("article", { name: /to-tickets/ })
    fireEvent.click(screen.getByRole("button", { name: "Fold the rail" }))
    const strip = screen.getByRole("complementary", { name: "Needs you, folded" })
    expect(within(strip).getByLabelText("2 need you")).toBeTruthy()
    const icon = within(strip).getByRole("button", { name: "Action: Type /to-tickets in LEG-363's session" })
    fireEvent.mouseEnter(icon)
    const tip = within(strip).getByRole("tooltip")
    expect(tip.textContent).toContain("Blocks 7 LEG-365 build tickets")

    cleanup()
    renderRail()
    expect(await screen.findByRole("complementary", { name: "Needs you, folded" })).toBeTruthy()
    fireEvent.click(screen.getByRole("button", { name: "Open the rail" }))
    expect(screen.getByRole("complementary", { name: "Needs you" })).toBeTruthy()
  })

  it("says how many kinds its settings hide, and when nothing needs the operator", async () => {
    mockFetchRail.mockResolvedValue({ items: [], hidden: 2 })
    renderRail()
    expect(await screen.findByText("Nothing needs you")).toBeTruthy()
    expect(screen.getByText("2 kinds hidden here")).toBeTruthy()
    expect(screen.getByRole("link", { name: "All in Attention" }).getAttribute("href")).toBe("/attention")
  })

  it("marks the items of the agent in view", async () => {
    mockSelected = "LEG-363"
    renderRail("/agents?task=LEG-363")
    const action = await screen.findByRole("article", { name: /to-tickets/ })
    expect(action.getAttribute("data-here")).toBe("true")
    expect(within(action).getByText("· this agent")).toBeTruthy()
    expect(screen.getByRole("article", { name: /draft PRs/ }).getAttribute("data-here")).toBeNull()
  })

  it("asks again when Attention changes", async () => {
    renderRail()
    await screen.findByRole("article", { name: /to-tickets/ })
    mockFetchRail.mockResolvedValue({ items: [], hidden: 0 })
    await client.invalidateQueries({ queryKey: ["http://legato", "attention"] })
    expect(await screen.findByText("Nothing needs you")).toBeTruthy()
  })
})
