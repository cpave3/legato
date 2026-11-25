import { describe, expect, it } from "vitest"
import type { WorkflowRunView } from "../workflow"
import type { WorkflowGraphModel } from "../workflowGraph"
import { instancesOf, runNodeExtras, templatePath } from "../workflowRunGraph"

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

// Graph version 2 (LEG-484): a loop's body's steps are nodes of their own,
// each drawn in the state of its newest pass, and the loop says how each
// pass went.
describe("a v2 run's tree", () => {
  const graph = { version: 2, edges: [], nodes: [
    { id: "step:each", kind: "step", step: { id: "each", path: "each", title: "Each", type: "loop", group: "loop", loop: { form: "over", max: 5 }, key: {} } },
    { id: "step:each/read", kind: "step", parent: "step:each", step: { id: "read", path: "each/read", title: "Read", type: "task.context", key: {} } },
    { id: "step:each/write", kind: "step", parent: "step:each", step: { id: "write", path: "each/write", title: "Write", type: "task.note", key: {} } },
  ] } as unknown as WorkflowGraphModel
  const row = (path: string, status: string, pass?: number) => ({ run_id: "wr-1", step_id: path.split("/").pop(), path, parent_path: pass === undefined ? "" : path.split("/").slice(0, -1).join("/"), pass, step_order: 0, status, started_at: "2026-11-25T00:00:00Z" })
  const view = {
    run: { id: "wr-1", status: "running" },
    steps: [row("each", "running"), row("each/0/read", "succeeded", 0), row("each/0/write", "skipped", 0), row("each/1/read", "succeeded", 1), row("each/1/write", "running", 1)],
    attempts: [{ id: "wa-1", path: "each/1/write", step_id: "write", status: "running", agent_kind: "codex" }],
    artifacts: [],
  } as unknown as WorkflowRunView
  const now = Date.parse("2026-11-25T00:10:00Z")

  it("names a step instance's node by its path without its passes", () => {
    expect(templatePath("each/1/write")).toBe("each/write")
    expect(templatePath("outer/0/inner/3/step")).toBe("outer/inner/step")
    expect(templatePath("build")).toBe("build")
    expect(instancesOf(view, "each/write").map(step => step.path)).toEqual(["each/0/write", "each/1/write"])
  })

  it("orders a nested loop's instances by every pass on their path, outermost first", () => {
    const nested = {
      run: { id: "wr-1", status: "running" },
      steps: [row("outer/0/inner/2/build", "succeeded", 2), row("outer/1/inner/0/build", "running", 0), row("outer/0/inner/0/build", "succeeded", 0)],
      attempts: [], artifacts: [],
    } as unknown as WorkflowRunView
    expect(instancesOf(nested, "outer/inner/build").map(step => step.path)).toEqual(["outer/0/inner/0/build", "outer/0/inner/2/build", "outer/1/inner/0/build"])
    const nestedGraph = { version: 2, edges: [], nodes: [
      { id: "step:outer/inner/build", kind: "step", step: { id: "build", path: "outer/inner/build", title: "Build", type: "task.note", key: {} } },
    ] } as unknown as WorkflowGraphModel
    expect(runNodeExtras(nested, nestedGraph, now)["step:outer/inner/build"]).toMatchObject({ state: "running", pass: "Pass 1" })
  })

  it("draws a body step in its newest pass's state", () => {
    const extras = runNodeExtras(view, graph, now)
    expect(extras["step:each/write"]).toMatchObject({ state: "running", pass: "Pass 2", agent: "codex" })
    expect(extras["step:each/read"]).toMatchObject({ state: "done", pass: "Pass 2" })
  })

  it("gives the loop each pass's state and how far it has come", () => {
    const extras = runNodeExtras(view, graph, now)
    expect(extras["step:each"]).toMatchObject({ state: "running", pass: "Pass 2 of 5", passes: [{ pass: 0, state: "done" }, { pass: 1, state: "running" }] })
  })
})
