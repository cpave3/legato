// Geometry of a laid-out agents graph, for its tests (LEG-615): an edge's
// segments, where two edges cross, and the boxes and nodes a line passes
// through that it does not end in.
import type { AgentGraphLayout, GraphEdge, Point } from "../agentGraph"

export type Segment = [Point, Point]

export function segments(edge: GraphEdge): Segment[] {
  const out: Segment[] = []
  for (let i = 1; i < edge.points.length; i++) out.push([edge.points[i - 1], edge.points[i]])
  return out
}

const cross = (o: Point, a: Point, b: Point) => (a.x - o.x) * (b.y - o.y) - (a.y - o.y) * (b.x - o.x)

// properlyCross is whether two segments cross at a point inside both: a line
// that touches another, or runs along it, does not cross it.
function properlyCross([a, b]: Segment, [c, d]: Segment): boolean {
  const d1 = cross(c, d, a), d2 = cross(c, d, b), d3 = cross(a, b, c), d4 = cross(a, b, d)
  return d1 * d2 < 0 && d3 * d4 < 0
}

// crossings lists each place two links cross, as "edge × edge". Sub-agent
// links are left out: they stay inside their agent's tree.
export function crossings(layout: AgentGraphLayout): string[] {
  const edges = layout.edges.filter((edge) => edge.kind !== "subagent")
  const out: string[] = []
  for (let i = 0; i < edges.length; i++) {
    for (let j = i + 1; j < edges.length; j++) {
      for (const s of segments(edges[i])) for (const t of segments(edges[j])) {
        if (properlyCross(s, t)) out.push(`${edges[i].id} × ${edges[j].id}`)
      }
    }
  }
  return out
}

interface Rect { id: string; x: number; y: number; width: number; height: number }

// enters is whether an axis-aligned segment passes inside a rectangle (its
// edge does not count).
function enters([a, b]: Segment, r: Rect): boolean {
  const lo = { x: Math.min(a.x, b.x), y: Math.min(a.y, b.y) }, hi = { x: Math.max(a.x, b.x), y: Math.max(a.y, b.y) }
  return lo.x < r.x + r.width && hi.x > r.x && lo.y < r.y + r.height && hi.y > r.y
}

// boxesCrossed lists the boxes each link passes through that neither of its
// ends is in, as "edge through box".
export function boxesCrossed(layout: AgentGraphLayout): string[] {
  const boxOf = new Map(layout.nodes.map((node) => [node.id, node.box]))
  const out: string[] = []
  for (const edge of layout.edges) {
    const own = new Set([boxOf.get(edge.source) ?? edge.source, boxOf.get(edge.target) ?? edge.target])
    for (const box of layout.boxes) {
      if (own.has(box.id)) continue
      if (segments(edge).some((s) => enters(s, box))) out.push(`${edge.id} through ${box.id}`)
    }
  }
  return out
}

// nodesCrossed lists the nodes each link passes through that it does not
// end at, as "edge through node".
export function nodesCrossed(layout: AgentGraphLayout): string[] {
  const out: string[] = []
  for (const edge of layout.edges) {
    for (const node of layout.nodes) {
      if (node.id === edge.source || node.id === edge.target) continue
      if (segments(edge).some((s) => enters(s, node))) out.push(`${edge.id} through ${node.id}`)
    }
  }
  return out
}

// orthogonal is whether every segment of every link runs straight across or
// straight down.
export function orthogonal(layout: AgentGraphLayout): boolean {
  return layout.edges.every((edge) => edge.points.length >= 2 && segments(edge).every(([a, b]) => a.x === b.x || a.y === b.y))
}

// inside is whether a point lies in a rectangle, its edge included.
export function inside(p: Point, r: Rect): boolean {
  return p.x >= r.x - 0.01 && p.x <= r.x + r.width + 0.01 && p.y >= r.y - 0.01 && p.y <= r.y + r.height + 0.01
}

// geometryOf is everything a graph's drawing depends on, to compare two
// layouts by.
export function geometryOf(layout: AgentGraphLayout) {
  return {
    boxes: layout.boxes.map(({ id, x, y, width, height }) => ({ id, x, y, width, height })),
    nodes: layout.nodes.map(({ id, x, y, width, height }) => ({ id, x, y, width, height })),
    edges: layout.edges.map(({ id, points }) => ({ id, points })),
  }
}
