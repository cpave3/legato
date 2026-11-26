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

// A review-loop step waiting on its loop that needs the operator (LEG-593):
// the server names the signal, the reason and the actions, the first the
// primary; End asks in a steward's words when the loop is stewarded.
describe("runNeedsYou on a review loop's steps", () => {
  const attempt = (extra = {}) => ({ id: "at-1", run_id: "wr-1", step_id: "watch", path: "steward/1/watch", attempt_number: 1, executor_type: "github.pr.review.wait", status: "waiting", started_at: "2026-11-25T10:00:00Z", ...extra })
  const view = (extra = {}) => ({ run: { id: "wr-1", status: "running", current_step_id: "steward/1/watch", created_at: "2026-11-25T10:00:00Z" },
    steps: [step("steward", "running"), step("steward/1/watch", "waiting", { executor_type: "github.pr.review.wait" })],
    attempts: [attempt(extra)], artifacts: [] }) as unknown as WorkflowRunView

  it("draws a held steward's item with Watch again and End", () => {
    const { rows } = runNeedsYou(view({ needs_attention: true, attention_reason: "Steward held (Stop watching). legato-reviews' approval at bbb222 still stands.",
      attention_signal: "workflow_review_steward", attention_actions: ["watch_again", "end"], attention_steward: true }), [], [], id => `/agents/${id}`)
    expect(rows).toHaveLength(1)
    const row = rows[0]
    if (row.kind !== "item") throw new Error("not an item")
    expect(row.run).toEqual({ step: "steward/1/watch", stepActions: true })
    expect(row.node).toBe("step:steward/watch")
    expect(row.item.signal_kind).toBe("workflow_review_steward")
    expect(row.item.reason).toContain("approval at bbb222 still stands")
    const actions = row.item.actions ?? []
    expect(actions.map(action => action.label)).toEqual(["Watch again", "End"])
    const end = actions[1]
    expect(end.requires_confirmation).toBe(true)
    expect(end.confirmation).toContain("End the steward?")
  })

  it("offers Message the author as a reply, and End as stuck when no steward watches", () => {
    const { rows } = runNeedsYou(view({ needs_attention: true, attention_reason: "Review loop stuck: the limit.",
      attention_signal: "workflow_review_stuck", attention_actions: ["another_pass", "end", "message"] }), [], [], id => `/agents/${id}`)
    const row = rows[0]
    if (row.kind !== "item") throw new Error("not an item")
    const actions = row.item.actions ?? []
    expect(actions.map(action => action.id)).toEqual(["another_pass", "end", "message"])
    expect(actions[1].confirmation).toContain("as stuck")
    expect(actions[2].requires_text).toBe(true)
  })

  it("draws nothing while the loop needs no one", () => {
    expect(runNeedsYou(view(), [], [], id => `/agents/${id}`).rows).toHaveLength(0)
  })
})
