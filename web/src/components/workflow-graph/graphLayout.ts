// Where the workflow graph's nodes go: their sizes and the dagre layout,
// apart from the components that draw them.
import dagre from "@dagrejs/dagre"
import type { Node } from "@xyflow/react"
import type { GraphNodeExtra } from "../../lib/workflowGraph"
import type { GraphNode, WorkflowGraphModel } from "../../lib/workflow"

// Node sizes come from fixed classes, so the layout never waits for the
// nodes to be measured.
export const sizes = { step: { width: 240, height: 76 }, runStep: { width: 240, height: 98 }, trigger: { width: 220, height: 60 }, loop: { width: 340, height: 168 }, runLoop: { width: 340, height: 188 }, inner: { width: 132, height: 44 } } as const
// Where a group's inner nodes sit inside it, by role.
const innerPlaces: Record<string, { x: number; y: number }> = { pass: { x: 20, y: 72 }, fixes: { x: 340 - 20 - 132, y: 72 } }

export type Size = { width: number; height: number }

// A loop (graph version 2) is a box around its body's steps: its header
// above them, a margin round them, at least a step's width.
export const repeatBox = { header: 92, runHeader: 112, pad: 16, minWidth: 280 }

export function sizeOf(node: GraphNode, run = false): Size {
  if (node.kind === "inner") return sizes.inner
  if (node.kind === "step" && node.step?.group === "review_loop") return run ? sizes.runLoop : sizes.loop
  if (node.kind === "step" && node.step?.group === "loop") return { width: repeatBox.minWidth, height: run ? repeatBox.runHeader : repeatBox.header }
  return node.kind === "trigger" ? sizes.trigger : run ? sizes.runStep : sizes.step
}

// isRepeat is a loop of graph version 2, drawn as a box holding its body.
export const isRepeat = (node: GraphNode | undefined) => node?.kind === "step" && node.step?.group === "loop"

export type FlowData = { node: GraphNode; extra?: GraphNodeExtra; direction: "LR" | "TB" }
export type FlowNode = Node<FlowData>

// shapes is whether an edge shapes the layout: start, order and a branch
// into a step, between nodes of one scope.
const shapes = (edge: GraphWorkflowEdge) => edge.kind === "start" || edge.kind === "order" || (edge.kind === "branch" && !edge.skip)
type GraphWorkflowEdge = WorkflowGraphModel["edges"][number]

// layout places the nodes once per graph. Only the edges that shape it
// (shapes) count; the other kinds are drawn over the result. A loop of
// graph version 2 is laid out from the inside out: its body first, as a
// graph of its own, whose size makes the loop's box.
export function layout(graph: WorkflowGraphModel, direction: "LR" | "TB", run: boolean): Omit<FlowNode, "ariaLabel">[] {
  const byId = new Map(graph.nodes.map(node => [node.id, node]))
  const sized = new Map<string, Size>()
  const relative = new Map<string, { x: number; y: number }>()
  // A scope's nodes: the run's own (no parent), or a loop's body.
  const scopeOf = (node: GraphNode) => node.kind === "inner" ? null : node.parent && isRepeat(byId.get(node.parent)) ? node.parent : ""
  const place = (scope: string): Size => {
    const members = graph.nodes.filter(node => scopeOf(node) === scope)
    for (const member of members) if (isRepeat(member)) {
      const body = place(member.id)
      const header = run ? repeatBox.runHeader : repeatBox.header
      sized.set(member.id, body.width === 0 ? sizeOf(member, run) : { width: Math.max(repeatBox.minWidth, body.width + 2 * repeatBox.pad), height: header + body.height + repeatBox.pad })
    }
    if (members.length === 0) return { width: 0, height: 0 }
    const g = new dagre.graphlib.Graph()
    g.setGraph({ rankdir: direction, nodesep: 28, ranksep: 56, marginx: 0, marginy: 0 })
    g.setDefaultEdgeLabel(() => ({}))
    for (const member of members) g.setNode(member.id, { ...(sized.get(member.id) ?? sizeOf(member, run)) })
    const inScope = new Set(members.map(member => member.id))
    for (const edge of graph.edges) if (shapes(edge) && inScope.has(edge.source) && inScope.has(edge.target)) g.setEdge(edge.source, edge.target)
    dagre.layout(g)
    let maxX = 0, maxY = 0
    for (const member of members) {
      const at = g.node(member.id)
      const size = sized.get(member.id) ?? sizeOf(member, run)
      const x = at.x - size.width / 2, y = at.y - size.height / 2
      relative.set(member.id, scope === "" ? { x: x + 8, y: y + 8 } : { x: x + repeatBox.pad, y: y + (run ? repeatBox.runHeader : repeatBox.header) })
      maxX = Math.max(maxX, x + size.width)
      maxY = Math.max(maxY, y + size.height)
    }
    return { width: maxX, height: maxY }
  }
  place("")
  return graph.nodes.map(node => {
    const size = sized.get(node.id) ?? sizeOf(node, run)
    if (node.kind === "inner") return {
      id: node.id,
      type: "inner",
      parentId: node.parent,
      extent: "parent" as const,
      position: { ...(innerPlaces[node.inner?.role ?? ""] ?? { x: 0, y: 0 }), ...(run ? { y: innerPlaces.pass.y + 20 } : {}) },
      width: size.width,
      height: size.height,
      data: { node, direction },
      draggable: false,
      connectable: false,
      selectable: false,
      focusable: false,
      ariaRole: "img",
    }
    const inLoop = node.parent && isRepeat(byId.get(node.parent))
    return {
      id: node.id,
      type: isRepeat(node) ? "repeat" : node.kind === "step" && node.step?.group ? "loop" : node.kind,
      ...(inLoop ? { parentId: node.parent, extent: "parent" as const } : {}),
      position: relative.get(node.id) ?? { x: 0, y: 0 },
      width: size.width,
      height: size.height,
      data: { node, direction },
      draggable: false,
      connectable: false,
      ariaRole: "button",
    }
  })
}
