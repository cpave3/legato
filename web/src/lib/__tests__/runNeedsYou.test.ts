import { describe, expect, it } from "vitest"
import { runNeedsYou } from "../runNeedsYou"
import type { WorkflowRunView } from "../workflow"

const step = (path: string, status: string, extra = {}) => {
  const slash = path.lastIndexOf("/")
  return { run_id: "wr-1", path, parent_path: slash < 0 ? "" : path.slice(0, slash), step_id: path.slice(slash + 1), step_order: 0, title: path, executor_type: "task.context", resolved_inputs_json: "{}", outputs_json: "{}", status, latest_attempt: 1, ...extra }
}

// A loop's pass's step (LEG-490) is drawn in its own node inside its
// loop's (graph version 2, LEG-484) and acted on by its path.
describe("runNeedsYou in a loop", () => {
  it("puts a pass's paused step in its own node, by its path", () => {
    const view = { run: { id: "wr-1", status: "waiting", current_step_id: "review/1/pass", created_at: "2026-11-25T10:00:00Z" }, steps: [step("review", "running"), step("review/1/pass", "paused", { error: "broke" })], attempts: [], artifacts: [] } as unknown as WorkflowRunView
    const { rows } = runNeedsYou(view, [], [], id => `/agents/${id}`)
    expect(rows).toHaveLength(1)
    expect(rows[0]).toMatchObject({ kind: "item", node: "step:review/pass", run: { step: "review/1/pass" } })
  })

  it("offers Retry on a failed run's loop, not on its pass's failed step", () => {
    const view = { run: { id: "wr-1", status: "failed", current_step_id: "review", created_at: "2026-11-25T10:00:00Z" }, steps: [step("review", "failed", { error: "pass 1 failed" }), step("review/1/pass", "failed", { error: "broke" })], attempts: [], artifacts: [] } as unknown as WorkflowRunView
    const { rows } = runNeedsYou(view, [], [], id => `/agents/${id}`)
    expect(rows.map(row => row.kind === "item" && row.run?.step)).toEqual(["review"])
  })
})
