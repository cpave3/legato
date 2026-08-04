import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import { MemoryRouter } from "react-router-dom"
import { AgentSelectionProvider } from "../../hooks/useAgentSelection"
import { BoardPage } from "../Board"

const mockFetch = vi.fn()
const card = { id: "TASK-1", title: "Context task", status: "Backlog", priority: "High", issue_type: "Task", provider: "", has_warning: false, agent_active: false, agent_state: "", working_seconds: 0, waiting_seconds: 0, error_seconds: 0, workspace_name: "", workspace_color: "", pr_check_status: "", pr_review_decision: "", pr_comment_count: 0, pr_is_draft: false, pr_number: 0 }

vi.stubGlobal("fetch", mockFetch)
vi.mock("../../hooks/useServer", () => ({ useServer: () => ({ baseUrl: "" }) }))
vi.mock("../../hooks/useBoard", () => ({ useBoard: () => ({ columns: [{ name: "Backlog", cards: [card] }, { name: "Doing", cards: [] }], workspaces: [], loading: false, error: null, refresh: vi.fn() }) }))
vi.mock("../../hooks/useToast", () => ({ useToast: () => ({ addToast: vi.fn() }) }))
vi.mock("../../hooks/useWorkflow", () => ({ useWorkflows: () => ({ data: { definitions: [{ schema_version: 1, name: "triage", title: "Triage card", source: "triage.yaml", inputs: { subject: { type: "task", required: true }, count: { type: "integer" } }, steps: [] }], errors: [], repositories: [] }, loading: false, error: null }) }))

afterEach(cleanup)

describe("Board card context menu", () => {
  beforeEach(() => {
    vi.resetAllMocks()
    mockFetch.mockImplementation(async (input: RequestInfo | URL) => {
      const url = String(input)
      if (url.includes("/api/settings")) return { ok: true, json: async () => ({ repositories: [] }) }
      if (url.includes("/api/tasks/TASK-1")) return { ok: true, json: async () => ({ ...card, description_md: "Context details" }) }
      throw new Error(`unexpected fetch: ${url}`)
    })
  })

  function renderBoard() {
    render(<MemoryRouter><AgentSelectionProvider><BoardPage /></AgentSelectionProvider></MemoryRouter>)
    const title = screen.getAllByText("Context task")[0]
    return title.closest("div.relative") as HTMLElement
  }

  it.each([
    ["Move", "Move TASK-1"],
    ["Delete", "Delete TASK-1"],
  ])("opens on right click and reuses the %s overlay", (action, heading) => {
    const element = renderBoard()
    fireEvent.contextMenu(element, { clientX: 40, clientY: 60 })
    expect(screen.getByRole("menu", { name: "Actions for Context task" })).toBeTruthy()
    fireEvent.click(screen.getByRole("menuitem", { name: action }))
    expect(screen.getByRole("heading", { name: heading })).toBeTruthy()
  })

  it("opens the existing task detail path", async () => {
    const element = renderBoard()
    fireEvent.contextMenu(element, { clientX: 40, clientY: 60 })
    fireEvent.click(screen.getByRole("menuitem", { name: "Open details" }))
    await waitFor(() => expect(screen.getByText("Context details")).toBeTruthy())
  })

  it("opens an eligible workflow with the selected card bound read-only", () => {
    const element = renderBoard()
    fireEvent.contextMenu(element, { clientX: 40, clientY: 60 })
    fireEvent.mouseEnter(screen.getByRole("menuitem", { name: "Workflows" }))
    fireEvent.click(screen.getByRole("menuitem", { name: "Triage card" }))
    expect(screen.getByRole("dialog", { name: "Run Triage card" })).toBeTruthy()
    const task = screen.getByLabelText("subject") as HTMLInputElement
    expect(task.value).toBe("TASK-1")
    expect(task.readOnly).toBe(true)
  })
})
