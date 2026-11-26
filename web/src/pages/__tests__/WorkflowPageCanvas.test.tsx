import { cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react"
import { MemoryRouter, Route, Routes } from "react-router-dom"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import { mockReactFlow } from "../../test/reactFlow"
import type { WorkflowDiscovery, WorkflowRun } from "../../lib/workflow"
import { WorkflowPage } from "../WorkflowPage"

// LEG-655: a workflow's page is its graph, with the workflow's details as
// cards over it, as a run's page is (LEG-651).
mockReactFlow()

const steps = [
  { id: "triage", title: "Triage the task", type: "agent.run", start: "automatic", with: { prompt: "Triage it" } },
  { id: "implement", title: "Implement it", type: "agent.run", start: "automatic", with: { prompt: "Build it" } },
  { id: "done", title: "Move to Done", type: "task.move", start: "manual", with: { column: "Done" } },
]
const definition = {
  schema_version: 2, name: "one-off-task", title: "One-off task to an approved pull request", description: "For small one-off tasks, mostly unattended.",
  source: "/home/me/.config/legato/workflows/one-off-task.workflow.yaml", editable: true,
  inputs: {
    task: { type: "task", required: true, description: "The task to triage and implement" },
    ask_reeve: { type: "boolean", default: true, description: "Questions go to the Reeve" },
    token: { type: "string", secret: true, default: "s3cr3t-token" },
  },
  outputs: { pr_number: { type: "integer", value: "${{ steps.implement.outputs.pr_number }}", description: "The pull request the agent opened" } },
  on: { "task.entered_column": { column: "Ready" } },
  steps,
  graph: { version: 2, nodes: [
    { id: "trigger:manual", kind: "trigger", trigger: { kind: "manual", summary: "Run by hand", enabled: true } },
    { id: "trigger:task.entered_column", kind: "trigger", trigger: { kind: "task.entered_column", summary: "A card enters Ready", enabled: true, definition: { column: "Ready" } } },
    ...steps.map(step => ({ id: `step:${step.id}`, kind: "step", step: { ...step, key: {}, definition: step, gate: step.start === "manual" ? "manual" : undefined } })),
  ], edges: [
    { id: "start:trigger:manual>step:triage", kind: "start", source: "trigger:manual", target: "step:triage" },
    { id: "order:step:triage>step:implement", kind: "order", source: "step:triage", target: "step:implement" },
    { id: "order:step:implement>step:done", kind: "order", source: "step:implement", target: "step:done" },
  ] },
}

let discovery: WorkflowDiscovery
let runs: WorkflowRun[]
const refresh = vi.fn(async () => {})
const setTrigger = vi.fn(async () => ({ status: "ok" }))

vi.mock("../../hooks/useWorkflow", () => ({
  useWorkflows: () => ({ data: discovery, loading: false, error: null, refresh }),
  useWorkflowRunsOf: () => ({ data: runs, loading: false, error: null, refresh: vi.fn() }),
}))
vi.mock("../../hooks/useServer", () => ({ useServer: () => ({ baseUrl: "" }) }))
vi.mock("../../lib/workflow", async importOriginal => ({ ...(await importOriginal<typeof import("../../lib/workflow")>()), setWorkflowTrigger: (...args: unknown[]) => setTrigger(...(args as [])) }))

function renderAt(path = "/workflows/one-off-task") {
  return render(<MemoryRouter initialEntries={[path]}><Routes>
    <Route path="/workflows/:name" element={<WorkflowPage />} />
  </Routes></MemoryRouter>)
}

// A wide screen; tall makes it a large one too.
function screenSize(tall: boolean) {
  vi.stubGlobal("matchMedia", (query: string) => ({ matches: query.includes("min-width") && (tall || !query.includes("min-height")), media: query, addEventListener: () => {}, removeEventListener: () => {} }))
}
const card = (name: string) => screen.getByRole("region", { name })
const fold = (name: string) => within(card(name)).getAllByRole("button")[0]

beforeEach(() => {
  cleanup()
  localStorage.clear()
  screenSize(true)
  discovery = {
    definitions: [definition] as unknown as WorkflowDiscovery["definitions"], errors: [],
    trigger_states: { "one-off-task": [{ trigger: "task.entered_column", file_enabled: true, enabled: false, overridden: true }] },
  }
  runs = [{ id: "wr-1", workflow_name: "one-off-task", workflow_title: "LEG-1 one-off", definition_source: "x", definition_digest: "d", inputs_json: "{}", status: "succeeded", current_step_id: "", created_at: "2026-11-20T10:00:00Z" }]
})
afterEach(() => { vi.unstubAllGlobals(); setTrigger.mockClear() })

describe("a workflow's page on the canvas", () => {
  it("is the graph under a one-row header with the workflow's name, state and actions", async () => {
    renderAt()
    const header = screen.getByRole("banner")
    expect(within(header).getByRole("heading", { level: 1, name: "One-off task to an approved pull request" })).toBeTruthy()
    expect(within(header).getByRole("link", { name: "← Workflows" }).getAttribute("href")).toBe("/workflows?tab=workflows")
    expect(within(header).getByText("one-off-task.workflow.yaml").getAttribute("title")).toBe(definition.source)
    expect(within(header).getByText("1 trigger paused")).toBeTruthy()
    expect(within(header).getByRole("link", { name: /Edit/ }).getAttribute("href")).toBe("/workflows/one-off-task/edit")
    expect(within(header).getByRole("button", { name: "Run One-off task to an approved pull request" })).toBeTruthy()
    const graph = screen.getByRole("region", { name: "Workflow graph" })
    expect(await within(graph).findByRole("button", { name: "Step: Implement it" })).toBeTruthy()
    // A definition has no current step: Fit and Panels, no Where it is.
    const toolbar = within(graph).getByRole("toolbar", { name: "View" })
    expect(within(toolbar).getByRole("button", { name: "Panels" })).toBeTruthy()
    expect(within(toolbar).getByRole("button", { name: /Fit/ })).toBeTruthy()
    expect(within(toolbar).queryByRole("button", { name: /Where it is/ })).toBeNull()
  })

  it("lists the triggers with their switches here, and a trigger's row shows its node", async () => {
    renderAt()
    const triggers = card("Triggers")
    expect(within(triggers).getByRole("button", { name: "Run by hand" })).toBeTruthy()
    const toggle = within(triggers).getByRole("switch")
    expect(toggle.getAttribute("aria-checked")).toBe("false")
    fireEvent.click(toggle)
    await waitFor(() => expect(setTrigger).toHaveBeenCalledWith("", "one-off-task", "task.entered_column", true))
    expect(refresh).toHaveBeenCalled()
    fireEvent.click(within(triggers).getByRole("button", { name: "A card enters Ready" }))
    expect(screen.getByRole("complementary", { name: "A card enters Ready" })).toBeTruthy()
  })

  it("shows the inputs, open on a large screen, without a secret's default", () => {
    renderAt()
    const inputs = card("Inputs")
    expect(fold("Inputs").getAttribute("aria-expanded")).toBe("true")
    expect(within(inputs).getByText("ask_reeve")).toBeTruthy()
    expect(within(inputs).getByText("task · required")).toBeTruthy()
    expect(within(inputs).getByText("Questions go to the Reeve")).toBeTruthy()
    expect(within(inputs).getByText("true")).toBeTruthy()
    expect(within(inputs).getByText("string · secret")).toBeTruthy()
    expect(screen.queryByText(/s3cr3t-token/)).toBeNull()
  })

  it("folds the inputs at first on a smaller screen", () => {
    screenSize(false)
    renderAt()
    expect(fold("Inputs").getAttribute("aria-expanded")).toBe("false")
  })

  it("shows the workflow's own outputs, and no card when it has none", () => {
    renderAt()
    fireEvent.click(fold("Outputs"))
    expect(within(card("Outputs")).getByText("pr_number")).toBeTruthy()
    expect(within(card("Outputs")).getByText("The pull request the agent opened")).toBeTruthy()
    expect(within(card("Outputs")).getByText("${{ steps.implement.outputs.pr_number }}")).toBeTruthy()
    cleanup()
    discovery = { ...discovery, definitions: [{ ...definition, outputs: undefined }] as unknown as WorkflowDiscovery["definitions"] }
    renderAt()
    expect(screen.queryByRole("region", { name: "Outputs" })).toBeNull()
  })

  it("lists the steps, and a step's row docks its details on the right; ✕ and Escape close them", async () => {
    renderAt()
    fireEvent.click(fold("Steps"))
    const outline = within(card("Steps")).getByRole("list", { name: "Steps outline" })
    expect(within(outline).getByRole("button", { name: /Move to Done.*Waits for you/ })).toBeTruthy()
    fireEvent.click(within(outline).getByRole("button", { name: /Implement it/ }))
    const panel = screen.getByRole("complementary", { name: "Implement it" })
    expect(within(panel).getByLabelText("Step YAML").textContent).toContain("id: implement")
    fireEvent.click(within(panel).getByRole("button", { name: "Close" }))
    expect(screen.queryByRole("complementary", { name: "Implement it" })).toBeNull()
    fireEvent.click(await screen.findByRole("button", { name: "Step: Triage the task" }))
    expect(screen.getByRole("complementary", { name: "Triage the task" })).toBeTruthy()
    fireEvent.keyDown(document.body, { key: "Escape" })
    expect(screen.queryByRole("complementary", { name: "Triage the task" })).toBeNull()
  })

  it("links each recent run, and says when there are none", () => {
    renderAt()
    expect(within(card("Recent runs")).getByRole("link", { name: /LEG-1 one-off/ }).getAttribute("href")).toBe("/workflows/runs/wr-1")
    cleanup()
    runs = []
    renderAt()
    expect(within(card("Recent runs")).getByText("No runs yet")).toBeTruthy()
  })

  it("hides and shows the cards with . and Show panels", () => {
    renderAt()
    fireEvent.keyDown(document.body, { key: "." })
    expect(screen.queryByRole("region", { name: "Triggers" })).toBeNull()
    fireEvent.click(screen.getByRole("button", { name: /1 trigger paused.*Show panels/ }))
    expect(card("Triggers")).toBeTruthy()
  })

  it("keeps its folds apart from a run page's", () => {
    renderAt()
    fireEvent.click(fold("Steps"))
    expect(JSON.parse(localStorage.getItem("legato:workflow-page:folds") ?? "{}")).toEqual({ steps: false })
    expect(localStorage.getItem("legato:run-page:folds")).toBeNull()
  })

  it("says a workflow with no triggers of its own runs by hand only, and shows its file's warnings and what it replaces", () => {
    discovery = { definitions: [{ ...definition, on: {}, graph: { ...definition.graph, nodes: definition.graph.nodes.filter(node => node.id !== "trigger:task.entered_column") }, shadows: ["/repo/.legato/workflows/one-off-task.workflow.yaml"], warnings: [{ severity: "warning", message: "step done: column is not on the board", line: 12, column: 3 }] }] as unknown as WorkflowDiscovery["definitions"], errors: [] }
    renderAt()
    expect(within(screen.getByRole("banner")).getByText("Run by hand only")).toBeTruthy()
    expect(within(screen.getByRole("list", { name: "Warnings" })).getByText(/column is not on the board/)).toBeTruthy()
    fireEvent.click(fold("About"))
    expect(within(card("About")).getByText("/repo/.legato/workflows/one-off-task.workflow.yaml")).toBeTruthy()
  })

  it("on a phone, keeps the cards in a sheet under the graph, Steps among them, and a node's details in another", async () => {
    vi.stubGlobal("matchMedia", (query: string) => ({ matches: query.includes("max-width"), media: query, addEventListener: () => {}, removeEventListener: () => {} }))
    renderAt()
    fireEvent.click(fold("Steps"))
    fireEvent.click(within(card("Steps")).getByRole("button", { name: /Implement it/ }))
    const sheet = screen.getByRole("dialog", { name: "Implement it" })
    expect(within(sheet).getAllByRole("button", { name: "Close" })).toHaveLength(1)
    expect(within(sheet).getByLabelText("Step YAML").textContent).toContain("id: implement")
  })
})
