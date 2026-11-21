import { QueryClientProvider } from "@tanstack/react-query"
import { cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react"
import { MemoryRouter, Route, Routes } from "react-router-dom"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import { createQueryClient, queryKeys } from "../../lib/query"
import type { WorkflowDiscovery, WorkflowRunView } from "../../lib/workflow"
import { mockReactFlow } from "../../test/reactFlow"
import { WorkflowPage } from "../WorkflowPage"
import { WorkflowRunPage } from "../WorkflowRunPage"

mockReactFlow()

const calls = vi.hoisted(() => ({ fetchRun: vi.fn() }))
vi.mock("../../hooks/useServer", () => ({ useServer: () => ({ baseUrl: "" }) }))
vi.mock("../../lib/workflow", async importOriginal => ({ ...(await importOriginal<typeof import("../../lib/workflow")>()), fetchWorkflowRun: calls.fetchRun }))
vi.mock("../../lib/approvals", async importOriginal => ({ ...(await importOriginal<typeof import("../../lib/approvals")>()), fetchPendingApprovals: async () => [] }))
vi.mock("../../lib/attention", async importOriginal => ({ ...(await importOriginal<typeof import("../../lib/attention")>()), fetchAttention: async () => [] }))

const write = { id: "write", title: "Write the report", type: "agent.run", trigger: "automatic", with: { prompt: "Write it" } }
const note = { id: "note", title: "Add a note", type: "task.note", trigger: "automatic", with: { body: "x" } }
const graph = { version: 1, nodes: [
  { id: "trigger:manual", kind: "trigger", trigger: { kind: "manual", summary: "Run by hand", enabled: true } },
  { id: "step:write", kind: "step", step: { id: "write", title: "Write the report", type: "agent.run", trigger: "automatic", key: {}, definition: write } },
  { id: "step:note", kind: "step", step: { id: "note", title: "Add a note", type: "task.note", trigger: "automatic", key: {}, definition: note } },
], edges: [
  { id: "start:trigger:manual>step:write", kind: "start", source: "trigger:manual", target: "step:write" },
  { id: "order:step:write>step:note", kind: "order", source: "step:write", target: "step:note" },
] }
const definition = { schema_version: 1, name: "feed", title: "Feeding", source: "/w/feed.workflow.yaml", steps: [write, note], graph }
const stepRun = (step_id: string, title: string, executor_type: string, status: string) => ({ run_id: "wr-1", step_id, step_order: 0, title, executor_type, definition_json: "{}", resolved_inputs_json: "{}", outputs_json: "{}", status, latest_attempt: 1 })
const run = { run: { id: "wr-1", workflow_name: "feed", workflow_title: "Feeding run", definition_source: "/w/feed.workflow.yaml", definition_digest: "d", definition_json: JSON.stringify(definition), inputs_json: "{}", status: "running", current_step_id: "write", created_at: "2026-11-21T10:00:00Z" }, steps: [stepRun("write", "Write the report", "agent.run", "running"), stepRun("note", "Add a note", "task.note", "pending")], attempts: [], artifacts: [], graph }

vi.mock("../../hooks/useWorkflow", async importOriginal => ({
  ...(await importOriginal<typeof import("../../hooks/useWorkflow")>()),
  useWorkflows: () => ({ data: { definitions: [definition], errors: [] } as unknown as WorkflowDiscovery, loading: false, error: null, refresh: vi.fn() }),
  useWorkflowRuns: () => ({ data: [], loading: false, error: null, refresh: vi.fn() }),
  useWorkflowRunsOf: () => ({ data: [], loading: false, error: null, refresh: vi.fn() }),
}))

function renderAt(path: string) {
  calls.fetchRun.mockResolvedValue(run)
  const client = createQueryClient()
  client.setQueryData(queryKeys.workflowRun("", "wr-1"), run as unknown as WorkflowRunView)
  render(<QueryClientProvider client={client}><MemoryRouter initialEntries={[path]}><Routes>
    <Route path="/workflows/:name" element={<WorkflowPage />} />
    <Route path="/workflows/runs/:runId" element={<WorkflowRunPage />} />
  </Routes></MemoryRouter></QueryClientProvider>)
}

// topDown says a node is laid out top to bottom: edges enter at its top.
const topDown = (node: HTMLElement) => node.querySelector(".react-flow__handle.target")?.classList.contains("react-flow__handle-top")

beforeEach(() => {
  cleanup()
  vi.stubGlobal("matchMedia", (query: string) => ({ matches: query.includes("max-width"), media: query, addEventListener: () => {}, removeEventListener: () => {} }))
})
afterEach(() => { vi.unstubAllGlobals() })

describe("workflow graphs at phone width", () => {
  it("keep the run's step list, with a Graph toggle that shows the graph top to bottom", async () => {
    renderAt("/workflows/runs/wr-1")
    expect(screen.getByRole("list", { name: "Steps" })).toBeTruthy()
    expect(screen.queryByRole("button", { name: /^Step:/ })).toBeNull()
    const toggle = screen.getByRole("button", { name: "Graph" })
    expect(toggle.getAttribute("aria-pressed")).toBe("false")
    fireEvent.click(toggle)
    const node = await screen.findByRole("button", { name: "Step: Write the report, Running" })
    expect(topDown(node)).toBe(true)
    expect(screen.queryByRole("list", { name: "Steps" })).toBeNull()
    fireEvent.click(screen.getByRole("button", { name: "Graph" }))
    expect(screen.getByRole("list", { name: "Steps" })).toBeTruthy()
  })

  it("open a run step's details in a bottom sheet that closes", async () => {
    renderAt("/workflows/runs/wr-1")
    fireEvent.click(screen.getByRole("button", { name: "Graph" }))
    fireEvent.click(await screen.findByRole("button", { name: "Step: Add a note, Waiting" }))
    const sheet = screen.getByRole("dialog", { name: "Add a note" })
    expect(within(sheet).getByLabelText("Step YAML")).toBeTruthy()
    fireEvent.click(within(sheet).getByRole("button", { name: "Close" }))
    await waitFor(() => expect(screen.queryByRole("dialog", { name: "Add a note" })).toBeNull())
  })

  it("lay out a workflow's page top to bottom, with details in a bottom sheet", async () => {
    renderAt("/workflows/feed")
    const node = await screen.findByRole("button", { name: "Step: Write the report" })
    expect(topDown(node)).toBe(true)
    fireEvent.click(node)
    const sheet = screen.getByRole("dialog", { name: "Write the report" })
    expect(within(sheet).getByLabelText("Step YAML").textContent).toContain("id: write")
    expect(screen.queryByRole("complementary", { name: "Write the report" })?.closest("[role=dialog]")).toBe(sheet)
    fireEvent.click(within(sheet).getByRole("button", { name: "Close" }))
    await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull())
  })
})
