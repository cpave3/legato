import { renderWithQueryClient as render } from "../../test/renderWithQueryClient"
import { cleanup, fireEvent, screen } from "@testing-library/react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import { MemoryRouter } from "react-router-dom"
import { AgentSelectionProvider } from "../../hooks/useAgentSelection"
import { BoardPage } from "../Board"

const mockFetch = vi.fn()
const pr = { repo: "acme/app", branch: "b12", pr_number: 12, pr_url: "", state: "OPEN", is_draft: false, review_decision: "", check_status: "", comment_count: 0, pinned: false, match_method: "", sync_outcome: "", sync_error: "" }
const card = { id: "TASK-1", title: "Review task", status: "Doing", priority: "", issue_type: "Task", provider: "", has_warning: false, agent_active: false, agent_state: "", working_seconds: 0, waiting_seconds: 0, error_seconds: 0, workspace_name: "", workspace_color: "", pull_requests: [pr] }

vi.stubGlobal("fetch", mockFetch)
vi.mock("../../hooks/useServer", () => ({ useServer: () => ({ baseUrl: "" }) }))
vi.mock("../../hooks/useSettings", () => ({ useSettings: () => ({ data: { repositories: [], working_dir: "", worktrees_enabled: false } }) }))
vi.mock("../../hooks/useBoard", () => ({ useBoard: () => ({ columns: [{ name: "Doing", cards: [card] }], workspaces: [], loading: false, error: null, refresh: vi.fn() }) }))
vi.mock("../../hooks/useToast", () => ({ useToast: () => ({ addToast: vi.fn() }) }))
vi.mock("../../hooks/useWorkflow", () => ({ useWorkflows: () => ({ data: { definitions: [{ schema_version: 2, name: "review-loop", title: "Review loop", source: "builtin:review-loop", inputs: { repo: { type: "string" }, pr_number: { type: "integer" }, scope: { type: "string" }, agent_author: { type: "boolean" } }, steps: [] }], errors: [], repositories: [] }, loading: false, error: null }) }))

afterEach(cleanup)

// The card menu offers review passes only while some Legato on the data
// folder runs the review engine, and otherwise says why (spec LEG-559 AC 5).
describe("Board card menu and the review engine", () => {
  let engine: { running: boolean; reason?: string }
  beforeEach(() => {
    vi.resetAllMocks()
    mockFetch.mockImplementation(async (input: RequestInfo | URL) => {
      const url = String(input)
      if (url.endsWith("/api/review-engine")) return new Response(JSON.stringify(engine))
      if (url.endsWith("/api/macros")) return new Response(JSON.stringify({ macros: [] }))
      if (url.includes("/api/review-loops?")) return new Response(JSON.stringify({ loop: null, agent_author: true }))
      throw new Error(`unexpected fetch: ${url}`)
    })
  })

  function openMenu() {
    render(<MemoryRouter><AgentSelectionProvider><BoardPage /></AgentSelectionProvider></MemoryRouter>)
    const element = screen.getAllByText("Review task")[0].closest("[data-card]") as HTMLElement
    fireEvent.contextMenu(element, { clientX: 40, clientY: 60 })
  }

  it("says why it offers no review when no process runs the engine", async () => {
    engine = { running: false, reason: "Review passes need `legato serve` running." }
    openMenu()
    expect(await screen.findByText("Review passes need `legato serve` running.")).toBeTruthy()
    expect(screen.queryByRole("menuitem", { name: /Review passes/ })).toBeNull()
  })

  it("names the setting when review passes are off", async () => {
    engine = { running: false, reason: "Review passes are off: turn on review_passes.enabled (Settings › Review passes)." }
    openMenu()
    expect(await screen.findByText(/review_passes\.enabled/)).toBeTruthy()
    expect(screen.queryByRole("menuitem", { name: /Review passes/ })).toBeNull()
  })

  it("offers the review when the engine runs in another process", async () => {
    engine = { running: true }
    openMenu()
    expect(await screen.findByRole("menuitem", { name: "Review passes · #12 · agent" })).toBeTruthy()
    expect(screen.queryByText(/legato serve/)).toBeNull()
  })
})
