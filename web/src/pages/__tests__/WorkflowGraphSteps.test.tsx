import { fireEvent, render, screen, waitFor, within } from "@testing-library/react"
import { MemoryRouter, Route, Routes } from "react-router-dom"
import { beforeEach, describe, expect, it, vi } from "vitest"
import { mockReactFlow } from "../../test/reactFlow"
import type { WorkflowDiscovery } from "../../lib/workflow"
import { WorkflowPage } from "../WorkflowPage"

mockReactFlow()

const write = { id: "write", title: "Write the report", type: "agent.run", trigger: "automatic", with: { prompt: "Write it", token: "hunter2" }, outputs: { summary: { type: "string" } } }
const wait = { id: "wait", title: "Wait for me", type: "task.note", trigger: "manual", with: { body: "${{ steps.write.outputs.summary }}" } }
const after = { id: "after", title: "After the agent", type: "task.note", trigger: "agent_ended", with: { body: "done" } }
const step = (s: typeof write | typeof wait | typeof after, gate?: string) => ({ id: `step:${s.id}`, kind: "step", step: { id: s.id, title: s.title, type: s.type, trigger: s.trigger, gate, key: {}, definition: s } })
const definition = {
  schema_version: 1, name: "feed", title: "Feeding", source: "/w/feed.workflow.yaml",
  inputs: { token: { type: "string", secret: true, default: "hunter2" } },
  steps: [write, wait, after],
  graph: { version: 1, nodes: [
    { id: "trigger:manual", kind: "trigger", trigger: { kind: "manual", summary: "Run by hand", enabled: true } },
    step(write), step(wait, "manual"), step(after, "agent_ended"),
  ], edges: [
    { id: "start:trigger:manual>step:write", kind: "start", source: "trigger:manual", target: "step:write" },
    { id: "order:step:write>step:wait", kind: "order", source: "step:write", target: "step:wait" },
    { id: "order:step:wait>step:after", kind: "order", source: "step:wait", target: "step:after" },
    { id: "data:step:write>step:wait", kind: "data", source: "step:write", target: "step:wait", outputs: ["summary"] },
  ] },
}

vi.mock("../../hooks/useWorkflow", () => ({
  useWorkflows: () => ({ data: { definitions: [definition], errors: [] } as unknown as WorkflowDiscovery, loading: false, error: null, refresh: vi.fn() }),
  useWorkflowRuns: () => ({ data: [], loading: false, error: null, refresh: vi.fn() }),
  useWorkflowRunsOf: () => ({ data: [], loading: false, error: null, refresh: vi.fn() }),
}))
vi.mock("../../hooks/useServer", () => ({ useServer: () => ({ baseUrl: "" }) }))

const writeText = vi.fn(async () => {})
beforeEach(() => {
  writeText.mockClear()
  Object.defineProperty(navigator, "clipboard", { configurable: true, value: { writeText } })
})

function renderPage() {
  return render(<MemoryRouter initialEntries={["/workflows/feed"]}><Routes><Route path="/workflows/:name" element={<WorkflowPage />} /></Routes></MemoryRouter>)
}

describe("gates, data edges and the step side panel", () => {
  it("marks a step that waits for the operator and one that waits for its agent", async () => {
    renderPage()
    const waiting = await screen.findByRole("button", { name: "Step: Wait for me, waits for you" })
    expect(within(waiting).getByRole("img", { name: "Waits for you" })).toBeTruthy()
    const after = screen.getByRole("button", { name: "Step: After the agent, waits for the agent to end" })
    expect(within(after).getByRole("img", { name: "Waits for the agent to end" })).toBeTruthy()
    expect(within(screen.getByRole("button", { name: "Step: Write the report" })).queryByRole("img", { name: /^Waits/ })).toBeNull()
  })

  it("draws a data edge only while its source or target is selected", async () => {
    renderPage()
    const writeNode = await screen.findByRole("button", { name: "Step: Write the report" })
    expect(screen.queryByRole("img", { name: /^Uses summary/ })).toBeNull()
    fireEvent.click(writeNode)
    expect(await screen.findByRole("img", { name: "Uses summary of Write the report in Wait for me" })).toBeTruthy()
    fireEvent.click(screen.getByRole("button", { name: "Step: After the agent, waits for the agent to end" }))
    await waitFor(() => expect(screen.queryByRole("img", { name: /^Uses summary/ })).toBeNull())
    fireEvent.click(screen.getByRole("button", { name: "Step: Wait for me, waits for you" }))
    expect(await screen.findByRole("img", { name: "Uses summary of Write the report in Wait for me" })).toBeTruthy()
  })

  it("shows a selected step's YAML with Copy and the source path, secrets redacted", async () => {
    renderPage()
    fireEvent.click(await screen.findByRole("button", { name: "Step: Write the report" }))
    const panel = screen.getByRole("complementary", { name: "Write the report" })
    const yaml = within(panel).getByLabelText("Step YAML").textContent ?? ""
    expect(yaml).toContain("id: write")
    expect(yaml).toContain("prompt: Write it")
    expect(yaml).not.toContain("hunter2")
    expect(panel.textContent).toContain("/w/feed.workflow.yaml")
    fireEvent.click(within(panel).getByRole("button", { name: "Copy YAML" }))
    await waitFor(() => expect(writeText).toHaveBeenCalled())
    expect(String((writeText.mock.calls[0] as unknown[])[0])).not.toContain("hunter2")
  })
})
