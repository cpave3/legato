import { QueryClientProvider } from "@tanstack/react-query"
import { cleanup, fireEvent, render, screen, within } from "@testing-library/react"
import { MemoryRouter, Route, Routes } from "react-router-dom"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import { createQueryClient, queryKeys } from "../../lib/query"
import type { WorkflowRunView } from "../../lib/workflow"
import { mockReactFlow } from "../../test/reactFlow"
import { WorkflowRunPage } from "../WorkflowRunPage"

mockReactFlow()

const { fetchRun } = vi.hoisted(() => ({ fetchRun: vi.fn() }))
vi.mock("../../hooks/useServer", () => ({ useServer: () => ({ baseUrl: "" }) }))
vi.mock("../../lib/workflow", async importOriginal => ({ ...(await importOriginal<typeof import("../../lib/workflow")>()), fetchWorkflowRun: fetchRun }))

// A version 2 run (LEG-484): find, then a loop over items whose pass reads
// and, when there is something to say, writes; the graph is version 2's.
const read = { id: "read", title: "Read the card", type: "task.context", start: "automatic", with: { task: "${{ workflow.task }}" } }
const write = { id: "write", title: "Write a note", type: "task.note", start: "automatic", if: { field: "steps.read.outputs.content", exists: true }, with: { task: "${{ workflow.task }}", body: "hi" } }
const each = { id: "each", title: "Each item", start: "automatic", loop: { over: "${{ workflow.inputs.items }}", max: 5 }, steps: [read, write] }
const find = { id: "find", title: "Find items", type: "task.context", start: "automatic", with: { task: "${{ workflow.task }}" } }
const definition = { schema_version: 2, name: "items", title: "Items", steps: [find, each] }
const node = (path: string, step: Record<string, unknown>, extra: Record<string, unknown> = {}) => ({ id: `step:${path}`, kind: "step", ...extra, step: { id: step.id, path, title: step.title, type: step.type ?? "loop", start: "automatic", key: {}, definition: step, ...(extra.step as object ?? {}) } })
const graph = { version: 2, nodes: [
  { id: "trigger:manual", kind: "trigger", trigger: { kind: "manual", summary: "Run by hand", enabled: true } },
  node("find", find),
  { ...node("each", each), step: { ...node("each", each).step, group: "loop", loop: { form: "over", max: 5, summary: "Each of ${{ workflow.inputs.items }}" } } },
  { ...node("each/read", read), parent: "step:each" },
  { ...node("each/write", write), parent: "step:each" },
], edges: [
  { id: "start:trigger:manual>step:find", kind: "start", source: "trigger:manual", target: "step:find" },
  { id: "order:step:find>step:each", kind: "order", source: "step:find", target: "step:each" },
  { id: "loop:step:each>step:each/read", kind: "loop", source: "step:each", target: "step:each/read" },
  { id: "branch:step:each/read>step:each/write", kind: "branch", source: "step:each/read", target: "step:each/write", condition: "steps.read.outputs.content exists" },
  { id: "branch:step:each/read>step:each", kind: "branch", source: "step:each/read", target: "step:each", skip: true },
  { id: "loop:step:each/write>step:each", kind: "loop", source: "step:each/write", target: "step:each" },
] }

const row = (path: string, status: string, pass?: number, extra = {}) => ({ run_id: "wr-1", step_id: path.split("/").pop(), path, parent_path: pass === undefined ? "" : path.split("/").slice(0, -1).join("/"), pass, step_order: 0, title: path, executor_type: "task.context", definition_json: "{}", resolved_inputs_json: "{}", outputs_json: "{}", status, latest_attempt: 1, started_at: "2026-11-25T10:00:00Z", ...extra })

function view() {
  return {
    run: { id: "wr-1", workflow_name: "items", workflow_title: "Items", definition_source: "/w/items.yaml", definition_digest: "d", definition_json: JSON.stringify(definition), inputs_json: "{}", status: "running", current_step_id: "each/1/write", created_at: "2026-11-25T10:00:00Z", started_at: "2026-11-25T10:00:00Z" },
    steps: [
      row("find", "succeeded"),
      row("each", "running", undefined, { executor_type: "loop" }),
      row("each/0/read", "succeeded", 0), row("each/0/write", "succeeded", 0, { outputs_json: JSON.stringify({ note_id: "n-first" }) }),
      row("each/1/read", "succeeded", 1), row("each/1/write", "running", 1, { resolved_inputs_json: JSON.stringify({ body: "second pass" }) }),
    ],
    attempts: [], artifacts: [], details: null, trigger_kind: "", definition_changed: false, definition_missing: false, graph,
  }
}

function show(data: unknown) {
  fetchRun.mockResolvedValue(data)
  const client = createQueryClient()
  client.setQueryData(queryKeys.workflowRun("", "wr-1"), data as WorkflowRunView)
  render(<QueryClientProvider client={client}><MemoryRouter initialEntries={["/workflows/runs/wr-1"]}><Routes><Route path="/workflows/runs/:runId" element={<WorkflowRunPage />} /></Routes></MemoryRouter></QueryClientProvider>)
}

beforeEach(() => {
  cleanup()
  fetchRun.mockReset()
  vi.stubGlobal("matchMedia", (query: string) => ({ matches: query.includes("min-width"), media: query, addEventListener: () => {}, removeEventListener: () => {} }))
})
afterEach(() => { vi.unstubAllGlobals() })

describe("a v2 run's graph", () => {
  it("draws the loop with its body's steps inside it, each in its newest pass's state", async () => {
    show(view())
    const loop = await screen.findByRole("button", { name: "Step: Each item, Running" })
    expect(loop.textContent).toContain("Pass 2 of 5")
    expect(within(loop).getByRole("img", { name: "Pass 1: Done" })).toBeTruthy()
    expect(within(loop).getByRole("img", { name: "Pass 2: Running" })).toBeTruthy()
    const writeNode = screen.getByRole("button", { name: "Step: Write a note, Running" })
    expect(writeNode.closest(".react-flow__node")?.getAttribute("data-id")).toBe("step:each/write")
    expect(screen.getByRole("button", { name: "Step: Read the card, Done" })).toBeTruthy()
  })

  it("labels the branch into a step with if: with its condition", async () => {
    show(view())
    await screen.findByRole("button", { name: "Step: Each item, Running" })
    expect(screen.getByText("if steps.read.outputs.content exists")).toBeTruthy()
  })

  it("opens a body step at its newest pass", async () => {
    show(view())
    fireEvent.click(await screen.findByRole("button", { name: "Step: Write a note, Running" }))
    const panel = screen.getByRole("complementary", { name: "Write a note" })
    expect(panel.textContent).toContain("Pass 2")
    expect(panel.textContent).toContain("second pass")
  })
})
