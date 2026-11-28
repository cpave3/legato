import { describe, expect, it } from "vitest"
import type { WorkflowMap } from "../../../lib/workflowMap"
import { layoutWorkflowMap, mapSizes } from "../mapLayout"

const node = (id: string) => ({ id, name: id, title: id })
const edge = (source: string, target: string) => ({ id: `${source}->${target}`, source, target, calls: 1, steps: ["s"] })

describe("the workflow map's layout", () => {
  const map: WorkflowMap = {
    nodes: [node("deliver"), node("ship"), node("review-loop"), node("a"), node("b"), node("c")],
    edges: [edge("deliver", "ship"), edge("ship", "review-loop"), edge("deliver", "review-loop")],
  }
  const layout = layoutWorkflowMap(map)
  const at = (id: string) => layout.nodes.find(item => item.id === id)!

  it("puts callers above the workflows they call", () => {
    expect(at("deliver").y).toBeLessThan(at("ship").y)
    expect(at("ship").y).toBeLessThan(at("review-loop").y)
    expect(layout.edges.map(item => item.id).sort()).toEqual(["deliver->review-loop", "deliver->ship", "ship->review-loop"])
    for (const item of layout.edges) expect(item.points.length).toBeGreaterThan(1)
  })

  it("puts the workflows that stand alone in their own box below the rest", () => {
    const box = layout.standalone!
    expect(box.count).toBe(3)
    const bottom = Math.max(...["deliver", "ship", "review-loop"].map(id => at(id).y + mapSizes.node.height))
    expect(box.y).toBeGreaterThan(bottom)
    for (const id of ["a", "b", "c"]) {
      const item = at(id)
      expect(item.x).toBeGreaterThanOrEqual(box.x)
      expect(item.y).toBeGreaterThan(box.y)
      expect(item.x + item.width).toBeLessThanOrEqual(box.x + box.width)
      expect(item.y + item.height).toBeLessThanOrEqual(box.y + box.height)
    }
  })

  it("draws no box when every workflow calls or is called", () => {
    expect(layoutWorkflowMap({ nodes: [node("x"), node("y")], edges: [edge("x", "y")] }).standalone).toBeNull()
  })

  it("lays out a map of standalone workflows alone, in columns", () => {
    const only = layoutWorkflowMap({ nodes: ["a", "b", "c", "d", "e"].map(node), edges: [] }, 2)
    expect(only.standalone!.count).toBe(5)
    expect(new Set(only.nodes.map(item => item.x)).size).toBe(2)
  })
})
