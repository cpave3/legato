import type { Node } from "@xyflow/react"
import { nodeName, type GraphNodeExtra, type NodeRunState } from "../../lib/workflowGraph"
import type { WorkflowGraphModel } from "../../lib/workflow"
import type { Rect } from "../../lib/graphViewport"
import type { FlowData, FlowNode } from "./graphLayout"

// What the graph shows over its layout, apart from the components: each
// node's state, where a node is on the page, and the canvas's look.

type Placed = Omit<FlowNode, "ariaLabel">[]

/** withState is each placed node with the graph's current node, what the
 * page knows of it, its name and whether it is chosen; a trigger that did
 * not start the run is dimmed. */
export function withState(placed: Placed, graph: WorkflowGraphModel, selected: string | null, extras?: Record<string, GraphNodeExtra>): FlowNode[] {
  const byId = new Map(graph.nodes.map(node => [node.id, node]))
  return placed.map(node => {
    const current = byId.get(node.id) ?? node.data.node
    const extra = extras?.[node.id]
    return { ...node, data: { ...node.data, node: current, extra }, ariaLabel: nodeName(current, extra), selected: node.id === selected, className: extra?.trigger === "other" ? "opacity-50" : undefined }
  })
}

const rectAt = (node: Placed[number], x: number, y: number): Rect => ({ x, y, width: node.width ?? 0, height: node.height ?? 0 })

/** rectOf is a node's place on the graph: a loop body's position is
 * relative to its loop, so every loop it is in adds its own. */
export function rectOf(placed: Placed, id: string): Rect | null {
  const node = placed.find(item => item.id === id)
  if (!node) return null
  let { x, y } = node.position
  for (let parent = placed.find(item => item.id === node.parentId); parent; parent = placed.find(item => item.id === parent!.parentId)) {
    x += parent.position.x
    y += parent.position.y
  }
  return rectAt(node, x, y)
}

/** topRects are the places of the nodes in no group. */
export const topRects = (placed: Placed): Rect[] => placed.filter(node => !node.parentId).map(node => rectAt(node, node.position.x, node.position.y))

/** chosenOf is the node to choose for one clicked or keyed: an inner node
 * chooses its group. */
export function chosenOf(graph: WorkflowGraphModel, id: string): string {
  const node = graph.nodes.find(item => item.id === id)
  return node?.kind === "inner" && node.parent ? node.parent : id
}

/** keyedNode is the node Enter or Space on a focused node chooses, or
 * null: a control inside a node (the child run's link) keeps its own
 * keys. */
export function keyedNode(key: string, target: HTMLElement): string | null {
  if (key !== "Enter" && key !== " ") return null
  if (target.closest("a, button, input, select, textarea")) return null
  return target.closest<HTMLElement>(".react-flow__node")?.dataset.id ?? null
}

// The colour each run state gives a node in a canvas's minimap.
const miniColors: Record<NodeRunState, string> = { waiting: "var(--color-lw-line-strong)", running: "var(--color-lw-live)", needs: "var(--color-lw-needs)", failed: "var(--color-lw-error)", done: "color-mix(in srgb, var(--color-lw-done) 70%, transparent)", skipped: "var(--color-lw-line)", suspended: "var(--color-lw-line-strong)" }

/** miniColor is a node's colour in a canvas's minimap, by its state. */
export function miniColor(node: Node): string {
  const state = (node.data as FlowData | undefined)?.extra?.state
  return state ? miniColors[state] : "var(--color-lw-line-strong)"
}

// Below this zoom a canvas draws its nodes compact: the state and a title
// that keeps one size on screen.
const compactBelow = 0.6

/** isCompact says a canvas is zoomed out enough to draw nodes compact. */
export const isCompact = (canvas: boolean, zoom: number) => canvas && zoom < compactBelow

/** lookOf is the graph's colours as React Flow's variables; zoomed out on
 * a canvas, edges keep about a pixel and a half on screen. */
export function lookOf(canvas: boolean, zoom: number): Record<string, string | number> {
  const look: Record<string, string | number> = { "--xy-background-color": "var(--color-lw-ground)", "--xy-edge-stroke": "var(--color-lw-line-strong)", "--xy-minimap-background-color": "var(--color-lw-surface)", "--xy-minimap-mask-background-color": "color-mix(in srgb, var(--color-lw-ground) 70%, transparent)", "--xy-minimap-mask-stroke-color": "var(--color-lw-line-strong)", "--xy-minimap-node-background-color": "var(--color-lw-line-strong)", "--xy-controls-button-background-color": "var(--color-lw-surface)", "--xy-controls-button-color": "var(--color-lw-text)", "--xy-controls-button-border-color": "var(--color-lw-line)" }
  if (canvas) Object.assign(look, { "--lw-zoom": zoom, "--xy-edge-stroke-width": isCompact(canvas, zoom) ? Math.min(6, 1.2 / zoom) : 1 })
  return look
}
