import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import { MemoryRouter } from "react-router-dom"
import { AgentSelectionProvider } from "../../hooks/useAgentSelection"
import { BoardPage } from "../Board"

const refresh = vi.fn()
const addToast = vi.fn()
const mockFetch = vi.fn()
const selectedCard = {
  id: "TASK-2", title: "Selected task", status: "Doing", priority: "High", issue_type: "Task", provider: "",
  has_warning: false, agent_active: false, agent_state: "", working_seconds: 0, waiting_seconds: 0, error_seconds: 0,
  workspace_id: 1, workspace_name: "Platform", workspace_color: "#123456", pr_check_status: "", pr_review_decision: "",
  pr_comment_count: 0, pr_is_draft: false, pr_number: 0,
}
const cursorCard = { ...selectedCard, id: "TASK-1", title: "Cursor task", status: "Backlog", workspace_id: null, workspace_name: "" }

vi.stubGlobal("fetch", mockFetch)
vi.mock("../../hooks/useServer", () => ({ useServer: () => ({ baseUrl: "" }) }))
vi.mock("../../hooks/useSettings", () => ({ useSettings: () => ({ data: { repositories: [], working_dir: "", worktrees_enabled: false } }) }))
vi.mock("../../hooks/useBoard", () => ({
  useBoard: () => ({
    columns: [{ name: "Backlog", cards: [cursorCard] }, { name: "Doing", cards: [selectedCard] }],
    workspaces: [{ id: 1, name: "Platform", color: "#123456" }, { id: 2, name: "Product", color: "#654321" }],
    loading: false, error: null, refresh,
  }),
}))
vi.mock("../../hooks/useToast", () => ({ useToast: () => ({ addToast }) }))
vi.mock("../../hooks/useWorkflow", () => ({ useWorkflows: () => ({ data: { definitions: [], errors: [], repositories: [] }, loading: false, error: null }) }))

function renderBoard() {
  render(<MemoryRouter><AgentSelectionProvider><BoardPage /></AgentSelectionProvider></MemoryRouter>)
}

describe("Board workspace movement", () => {
  beforeEach(() => {
    vi.resetAllMocks()
    mockFetch.mockImplementation(async (input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input)
      if (url.includes("/api/settings")) return { ok: true, json: async () => ({ repositories: [] }) }
      if (url === "/api/tasks/TASK-2" && init?.method === "PATCH") return { ok: true, json: async () => ({ status: "ok", triggers: [] }) }
      if (url === "/api/tasks/TASK-2") return { ok: true, json: async () => ({ ...selectedCard, description_md: "Details", local_description_md: "Details", remote_description_md: "", resumable_agent_kinds: [] }) }
      throw new Error(`unexpected fetch: ${url}`)
    })
  })
  afterEach(cleanup)

  it("moves the context-menu task using its own current state and only workspace_id", async () => {
    renderBoard()
    const task = screen.getAllByText("Selected task")[0].closest("div.relative") as HTMLElement
    fireEvent.contextMenu(task)
    fireEvent.click(screen.getByRole("menuitem", { name: "Move" }))

    expect((screen.getByRole("button", { name: /Doing.*current/ }) as HTMLButtonElement).disabled).toBe(true)
    expect((screen.getByRole("button", { name: /Platform.*current/ }) as HTMLButtonElement).disabled).toBe(true)
    fireEvent.click(screen.getByRole("button", { name: "Product" }))

    await waitFor(() => expect(mockFetch).toHaveBeenCalledWith("/api/tasks/TASK-2", expect.objectContaining({ method: "PATCH", body: '{"workspace_id":2}' })))
    expect(refresh).toHaveBeenCalled()
    expect(addToast).toHaveBeenCalledWith("Moved task to Product", "success")
    expect(screen.queryByRole("heading", { name: "Move TASK-2" })).toBeNull()
  })

  it("sends explicit null when moving to Unassigned", async () => {
    renderBoard()
    const task = screen.getAllByText("Selected task")[0].closest("div.relative") as HTMLElement
    fireEvent.contextMenu(task)
    fireEvent.click(screen.getByRole("menuitem", { name: "Move" }))
    fireEvent.click(screen.getByRole("button", { name: "Unassigned" }))

    await waitFor(() => expect(mockFetch).toHaveBeenCalledWith("/api/tasks/TASK-2", expect.objectContaining({ method: "PATCH", body: '{"workspace_id":null}' })))
    expect(addToast).toHaveBeenCalledWith("Moved task to Unassigned", "success")
  })

  it("keeps the move dialog usable when workspace movement fails", async () => {
    mockFetch.mockImplementation(async (input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input)
      if (url.includes("/api/settings")) return { ok: true, json: async () => ({ repositories: [] }) }
      if (url === "/api/tasks/TASK-2" && init?.method === "PATCH") return { ok: false, status: 500 }
      throw new Error(`unexpected fetch: ${url}`)
    })
    renderBoard()
    const task = screen.getAllByText("Selected task")[0].closest("div.relative") as HTMLElement
    fireEvent.contextMenu(task)
    fireEvent.click(screen.getByRole("menuitem", { name: "Move" }))
    fireEvent.click(screen.getByRole("button", { name: "Product" }))

    await waitFor(() => expect(addToast).toHaveBeenCalledWith("patchTask failed: 500", "error"))
    expect(screen.getByRole("heading", { name: "Move TASK-2" })).toBeTruthy()
    expect((screen.getByRole("button", { name: "Product" }) as HTMLButtonElement).disabled).toBe(false)
  })

  it("preserves detail status and workspace when opening Move task", async () => {
    renderBoard()
    fireEvent.click(screen.getAllByText("Selected task")[0])
    await screen.findByText("Details")
    fireEvent.click(screen.getByRole("button", { name: "Move task" }))

    expect((screen.getByRole("button", { name: /Doing.*current/ }) as HTMLButtonElement).disabled).toBe(true)
    expect((screen.getByRole("button", { name: /Platform.*current/ }) as HTMLButtonElement).disabled).toBe(true)
  })
})
