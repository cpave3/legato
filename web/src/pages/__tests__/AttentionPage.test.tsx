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

const mockStartReeve = vi.fn()
vi.mock("../../lib/reeve", async (importOriginal) => ({
  ...(await importOriginal<typeof import("../../lib/reeve")>()),
  fetchReeve: () => Promise.resolve({ history: [], folder: "/r" }),
  startReeve: (...a: unknown[]) => mockStartReeve(...a),
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

  it("shows Argus's cases with a link to the case and to the task that owns it", async () => {
    mockData = [
      item({
        id: "att-case", producer: "argus", signal_kind: "argus_case", category: "argus",
        title: "CI failing: acme/widget#12", reason: "real failure", task_id: "task-1",
        navigation: { kind: "external", target: "https://argus.lan/cases/1" }, actions: [],
      }),
      item({
        id: "att-needs", producer: "argus", signal_kind: "argus_needs_you", category: "argus",
        title: "Rerun proposed", reason: "waiting for you",
        navigation: { kind: "external", target: "https://argus.lan/cases/2" }, actions: [],
      }),
    ]
    const { AttentionPage } = await import("../AttentionPage")
    render(<MemoryRouter><AttentionPage /></MemoryRouter>)
    const merge = screen.getByRole("region", { name: /Merge blockers/ })
    const intervention = screen.getByRole("region", { name: /Needs intervention/ })
    expect(within(intervention).getByText("Rerun proposed")).toBeTruthy()
    const card = within(merge).getByText("CI failing: acme/widget#12").closest("li") as HTMLElement
    const open = within(card).getByRole("link", { name: /Open in Argus/ })
    expect(open.getAttribute("href")).toBe("https://argus.lan/cases/1")
    expect(open.getAttribute("target")).toBe("_blank")
    expect(open.getAttribute("rel")).toBe("noopener noreferrer")
    expect(within(card).getByRole("link", { name: /Open task/ }).getAttribute("href")).toBe("/board?highlight_task=task-1")
    const unattached = within(intervention).getByText("Rerun proposed").closest("li") as HTMLElement
    expect(within(unattached).queryByRole("link", { name: /Open task/ })).toBeNull()
  })

  it("puts a task done on a worker in Ready for review, with Return, Open on worker and the away task", async () => {
    mockData = [
      item({
        id: "att-done", producer: "handoff", signal_kind: "handoff_done", category: "handoff", urgency: "normal",
        title: "Fix it is done on vm-1", reason: "Return it", task_id: "task-1", source_revision: "done",
        navigation: { kind: "task", target: "task-1" },
        actions: [
          { id: "return", label: "Return", kind: "execute", intent: "primary" },
          { id: "open_worker", label: "Open on vm-1", kind: "navigate", href: "https://vm-1.lan:3080/board?highlight_task=w-1" },
          { id: "open", label: "Open task", kind: "navigate" },
        ],
      }),
      item({
        id: "att-down", producer: "handoff", signal_kind: "worker_unreachable", category: "handoff",
        title: "vm-1 unreachable since 21:40, 2 away tasks", reason: "Legato keeps trying",
        navigation: { kind: "settings", target: "workers" },
        actions: [{ id: "open", label: "Open Settings › Workers", kind: "navigate", intent: "primary" }],
      }),
    ]
    const { AttentionPage } = await import("../AttentionPage")
    render(<MemoryRouter><AttentionPage /></MemoryRouter>)
    const review = screen.getByRole("region", { name: /Ready for review/ })
    const card = within(review).getByText("Fix it is done on vm-1").closest("li") as HTMLElement
    const worker = within(card).getByRole("link", { name: /Open on vm-1/ })
    expect(worker.getAttribute("href")).toBe("https://vm-1.lan:3080/board?highlight_task=w-1")
    expect(worker.getAttribute("target")).toBe("_blank")
    expect(within(card).getByRole("link", { name: /Open task/ }).getAttribute("href")).toBe("/board?highlight_task=task-1")
    fireEvent.click(within(card).getByRole("button", { name: "Return" }))
    await waitFor(() => expect(mockExecute).toHaveBeenCalledWith("", "att-done", "return", "done", { confirmed: false }))

    const intervention = screen.getByRole("region", { name: /Needs intervention/ })
    const down = within(intervention).getByText(/vm-1 unreachable since/).closest("li") as HTMLElement
    expect(within(down).getByRole("link", { name: /Open Settings › Workers/ }).getAttribute("href")).toBe("/settings#workers")
  })

  it("asks before an action that says what it confirms, and does nothing when the person declines", async () => {
    mockData = [item({
      id: "att-lost", producer: "handoff", signal_kind: "handoff_lost", category: "handoff",
      title: "Gone is lost on vm-1", reason: "vm-1 no longer has this task", task_id: "task-1", source_revision: "lost:1",
      navigation: { kind: "task", target: "task-1" },
      actions: [{ id: "take_back", label: "Take back", kind: "execute", requires_confirmation: true, confirmation: "Take back Gone without its results?" }],
    })]
    const confirm = vi.spyOn(window, "confirm").mockReturnValueOnce(false).mockReturnValueOnce(true)
    const { AttentionPage } = await import("../AttentionPage")
    render(<MemoryRouter><AttentionPage /></MemoryRouter>)
    fireEvent.click(screen.getByRole("button", { name: "Take back" }))
    expect(confirm).toHaveBeenCalledWith("Take back Gone without its results?")
    expect(mockExecute).not.toHaveBeenCalled()
    fireEvent.click(screen.getByRole("button", { name: "Take back" }))
    await waitFor(() => expect(mockExecute).toHaveBeenCalledWith("", "att-lost", "take_back", "lost:1", { confirmed: true }))
    confirm.mockRestore()
  })

  it("shows an item's detail as Markdown without raw HTML, folded when long", async () => {
    const long = ["**Flaky** at 0.8.", "", "- rerun proposed", "- waiting on you", "", "Line five", "", "Line six", "<img src=x onerror=alert(1)>"].join("\n")
    mockData = [item({ detail: long })]
    const { AttentionPage } = await import("../AttentionPage")
    const { container } = render(<MemoryRouter><AttentionPage /></MemoryRouter>)
    expect(screen.getByText("Flaky").tagName).toBe("STRONG")
    expect(container.querySelector("img")).toBeNull()
    fireEvent.click(screen.getByRole("button", { name: "More" }))
    expect(screen.getByRole("button", { name: "Less" })).toBeTruthy()
  })

  it("opens the links in a detail in a new tab, and drops script links", async () => {
    mockData = [item({ detail: "**Links:**\n- [Pull request acme/widget#12](https://github.com/acme/widget/pull/12)\n- [Bad](javascript:alert(1))" })]
    const { AttentionPage } = await import("../AttentionPage")
    render(<MemoryRouter><AttentionPage /></MemoryRouter>)
    const pr = screen.getByRole("link", { name: "Pull request acme/widget#12" })
    expect(pr.getAttribute("href")).toBe("https://github.com/acme/widget/pull/12")
    expect(pr.getAttribute("target")).toBe("_blank")
    expect(pr.getAttribute("rel")).toContain("noreferrer")
    expect(screen.getByText("Bad").closest("a")?.getAttribute("href") ?? "").not.toContain("javascript")
  })

  it("leaves a short detail unfolded", async () => {
    mockData = [item({ detail: "Short and plain." })]
    const { AttentionPage } = await import("../AttentionPage")
    render(<MemoryRouter><AttentionPage /></MemoryRouter>)
    expect(screen.getByText("Short and plain.")).toBeTruthy()
    expect(screen.queryByRole("button", { name: "More" })).toBeNull()
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

describe("AttentionPage: Ask the Reeve", () => {
  afterEach(() => cleanup())
  beforeEach(() => {
    mockLoading = false
    mockError = null
  })

  it("starts the Reeve with what the item is, its IDs and its reason", async () => {
    mockData = [item({ id: "att-sync", producer: "sync", title: "Jira sync failed", reason: "Jira answered 401", task_id: "LEG-3" })]
    mockStartReeve.mockResolvedValue({ task_id: "rv-1" })
    const { AttentionPage } = await import("../AttentionPage")
    render(<MemoryRouter><AttentionPage /></MemoryRouter>)
    fireEvent.click(await screen.findByRole("button", { name: "Ask the Reeve" }))
    await waitFor(() => expect(mockStartReeve).toHaveBeenCalled())
    const brief = mockStartReeve.mock.calls[0][1].brief as string
    expect(brief).toContain("the Attention item “Jira sync failed”")
    expect(brief).toContain("- Attention item: att-sync")
    expect(brief).toContain("- task: LEG-3")
    expect(brief).toContain("- Error: Jira answered 401")
  })
})
