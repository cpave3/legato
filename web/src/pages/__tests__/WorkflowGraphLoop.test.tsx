import { fireEvent, render, screen, within } from "@testing-library/react"
import { MemoryRouter, Route, Routes } from "react-router-dom"
import { describe, expect, it, vi } from "vitest"
import { mockReactFlow } from "../../test/reactFlow"
import type { WorkflowDiscovery } from "../../lib/workflow"
import { WorkflowPage } from "../WorkflowPage"

mockReactFlow()

const review = { id: "review", title: "Review in passes", type: "github.pr.review_passes", trigger: "automatic", with: { repo: "${{ workflow.inputs.repo }}", pr_number: "${{ workflow.inputs.pr_number }}" } }
const definition = {
  schema_version: 1, name: "review-pr-passes", title: "Review pull request in passes", source: "/w/review-pr-passes.workflow.yaml", steps: [review],
  graph: { version: 1, nodes: [
    { id: "trigger:manual", kind: "trigger", trigger: { kind: "manual", summary: "Run by hand", enabled: true } },
    { id: "step:review", kind: "step", step: { id: "review", title: "Review in passes", type: "github.pr.review_passes", trigger: "automatic", group: "review_loop", key: { field: "pr", value: "${{ workflow.inputs.repo }}#${{ workflow.inputs.pr_number }}" }, definition: review } },
    { id: "step:review/pass", kind: "inner", parent: "step:review", inner: { role: "pass", title: "Review pass" } },
    { id: "step:review/fixes", kind: "inner", parent: "step:review", inner: { role: "fixes", title: "Author fixes" } },
  ], edges: [
    { id: "start:trigger:manual>step:review", kind: "start", source: "trigger:manual", target: "step:review" },
    { id: "loop:step:review/pass>step:review/fixes", kind: "loop", source: "step:review/pass", target: "step:review/fixes" },
    { id: "loop:step:review/fixes>step:review/pass", kind: "loop", source: "step:review/fixes", target: "step:review/pass" },
  ] },
}

vi.mock("../../hooks/useWorkflow", () => ({
  useWorkflows: () => ({ data: { definitions: [definition], errors: [] } as unknown as WorkflowDiscovery, loading: false, error: null, refresh: vi.fn() }),
  useWorkflowRuns: () => ({ data: [], loading: false, error: null, refresh: vi.fn() }),
  useWorkflowRunsOf: () => ({ data: [], loading: false, error: null, refresh: vi.fn() }),
}))
vi.mock("../../hooks/useServer", () => ({ useServer: () => ({ baseUrl: "" }) }))

describe("the review loop on the workflow graph", () => {
  it("is a group holding a review pass and the author's fixes, joined round", async () => {
    render(<MemoryRouter initialEntries={["/workflows/review-pr-passes"]}><Routes><Route path="/workflows/:name" element={<WorkflowPage />} /></Routes></MemoryRouter>)
    const group = await screen.findByRole("button", { name: "Step: Review in passes" })
    expect(group.textContent).toContain("{repo}#{pr_number}")
    const pass = screen.getByRole("img", { name: "Review pass" })
    const fixes = screen.getByRole("img", { name: "Author fixes" })
    expect(pass.closest(".react-flow__node")?.getAttribute("data-id")).toBe("step:review/pass")
    expect(fixes).toBeTruthy()
    expect(screen.getByRole("img", { name: "Author fixes, then another review pass" })).toBeTruthy()
    fireEvent.click(pass)
    expect(within(screen.getByRole("complementary", { name: "Review in passes" })).getByLabelText("Step YAML")).toBeTruthy()
  })
})
