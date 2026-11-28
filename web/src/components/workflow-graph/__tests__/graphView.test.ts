import { describe, expect, it } from "vitest"
import type { Node } from "@xyflow/react"
import type { WorkflowGraphModel } from "../../../lib/workflow"
import { chosenOf, isCompact, keyedNode, lookOf, miniColor, rectOf, topRects } from "../graphView"

type Placed = Parameters<typeof rectOf>[0]
const placed = [
  { id: "outer", position: { x: 10, y: 20 }, width: 300, height: 200 },
  { id: "inner", parentId: "outer", position: { x: 5, y: 6 }, width: 200, height: 100 },
  { id: "leaf", parentId: "inner", position: { x: 1, y: 2 }, width: 50, height: 40 },
] as unknown as Placed

describe("where a node is", () => {
  it("adds each loop's place to a body's", () => {
    expect(rectOf(placed, "leaf")).toEqual({ x: 16, y: 28, width: 50, height: 40 })
    expect(rectOf(placed, "outer")).toEqual({ x: 10, y: 20, width: 300, height: 200 })
    expect(rectOf(placed, "none")).toBeNull()
    expect(topRects(placed)).toEqual([{ x: 10, y: 20, width: 300, height: 200 }])
  })
})

describe("choosing", () => {
  const graph = { version: 2, nodes: [{ id: "g", kind: "step" }, { id: "i", kind: "inner", parent: "g" }], edges: [] } as unknown as WorkflowGraphModel

  it("chooses an inner node's group", () => {
    expect(chosenOf(graph, "i")).toBe("g")
    expect(chosenOf(graph, "g")).toBe("g")
    expect(chosenOf(graph, "x")).toBe("x")
  })

  it("chooses a focused node with Enter or Space, but not from a control in it", () => {
    const node = document.createElement("div")
    node.className = "react-flow__node"
    node.dataset.id = "n"
    const link = document.createElement("a")
    node.append(link)
    expect(keyedNode("Enter", node)).toBe("n")
    expect(keyedNode(" ", node)).toBe("n")
    expect(keyedNode("a", node)).toBeNull()
    expect(keyedNode("Enter", link)).toBeNull()
    expect(keyedNode("Enter", document.createElement("div"))).toBeNull()
  })
})

describe("the look", () => {
  it("draws compact below 0.6 on a canvas, its edges about a pixel and a half wide", () => {
    expect(isCompact(true, 0.5)).toBe(true)
    expect(isCompact(false, 0.5)).toBe(false)
    expect(isCompact(true, 0.6)).toBe(false)
    expect(lookOf(true, 0.4)["--lw-zoom"]).toBe(0.4)
    expect(lookOf(true, 0.4)["--xy-edge-stroke-width"]).toBeCloseTo(3)
    expect(lookOf(true, 0.1)["--xy-edge-stroke-width"]).toBe(6)
    expect(lookOf(true, 1)).toMatchObject({ "--xy-edge-stroke-width": 1 })
    expect(lookOf(false, 0.4)["--lw-zoom"]).toBeUndefined()
  })

  it("colours a minimap node by its state", () => {
    expect(miniColor({ data: { extra: { state: "running" } } } as unknown as Node)).toBe("var(--color-lw-live)")
    expect(miniColor({ data: {} } as unknown as Node)).toBe("var(--color-lw-line-strong)")
  })
})
