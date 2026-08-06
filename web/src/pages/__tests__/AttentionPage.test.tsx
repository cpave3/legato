import { describe, it, expect, vi, beforeEach, afterEach } from "vitest"
import { render, screen, waitFor, fireEvent, cleanup } from "@testing-library/react"
import { MemoryRouter } from "react-router-dom"

const mockExecute = vi.fn().mockResolvedValue(undefined)
const mockDismiss = vi.fn().mockResolvedValue(undefined)
let mockData: unknown[] = [
  {
    id: "att-1", producer: "plans", signal_kind: "plan_decision", source_key: "pl-1", source_revision: "1",
    title: "Add search", reason: "Plan is awaiting your decision", category: "approval",
    urgency: "high", navigation: { kind: "plan", target: "pl-1" },
    actions: [
      { id: "open", label: "Open plan", kind: "navigate" },
      { id: "approve", label: "Approve plan", kind: "execute", requires_confirmation: true },
    ],
    status: "open", created_at: "2026-07-28T10:00:00Z", updated_at: "2026-07-28T10:00:00Z",
  },
]
let mockLoading = false
let mockError: string | null = null

vi.mock("../../hooks/useServer", () => ({ useServer: () => ({ baseUrl: "" }) }))
vi.mock("../../hooks/useWebSocket", () => ({ useWebSocket: () => ({ subscribe: () => () => {} }) }))
vi.mock("../../hooks/useAttention", () => ({
  useAttention: () => ({ data: mockData, loading: mockLoading, error: mockError, refresh: vi.fn() }),
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
    mockData = [
      {
        id: "att-1", producer: "plans", signal_kind: "plan_decision", source_key: "pl-1", source_revision: "1",
        title: "Add search", reason: "Plan is awaiting your decision", category: "approval",
        urgency: "high", navigation: { kind: "plan", target: "pl-1" },
        actions: [
          { id: "open", label: "Open plan", kind: "navigate" },
          { id: "approve", label: "Approve plan", kind: "execute", requires_confirmation: true },
        ],
        status: "open", created_at: "2026-07-28T10:00:00Z", updated_at: "2026-07-28T10:00:00Z",
      },
    ]
    mockLoading = false
    mockError = null
  })

  it("lists attention items with title, reason, and action buttons", async () => {
    const { AttentionPage } = await import("../AttentionPage")
    render(<MemoryRouter><AttentionPage /></MemoryRouter>)
    expect(screen.getByText("Add search")).toBeTruthy()
    expect(screen.getByText("Plan is awaiting your decision")).toBeTruthy()
    expect(screen.getByText("Approve plan")).toBeTruthy()
    expect(screen.getByText("Dismiss")).toBeTruthy()
  })

  it("executes the primary action when clicked", async () => {
    const { AttentionPage } = await import("../AttentionPage")
    render(<MemoryRouter><AttentionPage /></MemoryRouter>)
    fireEvent.click(screen.getByRole("button", { name: "Approve plan" }))
    await waitFor(() => {
      expect(mockExecute).toHaveBeenCalledWith("", "att-1", "approve", "1", { confirmed: true })
    })
  })

  it("dismisses an item when clicked", async () => {
    const { AttentionPage } = await import("../AttentionPage")
    render(<MemoryRouter><AttentionPage /></MemoryRouter>)
    fireEvent.click(screen.getByRole("button", { name: "Dismiss" }))
    await waitFor(() => {
      expect(mockDismiss).toHaveBeenCalledWith("", "att-1", "1")
    })
  })

  it("shows empty state when no items", async () => {
    mockData = []
    const { AttentionPage } = await import("../AttentionPage")
    render(<MemoryRouter><AttentionPage /></MemoryRouter>)
    expect(screen.getByText("Nothing needs your attention right now")).toBeTruthy()
  })
})
