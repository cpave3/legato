import { fireEvent, render, screen, within } from "@testing-library/react"
import { MemoryRouter, Route, Routes } from "react-router-dom"
import { beforeEach, describe, expect, it, vi } from "vitest"
import { mockReactFlow } from "../../test/reactFlow"
import type { WorkflowDiscovery, WorkflowRun } from "../../lib/workflow"
import { WorkflowPage } from "../WorkflowPage"
import { WorkflowsPage } from "../WorkflowsPage"

mockReactFlow()

const triage = {
  schema_version: 1, name: "triage", title: "Triage a task", description: "Investigate a card", source: "/home/me/.config/legato/workflows/triage.workflow.yaml",
  inputs: { task: { type: "task", required: true } },
  steps: [
    { id: "investigate", title: "Investigate the task", type: "agent.run", trigger: "automatic", with: { prompt: "Look into it" } },
    { id: "ready", title: "Move to Ready", type: "task.move", trigger: "automatic", with: { column: "Ready" } },
  ],
  graph: { version: 1, nodes: [
    { id: "trigger:manual", kind: "trigger", trigger: { kind: "manual", enabled: true } },
    { id: "step:investigate", kind: "step", step: { id: "investigate", title: "Investigate the task", type: "agent.run", trigger: "automatic", key: { field: "prompt", value: "Look into it" }, definition: {} } },
    { id: "step:ready", kind: "step", step: { id: "ready", title: "Move to Ready", type: "task.move", trigger: "automatic", key: { field: "column", value: "Ready" }, definition: {} } },
  ], edges: [
    { id: "start:trigger:manual>step:investigate", kind: "start", source: "trigger:manual", target: "step:investigate" },
    { id: "order:step:investigate>step:ready", kind: "order", source: "step:investigate", target: "step:ready" },
  ] },
}

let discovery: WorkflowDiscovery
let runs: WorkflowRun[]

vi.mock("../../hooks/useWorkflow", () => ({
  useWorkflows: () => ({ data: discovery, loading: false, error: null, refresh: vi.fn() }),
  useWorkflowRuns: () => ({ data: runs, loading: false, error: null, refresh: vi.fn() }),
  useWorkflowMap: () => ({ data: null, loading: false, error: null, refresh: vi.fn() }), useWorkflowStats: () => ({ data: null, loading: false, error: null, refresh: vi.fn() }),
  // The server filters by workflow; this stands in for it.
  useWorkflowRunsOf: (name: string) => ({ data: runs.filter(run => run.workflow_name === name), loading: false, error: null, refresh: vi.fn() }),
}))
vi.mock("../../hooks/useServer", () => ({ useServer: () => ({ baseUrl: "" }) }))
const apiFetch = vi.hoisted(() => vi.fn())
vi.mock("../../lib/api", async importOriginal => ({ ...(await importOriginal<object>()), apiFetch }))
const copyBuiltInWorkflow = vi.fn()
vi.mock("../../lib/workflowFiles", async importOriginal => ({ ...(await importOriginal<object>()), copyBuiltInWorkflow: (...args: unknown[]) => copyBuiltInWorkflow(...args) }))

function renderAt(path: string) {
  return render(<MemoryRouter initialEntries={[path]}><Routes>
    <Route path="/workflows" element={<WorkflowsPage />} />
    <Route path="/workflows/:name" element={<WorkflowPage />} />
    <Route path="/workflows/:name/edit" element={<p>Editing</p>} />
  </Routes></MemoryRouter>)
}

beforeEach(() => {
  discovery = { definitions: [triage] as unknown as WorkflowDiscovery["definitions"], errors: [{ path: "/w/broken.workflow.yaml", error: "validate workflow: schema_version must be 1" }] }
  runs = [
    { id: "wr-1", workflow_name: "triage", workflow_title: "Triage LEG-1", definition_source: "x", definition_digest: "d", inputs_json: "{}", status: "succeeded", current_step_id: "", created_at: "2026-11-20T10:00:00Z" },
    { id: "wr-2", workflow_name: "other", workflow_title: "Other run", definition_source: "x", definition_digest: "d", inputs_json: "{}", status: "failed", current_step_id: "", created_at: "2026-11-20T09:00:00Z" },
  ]
})

describe("WorkflowPage", () => {
  it("opens a reserved built-in's copy, which the server names apart", async () => {
    discovery = { definitions: [{ ...triage, name: "review-loop", title: "Review loop", source: "builtin:review-loop" }] as unknown as WorkflowDiscovery["definitions"], errors: [] }
    copyBuiltInWorkflow.mockResolvedValue({ name: "review-loop-copy", path: "/w/review-loop-copy.workflow.yaml", folder: "global", digest: "d", diagnostics: [] })
    renderAt("/workflows/review-loop")
    fireEvent.click(screen.getByRole("button", { name: /Copy to my workflows/ }))
    expect(await screen.findByText("Editing")).toBeTruthy()
    expect(copyBuiltInWorkflow).toHaveBeenCalledWith("", "review-loop", "global")
  })

  it("shows a workflow's title, description, source and its steps as a graph", async () => {
    renderAt("/workflows/triage")
    expect(screen.getByRole("heading", { name: "Triage a task" })).toBeTruthy()
    // The description and the file are in the About card, folded at first
    // (LEG-655).
    fireEvent.click(within(screen.getByRole("region", { name: "About" })).getAllByRole("button")[0])
    expect(screen.getByText("Investigate a card")).toBeTruthy()
    expect(screen.getByText("/home/me/.config/legato/workflows/triage.workflow.yaml")).toBeTruthy()
    const investigate = await screen.findByRole("button", { name: "Step: Investigate the task" })
    expect(investigate.getAttribute("tabindex")).toBe("0")
    expect(investigate.textContent).toContain("Agent")
    expect(investigate.textContent).toContain("Look into it")
    const ready = screen.getByRole("button", { name: "Step: Move to Ready" })
    expect(ready.textContent).toContain("Move card")
    expect(ready.textContent).toContain("Ready")
    expect(screen.getByRole("button", { name: "Trigger: Run by hand" })).toBeTruthy()
  })

  it("selects a node with a single click", async () => {
    renderAt("/workflows/triage")
    const ready = await screen.findByRole("button", { name: "Step: Move to Ready" })
    fireEvent.click(ready)
    expect(ready.className).toContain("selected")
  })

  it("lists that workflow's recent runs and offers Run", async () => {
    renderAt("/workflows/triage")
    const recent = screen.getByRole("list", { name: "Recent runs" })
    expect(within(recent).getByRole("link", { name: /Triage LEG-1/ }).getAttribute("href")).toBe("/workflows/runs/wr-1")
    expect(within(recent).queryByText(/Other run/)).toBeNull()
    expect(screen.getByRole("button", { name: "Run Triage a task" })).toBeTruthy()
    await screen.findByRole("button", { name: "Step: Investigate the task" })
  })

  it("says why there is no graph when the file cannot be parsed", () => {
    renderAt("/workflows/broken")
    expect(screen.getByRole("alert").textContent).toContain("schema_version must be 1")
    expect(screen.queryByRole("button", { name: /^Step:/ })).toBeNull()
  })

  it("says so for a name no workflow has", () => {
    renderAt("/workflows/nothing")
    expect(screen.getByText("No workflow of that name")).toBeTruthy()
  })

  it("is opened from a row's title on the Workflows tab", async () => {
    renderAt("/workflows?tab=workflows")
    fireEvent.click(screen.getByRole("link", { name: "Triage a task" }))
    expect(await screen.findByRole("button", { name: "Step: Investigate the task" })).toBeTruthy()
  })

  it("shows a workflow's tags under its title, and removes one from its file", async () => {
    discovery = { definitions: [{ ...triage, tags: ["autopilot", "ops"], editable: true }] as unknown as WorkflowDiscovery["definitions"], errors: [] }
    apiFetch.mockResolvedValue(new Response(JSON.stringify({ name: "triage", tags: ["ops"] })))
    renderAt("/workflows/triage")
    const tags = screen.getByRole("list", { name: "Tags" })
    expect(within(tags).getAllByRole("listitem").map(item => item.textContent)).toEqual(["autopilot", "ops"])
    expect(screen.getByRole("button", { name: "Add tag" })).toBeTruthy()
    fireEvent.click(screen.getByRole("button", { name: "Remove tag autopilot" }))
    await vi.waitFor(() => expect(apiFetch).toHaveBeenCalled())
    const [, path, init] = apiFetch.mock.calls.at(-1)!
    expect(path).toBe("/api/workflows/triage/tags")
    expect(JSON.parse(init.body)).toEqual({ tags: ["ops"] })
  })

  it("shows a built-in's tags without a way to change them here", () => {
    discovery = { definitions: [{ ...triage, name: "review-loop", title: "Review loop", source: "builtin:review-loop", tags: ["reviews"], editable: false }] as unknown as WorkflowDiscovery["definitions"], errors: [] }
    renderAt("/workflows/review-loop")
    expect(within(screen.getByRole("list", { name: "Tags" })).getByText("reviews")).toBeTruthy()
    expect(screen.queryByRole("button", { name: /Remove tag/ })).toBeNull()
    expect(screen.queryByRole("button", { name: "Add tag" })).toBeNull()
  })
})
