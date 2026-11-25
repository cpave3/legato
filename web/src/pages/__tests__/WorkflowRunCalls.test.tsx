import { QueryClientProvider } from "@tanstack/react-query"
import { createQueryClient, queryKeys } from "../../lib/query"
import { cleanup, fireEvent, render, screen, within } from "@testing-library/react"
import { MemoryRouter, Route, Routes } from "react-router-dom"
import { beforeEach, describe, expect, it, vi } from "vitest"
import type { WorkflowRunView } from "../../lib/workflow"
import { WorkflowRunPage } from "../WorkflowRunPage"

const { fetchRun } = vi.hoisted(() => ({ fetchRun: vi.fn() }))
vi.mock("../../hooks/useServer", () => ({ useServer: () => ({ baseUrl: "" }) }))
vi.mock("../../lib/workflow", async importOriginal => ({ ...(await importOriginal<typeof import("../../lib/workflow")>()), fetchWorkflowRun: fetchRun }))

const run = { id: "wr-1", workflow_name: "build-milestone", workflow_title: "Build M-1", definition_source: "/w/build-milestone.yaml", definition_digest: "d", definition_json: JSON.stringify({ title: "Build a milestone" }), inputs_json: "{}", created_at: "2026-11-25T10:00:00Z", started_at: "2026-11-25T10:00:00Z" }
const stepRun = (step_id: string, title: string, executor_type: string, status: string, extra = {}) => ({ run_id: "wr-1", step_id, step_order: 0, title, executor_type, definition_json: "{}", resolved_inputs_json: "{}", outputs_json: "{}", status, latest_attempt: 1, ...extra })

function show(data: unknown) {
  fetchRun.mockResolvedValue(data)
  const client = createQueryClient()
  client.setQueryData(queryKeys.workflowRun("", "wr-1"), data as WorkflowRunView)
  render(<QueryClientProvider client={client}><MemoryRouter initialEntries={["/workflows/runs/wr-1"]}><Routes><Route path="/workflows/runs/:runId" element={<WorkflowRunPage />} /></Routes></MemoryRouter></QueryClientProvider>)
}
const now = () => screen.getByRole("region", { name: "Now" })

// Calling a workflow (LEG-489): a run's own outputs, the warnings of those
// that did not resolve, and the links between a parent and its child.
describe("WorkflowRunPage calls", () => {
  beforeEach(() => { cleanup(); fetchRun.mockReset() })

  it("shows a succeeded run's outputs, and names those that did not resolve", () => {
    show({ run: { ...run, status: "succeeded", outputs_json: '{"review":"not reviewed"}' }, output_warnings: ['output "top_branch" was not resolved: steps.push.outputs.top_branch is missing'], steps: [stepRun("push", "Push", "agent.run", "succeeded", { outputs_json: '{"prs":[]}' })], attempts: [], artifacts: [] })
    const outputs = within(now()).getByRole("region", { name: "Run outputs" })
    expect(outputs.textContent).toContain("review")
    expect(outputs.textContent).toContain("not reviewed")
    expect(within(now()).getByRole("list", { name: "Outputs not resolved" }).textContent).toContain('output "top_branch" was not resolved')
  })

  it("links a child run to the step that called it", () => {
    show({ run: { ...run, status: "running", current_step_id: "build", parent_run_id: "wr-parent", parent_step_path: "each/2/build" }, steps: [stepRun("note", "Note", "task.note", "pending")], attempts: [], artifacts: [] })
    const link = screen.getByRole("link", { name: "wr-parent" })
    expect(link.getAttribute("href")).toBe("/workflows/runs/wr-parent")
    expect(link.parentElement?.textContent).toContain("each/2/build")
  })

  it("links a workflow.run step to its child, while it waits and once it is done", () => {
    show({ run: { ...run, status: "running", current_step_id: "build" }, steps: [stepRun("build", "Build the milestone", "workflow.run", "waiting", { child_run_id: "wr-child" })], details: { build: { run_id: "wr-child", workflow: "build-milestone", title: "Build M-1", status: "running" } }, attempts: [], artifacts: [] })
    expect(within(now()).getByRole("link", { name: "Open child run" }).getAttribute("href")).toBe("/workflows/runs/wr-child")
    expect(now().textContent).toContain("Build M-1")
    fireEvent.click(within(screen.getByRole("list", { name: "Steps" })).getByRole("button", { name: /Build the milestone/ }))
    expect(within(screen.getByRole("list", { name: "Steps" })).getByRole("link", { name: "wr-child" }).getAttribute("href")).toBe("/workflows/runs/wr-child")
  })
})
