// The workflow map drawn with React Flow (LEG-710). Loaded only through
// LazyWorkflowMapCanvas, so React Flow stays out of every other page's
// bundle. Places come from layoutWorkflowMap; this file only draws them.
import { useCallback, useEffect, useMemo, useRef, useState } from "react"
import { BaseEdge, Controls, EdgeLabelRenderer, ReactFlow, ReactFlowProvider, useReactFlow, type Edge, type EdgeProps, type EdgeTypes, type Node, type NodeProps, type NodeTypes } from "@xyflow/react"
import "@xyflow/react/dist/base.css"
import { FlowHandles } from "../agent-graph/FlowHandles"
import { CircleAlert, Clock, Columns3, GitPullRequest, MessageSquare, Play, type LucideIcon } from "lucide-react"
import { edgeLabel, nodeStatus, related, triggerMarkers, type MarkerKind, type TriggerMarker, type WorkflowMap, type WorkflowMapNode } from "../../lib/workflowMap"
import { cn } from "../../lib/utils"
import { TagChip } from "../workflows/WorkflowTagsDialog"
import { roundedPath } from "../agent-graph/roundedPath"
import { layoutWorkflowMap, type MapLayout, type PlacedEdge, type PlacedNode, type StandaloneBox } from "./mapLayout"

export type WorkflowMapCanvasProps = {
  map: WorkflowMap
  // Workflows (by ID) drawn faint: kept only because they call or are
  // called by those the tag filter shows.
  context: ReadonlySet<string>
  // Opens a workflow: the whole node, so a broken file or a missing
  // workflow is never taken for a workflow of its name.
  onOpen: (node: WorkflowMapNode) => void
}

type NodeData = { placed?: PlacedNode; box?: StandaloneBox; called: boolean; dimmed: boolean }
type FlowNode = Node<NodeData>

const markerIcons: Record<MarkerKind, LucideIcon> = { schedule: Clock, column: Columns3, pr: GitPullRequest, chat: MessageSquare, manual: Play }
const statusWords = { on: "triggers on", paused: "triggers paused", mixed: "some triggers paused", none: "" } as const


const statusDot = { on: "bg-lw-live", mixed: "bg-lw-needs", paused: "bg-lw-faint", none: "" } as const

// nodeBorder is a node's frame: red for a broken file, dashed for a
// missing workflow.
function nodeBorder(node: WorkflowMapNode): string {
  if (node.broken) return "border-lw-danger-line"
  if (node.missing) return "border-dashed border-lw-line-strong bg-lw-ground"
  return "border-lw-line-strong"
}

function NodeTitle({ node }: { node: WorkflowMapNode }) {
  const status = nodeStatus(node)
  const title = node.title || node.name
  return <span className="flex min-w-0 items-center gap-1.5">
    {node.broken && <CircleAlert size={13} aria-hidden="true" className="shrink-0 text-lw-danger" />}
    <span className={cn("truncate text-[13px] font-semibold leading-5", node.missing && "font-medium text-lw-text-2")} title={title}>{title}</span>
    {status !== "none" && <span aria-hidden="true" title={statusWords[status]} className={cn("ml-auto size-2 shrink-0 rounded-full", statusDot[status])} />}
  </span>
}

// nodeNote is what a node is besides a workflow file: built in, broken or
// missing.
function nodeNote(node: WorkflowMapNode): { text: string; danger?: boolean } | null {
  if (node.broken) return { text: "does not parse", danger: true }
  if (node.missing) return { text: "no such workflow" }
  return node.built_in ? { text: "built in" } : null
}

function MarkerIcon({ marker }: { marker: TriggerMarker }) {
  const Icon = markerIcons[marker.kind]
  const manual = marker.kind === "manual"
  const state = manual ? "" : marker.enabled ? ", on" : ", paused"
  return <span title={marker.label + state} className={cn("shrink-0", manual || marker.enabled ? "text-lw-text-2" : "text-lw-faint")}><Icon size={12} aria-hidden="true" /></span>
}

function NodeMarks({ node, called }: { node: WorkflowMapNode; called: boolean }) {
  const markers = triggerMarkers(node, called)
  const tags = node.tags ?? []
  if (markers.length === 0 && tags.length === 0) return null
  return <span className="flex min-w-0 items-center gap-1.5 overflow-hidden">
    {markers.map(marker => <MarkerIcon key={marker.kind} marker={marker} />)}
    {tags.slice(0, 2).map(tag => <TagChip key={tag} tag={tag} className="h-4 px-1.5 text-[10px]" />)}
    {tags.length > 2 && <span className="shrink-0 font-lw-mono text-[10px] text-lw-faint">+{tags.length - 2}</span>}
  </span>
}

function WorkflowNode({ data, selected }: NodeProps<FlowNode>) {
  const { node, width, height } = data.placed!
  const note = nodeNote(node)
  return <div data-map-node style={{ width, height }} className={cn(
    "flex cursor-pointer flex-col justify-center gap-1 rounded-lg border bg-lw-surface px-3 font-lw text-lw-text shadow-sm transition-opacity hover:border-lw-accent motion-reduce:transition-none",
    nodeBorder(node), selected && "ring-2 ring-lw-accent-ring", data.dimmed && "opacity-30",
  )}>
    <FlowHandles across={false} />
    <NodeTitle node={node} />
    <span className="flex min-w-0 items-center gap-1.5 text-[11px] leading-4 text-lw-text-2">
      <span className="truncate font-lw-mono">{node.name}</span>
      {note && <span className={cn("shrink-0", note.danger && "text-lw-danger")}>· {note.text}</span>}
    </span>
    <NodeMarks node={node} called={data.called} />
  </div>
}

// BoxNode is the box of workflows that call none and that none call.
function BoxNode({ data }: NodeProps<FlowNode>) {
  const box = data.box!
  return <div style={{ width: box.width, height: box.height }} className="pointer-events-none rounded-xl border border-dashed border-lw-line-strong bg-lw-surface/40 font-lw">
    <p className="flex h-10 items-center gap-1.5 px-4 text-xs text-lw-text-2"><span className="font-medium text-lw-text">Stand alone</span>· call no workflow, and no workflow calls them</p>
  </div>
}

const nodeTypes: NodeTypes = { workflow: WorkflowNode, box: BoxNode }

type EdgeData = { placed: PlacedEdge; dimmed: boolean }

// CallEdge is one workflow calling another, along the layout's points: a
// dashed line when it calls it as one of an input's choices.
function CallEdge({ id, data }: EdgeProps & { data?: EdgeData }) {
  const { placed, dimmed } = data!
  const label = edgeLabel(placed.edge)
  const opacity = dimmed ? 0.12 : 1
  return <>
    <BaseEdge id={id} path={roundedPath(placed.points)} markerEnd="url(#workflow-map-arrow)"
      style={{ stroke: "var(--color-lw-text-2)", strokeWidth: 1.5, strokeDasharray: placed.edge.dynamic ? "6 4" : undefined, fill: "none", opacity }} />
    {label && <EdgeLabelRenderer>
      <span style={{ transform: `translate(-50%, -50%) translate(${placed.labelAt.x}px, ${placed.labelAt.y}px)`, opacity }}
        className="nodrag nopan pointer-events-none absolute rounded border border-lw-line bg-lw-ground px-1 font-lw-mono text-[11px] leading-4 text-lw-text">{label}</span>
    </EdgeLabelRenderer>}
  </>
}

const edgeTypes: EdgeTypes = { call: CallEdge }

function Arrow() {
  return <svg aria-hidden="true" style={{ position: "absolute", width: 0, height: 0 }}>
    <defs>
      <marker id="workflow-map-arrow" viewBox="0 0 10 10" refX="9" refY="5" markerWidth="7" markerHeight="7" orient="auto-start-reverse">
        <path d="M0,0 L10,5 L0,10 z" fill="var(--color-lw-text-2)" />
      </marker>
    </defs>
  </svg>
}

// nodeName is how a node is read out: its title, what it is, how it starts
// and whether its triggers are on.
function nodeName(node: WorkflowMapNode, called: boolean): string {
  const note = nodeNote(node)
  const parts = [node.title || node.name, node.title && node.title !== node.name ? node.name : "", note ? note.text : "", node.broken ? node.error ?? "" : ""]
  return [...parts.filter(Boolean), ...triggerMarkers(node, called).map(markerWords)].join(", ")
}

// markerWords is a trigger marker as it is read out.
function markerWords(marker: TriggerMarker): string {
  const label = marker.label.toLowerCase()
  if (marker.kind === "manual") return label
  return `${label}, ${marker.enabled ? "on" : "paused"}`
}

// hoverless says whether this screen cannot hover (a phone), where a first
// tap lights a workflow's calls and a second opens it.
const hoverless = () => typeof window !== "undefined" && typeof window.matchMedia === "function" && window.matchMedia("(hover: none)").matches

// isDimmed says whether a workflow is drawn faint: outside the lit chain,
// or, with nothing lit, kept only as the tag filter's context.
function isDimmed(lit: Set<string> | null, context: ReadonlySet<string>, ...ids: string[]): boolean {
  if (lit) return !ids.every(id => lit.has(id))
  return ids.every(id => context.has(id))
}

function boxNode(box: StandaloneBox): FlowNode {
  return {
    id: "box:standalone", type: "box", position: { x: box.x, y: box.y }, width: box.width, height: box.height, zIndex: -1,
    data: { box, called: false, dimmed: false }, draggable: false, selectable: false, focusable: false, connectable: false,
    ariaRole: "group", ariaLabel: `Stand alone, ${box.count} ${box.count === 1 ? "workflow" : "workflows"}`,
  }
}

// flowNodes are the layout's nodes as React Flow draws them.
function flowNodes(layout: MapLayout, called: Set<string>, lit: Set<string> | null, context: ReadonlySet<string>, selected: string | null): FlowNode[] {
  const workflows = layout.nodes.map((placed): FlowNode => ({
    id: placed.id, type: "workflow", position: { x: placed.x, y: placed.y }, width: placed.width, height: placed.height,
    data: { placed, called: called.has(placed.id), dimmed: isDimmed(lit, context, placed.id) },
    draggable: false, connectable: false, selected: placed.id === selected, ariaRole: "button", ariaLabel: nodeName(placed.node, called.has(placed.id)),
  }))
  return layout.standalone ? [boxNode(layout.standalone), ...workflows] : workflows
}

function flowEdges(layout: MapLayout, lit: Set<string> | null, context: ReadonlySet<string>): Edge[] {
  return layout.edges.map(placed => {
    const { source, target } = placed.edge
    const label = edgeLabel(placed.edge)
    return {
      id: placed.id, source, target, type: "call", sourceHandle: "s", targetHandle: "t",
      data: { placed, dimmed: isDimmed(lit, context, source, target) },
      focusable: false, selectable: false, ariaLabel: `${source} calls ${target}${label ? `, ${label}` : ""}`,
    }
  })
}

// phoneViewport is where a narrow screen starts: the map's width fitted
// (at a readable zoom at least), from its top, to scroll down.
function phoneViewport(layout: MapLayout, width: number) {
  const all = [...layout.nodes, ...(layout.standalone ? [layout.standalone] : [])]
  const minX = Math.min(...all.map(item => item.x)), maxX = Math.max(...all.map(item => item.x + item.width))
  const minY = Math.min(...all.map(item => item.y))
  const pad = 12
  const zoom = Math.min(1, Math.max(0.45, (width - 2 * pad) / (maxX - minX)))
  return { x: pad - minX * zoom, y: pad - minY * zoom, zoom }
}

// workflowId is a React Flow node's ID when it is a workflow, not the box.
const workflowId = (node: { id: string; type?: string }) => node.type === "workflow" ? node.id : null

// nodeIdOf is the map node an event happened in.
const nodeIdOf = (event: { target: EventTarget }) => (event.target as HTMLElement).closest<HTMLElement>(".react-flow__node")?.dataset.id

// useMapChoice is what the operator points at: the workflow hovered or
// selected (by focus or a tap), and the handlers that choose and open one.
function useMapChoice(layout: MapLayout, onOpen: (node: WorkflowMapNode) => void) {
  const [hovered, setHovered] = useState<string | null>(null)
  const [selected, setSelected] = useState<string | null>(null)
  const nodeOf = useCallback((id: string | undefined) => layout.nodes.find(item => item.id === id)?.node, [layout])
  // tapped is the workflow a touch screen's last tap lit: a tap focuses a
  // node before it clicks it, so focus alone never counts as the first tap.
  const tapped = useRef<string | null>(null)
  const choose = useCallback((id: string) => {
    if (hoverless() && tapped.current !== id) { tapped.current = id; setSelected(id); return }
    const node = nodeOf(id)
    if (node) onOpen(node)
  }, [nodeOf, onOpen])
  // Enter opens a focused workflow; focus lights its calls.
  const onKeyDown = useCallback((event: React.KeyboardEvent) => {
    const node = event.key === "Enter" || event.key === " " ? nodeOf(nodeIdOf(event)) : undefined
    if (!node) return
    event.preventDefault()
    onOpen(node)
  }, [nodeOf, onOpen])
  const onFocus = useCallback((event: React.FocusEvent) => {
    const id = nodeIdOf(event)
    if (nodeOf(id)) setSelected(id!)
  }, [nodeOf])
  const clear = useCallback(() => { tapped.current = null; setSelected(null) }, [])
  return { pointed: hovered ?? selected, selected, setHovered, choose, onKeyDown, onFocus, clear }
}

// useFraming fits the map to the frame: all of it on a wide screen, its
// width from its top on a phone; again whenever the layout changes (a tag
// chosen), since the old view may show none of it. onInit frames the first.
function useFraming(layout: MapLayout) {
  const flow = useReactFlow()
  const frame = useRef<HTMLDivElement>(null)
  const fitAll = useCallback(() => {
    const width = frame.current?.clientWidth ?? 0
    if (!width || layout.nodes.length === 0) return
    if (width >= 768) void flow.fitView({ padding: 0.1, minZoom: 0.3, maxZoom: 1 })
    else void flow.setViewport(phoneViewport(layout, width))
  }, [flow, layout])
  const framed = useRef(layout)
  useEffect(() => {
    if (framed.current === layout) return
    framed.current = layout
    fitAll()
  }, [layout, fitAll])
  return { frame, fitAll }
}

function MapView({ map, context, onOpen }: WorkflowMapCanvasProps) {
  const layout: MapLayout = useMemo(() => layoutWorkflowMap(map), [map])
  const { pointed, selected, setHovered, choose, onKeyDown, onFocus, clear } = useMapChoice(layout, onOpen)
  const { frame, fitAll } = useFraming(layout)
  const lit = useMemo(() => related(map, pointed), [map, pointed])
  const called = useMemo(() => new Set(map.edges.map(edge => edge.target)), [map])
  const nodes = useMemo(() => flowNodes(layout, called, lit, context, selected), [layout, lit, context, selected, called])
  const edges = useMemo(() => flowEdges(layout, lit, context), [layout, lit, context])

  return <div ref={frame} onKeyDown={onKeyDown} onFocus={onFocus} className="relative h-full w-full"
    style={{ "--xy-background-color": "var(--color-lw-ground)", "--xy-controls-button-background-color": "var(--color-lw-surface)", "--xy-controls-button-color": "var(--color-lw-text)", "--xy-controls-button-border-color": "var(--color-lw-line)" } as React.CSSProperties}>
    <Arrow />
    <ReactFlow
      nodes={nodes}
      edges={edges}
      nodeTypes={nodeTypes}
      edgeTypes={edgeTypes}
      onInit={fitAll}
      minZoom={0.2}
      maxZoom={1.75}
      nodesDraggable={false}
      nodesConnectable={false}
      elementsSelectable
      nodesFocusable
      multiSelectionKeyCode={null}
      selectionKeyCode={null}
      deleteKeyCode={null}
      zoomOnDoubleClick={false}
      proOptions={{ hideAttribution: true }}
      onNodeMouseEnter={(_, node) => setHovered(workflowId(node))}
      onNodeMouseLeave={() => setHovered(null)}
      onNodeClick={(_, node) => { const id = workflowId(node); if (id) choose(id) }}
      onPaneClick={clear}
    >
      <Controls showInteractive={false} position="bottom-left" />
    </ReactFlow>
  </div>
}

export default function WorkflowMapCanvas(props: WorkflowMapCanvasProps) {
  return <ReactFlowProvider><MapView {...props} /></ReactFlowProvider>
}
