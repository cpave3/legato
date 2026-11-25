import { describe, expect, it } from "vitest"
import type { WorkflowRunView } from "../workflow"
import type { WorkflowGraphModel } from "../workflowGraph"
import { runNodeExtras } from "../workflowRunGraph"

describe("runNodeExtras", () => {
  it("shows a suspended step as suspended", () => {
    const graph = { version: 1, edges: [], nodes: [{ id: "step:build", kind: "step", step: { id: "build", title: "Build", type: "agent.run" } }] } as unknown as WorkflowGraphModel
    const view = {
      run: { id: "wr-1", status: "suspended" },
      steps: [{ step_id: "build", title: "Build", executor_type: "agent.run", status: "suspended" }],
      attempts: [{ id: "wa-1", step_id: "build", executor_type: "agent.run", status: "suspended", started_at: "2026-11-25T00:00:00Z" }],
      artifacts: [],
    } as unknown as WorkflowRunView
    expect(runNodeExtras(view, graph, Date.parse("2026-11-25T01:00:00Z"))["step:build"].state).toBe("suspended")
  })
})
