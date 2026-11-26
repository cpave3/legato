import { renderWithQueryClient as render } from "../../test/renderWithQueryClient"
import { cleanup, fireEvent, screen, waitFor, within } from "@testing-library/react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import { MemoryRouter } from "react-router-dom"
import { AgentSelectionProvider } from "../../hooks/useAgentSelection"
import { BoardPage } from "../Board"

const mockFetch = vi.fn()
const shown: { card: Record<string, unknown> | null } = { card: null }
const card = { id: "TASK-1", title: "Context task", status: "Backlog", priority: "High", issue_type: "Task", provider: "", has_warning: false, agent_active: false, agent_state: "", working_seconds: 0, waiting_seconds: 0, error_seconds: 0, workspace_name: "", workspace_color: "" }

vi.stubGlobal("fetch", mockFetch)
vi.mock("../../hooks/useServer", () => ({ useServer: () => ({ baseUrl: "" }) }))
vi.mock("../../hooks/useSettings", () => ({ useSettings: () => ({ data: { repositories: [], working_dir: "", worktrees_enabled: false } }) }))
vi.mock("../../hooks/useBoard", () => ({ useBoard: () => ({ columns: [{ name: "Backlog", cards: [shown.card ?? card] }, { name: "Doing", cards: [] }], workspaces: [], loading: false, error: null, refresh: vi.fn() }) }))
vi.mock("../../hooks/useToast", () => ({ useToast: () => ({ addToast: vi.fn() }) }))
vi.mock("../../hooks/useWorkflow", () => ({ useWorkflows: () => ({ data: { definitions: [{ schema_version: 1, name: "triage", title: "Triage card", source: "triage.yaml", inputs: { subject: { type: "task", required: true }, count: { type: "integer" } }, steps: [] }], errors: [], repositories: [] }, loading: false, error: null }) }))

afterEach(() => { cleanup(); shown.card = null })

describe("Board card context menu", () => {
  beforeEach(() => {
    vi.resetAllMocks()
    mockFetch.mockImplementation(async (input: RequestInfo | URL) => {
      const url = String(input)
      if (url.includes("/api/settings")) return { ok: true, json: async () => ({ repositories: [] }) }
      if (url.includes("/api/tasks/TASK-1")) return { ok: true, json: async () => ({ ...card, provider: "jira", description_md: "Provider details", local_description_md: "Legato details", remote_description_md: "Provider details", resumable_agent_kinds: [] }) }
      throw new Error(`unexpected fetch: ${url}`)
    })
  })

  function renderBoard() {
    render(<MemoryRouter><AgentSelectionProvider><BoardPage /></AgentSelectionProvider></MemoryRouter>)
    const title = screen.getAllByText("Context task")[0]
    return title.closest("[data-card]") as HTMLElement
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
    await waitFor(() => expect(screen.getByText("Provider details")).toBeTruthy())
  })

  it("opens Legato detail editing for a provider-backed task", async () => {
    const element = renderBoard()
    fireEvent.contextMenu(element, { clientX: 40, clientY: 60 })
    fireEvent.click(screen.getByRole("menuitem", { name: "Open details" }))
    const edit = await screen.findAllByRole("button", { name: "Edit Legato details" })
    fireEvent.click(edit[0])
    const dialog = screen.getByRole("dialog", { name: "Edit Legato details" })
    expect(dialog).toBeTruthy()
    expect((within(dialog).getByLabelText("Description") as HTMLTextAreaElement).value).toBe("Legato details")
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

  it("starts a macro on the card through the macro endpoint", async () => {
    const base = mockFetch.getMockImplementation()!
    mockFetch.mockImplementation(async (input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input)
      if (url.endsWith("/api/macros")) return new Response(JSON.stringify({ macros: [{ name: "Plan", keys: "p", applies_to: "task" }, { name: "Review", keys: "r", applies_to: "pr" }] }))
      if (url.endsWith("/api/tasks/TASK-1/macro")) return new Response(JSON.stringify({ task_id: "TASK-1", started: true, queued: false }), { status: 201 })
      return base(input, init)
    })
    const element = renderBoard()
    fireEvent.contextMenu(element, { clientX: 40, clientY: 60 })
    const plan = await screen.findByRole("menuitem", { name: "Macro: Plan" })
    expect(screen.queryByRole("menuitem", { name: "Macro: Review" })).toBeNull()
    fireEvent.click(plan)
    await waitFor(() => expect(mockFetch.mock.calls.some(([url]) => String(url).endsWith("/api/tasks/TASK-1/macro"))).toBe(true))
    const call = mockFetch.mock.calls.find(([url]) => String(url).endsWith("/api/tasks/TASK-1/macro"))!
    expect(JSON.parse(String(call[1]?.body))).toEqual({ name: "Plan" })
  })

  it("starts a macro on one of the card's open pull requests, and opens the ticket at its source", async () => {
    const pr = (n: number, state = "OPEN") => ({ repo: "acme/app", branch: `b${n}`, pr_number: n, pr_url: "", state, is_draft: false, review_decision: "", check_status: "fail", comment_count: 0, pinned: false, match_method: "", sync_outcome: "", sync_error: "" })
    shown.card = { ...card, provider: "burnlite", source_url: "https://burnlite.dev/LEG-386", pull_requests: [pr(194, "MERGED"), pr(196), pr(200)] }
    const base = mockFetch.getMockImplementation()!
    mockFetch.mockImplementation(async (input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input)
      if (url.endsWith("/api/macros")) return new Response(JSON.stringify({ macros: [{ name: "Plan", keys: "p", applies_to: "task" }, { name: "Address feedback", keys: "a", applies_to: "pr" }] }))
      if (url.endsWith("/api/tasks/TASK-1/macro")) return new Response(JSON.stringify({ task_id: "TASK-1", started: true, queued: false }), { status: 201 })
      return base(input, init)
    })
    const open = vi.spyOn(window, "open").mockImplementation(() => null)
    const element = renderBoard()
    fireEvent.contextMenu(element, { clientX: 40, clientY: 60 })
    await screen.findByRole("menuitem", { name: "Macros for #196" })
    expect(screen.queryByRole("menuitem", { name: "Macros for #194" })).toBeNull()
    fireEvent.click(screen.getByRole("menuitem", { name: "Macros for #200" }))
    fireEvent.click(screen.getByRole("menuitem", { name: "Macro: Address feedback" }))
    await waitFor(() => expect(mockFetch.mock.calls.some(([url]) => String(url).endsWith("/api/tasks/TASK-1/macro"))).toBe(true))
    const call = mockFetch.mock.calls.find(([url]) => String(url).endsWith("/api/tasks/TASK-1/macro"))!
    expect(JSON.parse(String(call[1]?.body))).toEqual({ name: "Address feedback", pr: { repo: "acme/app", pr_number: 200 } })

    fireEvent.contextMenu(element, { clientX: 40, clientY: 60 })
    fireEvent.click(screen.getByRole("menuitem", { name: "Open in Burnlite" }))
    expect(open).toHaveBeenCalledWith("https://burnlite.dev/LEG-386", "_blank", "noopener")
  })
})
