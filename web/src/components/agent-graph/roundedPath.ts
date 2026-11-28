import type { Point } from "../../lib/agentGraph"

// roundedPath is an SVG path through points, each corner rounded.
export function roundedPath(points: Point[], radius = 6): string {
  if (points.length === 0) return ""
  let path = `M ${points[0].x} ${points[0].y}`
  for (let i = 1; i < points.length - 1; i++) {
    const [a, b, c] = [points[i - 1], points[i], points[i + 1]]
    const r = Math.min(radius, Math.hypot(b.x - a.x, b.y - a.y) / 2, Math.hypot(c.x - b.x, c.y - b.y) / 2)
    const toward = (p: Point, q: Point) => {
      const d = Math.hypot(q.x - p.x, q.y - p.y) || 1
      return { x: p.x + ((q.x - p.x) / d) * r, y: p.y + ((q.y - p.y) / d) * r }
    }
    const [enter, leave] = [toward(b, a), toward(b, c)]
    path += ` L ${enter.x} ${enter.y} Q ${b.x} ${b.y} ${leave.x} ${leave.y}`
  }
  const last = points[points.length - 1]
  return `${path} L ${last.x} ${last.y}`
}
