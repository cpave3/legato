import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import { cleanup, createEvent, fireEvent, render, screen, waitFor, within } from "@testing-library/react"
import { MemoryRouter } from "react-router-dom"
import { AgentSelectionProvider } from "../../hooks/useAgentSelection"
import { BoardPage } from "../Board"
import type { BoardCard } from "../../lib/board-types"

// Dragging cards on the web board: within a column to reorder it, and into
// another column to land where the card is let go.

function card(id: string, title: string, status: string): BoardCard {
  return {
    id, title, priority: "", issue_type: "", status, provider: "", has_warning: false, agent_active: false, agent_state: "",
    working_seconds: 0, waiting_seconds: 0, error_seconds: 0, workspace_id: null, workspace_name: "", workspace_color: "",
  }
}

const mockFetch = vi.fn()
vi.stubGlobal("fetch", mockFetch)
vi.mock("../../hooks/useServer", () => ({ useServer: () => ({ baseUrl: "" }) }))
vi.mock("../../hooks/useSettings", () => ({ useSettings: () => ({ data: { repositories: [], working_dir: "", worktrees_enabled: false } }) }))
vi.mock("../../hooks/useBoard", () => ({
  useBoard: () => ({
    columns: [
      { name: "Ready", cards: [card("T-1", "Fix login", "Ready"), card("T-2", "Tidy the logs", "Ready")] },
      { name: "Doing", cards: [card("T-4", "Ship it", "Doing")] },
    ],
    workspaces: [],
    workers: [],
    loading: false,
    error: null,
    refresh: vi.fn(),
  }),
}))
vi.mock("../../hooks/useToast", () => ({ useToast: () => ({ addToast: vi.fn() }) }))
vi.mock("../../hooks/useWorkflow", () => ({ useWorkflows: () => ({ data: null, loading: false, error: null }) }))
vi.mock("../../hooks/useMediaQuery", () => ({ useMediaQuery: () => true }))

beforeEach(() => {
  mockFetch.mockReset()
  mockFetch.mockImplementation(async () => new Response(JSON.stringify({ status: "ok" }), { status: 200, headers: { "Content-Type": "application/json" } }))
})

afterEach(cleanup)

function renderBoard() {
  render(<MemoryRouter><AgentSelectionProvider><BoardPage /></AgentSelectionProvider></MemoryRouter>)
  // Each column's cards 80px tall with 8px between them.
  for (const name of ["Ready", "Doing"]) {
    screen.getByRole("region", { name }).querySelectorAll<HTMLElement>("[data-board-column-card]").forEach((element, i) => {
      element.getBoundingClientRect = () => ({ top: i * 88, height: 80, bottom: i * 88 + 80, left: 0, right: 200, width: 200, x: 0, y: i * 88, toJSON: () => ({}) })
    })
  }
}

// The card titled title, in column from, dragged and let go over column at
// height clientY.
function dragTo(title: string, from: string, column: string, clientY: number) {
  const data = new Map<string, string>()
  const dataTransfer = { dropEffect: "", effectAllowed: "", getData: (type: string) => data.get(type) ?? "", setData: (type: string, value: string) => data.set(type, value) }
  fireEvent.dragStart(within(screen.getByRole("region", { name: from })).getByText(title).closest("[draggable=true]")!, { dataTransfer })
  const target = screen.getByRole("region", { name: column })
  for (const type of ["dragOver", "drop"] as const) {
    const event = createEvent[type](target, { dataTransfer })
    Object.defineProperty(event, "clientY", { value: clientY })
    fireEvent(target, event)
  }
}

function patches() {
  return mockFetch.mock.calls
    .filter(([, init]) => init?.method === "PATCH")
    .map(([input, init]) => [String(input), JSON.parse(String(init!.body))])
}

describe("Dragging cards on the board", () => {
  it("reorders a card dropped above another in its own column", async () => {
    renderBoard()
    dragTo("Tidy the logs", "Ready", "Ready", 10)
    await waitFor(() => expect(patches()).toEqual([["/api/tasks/T-2", { before: "T-1" }]]))
  })

  it("sends nothing for a card dropped where it already is", async () => {
    renderBoard()
    dragTo("Fix login", "Ready", "Ready", 60)
    await new Promise((resolve) => setTimeout(resolve, 0))
    expect(patches()).toEqual([])
  })

  it("moves a card into another column above the card it is dropped on", async () => {
    renderBoard()
    dragTo("Fix login", "Ready", "Doing", 10)
    await waitFor(() => expect(patches()).toEqual([["/api/tasks/T-1", { column: "Doing", before: "T-4" }]]))
  })

  it("moves a card dropped below another column's cards to its end", async () => {
    renderBoard()
    dragTo("Fix login", "Ready", "Doing", 300)
    await waitFor(() => expect(patches()).toEqual([["/api/tasks/T-1", { column: "Doing", before: null }]]))
    expect(within(screen.getByRole("region", { name: "Ready" })).getByText("Fix login")).toBeTruthy()
  })
})
