import { describe, it, expect, vi, beforeEach, afterEach } from "vitest"
import { act, render, screen, waitFor, fireEvent, cleanup, within } from "@testing-library/react"
import { MemoryRouter } from "react-router-dom"
import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import type { AttentionItem } from "../../lib/attention"
import type { Approval } from "../../lib/approvals"

const mockFetchRail = vi.fn()
const mockExecute = vi.fn()
const mockDismiss = vi.fn()
let mockSelected: string | null = null
let mockApprovals: Approval[] = []
let mockPeers: unknown[] = []
const mockDecide = vi.fn()
const mockDismissApproval = vi.fn()
const mockRefreshApprovals = vi.fn().mockResolvedValue(undefined)
let mockPasskey: unknown = undefined
const mockSign = vi.fn()

vi.mock("../../hooks/useServer", () => ({ useServer: () => ({ baseUrl: "http://legato" }) }))
vi.mock("../../hooks/useApprovals", () => ({ useApprovals: () => ({ data: mockApprovals, error: null, refresh: mockRefreshApprovals }) }))
vi.mock("../../hooks/useNavigationSummary", () => ({ useNavigationSummary: () => ({ data: { approvals: mockApprovals.length, peer_approvals: mockPeers.length } }) }))
vi.mock("../../hooks/usePeerApprovals", () => ({ usePeerApprovals: () => mockPeers }))
vi.mock("../../hooks/usePasskeyDevice", () => ({ usePasskeyDevice: () => mockPasskey }))
vi.mock("../../lib/passkeys", () => ({ signWithPasskey: (...a: unknown[]) => mockSign(...a) }))
vi.mock("../../lib/approvals", async (importOriginal) => ({
  ...(await importOriginal<typeof import("../../lib/approvals")>()),
  decideApproval: (...a: unknown[]) => mockDecide(...a),
  dismissApproval: (...a: unknown[]) => mockDismissApproval(...a),
}))
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
import { resetRailState, showApproval } from "../../lib/railState"
import { getFocusMode, getFocusStart, setFocusMode } from "../../lib/focusMode"

function permission(overrides: Partial<Approval> = {}): Approval {
  return {
    id: "ap-11111111", source: "agent_permission", source_key: "ex/s/r1", task_id: "task-1",
    title: "Run a command", summary: "pnpm -C web exec tsc -b",
    details: { request_id: "r1", agent_kind: "claude-code", task_title: "Fix the rail", tool: "Bash", command: "pnpm -C web exec tsc -b" },
    options: [
      { id: "once", label: "Allow once", kind: "allow" },
      { id: "always", label: "Allow, and don't ask again", kind: "allow" },
      { id: "deny", label: "Deny", kind: "deny" },
    ],
    blocking: true, status: "pending", revision: 1, created_at: "2026-11-20T08:00:00Z",
    ...overrides,
  } as Approval
}

function questionApproval(): Approval {
  return permission({
    id: "ap-22222222", source: "agent_question", title: "Which database?", summary: "",
    details: { request_id: "q1", agent_kind: "codex", task_title: "Add search",
      questions: [{ id: "q1", question: "Which database?", options: [{ label: "SQLite" }, { label: "Postgres" }], multiSelect: false, allowOther: false }] },
    options: [],
  })
}

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
    mockFetchRail.mockResolvedValue({ items: [item(), question()], hidden: 0, hiddenNames: [] })
    mockApprovals = []
    mockPeers = []
    mockDecide.mockResolvedValue(undefined)
    mockPasskey = undefined
    setFocusMode(false)
    mockExecute.mockResolvedValue(undefined)
    mockDismiss.mockResolvedValue(undefined)
    mockSelected = null
    window.localStorage.clear()
    resetRailState()
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
    // A reload reads the fold from this browser's storage.
    resetRailState()
    renderRail()
    expect(await screen.findByRole("complementary", { name: "Needs you, folded" })).toBeTruthy()
    fireEvent.click(screen.getByRole("button", { name: "Open the rail" }))
    expect(screen.getByRole("complementary", { name: "Needs you" })).toBeTruthy()
  })

  it("says how many kinds its settings hide, and when nothing needs the operator", async () => {
    mockFetchRail.mockResolvedValue({ items: [], hidden: 2, hiddenNames: ["review", "merge"] })
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
    mockFetchRail.mockResolvedValue({ items: [], hidden: 0, hiddenNames: [] })
    await client.invalidateQueries({ queryKey: ["http://legato", "attention"] })
    expect(await screen.findByText("Nothing needs you")).toBeTruthy()
  })

  describe("approvals", () => {
    it("come first, with the tray's controls, and are not listed twice", async () => {
      mockApprovals = [permission()]
      mockFetchRail.mockResolvedValue({ items: [item({ id: "att-ap", producer: "approvals", signal_kind: "approval", title: "Run a command" }), item()], hidden: 0, hiddenNames: [] })
      renderRail()
      const rail = screen.getByRole("complementary", { name: "Needs you" })
      await within(rail).findByRole("article", { name: /to-tickets/ })
      const articles = within(rail).getAllByRole("article")
      expect(articles[0].textContent).toContain("Run a command")
      expect(within(rail).getAllByText("Run a command")).toHaveLength(1)
      expect(within(rail).getByText("2")).toBeTruthy()

      fireEvent.click(within(articles[0]).getByRole("button", { name: "Allow once" }))
      await waitFor(() => expect(mockDecide).toHaveBeenCalledWith("http://legato", "ap-11111111", { decision: "allow", option: "once" }))
    })

    it("deny with a note", async () => {
      mockApprovals = [permission()]
      renderRail()
      const card = (await screen.findByText("Run a command")).closest("article")!
      fireEvent.click(within(card).getByRole("button", { name: "Deny…" }))
      fireEvent.change(within(card).getByRole("textbox", { name: "Note for the agent" }), { target: { value: "Run the narrower check" } })
      fireEvent.click(within(card).getByRole("button", { name: "Deny" }))
      await waitFor(() => expect(mockDecide).toHaveBeenCalledWith("http://legato", "ap-11111111", { decision: "deny", feedback: "Run the narrower check" }))
    })

    it("signs an approval that needs a signature with this computer's passkey", async () => {
      mockApprovals = [permission({ signed: true })]
      mockPasskey = { id: "pk-1" }
      mockSign.mockResolvedValue(undefined)
      renderRail()
      fireEvent.click(await screen.findByRole("button", { name: "Sign with passkey: Allow once" }))
      await waitFor(() => expect(mockSign).toHaveBeenCalledWith("http://legato", "ap-11111111", { id: "pk-1" }, "once"))
    })

    it("draws an agent's question as one compact row: its options, Reply, Open agent and Focus", async () => {
      const asked = questionApproval()
      asked.details.questions = [{ id: "q1", question: "Which database?", options: [{ label: "SQLite", description: "No new service" }, { label: "Postgres" }], multiSelect: false }]
      mockApprovals = [asked]
      mockFetchRail.mockResolvedValue({ items: [], hidden: 0, hiddenNames: [] })
      renderRail()
      const row = await screen.findByRole("article", { name: "Which database?" })
      expect(within(row).getByText("Question")).toBeTruthy()
      expect(within(row).getByRole("button", { name: "SQLite" }).getAttribute("title")).toBe("No new service")
      expect(within(row).getByRole("link", { name: "Open agent" }).getAttribute("href")).toBe("/agents?task=task-1")
      expect(within(row).getByRole("button", { name: "Focus" })).toBeTruthy()
      // Not the full card: no radio choices and no Send answer.
      expect(within(row).queryByRole("radio")).toBeNull()
      expect(within(row).queryByRole("button", { name: /Send answer/ })).toBeNull()

      fireEvent.click(within(row).getByRole("button", { name: "Postgres" }))
      await waitFor(() => expect(mockDecide).toHaveBeenCalledWith("http://legato", "ap-22222222", { decision: "answer", answers: { q1: ["Postgres"] } }))
      expect(mockRefreshApprovals).toHaveBeenCalled()

      fireEvent.click(within(row).getByRole("button", { name: "Reply…" }))
      fireEvent.change(within(row).getByRole("textbox", { name: "Reply" }), { target: { value: "Neither: use the file" } })
      fireEvent.click(within(row).getByRole("button", { name: "Send" }))
      await waitFor(() => expect(mockDecide).toHaveBeenCalledWith("http://legato", "ap-22222222", { decision: "answer", answers: { q1: ["Neither: use the file"] } }))
    })

    it("offers no Reply for a question that takes no answer of the operator's own", async () => {
      mockApprovals = [questionApproval()]
      renderRail()
      const row = await screen.findByRole("article", { name: "Which database?" })
      expect(within(row).getByRole("button", { name: "SQLite" })).toBeTruthy()
      expect(within(row).queryByRole("button", { name: "Reply…" })).toBeNull()
    })

    it("opens focus mode at the question whose Focus was pressed; the header's opens at the oldest", async () => {
      const later = questionApproval()
      later.id = "ap-33333333"
      later.title = "Which port?"
      mockApprovals = [questionApproval(), later]
      renderRail()
      const row = await screen.findByRole("article", { name: "Which port?" })
      fireEvent.click(within(row).getByRole("button", { name: "Focus" }))
      expect(getFocusMode()).toBe(true)
      expect(getFocusStart()).toBe("ap-33333333")
      setFocusMode(false)
      fireEvent.click(screen.getByRole("button", { name: "Answer in focus mode" }))
      expect(getFocusMode()).toBe(true)
      expect(getFocusStart()).toBeNull()
    })

    it("sends several questions to focus mode with Answer…", async () => {
      const asked = questionApproval()
      asked.title = "Share links"
      asked.details.questions = [
        { id: "a", question: "Say which conflict?", options: [{ label: "Yes" }, { label: "No" }] },
        { id: "b", question: "Fresh schema?", options: [{ label: "Yes" }, { label: "No" }] },
      ]
      mockApprovals = [asked]
      renderRail()
      const row = await screen.findByRole("article", { name: "Share links" })
      expect(within(row).getByText("2 questions")).toBeTruthy()
      expect(within(row).queryByRole("button", { name: "Yes" })).toBeNull()
      expect(within(row).queryByRole("button", { name: "Reply…" })).toBeNull()
      fireEvent.click(within(row).getByRole("button", { name: "Answer…" }))
      expect(getFocusMode()).toBe(true)
      expect(getFocusStart()).toBe("ap-22222222")
    })

    it("dismisses a question", async () => {
      mockApprovals = [questionApproval()]
      mockDismissApproval.mockResolvedValue(undefined)
      renderRail()
      const row = await screen.findByRole("article", { name: "Which database?" })
      fireEvent.click(within(row).getByRole("button", { name: "Dismiss" }))
      await waitFor(() => expect(mockDismissApproval).toHaveBeenCalledWith("http://legato", "ap-22222222"))
    })

    it("marks a paired instance's change set with its instance", async () => {
      mockPeers = [{ id: "cs-1", instance: "laptop", instance_id: "i-2", instance_url: "https://laptop.lan", url: "https://laptop.lan/attention?approval=cs-1", title: "Restart the worker", signed: true }]
      renderRail()
      const peer = await screen.findByTestId("peer-approval-cs-1")
      expect(within(peer).getByText("laptop")).toBeTruthy()
      expect(within(peer).getByRole("link", { name: "◈ Approve on laptop" })).toBeTruthy()
    })

    it("a new approval unfolds a folded rail", async () => {
      const view = renderRail()
      await screen.findByRole("article", { name: /to-tickets/ })
      fireEvent.click(screen.getByRole("button", { name: "Fold the rail" }))
      expect(screen.getByRole("complementary", { name: "Needs you, folded" })).toBeTruthy()
      // approvals_changed refetches the approvals, and one is new.
      mockApprovals = [permission()]
      view.rerender(<QueryClientProvider client={client}><MemoryRouter initialEntries={["/board"]}><NeedsYouRail /></MemoryRouter></QueryClientProvider>)
      expect(await screen.findByRole("complementary", { name: "Needs you" })).toBeTruthy()
      expect(screen.getByText("Run a command")).toBeTruthy()
    })

    it("opens on a linked approval and shows it even with approvals switched off", async () => {
      mockApprovals = [permission(), permission({ id: "ap-33333333", title: "Push the branch" })]
      mockFetchRail.mockResolvedValue({ items: [], hidden: 1, hiddenNames: ["approvals"] })
      window.localStorage.setItem("legato:rail-folded", "1")
      resetRailState()
      renderRail()
      expect(await screen.findByRole("complementary", { name: "Needs you, folded" })).toBeTruthy()
      act(() => showApproval("ap-33333333"))
      const rail = await screen.findByRole("complementary", { name: "Needs you" })
      const card = (await within(rail).findByText("Push the branch")).closest("article")!
      expect(card.getAttribute("data-focused")).toBe("true")
      expect(within(rail).queryByText("Run a command")).toBeNull()
    })
  })
})
