import { cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import { MemoryRouter } from "react-router-dom"
import { AgentSelectionProvider } from "../../hooks/useAgentSelection"
import { BoardPage } from "../Board"

const mockFetch = vi.fn()
const base = { status: "Backlog", priority: "", issue_type: "Task", provider: "", has_warning: false, agent_active: false, agent_state: "", working_seconds: 0, waiting_seconds: 0, error_seconds: 0, workspace_name: "", workspace_color: "" }
const first = { ...base, id: "TASK-1", title: "First task" }
const second = { ...base, id: "TASK-2", title: "Second task" }
const doing = { ...base, id: "TASK-3", title: "Doing task", status: "Doing" }

vi.stubGlobal("fetch", mockFetch)
vi.mock("../../hooks/useServer", () => ({ useServer: () => ({ baseUrl: "" }) }))
vi.mock("../../hooks/useSettings", () => ({ useSettings: () => ({ data: { repositories: [], working_dir: "", worktrees_enabled: false } }) }))
vi.mock("../../hooks/useBoard", () => ({ useBoard: () => ({ columns: [{ name: "Backlog", cards: [first, second] }, { name: "Doing", cards: [doing] }, { name: "Done", cards: [] }], workspaces: [], loading: false, error: null, refresh: vi.fn() }) }))
vi.mock("../../hooks/useToast", () => ({ useToast: () => ({ addToast: vi.fn() }) }))
vi.mock("../../hooks/useWorkflow", () => ({ useWorkflows: () => ({ data: { definitions: [], errors: [], repositories: [] }, loading: false, error: null }) }))

afterEach(cleanup)

function mobileBoard() {
  render(<MemoryRouter><AgentSelectionProvider><BoardPage /></AgentSelectionProvider></MemoryRouter>)
  return document.querySelector("[data-mobile-board]") as HTMLElement
}

describe("Board on a phone", () => {
  beforeEach(() => {
    vi.resetAllMocks()
    try { localStorage.clear() } catch { /* not available */ }
    mockFetch.mockImplementation(async (input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input)
      if (url.includes("/api/settings")) return { ok: true, json: async () => ({ repositories: [] }) }
      if (url.includes("/api/tasks/") && (init?.method === "DELETE" || init?.method === "PATCH")) return { ok: true, json: async () => ({}) }
      if (url.endsWith("/api/tasks/TASK-2")) return { ok: true, json: async () => ({ ...second, description_md: "", resumable_agent_kinds: [] }) }
      throw new Error(`unexpected fetch: ${url}`)
    })
  })

  it("deletes the card that was pressed, not the one under the cursor", async () => {
    const board = mobileBoard()
    const pressed = within(board).getByText("Second task").closest("[data-card]") as HTMLElement
    fireEvent.contextMenu(pressed, { clientX: 40, clientY: 60 })
    fireEvent.click(screen.getByRole("menuitem", { name: "Delete" }))
    expect(screen.getByRole("heading", { name: "Delete TASK-2" })).toBeTruthy()
    fireEvent.click(screen.getByRole("button", { name: "Delete" }))
    await waitFor(() => {
      const deletes = mockFetch.mock.calls.filter(([, init]) => (init as RequestInit | undefined)?.method === "DELETE")
      expect(deletes.map(([url]) => String(url))).toEqual(["/api/tasks/TASK-2"])
    })
  })

  it("renames the task whose details were opened from its menu", async () => {
    const board = mobileBoard()
    const pressed = within(board).getByText("Second task").closest("[data-card]") as HTMLElement
    fireEvent.contextMenu(pressed, { clientX: 40, clientY: 60 })
    fireEvent.click(screen.getByRole("menuitem", { name: "Open details" }))
    fireEvent.click((await screen.findAllByRole("button", { name: "Edit title" }))[0])
    const input = screen.getByDisplayValue("Second task")
    fireEvent.change(input, { target: { value: "Renamed" } })
    fireEvent.click(screen.getByRole("button", { name: "Save" }))
    await waitFor(() => {
      const patches = mockFetch.mock.calls.filter(([, init]) => (init as RequestInit | undefined)?.method === "PATCH")
      expect(patches.map(([url]) => String(url))).toEqual(["/api/tasks/TASK-2"])
    })
  })

  it("starts a new task in the column being viewed", () => {
    const board = mobileBoard()
    fireEvent.click(within(board).getByRole("tab", { name: /^Doing/ }))
    fireEvent.click(screen.getByRole("button", { name: "New card" }))
    expect((screen.getByLabelText("Column") as HTMLSelectElement).value).toBe("Doing")
  })

  it("starts a new task in the column shown when the board opens", () => {
    try { localStorage.setItem("legato.mobile-board.column", "Done") } catch { /* not available */ }
    mobileBoard()
    fireEvent.click(screen.getByRole("button", { name: "New card" }))
    expect((screen.getByLabelText("Column") as HTMLSelectElement).value).toBe("Done")
  })
})
