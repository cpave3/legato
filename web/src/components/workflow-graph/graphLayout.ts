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

// headerOf is the height of a loop's header over its body.
const headerOf = (run: boolean) => run ? repeatBox.runHeader : repeatBox.header

export function sizeOf(node: GraphNode, run = false): Size {
  if (node.kind === "inner") return sizes.inner
  if (node.kind === "step" && node.step?.group === "review_loop") return run ? sizes.runLoop : sizes.loop
  if (node.kind === "step" && node.step?.group === "loop") return { width: repeatBox.minWidth, height: headerOf(run) }
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

type Point = { x: number; y: number }
type Direction = "LR" | "TB"

// Layout is one layout's work: the graph, and the sizes and places (within
// their scope) found so far.
type Layout = { graph: WorkflowGraphModel; byId: Map<string, GraphNode>; direction: Direction; run: boolean; sized: Map<string, Size>; relative: Map<string, Point> }

// inLoopBody is a node in a loop's body (graph version 2).
const inLoopBody = (work: Layout, node: GraphNode) => Boolean(node.parent) && isRepeat(work.byId.get(node.parent!))

// scopeOf is a node's scope: the run's own (""), a loop's body (the
// loop's ID), or none for a review loop's inner nodes.
function scopeOf(work: Layout, node: GraphNode): string | null {
  if (node.kind === "inner") return null
  return inLoopBody(work, node) ? node.parent! : ""
}

const sizeIn = (work: Layout, node: GraphNode) => work.sized.get(node.id) ?? sizeOf(node, work.run)

// boxOf is a loop's box round its body: its header above, a margin round
// it, at least a step's width; its own size while it has no body.
function boxOf(loop: GraphNode, body: Size, run: boolean): Size {
  if (body.width === 0) return sizeOf(loop, run)
  return { width: Math.max(repeatBox.minWidth, body.width + 2 * repeatBox.pad), height: headerOf(run) + body.height + repeatBox.pad }
}

// arrange runs dagre over a scope's nodes and the edges between them that
// shape the layout; it gives each node's centre.
function arrange(work: Layout, members: GraphNode[]) {
  const g = new dagre.graphlib.Graph()
  g.setGraph({ rankdir: work.direction, nodesep: 28, ranksep: 56, marginx: 0, marginy: 0 })
  g.setDefaultEdgeLabel(() => ({}))
  for (const member of members) g.setNode(member.id, { ...sizeIn(work, member) })
  const inScope = new Set(members.map(member => member.id))
  for (const edge of work.graph.edges) if (shapes(edge) && inScope.has(edge.source) && inScope.has(edge.target)) g.setEdge(edge.source, edge.target)
  dagre.layout(g)
  return (id: string): Point => g.node(id)
}

// A scope's places start from a margin: the run's own round the graph, a
// loop body's under its box's header.
const originOf = (scope: string, run: boolean): Point => scope === "" ? { x: 8, y: 8 } : { x: repeatBox.pad, y: headerOf(run) }

// placeScope lays a scope out from the inside out: each loop's body
// first, whose size makes the loop's box, then the scope itself. It
// returns the scope's size.
function placeScope(work: Layout, scope: string): Size {
  const members = work.graph.nodes.filter(node => scopeOf(work, node) === scope)
  for (const member of members) if (isRepeat(member)) work.sized.set(member.id, boxOf(member, placeScope(work, member.id), work.run))
  if (members.length === 0) return { width: 0, height: 0 }
  const centre = arrange(work, members)
  const origin = originOf(scope, work.run)
  let maxX = 0, maxY = 0
  for (const member of members) {
    const at = centre(member.id)
    const size = sizeIn(work, member)
    const x = at.x - size.width / 2, y = at.y - size.height / 2
    work.relative.set(member.id, { x: x + origin.x, y: y + origin.y })
    maxX = Math.max(maxX, x + size.width)
    maxY = Math.max(maxY, y + size.height)
  }
  return { width: maxX, height: maxY }
}

// innerNode is a review loop's inner node, placed in its group by role.
function innerNode(work: Layout, node: GraphNode, size: Size): Omit<FlowNode, "ariaLabel"> {
  return {
    id: node.id,
    type: "inner",
    parentId: node.parent,
    extent: "parent" as const,
    position: { ...(innerPlaces[node.inner?.role ?? ""] ?? { x: 0, y: 0 }), ...(work.run ? { y: innerPlaces.pass.y + 20 } : {}) },
    width: size.width,
    height: size.height,
    data: { node, direction: work.direction },
    draggable: false,
    connectable: false,
    selectable: false,
    focusable: false,
    ariaRole: "img",
  }
}

// typeOf is a node's type: a loop's box, a review loop's group, or its
// kind.
function typeOf(node: GraphNode): string {
  if (isRepeat(node)) return "repeat"
  return node.kind === "step" && node.step?.group ? "loop" : node.kind
}

// flowNode is a node as React Flow draws it, in its loop's box when it is
// in a loop's body.
function flowNode(work: Layout, node: GraphNode): Omit<FlowNode, "ariaLabel"> {
  const size = sizeIn(work, node)
  if (node.kind === "inner") return innerNode(work, node, size)
  return {
    id: node.id,
    type: typeOf(node),
    ...(inLoopBody(work, node) ? { parentId: node.parent, extent: "parent" as const } : {}),
    position: work.relative.get(node.id) ?? { x: 0, y: 0 },
    width: size.width,
    height: size.height,
    data: { node, direction: work.direction },
    draggable: false,
    connectable: false,
    ariaRole: "button",
  }
}

// layout places the nodes once per graph. Only the edges that shape it
// (shapes) count; the other kinds are drawn over the result. A loop of
// graph version 2 is laid out from the inside out: its body first, as a
// graph of its own, whose size makes the loop's box.
export function layout(graph: WorkflowGraphModel, direction: Direction, run: boolean): Omit<FlowNode, "ariaLabel">[] {
  const work: Layout = { graph, byId: new Map(graph.nodes.map(node => [node.id, node])), direction, run, sized: new Map(), relative: new Map() }
  placeScope(work, "")
  return graph.nodes.map(node => flowNode(work, node))
}
