import { cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import { MemoryRouter } from "react-router-dom"
import { AgentSelectionProvider } from "../../hooks/useAgentSelection"
import { BoardPage } from "../Board"

const mockFetch = vi.fn()
const addToast = vi.fn()
const card = { id: "TASK-1", title: "First task", status: "Backlog", priority: "", issue_type: "Task", provider: "", has_warning: false, agent_active: false, agent_state: "", working_seconds: 0, waiting_seconds: 0, error_seconds: 0, workspace_name: "", workspace_color: "" }

vi.stubGlobal("fetch", mockFetch)
vi.mock("../../hooks/useServer", () => ({ useServer: () => ({ baseUrl: "" }) }))
vi.mock("../../hooks/useSettings", () => ({ useSettings: () => ({ data: { repositories: [], working_dir: "", worktrees_enabled: false, default_import_workspace_id: null } }) }))
vi.mock("../../hooks/useBoard", () => ({
  useBoard: () => ({
    columns: [{ name: "Backlog", cards: [card] }, { name: "Ready", cards: [] }, { name: "Done", cards: [] }],
    workspaces: [{ id: 1, name: "Legato", color: "#123456" }], loading: false, error: null, refresh: vi.fn(),
  }),
}))
vi.mock("../../hooks/useToast", () => ({ useToast: () => ({ addToast }) }))
vi.mock("../../hooks/useWorkflow", () => ({ useWorkflows: () => ({ data: { definitions: [], errors: [], repositories: [] }, loading: false, error: null }) }))

afterEach(cleanup)

function renderBoard() {
  render(<MemoryRouter><AgentSelectionProvider><BoardPage /></AgentSelectionProvider></MemoryRouter>)
}

describe("Importing a ticket from the board", () => {
  beforeEach(() => {
    vi.resetAllMocks()
    mockFetch.mockImplementation(async (input: RequestInfo | URL) => {
      const url = String(input)
      if (url.startsWith("/api/ticket-providers")) return { ok: true, json: async () => ({ providers: [{ name: "burnlite", label: "Burnlite" }] }) }
      if (url.startsWith("/api/remote/search")) return { ok: true, json: async () => [{ id: "LEG-12", summary: "A Burnlite task", status: "Ready", priority: "", issue_type: "", provider: "burnlite" }] }
      if (url.startsWith("/api/remote/import")) return { ok: true, status: 201, json: async () => ({ id: "LEG-12", status: "Ready", workspace_id: 1, workspace_name: "Legato" }) }
      throw new Error(`unexpected fetch: ${url}`)
    })
  })

  it("opens the import dialog from either header, offering the board's columns", () => {
    renderBoard()
    for (const header of ["board-header", "board-header-mobile"]) {
      const button = within(screen.getByTestId(header)).getByRole("button", { name: "Import ticket" })
      expect(button.getAttribute("title")).toContain("(i)")
      fireEvent.click(button)
      expect(screen.getByRole("heading", { name: "Import ticket" })).toBeTruthy()
      const columns = screen.getByRole("combobox", { name: "Column" }) as HTMLSelectElement
      expect(Array.from(columns.options).map((o) => o.text)).toEqual(["Provider default", "Backlog", "Ready", "Done"])
      fireEvent.click(screen.getByRole("button", { name: "Close" }))
      expect(screen.queryByRole("heading", { name: "Import ticket" })).toBeNull()
    }
  })

  it("says where the imported ticket landed", async () => {
    renderBoard()
    fireEvent.click(within(screen.getByTestId("board-header")).getByRole("button", { name: "Import ticket" }))
    fireEvent.change(screen.getByPlaceholderText("Type to search..."), { target: { value: "task" } })
    fireEvent.click(await screen.findByText("A Burnlite task", {}, { timeout: 2000 }))
    await waitFor(() => expect(addToast).toHaveBeenCalledWith("Imported LEG-12 to Ready · Legato", "success"))
  })
})
