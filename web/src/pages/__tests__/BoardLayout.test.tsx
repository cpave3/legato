import { cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import { MemoryRouter } from "react-router-dom"
import { AgentSelectionProvider } from "../../hooks/useAgentSelection"
import { BoardPage } from "../Board"
import type { BoardCard } from "../../lib/board-types"

const mockFetch = vi.fn()
const useBoard = vi.fn()

function card(id: string, title: string, status: string, overrides: Partial<BoardCard> = {}): BoardCard {
  return { id, title, status, priority: "Medium", issue_type: "Bug", provider: "", has_warning: false, agent_active: false, agent_state: "", working_seconds: 0, waiting_seconds: 0, error_seconds: 0, workspace_id: null, workspace_name: "", workspace_color: "", pr_check_status: "", pr_review_decision: "", pr_comment_count: 0, pr_is_draft: false, pr_number: 0, ...overrides }
}

const columns = [
  { name: "Backlog", cards: [card("AG-7467", "CMA selector view", "Backlog", { provider: "jira" }), card("f41p6uf0", "Package registry", "Backlog")] },
  { name: "Ready", cards: [] },
  { name: "Doing", cards: [card("jy6mab3e", "Record video", "Doing", { agent_active: true, agent_state: "working", agent_kind: "claude-code" })] },
]

vi.stubGlobal("fetch", mockFetch)
vi.mock("../../hooks/useServer", () => ({ useServer: () => ({ baseUrl: "" }) }))
vi.mock("../../hooks/useSettings", () => ({ useSettings: () => ({ data: { repositories: [], working_dir: "", worktrees_enabled: false } }) }))
vi.mock("../../hooks/useBoard", () => ({ useBoard: (filter: string) => useBoard(filter) }))
vi.mock("../../hooks/useToast", () => ({ useToast: () => ({ addToast: vi.fn() }) }))
vi.mock("../../hooks/useWorkflow", () => ({ useWorkflows: () => ({ data: { definitions: [], errors: [], repositories: [] }, loading: false, error: null }) }))

function wideScreen(wide: boolean) {
  vi.stubGlobal("matchMedia", (query: string) => ({ matches: wide, media: query, addEventListener: () => {}, removeEventListener: () => {} }))
}

function renderBoard() {
  render(<MemoryRouter><AgentSelectionProvider><BoardPage /></AgentSelectionProvider></MemoryRouter>)
}

function desktopHeader() {
  return screen.getByTestId("board-header")
}

afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
  vi.stubGlobal("fetch", mockFetch)
})

beforeEach(() => {
  localStorage.clear()
  useBoard.mockReset()
  useBoard.mockReturnValue({ columns, workspaces: [{ id: 1, name: "Work", color: "#fff" }, { id: 2, name: "Personal", color: "#000" }], loading: false, error: null, refresh: vi.fn() })
  mockFetch.mockReset()
  mockFetch.mockImplementation(async (input: RequestInfo | URL) => {
    const url = String(input)
    if (url.includes("/api/tasks/AG-7467")) return { ok: true, json: async () => ({ ...columns[0].cards[0], description_md: "The CMA selector has several issues.", local_description_md: "", remote_description_md: "The CMA selector has several issues.", remote_meta: { url: "https://jira.example/AG-7467", issue_type: "Bug" }, resumable_agent_kinds: [], pr_meta: null, code_context: null }) }
    if (url.includes("/api/agents")) return { ok: true, json: async () => [] }
    return { ok: true, json: async () => ({}) }
  })
  wideScreen(false)
})

describe("Board layout", () => {
  it("counts the cards and those with live agents", () => {
    renderBoard()
    expect(within(desktopHeader()).getByRole("heading", { name: "Board" })).toBeTruthy()
    expect(within(desktopHeader()).getByText("3 cards · 1 with live agents")).toBeTruthy()
  })

  it("filters the cards by title or key as the person types", () => {
    renderBoard()
    const filter = within(desktopHeader()).getByRole("textbox", { name: "Filter cards" })
    fireEvent.change(filter, { target: { value: "registry" } })
    expect(screen.getAllByText("Package registry").length).toBeGreaterThan(0)
    expect(screen.queryByText("CMA selector view")).toBeNull()
    fireEvent.change(filter, { target: { value: "ag-74" } })
    expect(screen.getAllByText("CMA selector view").length).toBeGreaterThan(0)
    expect(screen.queryByText("Package registry")).toBeNull()
    fireEvent.keyDown(filter, { key: "Escape" })
    expect((filter as HTMLInputElement).value).toBe("")
    expect(screen.getAllByText("Package registry").length).toBeGreaterThan(0)
  })

  it("switches workspace from the segmented control", () => {
    renderBoard()
    const switcher = within(desktopHeader()).getByRole("group", { name: "Workspace" })
    expect(within(switcher).getByRole("button", { name: "All" }).getAttribute("aria-pressed")).toBe("true")
    fireEvent.click(within(switcher).getByRole("button", { name: "Personal" }))
    expect(within(switcher).getByRole("button", { name: "Personal" }).getAttribute("aria-pressed")).toBe("true")
    expect(useBoard).toHaveBeenLastCalledWith("2")
    expect(within(switcher).getByRole("button", { name: "Unassigned" })).toBeTruthy()
  })

  it("folds empty columns into rails, and z opens and folds them", () => {
    renderBoard()
    expect(screen.getByRole("button", { name: "Ready, 0 cards" })).toBeTruthy()
    fireEvent.keyDown(window, { key: "z" })
    expect(screen.queryByRole("button", { name: "Ready, 0 cards" })).toBeNull()
    expect(screen.getByRole("region", { name: "Ready" })).toBeTruthy()
    fireEvent.keyDown(window, { key: "z" })
    expect(screen.getByRole("button", { name: "Ready, 0 cards" })).toBeTruthy()
  })

  it("steps over a folded column with l and h", () => {
    renderBoard()
    const header = (name: string) => within(screen.getByRole("region", { name })).getByText(name).closest("[data-column-header]")!
    fireEvent.keyDown(window, { key: "l" })
    expect(header("Doing").getAttribute("data-active")).toBe("true")
    fireEvent.keyDown(window, { key: "h" })
    expect(header("Backlog").getAttribute("data-active")).toBe("true")
  })

  it("lists the board's keys along the bottom", () => {
    renderBoard()
    const keys = screen.getByTestId("board-keys")
    for (const text of ["h/l", "j/k", "z"]) expect(within(keys).getByText(text)).toBeTruthy()
  })

  it("starts the cursor in the first open column when the first is folded", () => {
    useBoard.mockReturnValue({ columns: [{ name: "Triage", cards: [] }, ...columns], workspaces: [], loading: false, error: null, refresh: vi.fn() })
    renderBoard()
    const backlog = within(screen.getByRole("region", { name: "Backlog" })).getByText("Backlog").closest("[data-column-header]")!
    expect(backlog.getAttribute("data-active")).toBe("true")
  })

  describe("on a wide screen", () => {
    beforeEach(() => wideScreen(true))

    function panel() {
      return screen.getByRole("complementary", { name: "Selected task" })
    }

    it("describes the selected card beside the board", async () => {
      renderBoard()
      expect(within(panel()).getByRole("heading", { name: "CMA selector view" })).toBeTruthy()
      expect(within(panel()).getByText("jira · AG-7467")).toBeTruthy()
      expect(within(panel()).getByText("Backlog")).toBeTruthy()
      expect(within(panel()).getByText("None yet")).toBeTruthy()
      await waitFor(() => expect(within(panel()).getByText("The CMA selector has several issues.")).toBeTruthy())
      expect(within(panel()).getByRole("button", { name: "Open in Jira" })).toBeTruthy()
    })

    it("selects a clicked card without opening its details", () => {
      renderBoard()
      fireEvent.click(screen.getAllByText("Record video")[0])
      expect(within(panel()).getByRole("heading", { name: "Record video" })).toBeTruthy()
      expect(within(panel()).getByText("Running · Claude Code")).toBeTruthy()
      expect(screen.queryByRole("dialog")).toBeNull()
    })

    it("opens the full details with Enter, a double click, or the panel's button", async () => {
      renderBoard()
      fireEvent.keyDown(window, { key: "Enter" })
      expect(await screen.findByRole("dialog", { name: "CMA selector view" })).toBeTruthy()
      fireEvent.keyDown(window, { key: "Escape" })
      expect(screen.queryByRole("dialog")).toBeNull()
      fireEvent.doubleClick(screen.getAllByText("CMA selector view")[0])
      expect(await screen.findByRole("dialog", { name: "CMA selector view" })).toBeTruthy()
      fireEvent.keyDown(window, { key: "Escape" })
      fireEvent.click(within(panel()).getByRole("button", { name: "Open details" }))
      expect(await screen.findByRole("dialog", { name: "CMA selector view" })).toBeTruthy()
    })

    it("starts an agent on the selected card with s", async () => {
      renderBoard()
      await within(panel()).findByRole("button", { name: "Open agent" })
      expect(within(screen.getByTestId("board-keys")).getByText("s")).toBeTruthy()
      fireEvent.keyDown(window, { key: "s" })
      expect(await screen.findByRole("dialog", { name: "Open task agent" })).toBeTruthy()
    })

    it("leaves board keys alone while a dialog from the panel is open", async () => {
      renderBoard()
      fireEvent.click(await within(panel()).findByRole("button", { name: "Open agent" }))
      await screen.findByRole("dialog", { name: "Open task agent" })
      fireEvent.keyDown(document.body, { key: "j" })
      expect(within(panel()).getByRole("heading", { name: "CMA selector view" })).toBeTruthy()
      expect(screen.getByRole("dialog", { name: "Open task agent" })).toBeTruthy()
    })

    it("hides the panel until another card is selected", () => {
      renderBoard()
      fireEvent.click(within(panel()).getByRole("button", { name: "Close task panel" }))
      expect(screen.queryByRole("complementary", { name: "Selected task" })).toBeNull()
      fireEvent.keyDown(window, { key: "j" })
      expect(within(panel()).getByRole("heading", { name: "Package registry" })).toBeTruthy()
    })
  })

  it("has no panel on a narrower screen, where a click opens the details", async () => {
    renderBoard()
    expect(screen.queryByRole("complementary", { name: "Selected task" })).toBeNull()
    fireEvent.click(screen.getAllByText("CMA selector view")[0])
    expect(await screen.findByRole("dialog", { name: "CMA selector view" })).toBeTruthy()
  })

  it("keeps the filter behind a button on a phone", () => {
    renderBoard()
    const header = screen.getByTestId("board-header-mobile")
    expect(within(header).getByRole("heading", { name: "Board" })).toBeTruthy()
    expect(within(header).queryByRole("textbox", { name: "Filter cards" })).toBeNull()
    fireEvent.click(within(header).getByRole("button", { name: "Filter" }))
    fireEvent.change(within(header).getByRole("textbox", { name: "Filter cards" }), { target: { value: "registry" } })
    expect(within(header).getByRole("group", { name: "Workspace" })).toBeTruthy()
    expect(screen.queryByText("CMA selector view")).toBeNull()
    fireEvent.click(within(header).getByRole("button", { name: "New card" }))
    expect(screen.getByRole("dialog")).toBeTruthy()
  })
})
