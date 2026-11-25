import { QueryClientProvider } from "@tanstack/react-query"
import { createQueryClient, queryKeys } from "../../lib/query"
import { cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react"
import { MemoryRouter, Route, Routes } from "react-router-dom"
import { beforeEach, describe, expect, it, vi } from "vitest"
import type { WorkflowRunView } from "../../lib/workflow"
import { WorkflowRunPage } from "../WorkflowRunPage"

const { fetchRun, stepAction } = vi.hoisted(() => ({ fetchRun: vi.fn(), stepAction: vi.fn() }))
vi.mock("../../hooks/useServer", () => ({ useServer: () => ({ baseUrl: "" }) }))
vi.mock("../../lib/workflow", async importOriginal => ({ ...(await importOriginal<typeof import("../../lib/workflow")>()), fetchWorkflowRun: fetchRun, workflowStepAction: stepAction }))

const run = { id: "wr-1", workflow_name: "review-loop", workflow_title: "Review", definition_source: "/w/review-loop.yaml", definition_digest: "d", definition_json: JSON.stringify({ title: "Review" }), inputs_json: "{}", created_at: "2026-11-25T10:00:00Z", started_at: "2026-11-25T10:00:00Z" }
const stepRun = (path: string, step_order: number, title: string, executor_type: string, status: string, extra = {}) => {
  const slash = path.lastIndexOf("/")
  const parent_path = slash < 0 ? "" : path.slice(0, slash)
  return { run_id: "wr-1", path, parent_path, step_id: path.slice(slash + 1), step_order, title, executor_type, definition_json: "{}", resolved_inputs_json: "{}", outputs_json: "{}", status, latest_attempt: 1, ...extra }
}

function show(data: unknown) {
  fetchRun.mockResolvedValue(data)
  const client = createQueryClient()
  client.setQueryData(queryKeys.workflowRun("", "wr-1"), data as WorkflowRunView)
  render(<QueryClientProvider client={client}><MemoryRouter initialEntries={["/workflows/runs/wr-1"]}><Routes><Route path="/workflows/runs/:runId" element={<WorkflowRunPage />} /></Routes></MemoryRouter></QueryClientProvider>)
}
const now = () => screen.getByRole("region", { name: "Now" })

// Loops (LEG-490): a loop's passes under it, each with its steps, and a
// pass's step addressed by its path.
const looping = {
  run: { ...run, status: "waiting", current_step_id: "review/1/pass", needs_attention: true },
  steps: [
    stepRun("open", 0, "Open", "task.context", "succeeded"),
    stepRun("review", 1, "Review", "", "running", { definition_json: JSON.stringify({ id: "review", loop: { repeat: true } }) }),
    stepRun("close", 2, "Close", "task.context", "pending"),
    stepRun("review/0/pass", 0, "Review pass", "task.context", "succeeded"),
    stepRun("review/0/confirm", 1, "Confirm", "task.context", "skipped"),
    stepRun("review/1/pass", 0, "Review pass", "task.context", "paused", { error: "the reviewer broke" }),
    stepRun("review/1/confirm", 1, "Confirm", "task.context", "pending"),
  ],
  attempts: [], artifacts: [],
}

describe("WorkflowRunPage loops", () => {
  beforeEach(() => { cleanup(); fetchRun.mockReset(); stepAction.mockReset() })

  it("lists each pass's steps under its loop", () => {
    show(looping)
    const steps = screen.getByRole("list", { name: "Steps" })
    const passes = within(steps).getAllByRole("list", { name: /^Pass \d+$/ })
    expect(passes.map(pass => pass.getAttribute("aria-label"))).toEqual(["Pass 1", "Pass 2"])
    expect(within(passes[1]).getByText("Review pass")).toBeTruthy()
    expect(within(passes[1]).getByText("Confirm")).toBeTruthy()
  })

  it("counts the run's own steps on its bar", () => {
    show(looping)
    expect(document.body.textContent).toContain("Step 2 of 3 · Review")
  })

  it("retries a pass's paused step by its path", async () => {
    stepAction.mockResolvedValue({})
    show(looping)
    expect(within(now()).getByText("Review pass failed")).toBeTruthy()
    fireEvent.click(within(now()).getByRole("button", { name: "Retry" }))
    await waitFor(() => expect(stepAction).toHaveBeenCalledWith("", "wr-1", "review/1/pass", "retry"))
  })
})
