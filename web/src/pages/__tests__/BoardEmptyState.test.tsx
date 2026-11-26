import { renderWithQueryClient as render } from "../../test/renderWithQueryClient"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import { cleanup, fireEvent, screen, within } from "@testing-library/react"
import { MemoryRouter } from "react-router-dom"
import { AgentSelectionProvider } from "../../hooks/useAgentSelection"
import { BoardPage } from "../Board"

// An empty board offers the next step: a new task, or one imported.

const board = vi.hoisted(() => ({ cards: [] as unknown[] }))
const mockFetch = vi.fn()
vi.stubGlobal("fetch", mockFetch)
vi.mock("../../hooks/useServer", () => ({ useServer: () => ({ baseUrl: "" }) }))
vi.mock("../../hooks/useSettings", () => ({ useSettings: () => ({ data: { repositories: [], working_dir: "", worktrees_enabled: false } }) }))
vi.mock("../../hooks/useBoard", () => ({
  useBoard: () => ({
    columns: [{ name: "Backlog", cards: board.cards }, { name: "Done", cards: [] }],
    workspaces: [], workers: [], loading: false, error: null, refresh: vi.fn(),
  }),
}))
vi.mock("../../hooks/useToast", () => ({ useToast: () => ({ addToast: vi.fn() }) }))
vi.mock("../../hooks/useWorkflow", () => ({ useWorkflows: () => ({ data: null, loading: false, error: null }) }))
vi.mock("../../hooks/useMediaQuery", () => ({ useMediaQuery: () => true }))

beforeEach(() => {
  board.cards = []
  mockFetch.mockImplementation(async () => new Response(JSON.stringify({}), { status: 200, headers: { "Content-Type": "application/json" } }))
})
afterEach(cleanup)

function renderBoard() {
  render(<MemoryRouter initialEntries={["/board"]}><AgentSelectionProvider><BoardPage /></AgentSelectionProvider></MemoryRouter>)
}

describe("an empty board", () => {
  it("offers a new task", async () => {
    renderBoard()
    const empty = screen.getByRole("region", { name: "No tasks yet" })
    fireEvent.click(within(empty).getByRole("button", { name: "New task" }))
    expect(await screen.findByText("New Task")).toBeTruthy()
  })

  it("offers an import", async () => {
    renderBoard()
    const empty = screen.getByRole("region", { name: "No tasks yet" })
    fireEvent.click(within(empty).getByRole("button", { name: "Import" }))
    expect(await screen.findByText("Import ticket")).toBeTruthy()
  })

  it("is not a board whose tasks a search filters out", () => {
    board.cards = [{
      id: "T-1", title: "Fix login", priority: "", issue_type: "", status: "Backlog", provider: "", has_warning: false, agent_active: false, agent_state: "",
      working_seconds: 0, waiting_seconds: 0, error_seconds: 0, workspace_id: null, workspace_name: "", workspace_color: "",
    }]
    renderBoard()
    fireEvent.change(screen.getAllByLabelText("Filter cards")[0], { target: { value: "zzz-no-match" } })
    expect(screen.queryByRole("region", { name: "No tasks yet" })).toBeNull()
  })
})
