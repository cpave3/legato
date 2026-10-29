import { describe, it, expect, vi, beforeEach, afterEach } from "vitest"
import { render, screen, waitFor, fireEvent, cleanup, within } from "@testing-library/react"
import { MemoryRouter } from "react-router-dom"

const mockExecute = vi.fn().mockResolvedValue(undefined)
const mockDismiss = vi.fn().mockResolvedValue(undefined)
const mockRefresh = vi.fn().mockResolvedValue(undefined)
let mockData: unknown[] = []
let mockLoading = false
let mockError: string | null = null

function item(overrides: Record<string, unknown> = {}) {
  return {
    id: "att-plan", producer: "plans", signal_kind: "plan_decision", source_key: "pl-1", source_revision: "1",
    title: "Add search", reason: "Plan is awaiting your decision", category: "approval",
    urgency: "high", navigation: { kind: "plan", target: "pl-1" },
    actions: [
      { id: "open", label: "Open plan", kind: "navigate" },
      { id: "approve", label: "Approve plan", kind: "execute", requires_confirmation: true },
    ],
    status: "open", created_at: "2026-07-28T10:00:00Z", updated_at: "2026-07-28T10:00:00Z",
    ...overrides,
  }
}

vi.mock("../../hooks/useServer", () => ({ useServer: () => ({ baseUrl: "" }) }))
vi.mock("../../hooks/useWebSocket", () => ({ useWebSocket: () => ({ subscribe: () => () => {} }) }))
vi.mock("../../hooks/useAttention", () => ({
  useAttention: () => ({ data: mockData, loading: mockLoading, error: mockError, refresh: mockRefresh }),
}))
vi.mock("../../lib/attention", () => ({
  fetchAttention: vi.fn(),
  executeAttentionAction: (...args: unknown[]) => mockExecute(...args),
  dismissAttentionItem: (...args: unknown[]) => mockDismiss(...args),
}))

describe("AttentionPage", () => {
  afterEach(() => cleanup())
  beforeEach(() => {
    vi.clearAllMocks()
    mockData = [item()]
    mockLoading = false
    mockError = null
  })

  it("groups intervention, review, and PR items into a single dashboard", async () => {
    mockData = [
      item(),
      item({
        id: "att-review", producer: "reviews", signal_kind: "review_ready",
        title: "Review dashboard", reason: "Ready for your review",
        navigation: { kind: "review", target: "rt-1" },
        actions: [{ id: "open", label: "Open review", kind: "navigate" }],
      }),
      item({
        id: "att-pr", producer: "pull_requests", signal_kind: "pr_merge_blocked",
        title: "PR #42 · Provider sync", reason: "Changes requested · CI failing",
        navigation: { kind: "task", target: "task-1" },
        actions: [{ id: "open", label: "Open task", kind: "navigate" }],
      }),
    ]
    const { AttentionPage } = await import("../AttentionPage")
    render(<MemoryRouter><AttentionPage /></MemoryRouter>)

    expect(screen.getByRole("heading", { name: /Attention dashboard/ })).toBeTruthy()
    const intervention = screen.getByRole("region", { name: /Needs intervention/ })
    const review = screen.getByRole("region", { name: /Ready for review/ })
    const merge = screen.getByRole("region", { name: /Merge blockers/ })
    expect(within(intervention).getByText("Add search")).toBeTruthy()
    expect(within(review).getByText("Review dashboard")).toBeTruthy()
    expect(within(merge).getByText("PR #42 · Provider sync")).toBeTruthy()
    expect(screen.getByText("3 items")).toBeTruthy()
  })

  it("keeps empty sections visible while another section has work", async () => {
    const { AttentionPage } = await import("../AttentionPage")
    render(<MemoryRouter><AttentionPage /></MemoryRouter>)
    expect(screen.getByText("No work is ready for review")).toBeTruthy()
    expect(screen.getByText("No known merge blockers")).toBeTruthy()
    expect(screen.queryByText("Nothing needs your attention right now")).toBeNull()
  })

  it("puts unknown allowed signals in needs intervention", async () => {
    mockData = [item({ signal_kind: "future_signal", title: "Future work" })]
    const { AttentionPage } = await import("../AttentionPage")
    render(<MemoryRouter><AttentionPage /></MemoryRouter>)
    const intervention = screen.getByRole("region", { name: /Needs intervention/ })
    expect(within(intervention).getByText("Future work")).toBeTruthy()
  })

  it("renders an item whose actions are missing", async () => {
    mockData = [item({ id: "att-r", title: "Approval devices were reset", signal_kind: "devices_reset", actions: null })]
    const { AttentionPage } = await import("../AttentionPage")
    render(<MemoryRouter><AttentionPage /></MemoryRouter>)
    expect(screen.getByText("Approval devices were reset")).toBeTruthy()
  })

  it("renders every source-owned executable action", async () => {
    mockData = [item({ actions: [
      { id: "confirm", label: "Confirm", kind: "execute", requires_confirmation: true },
      { id: "skip", label: "Skip", kind: "execute", requires_confirmation: true },
      { id: "open", label: "Open workflow", kind: "navigate" },
    ] })]
    const { AttentionPage } = await import("../AttentionPage")
    render(<MemoryRouter><AttentionPage /></MemoryRouter>)
    expect(screen.getByRole("button", { name: "Confirm" })).toBeTruthy()
    expect(screen.getByRole("button", { name: "Skip" })).toBeTruthy()
    expect(screen.getByRole("link", { name: /Open workflow/ })).toBeTruthy()
  })

  it("executes the primary action and refreshes", async () => {
    const { AttentionPage } = await import("../AttentionPage")
    render(<MemoryRouter><AttentionPage /></MemoryRouter>)
    fireEvent.click(screen.getByRole("button", { name: "Approve plan" }))
    await waitFor(() => {
      expect(mockExecute).toHaveBeenCalledWith("", "att-plan", "approve", "1", { confirmed: true })
      expect(mockRefresh).toHaveBeenCalled()
    })
  })

  it("dismisses an item and refreshes", async () => {
    const { AttentionPage } = await import("../AttentionPage")
    render(<MemoryRouter><AttentionPage /></MemoryRouter>)
    fireEvent.click(screen.getByRole("button", { name: "Dismiss" }))
    await waitFor(() => {
      expect(mockDismiss).toHaveBeenCalledWith("", "att-plan", "1")
      expect(mockRefresh).toHaveBeenCalled()
    })
  })

  it("shows action conflicts without hiding the dashboard", async () => {
    mockDismiss.mockRejectedValueOnce(new Error("attention item changed; refresh and try again"))
    const { AttentionPage } = await import("../AttentionPage")
    render(<MemoryRouter><AttentionPage /></MemoryRouter>)
    fireEvent.click(screen.getByRole("button", { name: "Dismiss" }))
    expect((await screen.findByRole("alert")).textContent).toContain("attention item changed")
    expect(screen.getByText("Add search")).toBeTruthy()
  })

  it("shows the global and section all-clear states when empty", async () => {
    mockData = []
    const { AttentionPage } = await import("../AttentionPage")
    render(<MemoryRouter><AttentionPage /></MemoryRouter>)
    expect(screen.getByText("Nothing needs your attention right now")).toBeTruthy()
    expect(screen.getByText("No interventions needed")).toBeTruthy()
    expect(screen.getByText("No work is ready for review")).toBeTruthy()
    expect(screen.getByText("No known merge blockers")).toBeTruthy()
  })

  it("renders loading and source errors", async () => {
    const { AttentionPage } = await import("../AttentionPage")
    mockLoading = true
    const loading = render(<MemoryRouter><AttentionPage /></MemoryRouter>)
    expect(screen.getByRole("status", { name: "Loading attention…" })).toBeTruthy()
    loading.unmount()

    mockLoading = false
    mockError = "attention unavailable"
    render(<MemoryRouter><AttentionPage /></MemoryRouter>)
    expect(screen.getByRole("alert").textContent).toContain("attention unavailable")
  })
})
