import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import { cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react"
import { MemoryRouter } from "react-router-dom"
import { AgentSelectionProvider } from "../../hooks/useAgentSelection"
import { BoardPage } from "../Board"
import type { BoardCard } from "../../lib/board-types"

// The main's web board with one worker paired: Hand off… from a card's menu
// opens the form, and an away task sits in the Away lane, not its column.

function card(id: string, title: string, status: string, more: Partial<BoardCard> = {}): BoardCard {
  return {
    id, title, priority: "", issue_type: "", status, provider: "", has_warning: false, agent_active: false, agent_state: "",
    working_seconds: 0, waiting_seconds: 0, error_seconds: 0, workspace_id: null, workspace_name: "", workspace_color: "", ...more,
  }
}

const home = card("T-1", "Fix login", "Ready")
const away = card("T-2", "Find the leak", "Doing", {
  away: {
    worker_instance_id: "srv-1", worker: "vm-1", handoff_id: "ho-1", worker_task_id: "w-1", purpose: "find-out", kind: "new-branch",
    repo: "acme/widget", branch: "legato/t-2", left_column: "Doing", handed_off_at: "2026-11-05T09:00:00Z", state: "pending",
  },
})

const mockFetch = vi.fn()
vi.stubGlobal("fetch", mockFetch)
vi.mock("../../hooks/useServer", () => ({ useServer: () => ({ baseUrl: "" }) }))
vi.mock("../../hooks/useSettings", () => ({ useSettings: () => ({ data: { repositories: [{ name: "widget", repo: "acme/widget", primary_dir: "" }], working_dir: "", worktrees_enabled: false } }) }))
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
  mockFetch.mockImplementation(async (input: RequestInfo | URL) => {
    const url = String(input)
    const json = (body: unknown) => new Response(JSON.stringify(body), { status: 200, headers: { "Content-Type": "application/json" } })
    if (url.endsWith("/api/settings/workers")) {
      return json({ workers: [{ instance_id: "srv-1", address: "https://vm-1.lan:3080", name: "vm-1", main_name: "workstation", sandbox: true, handoff_version: 2, paired_at: "2026-11-03T09:00:00Z" }], instance_id: "srv-main", main_name: "workstation" })
    }
    if (url.endsWith("/api/adapters")) return json({ adapters: ["claude-code"], default: "claude-code" })
    if (url.includes("/api/tasks/")) return json({ id: url.split("/").pop(), title: "", description_md: "", status: "" })
    return json({})
  })
})

afterEach(cleanup)

function renderBoard() {
  render(<MemoryRouter><AgentSelectionProvider><BoardPage /></AgentSelectionProvider></MemoryRouter>)
}

describe("Handing off from the board", () => {
  it("puts away tasks in the Away lane, grouped by worker, and out of their columns", () => {
    renderBoard()
    // Its column is empty now, so folded to a rail.
    expect(screen.getByRole("button", { name: "Doing, 0 cards" })).toBeTruthy()
    const lane = screen.getByRole("region", { name: "Away" })
    const vm1 = within(lane).getByRole("group", { name: "vm-1" })
    expect(within(vm1).getByRole("button", { name: /Find the leak/ }).textContent).toContain("from Doing")
  })

  it("opens the Hand off form from a card's right-click menu", async () => {
    renderBoard()
    const ready = screen.getByRole("region", { name: "Ready" })
    fireEvent.contextMenu(within(ready).getByText("Fix login"))
    fireEvent.click(screen.getByRole("menuitem", { name: "Hand off…" }))
    expect(await screen.findByRole("dialog", { name: "Hand off “Fix login”" })).toBeTruthy()
  })

  it("opens the Hand off form from the task panel", async () => {
    renderBoard()
    fireEvent.click(within(screen.getByRole("region", { name: "Ready" })).getByText("Fix login"))
    const panel = screen.getByRole("complementary", { name: "Selected task" })
    fireEvent.click(within(panel).getByRole("button", { name: "Hand off…" }))
    expect(await screen.findByRole("dialog", { name: "Hand off “Fix login”" })).toBeTruthy()
  })

  it("shows a selected away task's panel read-only", async () => {
    renderBoard()
    fireEvent.click(screen.getByRole("button", { name: /Find the leak/ }))
    const panel = await screen.findByRole("complementary", { name: "Selected task" })
    await waitFor(() => expect(within(panel).getByText(/read-only while away/i)).toBeTruthy())
    expect(within(panel).queryByRole("button", { name: "Move" })).toBeNull()
  })
})
