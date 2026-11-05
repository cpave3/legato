import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import { cleanup, render, screen, within } from "@testing-library/react"
import { MemoryRouter } from "react-router-dom"
import { AgentSelectionProvider } from "../../hooks/useAgentSelection"
import { BoardPage } from "../Board"
import { legatoHref } from "../../lib/navigation"
import type { BoardCard } from "../../lib/board-types"

// An Attention item about an away task (a Merge blocker on one of its PRs)
// opens the board with the task picked out in the Away lane.

function card(id: string, title: string, status: string, more: Partial<BoardCard> = {}): BoardCard {
  return {
    id, title, priority: "", issue_type: "", status, provider: "", has_warning: false, agent_active: false, agent_state: "",
    working_seconds: 0, waiting_seconds: 0, error_seconds: 0, workspace_id: null, workspace_name: "", workspace_color: "", ...more,
  }
}

const home = card("T-1", "Fix login", "Ready")
const away = card("T-2", "Split the parser", "Doing", {
  pull_requests: [{ repo: "acme/widget", branch: "legato/t-2-api", pr_number: 8, pr_url: "https://github.com/acme/widget/pull/8", state: "OPEN", is_draft: false, review_decision: "CHANGES_REQUESTED", check_status: "fail", comment_count: 0, pinned: false, match_method: "branch", sync_outcome: "linked", sync_error: "" }],
  away: {
    worker_instance_id: "srv-1", worker: "vm-1", handoff_id: "ho-1", worker_task_id: "w-1", purpose: "make-change", kind: "new-branch",
    repo: "acme/widget", branch: "legato/t-2", left_column: "Doing", handed_off_at: "2026-11-05T09:00:00Z", state: "in_progress", view: "working",
    branches: ["legato/t-2-api"],
  },
})

const mockFetch = vi.fn()
vi.stubGlobal("fetch", mockFetch)
vi.mock("../../hooks/useServer", () => ({ useServer: () => ({ baseUrl: "" }) }))
vi.mock("../../hooks/useSettings", () => ({ useSettings: () => ({ data: { repositories: [], working_dir: "", worktrees_enabled: false } }) }))
vi.mock("../../hooks/useBoard", () => ({
  useBoard: () => ({
    columns: [{ name: "Ready", cards: [home] }, { name: "Doing", cards: [away] }],
    workspaces: [],
    workers: [{ instance_id: "srv-1", name: "vm-1", address: "https://vm-1.lan:3080" }],
    loading: false,
    error: null,
    refresh: vi.fn(),
  }),
}))
vi.mock("../../hooks/useToast", () => ({ useToast: () => ({ addToast: vi.fn() }) }))
vi.mock("../../hooks/useWorkflow", () => ({ useWorkflows: () => ({ data: null, loading: false, error: null }) }))
vi.mock("../../hooks/useMediaQuery", () => ({ useMediaQuery: () => true }))

beforeEach(() => {
  Object.defineProperty(HTMLElement.prototype, "scrollIntoView", { configurable: true, value: vi.fn() })
  mockFetch.mockImplementation(async (input: RequestInfo | URL) => {
    const url = String(input)
    const json = (body: unknown) => new Response(JSON.stringify(body), { status: 200, headers: { "Content-Type": "application/json" } })
    if (url.includes("/api/tasks/")) return json({ id: url.split("/").pop(), title: "", description_md: "", status: "" })
    return json({})
  })
})

afterEach(cleanup)

describe("an Attention item about an away task", () => {
  it("opens the board with the away row selected and its panel showing its PRs", () => {
    const href = legatoHref({ kind: "task", target: "T-2" })!
    render(<MemoryRouter initialEntries={[href]}><AgentSelectionProvider><BoardPage /></AgentSelectionProvider></MemoryRouter>)
    const lane = screen.getAllByRole("region", { name: "Away" }).find(region => !region.closest("aside"))!
    const row = within(lane).getByRole("button", { name: /Split the parser/ })
    expect(row.getAttribute("data-selected")).toBe("true")
    const panel = screen.getByRole("complementary", { name: "Selected task" })
    expect(within(panel).getByRole("link", { name: "#8" })).toBeTruthy()
    expect(within(panel).getByText("changes requested")).toBeTruthy()
  })
})
