// Where the workflow map's nodes go (LEG-710): the workflows that call or
// are called by others laid out top to bottom with dagre, callers above
// callees, and those that stand alone in a box of columns below them.
import dagre from "@dagrejs/dagre"
import { splitStandalone, type WorkflowMap, type WorkflowMapEdge, type WorkflowMapNode } from "../../lib/workflowMap"

// Node sizes come from fixed classes, so the layout never waits for the
// nodes to be measured.
export const mapSizes = { node: { width: 232, height: 88 }, gap: 24, boxHeader: 40, boxPad: 16, boxGap: 56 } as const

export type Point = { x: number; y: number }
export type PlacedNode = { id: string; x: number; y: number; width: number; height: number; node: WorkflowMapNode }
export type PlacedEdge = { id: string; points: Point[]; labelAt: Point; edge: WorkflowMapEdge }
export type StandaloneBox = { x: number; y: number; width: number; height: number; count: number }
export type MapLayout = { nodes: PlacedNode[]; edges: PlacedEdge[]; standalone: StandaloneBox | null }

// layoutWorkflowMap places the map. columns is how many columns the
// standalone box has when nothing else sets its width (a map of standalone
// workflows only).
export function layoutWorkflowMap(map: WorkflowMap, columns = 3): MapLayout {
  const { linked, standalone } = splitStandalone(map)
  const graph = layoutLinked(linked, map.edges)
  if (standalone.length === 0) return { nodes: graph.nodes, edges: graph.edges, standalone: null }
  const cols = graph.width > 0 ? Math.max(2, columnsWithin(graph.width)) : columns
  const box = layoutStandalone(standalone, Math.max(1, Math.min(standalone.length, cols)), graph.bottom)
  return { nodes: [...graph.nodes, ...box.nodes], edges: graph.edges, standalone: box.box }
}

type LinkedLayout = { nodes: PlacedNode[]; edges: PlacedEdge[]; width: number; bottom: number }

// layoutLinked places the workflows with a call either way with dagre, top
// to bottom: callers above callees. bottom is where the box below starts.
function layoutLinked(linked: WorkflowMapNode[], calls: WorkflowMapEdge[]): LinkedLayout {
  if (linked.length === 0) return { nodes: [], edges: [], width: 0, bottom: 0 }
  const { width: W, height: H } = mapSizes.node
  const g = new dagre.graphlib.Graph({ multigraph: false })
  g.setGraph({ rankdir: "TB", nodesep: 32, ranksep: 72, marginx: 0, marginy: 0 })
  g.setDefaultEdgeLabel(() => ({}))
  for (const node of linked) g.setNode(node.id, { width: W, height: H })
  for (const edge of calls) g.setEdge(edge.source, edge.target)
  dagre.layout(g)
  const nodes = linked.map(node => {
    const placed = g.node(node.id)
    return { id: node.id, x: placed.x - W / 2, y: placed.y - H / 2, width: W, height: H, node }
  })
  const edges = calls.map(edge => {
    const points = (g.edge(edge.source, edge.target)?.points ?? []).map((point: Point) => ({ x: point.x, y: point.y }))
    return { id: edge.id, points, labelAt: points[Math.floor(points.length / 2)] ?? { x: 0, y: 0 }, edge }
  })
  return { nodes, edges, width: g.graph().width ?? 0, bottom: (g.graph().height ?? 0) + mapSizes.boxGap }
}

// columnsWithin is how many standalone columns fit in a width.
const columnsWithin = (width: number) => Math.floor((width - 2 * mapSizes.boxPad + mapSizes.gap) / (mapSizes.node.width + mapSizes.gap))

// layoutStandalone places the workflows with no calls in a box of cols
// columns whose top is at y.
function layoutStandalone(standalone: WorkflowMapNode[], cols: number, y: number): { box: StandaloneBox; nodes: PlacedNode[] } {
  const { width: W, height: H } = mapSizes.node
  const rows = Math.ceil(standalone.length / cols)
  const box: StandaloneBox = {
    x: 0, y,
    width: 2 * mapSizes.boxPad + cols * W + (cols - 1) * mapSizes.gap,
    height: mapSizes.boxHeader + mapSizes.boxPad + rows * H + (rows - 1) * mapSizes.gap,
    count: standalone.length,
  }
  const nodes = standalone.map((node, index) => {
    const col = index % cols, row = Math.floor(index / cols)
    return { id: node.id, x: box.x + mapSizes.boxPad + col * (W + mapSizes.gap), y: box.y + mapSizes.boxHeader + row * (H + mapSizes.gap), width: W, height: H, node }
  })
  return { box, nodes }
}
