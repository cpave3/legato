// The workflow map drawn with React Flow (LEG-710). Loaded only through
// LazyWorkflowMapCanvas, so React Flow stays out of every other page's
// bundle. Places come from layoutWorkflowMap; this file only draws them.
import { useCallback, useMemo, useRef, useState } from "react"
import { BaseEdge, Controls, EdgeLabelRenderer, Handle, Position, ReactFlow, ReactFlowProvider, useReactFlow, type Edge, type EdgeProps, type EdgeTypes, type Node, type NodeProps, type NodeTypes } from "@xyflow/react"
import "@xyflow/react/dist/base.css"
import { CircleAlert, Clock, Columns3, GitPullRequest, MessageSquare, Play, type LucideIcon } from "lucide-react"
import { edgeLabel, nodeStatus, related, triggerMarkers, type MarkerKind, type WorkflowMap, type WorkflowMapNode } from "../../lib/workflowMap"
import { cn } from "../../lib/utils"
import { TagChip } from "../workflows/WorkflowTagsDialog"
import { roundedPath } from "../agent-graph/roundedPath"
import { layoutWorkflowMap, type MapLayout, type PlacedEdge, type PlacedNode, type StandaloneBox } from "./mapLayout"

export type WorkflowMapCanvasProps = {
  map: WorkflowMap
  // Workflows (by ID) drawn faint: kept only because they call or are
  // called by those the tag filter shows.
  context: ReadonlySet<string>
  // Opens a workflow's page, by name.
  onOpen: (name: string) => void
}

type NodeData = { placed?: PlacedNode; box?: StandaloneBox; called: boolean; dimmed: boolean }
type FlowNode = Node<NodeData>

const markerIcons: Record<MarkerKind, LucideIcon> = { schedule: Clock, column: Columns3, pr: GitPullRequest, chat: MessageSquare, manual: Play }
const statusWords = { on: "triggers on", paused: "triggers paused", mixed: "some triggers paused", none: "" } as const

// React Flow joins edges to handles, so every node has one of each; where a
// line runs comes from the layout's points, not from them.
function Handles() {
  const hidden = "!h-1 !w-1 !min-w-0 !border-0 !bg-transparent"
  return <>
    <Handle id="t" type="target" position={Position.Top} isConnectable={false} className={hidden} />
    <Handle id="s" type="source" position={Position.Bottom} isConnectable={false} className={hidden} />
  </>
}

function WorkflowNode({ data, selected }: NodeProps<FlowNode>) {
  const { node, width, height } = data.placed!
  const markers = triggerMarkers(node, data.called)
  const status = nodeStatus(node)
  return <div data-map-node style={{ width, height }} className={cn(
    "flex cursor-pointer flex-col justify-center gap-1 rounded-lg border bg-lw-surface px-3 font-lw text-lw-text shadow-sm transition-opacity hover:border-lw-accent motion-reduce:transition-none",
    node.broken ? "border-lw-danger-line" : node.missing ? "border-dashed border-lw-line-strong bg-lw-ground" : "border-lw-line-strong",
    selected && "ring-2 ring-lw-accent-ring", data.dimmed && "opacity-30",
  )}>
    <Handles />
    <span className="flex min-w-0 items-center gap-1.5">
      {node.broken && <CircleAlert size={13} aria-hidden="true" className="shrink-0 text-lw-danger" />}
      <span className={cn("truncate text-[13px] font-semibold leading-5", node.missing && "font-medium text-lw-text-2")} title={node.title || node.name}>{node.title || node.name}</span>
      {status !== "none" && <span aria-hidden="true" title={statusWords[status]} className={cn("ml-auto size-2 shrink-0 rounded-full", status === "on" ? "bg-lw-live" : status === "mixed" ? "bg-lw-needs" : "bg-lw-faint")} />}
    </span>
    <span className="flex min-w-0 items-center gap-1.5 text-[11px] leading-4 text-lw-text-2">
      <span className="truncate font-lw-mono">{node.name}</span>
      {node.built_in && <span className="shrink-0">· built in</span>}
      {node.broken && <span className="shrink-0 text-lw-danger">· does not parse</span>}
      {node.missing && <span className="shrink-0">· no such workflow</span>}
    </span>
    {(markers.length > 0 || (node.tags ?? []).length > 0) && <span className="flex min-w-0 items-center gap-1.5 overflow-hidden">
      {markers.map(marker => {
        const Icon = markerIcons[marker.kind]
        return <span key={marker.kind} title={`${marker.label}${marker.kind === "manual" ? "" : marker.enabled ? ", on" : ", paused"}`}
          className={cn("shrink-0", marker.kind === "manual" || marker.enabled ? "text-lw-text-2" : "text-lw-faint")}><Icon size={12} aria-hidden="true" /></span>
      })}
      {(node.tags ?? []).slice(0, 2).map(tag => <TagChip key={tag} tag={tag} className="h-4 px-1.5 text-[10px]" />)}
      {(node.tags ?? []).length > 2 && <span className="shrink-0 font-lw-mono text-[10px] text-lw-faint">+{(node.tags ?? []).length - 2}</span>}
    </span>}
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
  const parts = [node.title || node.name]
  if (node.title && node.title !== node.name) parts.push(node.name)
  if (node.built_in) parts.push("built in")
  if (node.broken) parts.push(`does not parse: ${node.error ?? ""}`)
  if (node.missing) parts.push("no such workflow")
  const markers = triggerMarkers(node, called).map(marker => marker.kind === "manual" ? marker.label.toLowerCase() : `${marker.label.toLowerCase()}, ${marker.enabled ? "on" : "paused"}`)
  return [...parts, ...markers].join(", ")
}

// hoverless says whether this screen cannot hover (a phone), where a first
// tap lights a workflow's calls and a second opens it.
const hoverless = () => typeof window !== "undefined" && typeof window.matchMedia === "function" && window.matchMedia("(hover: none)").matches

function MapView({ map, context, onOpen }: WorkflowMapCanvasProps) {
  const flow = useReactFlow()
  const layout: MapLayout = useMemo(() => layoutWorkflowMap(map), [map])
  const [hovered, setHovered] = useState<string | null>(null)
  const [selected, setSelected] = useState<string | null>(null)
  const lit = useMemo(() => related(map, hovered ?? selected), [map, hovered, selected])
  const called = useMemo(() => new Set(map.edges.map(edge => edge.target)), [map])
  const nodes: FlowNode[] = useMemo(() => [
    ...(layout.standalone ? [{
      id: "box:standalone", type: "box", position: { x: layout.standalone.x, y: layout.standalone.y }, width: layout.standalone.width, height: layout.standalone.height, zIndex: -1,
      data: { box: layout.standalone, called: false, dimmed: false }, draggable: false, selectable: false, focusable: false, connectable: false,
      ariaRole: "group" as const, ariaLabel: `Stand alone, ${layout.standalone.count} ${layout.standalone.count === 1 ? "workflow" : "workflows"}`,
    }] : []),
    ...layout.nodes.map((placed): FlowNode => ({
      id: placed.id, type: "workflow", position: { x: placed.x, y: placed.y }, width: placed.width, height: placed.height,
      data: { placed, called: called.has(placed.id), dimmed: Boolean(lit && !lit.has(placed.id)) || (!lit && context.has(placed.id)) },
      draggable: false, connectable: false, selected: placed.id === selected, ariaRole: "button", ariaLabel: nodeName(placed.node, called.has(placed.id)),
    })),
  ], [layout, lit, context, selected, called])
  const edges: Edge[] = useMemo(() => layout.edges.map(placed => ({
    id: placed.id, source: placed.edge.source, target: placed.edge.target, type: "call", sourceHandle: "s", targetHandle: "t",
    data: { placed, dimmed: Boolean(lit && !(lit.has(placed.edge.source) && lit.has(placed.edge.target))) || (!lit && (context.has(placed.edge.source) && context.has(placed.edge.target))) },
    focusable: false, selectable: false,
    ariaLabel: `${placed.edge.source} calls ${placed.edge.target}${edgeLabel(placed.edge) ? `, ${edgeLabel(placed.edge)}` : ""}`,
  })), [layout, lit, context])

  const nameOf = useCallback((id: string) => layout.nodes.find(item => item.id === id)?.node.name, [layout])
  // tapped is the workflow a touch screen's last tap lit: a tap focuses a
  // node before it clicks it, so focus alone never counts as the first tap.
  const tapped = useRef<string | null>(null)
  const choose = useCallback((id: string) => {
    if (hoverless() && tapped.current !== id) { tapped.current = id; setSelected(id); return }
    const name = nameOf(id)
    if (name) onOpen(name)
  }, [nameOf, onOpen])
  const frame = useRef<HTMLDivElement>(null)
  // Enter opens a focused workflow; focus lights its calls.
  const onKeyDown = useCallback((event: React.KeyboardEvent) => {
    if (event.key !== "Enter" && event.key !== " ") return
    const id = (event.target as HTMLElement).closest<HTMLElement>(".react-flow__node")?.dataset.id
    const name = id ? nameOf(id) : undefined
    if (!name) return
    event.preventDefault()
    onOpen(name)
  }, [nameOf, onOpen])
  const onFocus = useCallback((event: React.FocusEvent) => {
    const id = (event.target as HTMLElement).closest<HTMLElement>(".react-flow__node")?.dataset.id
    if (id && nameOf(id)) setSelected(id)
  }, [nameOf])
  // A phone sees the map's width, from its top, to scroll down; a wider
  // screen sees all of it.
  const fitAll = useCallback(() => {
    const width = frame.current?.clientWidth ?? 0
    const all = [...layout.nodes, ...(layout.standalone ? [layout.standalone] : [])]
    if (!width || all.length === 0) return
    const minX = Math.min(...all.map(item => item.x)), maxX = Math.max(...all.map(item => item.x + item.width))
    const minY = Math.min(...all.map(item => item.y))
    if (width >= 768) {
      void flow.fitView({ padding: 0.1, minZoom: 0.3, maxZoom: 1 })
      return
    }
    const pad = 12
    const zoom = Math.min(1, Math.max(0.45, (width - 2 * pad) / (maxX - minX)))
    void flow.setViewport({ x: pad - minX * zoom, y: pad - minY * zoom, zoom })
  }, [flow, layout])

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
      onNodeMouseEnter={(_, node) => { if (node.type === "workflow") setHovered(node.id) }}
      onNodeMouseLeave={() => setHovered(null)}
      onNodeClick={(_, node) => { if (node.type === "workflow") choose(node.id) }}
      onPaneClick={() => { tapped.current = null; setSelected(null) }}
    >
      <Controls showInteractive={false} position="bottom-left" />
    </ReactFlow>
  </div>
}

export default function WorkflowMapCanvas(props: WorkflowMapCanvasProps) {
  return <ReactFlowProvider><MapView {...props} /></ReactFlowProvider>
}
