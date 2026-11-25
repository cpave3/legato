import { describe, expect, it } from "vitest"
import { runStepTree, stepPathOf } from "../workflowRunTree"

const row = (path: string, step_order: number) => {
  const slash = path.lastIndexOf("/")
  const parent_path = slash < 0 ? "" : path.slice(0, slash)
  const pass = parent_path ? Number(parent_path.slice(parent_path.lastIndexOf("/") + 1)) : undefined
  return { path, parent_path, pass, step_id: path.slice(slash + 1), step_order }
}

describe("runStepTree", () => {
  it("puts each loop pass's steps under its loop, passes in number order", () => {
    // The server lists scopes by their path as text: each/10 before each/2.
    const tree = runStepTree([row("find", 0), row("each", 1), row("tell", 2), row("each/0/next", -1), row("each/0/build", 0), row("each/10/build", 0), row("each/2/build", 0), row("each/2/card", -1)])
    expect(tree.map(node => stepPathOf(node.step))).toEqual(["find", "each", "tell"])
    const each = tree[1]
    expect(each.passes.map(pass => pass.pass)).toEqual([0, 2, 10])
    expect(each.passes[0].steps.map(node => stepPathOf(node.step))).toEqual(["each/0/next", "each/0/build"])
    expect(each.passes[1].steps.map(node => stepPathOf(node.step))).toEqual(["each/2/card", "each/2/build"])
    expect(tree[0].passes).toEqual([])
  })

  it("nests a loop inside a pass", () => {
    const tree = runStepTree([row("outer", 0), row("outer/0/inner", 0), row("outer/0/inner/1/work", 0)])
    expect(tree[0].passes[0].steps[0].passes[0].pass).toBe(1)
    expect(stepPathOf(tree[0].passes[0].steps[0].passes[0].steps[0].step)).toBe("outer/0/inner/1/work")
  })

  it("reads a run kept before paths as its step IDs", () => {
    expect(runStepTree([{ step_id: "a", step_order: 0 }]).map(node => stepPathOf(node.step))).toEqual(["a"])
  })
})
