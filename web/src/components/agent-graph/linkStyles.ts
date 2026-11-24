import type { GraphEdgeKind } from "../../lib/agentGraph"

// How each kind of link is drawn. The palette is the theme's, so a kind
// is told apart by its dash as well as its colour.
export const linkStyles: Record<GraphEdgeKind, { stroke: string; dash?: string; width: number; arrow?: boolean }> = {
  subagent: { stroke: "var(--color-lw-text-2)", width: 1.25 },
  swarm: { stroke: "var(--color-lw-accent)", width: 2, arrow: true },
  reviewer: { stroke: "var(--color-lw-done)", dash: "6 4", width: 1.75 },
  slot: { stroke: "var(--color-lw-text-2)", dash: "4 3", width: 1.5 },
  review_loop: { stroke: "var(--color-lw-done)", dash: "6 4", width: 1.75 },
  workflow: { stroke: "var(--color-lw-live)", dash: "8 3 2 3", width: 1.5 },
  reeve_watch: { stroke: "var(--color-lw-accent-text)", dash: "1 5", width: 2 },
  peer_message: { stroke: "var(--color-lw-link)", width: 1.75, arrow: true },
  worktree: { stroke: "var(--color-lw-line-strong)", dash: "2 6", width: 1.25 },
}
