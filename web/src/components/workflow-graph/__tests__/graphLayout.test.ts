import { describe, expect, it } from "vitest"
import type { GraphEdge, GraphNode, WorkflowGraphModel } from "../../../lib/workflow"
import { layout, sizeOf } from "../graphLayout"

// layout is pure: the same graph always lands in the same places.

const step = (id: string, extra: Partial<GraphNode> = {}, group?: "review_loop" | "loop"): GraphNode =>
  ({ id, kind: "step", step: { id, title: id, type: "task.note", start: "automatic", key: {}, definition: {} as never, ...(group ? { group } : {}) }, ...extra })
const trigger = (id: string): GraphNode => ({ id, kind: "trigger", trigger: { kind: "manual" } as never })
const edge = (source: string, target: string, kind = "order", more: Partial<GraphEdge> = {}): GraphEdge => ({ id: `${source}-${target}-${kind}`, kind, source, target, ...more })
const graphOf = (nodes: GraphNode[], edges: GraphEdge[]): WorkflowGraphModel => ({ version: 2, nodes, edges })
const at = (nodes: ReturnType<typeof layout>, id: string) => nodes.find(node => node.id === id)!

describe("sizeOf", () => {
  it("sizes each kind of node, taller in a run", () => {
    expect(sizeOf(step("a"))).toEqual({ width: 240, height: 76 })
    expect(sizeOf(step("a"), true)).toEqual({ width: 240, height: 98 })
    expect(sizeOf(trigger("t"))).toEqual({ width: 220, height: 60 })
    expect(sizeOf(step("r", {}, "review_loop"))).toEqual({ width: 340, height: 168 })
    expect(sizeOf(step("r", {}, "review_loop"), true)).toEqual({ width: 340, height: 188 })
    expect(sizeOf(step("l", {}, "loop"))).toEqual({ width: 280, height: 92 })
    expect(sizeOf(step("l", {}, "loop"), true)).toEqual({ width: 280, height: 112 })
    expect(sizeOf({ id: "i", kind: "inner", parent: "r" })).toEqual({ width: 132, height: 44 })
  })
})

describe("layout", () => {
  const chain = graphOf([trigger("t"), step("a"), step("b")], [edge("t", "a", "start"), edge("a", "b"), edge("t", "b", "data")])

  it("places a chain left to right from a margin, each node a button", () => {
    const nodes = layout(chain, "LR", false)
    expect(nodes.map(node => [node.id, node.type, node.position, node.width, node.height])).toEqual([
      ["t", "trigger", { x: 8, y: 16 }, 220, 60],
      ["a", "step", { x: 284, y: 8 }, 240, 76],
      ["b", "step", { x: 580, y: 8 }, 240, 76],
    ])
    expect(at(nodes, "a")).toMatchObject({ data: { node: chain.nodes[1], direction: "LR" }, draggable: false, connectable: false, ariaRole: "button" })
    expect(at(nodes, "a").parentId).toBeUndefined()
  })

  it("places a chain top to bottom, and taller in a run", () => {
    const nodes = layout(chain, "TB", true)
    expect(nodes.map(node => [node.id, node.position, node.height])).toEqual([
      ["t", { x: 18, y: 8 }, 60],
      ["a", { x: 8, y: 124 }, 98],
      ["b", { x: 8, y: 278 }, 98],
    ])
  })

  it("lets a branch into a step shape the layout, but not one past it", () => {
    const into = layout(graphOf([step("a"), step("b")], [edge("a", "b", "branch")]), "LR", false)
    expect(at(into, "b").position.x).toBeGreaterThan(at(into, "a").position.x)
    const past = layout(graphOf([step("a"), step("b")], [edge("a", "b", "branch", { skip: true })]), "LR", false)
    expect(at(past, "b").position.x).toBe(at(past, "a").position.x)
  })

  it("draws a review loop as a group, its inner nodes placed by role and not selectable", () => {
    const graph = graphOf([step("r", {}, "review_loop"), { id: "p", kind: "inner", parent: "r", inner: { role: "pass", title: "Pass" } }, { id: "f", kind: "inner", parent: "r", inner: { role: "fixes", title: "Fixes" } }, { id: "x", kind: "inner", parent: "r", inner: { role: "other", title: "?" } }], [])
    const nodes = layout(graph, "LR", false)
    expect(at(nodes, "r")).toMatchObject({ type: "loop", position: { x: 8, y: 8 }, width: 340, height: 168 })
    expect(at(nodes, "p")).toMatchObject({ type: "inner", parentId: "r", extent: "parent", position: { x: 20, y: 72 }, width: 132, height: 44, draggable: false, connectable: false, selectable: false, focusable: false, ariaRole: "img" })
    expect(at(nodes, "f").position).toEqual({ x: 188, y: 72 })
    expect(at(nodes, "x").position).toEqual({ x: 0, y: 0 })
    expect(at(layout(graph, "LR", true), "f").position).toEqual({ x: 188, y: 92 })
  })

  it("lays a loop's body out inside its box, the box sized round it", () => {
    const graph = graphOf([step("l", {}, "loop"), step("a", { parent: "l" }), step("b", { parent: "l" }), step("after")], [edge("a", "b"), edge("l", "after")])
    const nodes = layout(graph, "LR", false)
    expect(at(nodes, "l")).toMatchObject({ type: "repeat", position: { x: 8, y: 8 }, width: 568, height: 184 })
    expect(at(nodes, "a")).toMatchObject({ type: "step", parentId: "l", extent: "parent", position: { x: 16, y: 92 } })
    expect(at(nodes, "b")).toMatchObject({ parentId: "l", position: { x: 312, y: 92 } })
    expect(at(nodes, "after").position).toEqual({ x: 632, y: 62 })
    const run = layout(graph, "LR", true)
    expect(at(run, "l")).toMatchObject({ width: 568, height: 226 })
    expect(at(run, "a").position).toEqual({ x: 16, y: 112 })
  })

  it("keeps an empty loop at its own size, and a lone body at the box's least width", () => {
    expect(at(layout(graphOf([step("l", {}, "loop")], []), "LR", false), "l")).toMatchObject({ width: 280, height: 92 })
    const lone = layout(graphOf([step("l", {}, "loop"), { id: "n", kind: "trigger", parent: "l", trigger: { kind: "manual" } as never }], []), "LR", false)
    expect(at(lone, "l")).toMatchObject({ width: 280, height: 168 })
  })

  it("lays nothing out for an empty graph", () => {
    expect(layout(graphOf([], []), "LR", false)).toEqual([])
  })
})
