import { cleanup, fireEvent, render, screen } from "@testing-library/react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import { MemoryRouter, Route, Routes, useLocation } from "react-router-dom"
import { AgentSelectionProvider } from "../../hooks/useAgentSelection"
import { BoardPage } from "../Board"

const mockFetch = vi.fn()
const pr = { repo: "acme/app", branch: "b12", pr_number: 12, pr_url: "", state: "OPEN", is_draft: false, review_decision: "", check_status: "", comment_count: 0, pinned: false, match_method: "", sync_outcome: "", sync_error: "" }
// The card's pull requests: #12 alone, unless a test stacks #13 on it.
let cardPRs: (typeof pr)[] = [pr]
const card = { id: "TASK-1", title: "Review task", status: "Doing", priority: "", issue_type: "Task", provider: "", has_warning: false, agent_active: false, agent_state: "", working_seconds: 0, waiting_seconds: 0, error_seconds: 0, workspace_name: "", workspace_color: "", get pull_requests() { return cardPRs } }
const reviewLoop = { schema_version: 2, name: "review-loop", title: "Review loop", source: "builtin:review-loop", steps: [], inputs: {
  repo: { type: "string", required: true }, pr_number: { type: "integer", default: 0 }, prs: { type: "json" }, scope: { type: "string", default: "" },
  agent_author: { type: "boolean", default: false }, reviewer: { type: "string", default: "" }, skill: { type: "string", default: "" },
  max_passes: { type: "integer", default: 0 }, max_reentries: { type: "integer", default: 0 }, ready: { type: "boolean", default: false },
} }

vi.stubGlobal("fetch", mockFetch)
vi.mock("../../hooks/useServer", () => ({ useServer: () => ({ baseUrl: "" }) }))
vi.mock("../../hooks/useSettings", () => ({ useSettings: () => ({ data: { repositories: [], working_dir: "", worktrees_enabled: false } }) }))
vi.mock("../../hooks/useBoard", () => ({ useBoard: () => ({ columns: [{ name: "Doing", cards: [card] }], workspaces: [], loading: false, error: null, refresh: vi.fn() }) }))
vi.mock("../../hooks/useToast", () => ({ useToast: () => ({ addToast: vi.fn() }) }))
vi.mock("../../hooks/useWorkflow", () => ({ useWorkflows: () => ({ data: { definitions: [reviewLoop], errors: [], repositories: [] }, loading: false, error: null }) }))

afterEach(cleanup)

function Where() {
  const location = useLocation()
  return <p data-testid="where">{location.pathname + location.search}</p>
}

// The card menu looks each pull request up (GET /api/review-loops): a loop
// that holds it, whatever started it, is opened; otherwise review-loop is
// started with agent_author derived (spec LEG-559 stories 1 to 8, AC 1, 2, 6).
describe("Board card menu and review-loop", () => {
  let lookup: unknown
  let lookup13: unknown
  let started: { url: string; body: unknown }[]
  let startAnswer: unknown
  beforeEach(() => {
    vi.resetAllMocks()
    cardPRs = [pr]
    started = []
    startAnswer = { run: { id: "wr-5", status: "running" }, steps: [], attempts: [], artifacts: [] }
    mockFetch.mockImplementation(async (input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input)
      if (url.endsWith("/api/review-engine")) return new Response(JSON.stringify({ running: true }))
      if (url.endsWith("/api/macros")) return new Response(JSON.stringify({ macros: [] }))
      if (url.includes("/api/review-loops?repo=acme%2Fapp&pr=12")) return new Response(JSON.stringify(lookup))
      if (url.includes("/api/review-loops?repo=acme%2Fapp&pr=13")) return new Response(JSON.stringify(lookup13))
      if (url.endsWith("/api/workflows/review-loop/runs") && init?.method === "POST") {
        started.push({ url, body: JSON.parse(String(init.body)) })
        return new Response(JSON.stringify(startAnswer))
      }
      throw new Error(`unexpected fetch: ${url}`)
    })
  })

  function openMenu() {
    render(<MemoryRouter initialEntries={["/board"]}><AgentSelectionProvider><Routes><Route path="*" element={<><BoardPage /><Where /></>} /></Routes></AgentSelectionProvider></MemoryRouter>)
    const element = screen.getAllByText("Review task")[0].closest("[data-card]") as HTMLElement
    fireEvent.contextMenu(element, { clientX: 40, clientY: 60 })
  }

  it("starts review-loop on a pull request no loop holds, as an agent's, with no ready", async () => {
    lookup = { loop: null, agent_author: true }
    openMenu()
    fireEvent.click(await screen.findByRole("menuitem", { name: "Review passes · #12 · agent" }))
    const dialog = await screen.findByRole("dialog")
    expect((screen.getByLabelText("agent_author") as HTMLInputElement).readOnly).toBe(true)
    expect((screen.getByLabelText("agent_author") as HTMLInputElement).value).toBe("true")
    expect(dialog.textContent).not.toMatch(/ready|max_reentries|prs/)
    fireEvent.click(screen.getByRole("button", { name: "Start" }))
    expect(await screen.findByText("/workflows/runs/wr-5")).toBeTruthy()
    expect(started).toHaveLength(1)
    expect(started[0].body).toEqual({ inputs: expect.objectContaining({ repo: "acme/app", pr_number: "12", agent_author: "true" }) })
    expect(JSON.stringify(started[0].body)).not.toMatch(/ready|steward/)
  })

  it("says person for a pull request Legato's author App did not open, and starts it as one", async () => {
    lookup = { loop: null, agent_author: false }
    openMenu()
    fireEvent.click(await screen.findByRole("menuitem", { name: "Review passes · #12 · person" }))
    await screen.findByRole("dialog")
    expect((screen.getByLabelText("agent_author") as HTMLInputElement).value).toBe("false")
  })

  it("starts a stack as the menu named it, a person's when one layer is", async () => {
    cardPRs = [pr, { ...pr, branch: "b13", pr_number: 13 }]
    lookup = { loop: null, agent_author: true }
    lookup13 = { loop: null, agent_author: false }
    openMenu()
    fireEvent.click(await screen.findByRole("menuitem", { name: "Review passes · stack #12–#13 · person" }))
    await screen.findByRole("dialog")
    expect((screen.getByLabelText("agent_author") as HTMLInputElement).value).toBe("false")
  })

  it("opens the loop that holds the pull request, whatever started it", async () => {
    lookup = { loop: { loop_id: "rl-1", kind: "handoff", run_id: "", step_path: "", phase: "reviewing", state: "open", held: false, outcome: "", link: "/board?highlight_task=T-9" }, }
    openMenu()
    fireEvent.click(await screen.findByRole("menuitem", { name: "Open review loop · #12" }))
    expect(await screen.findByText("/board?highlight_task=T-9")).toBeTruthy()
    expect(screen.queryByRole("dialog")).toBeNull()
    expect(started).toHaveLength(0)
  })

  it("names the loop that holds the pull request when a start is refused, and links to it", async () => {
    lookup = { loop: null, agent_author: true }
    startAnswer = { run: { id: "wr-6", status: "failed", error: "step open failed" }, attempts: [], artifacts: [], steps: [{ run_id: "wr-6", step_id: "open", step_order: 0, title: "Open", executor_type: "github.pr.review.open", resolved_inputs_json: "{}", outputs_json: "{}", status: "failed", latest_attempt: 1,
      error: "review_loop_active: pull request #12 of acme/app already has an active review loop: run wr-7 (/workflows/runs/wr-7), loop rl-7", error_code: "review_loop_active", error_link: "/workflows/runs/wr-7" }] }
    openMenu()
    fireEvent.click(await screen.findByRole("menuitem", { name: "Review passes · #12 · agent" }))
    await screen.findByRole("dialog")
    fireEvent.click(screen.getByRole("button", { name: "Start" }))
    expect((await screen.findByRole("alert")).textContent).toContain("run wr-7")
    fireEvent.click(screen.getByRole("link", { name: "Open the review loop" }))
    expect(await screen.findByText("/workflows/runs/wr-7")).toBeTruthy()
  })
})
