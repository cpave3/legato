// The agents' graph drawn with React Flow. Loaded only through
// LazyAgentGraphCanvas, so React Flow stays out of every other page's
// bundle. Places come from layoutAgentGraph; this file only draws them.
import { useCallback, useEffect, useMemo, useRef } from "react"
import { BaseEdge, Controls, EdgeLabelRenderer, Handle, Position, ReactFlow, ReactFlowProvider, useReactFlow, type Edge, type EdgeProps, type EdgeTypes, type Node, type NodeProps, type NodeTypes } from "@xyflow/react"
import "@xyflow/react/dist/base.css"
import { ChevronDown, ChevronRight, Workflow } from "lucide-react"
import type { AgentGraphLayout, GraphBox, GraphEdge, GraphEdgeKind, GraphNode, NodeWorkflow } from "../../lib/agentGraph"
import { linkLabels, workflowWords } from "../../lib/agentGraph"
import { runState, stepPosition, stepSegments } from "../../lib/workflowView"
import { StepBar } from "../workflow-status/WorkflowRunStatus"
import { formatAgentTime, stateSeconds } from "../../lib/agentList"
import { stageLabel } from "../../lib/agentStage"
import { cn } from "../../lib/utils"
import { agentLook, subagentLook } from "./look"
import { linkStyles } from "./linkStyles"
import { roundedPath } from "./roundedPath"
import { useReeveName, type ReeveName } from "../../lib/reeveName"

export type AgentGraphCanvasProps = {
  layout: AgentGraphLayout
  direction: "LR" | "TB"
  selectedId: string | null
  onSelect: (id: string | null) => void
  // A double-click on an agent (or a sub-agent: its agent) opens it.
  onOpen: (agentId: string) => void
  onToggleFold: (parentId: string) => void
  onToggleBox: (boxId: string) => void
  // Moves the view to a node each time n changes.
  focusOn?: { id: string; n: number } | null
  // Fits the whole graph each time n changes.
  fit?: number
  // Agents (by ID) drawn faint: those the stage filter leaves out.
  faded?: ReadonlySet<string>
}

type NodeData = { node?: GraphNode; box?: GraphBox; dimmed: boolean; direction: "LR" | "TB"; onToggleBox: (id: string) => void }
type FlowNode = Node<NodeData>

// React Flow joins edges to handles, so every node has one of each; where a
// line runs comes from the layout's points, not from them.
function Handles() {
  const hidden = "!h-1 !w-1 !min-w-0 !border-0 !bg-transparent"
  return <>
    <Handle id="t" type="target" position={Position.Left} isConnectable={false} className={hidden} />
    <Handle id="s" type="source" position={Position.Right} isConnectable={false} className={hidden} />
  </>
}

function AgentNode({ data, selected }: NodeProps<FlowNode>) {
  const node = data.node!
  const agent = node.agent!
  const look = agentLook(agent, useReeveName())
  const time = formatAgentTime(stateSeconds(agent))
  const stage = stageLabel(agent.stage)
  const workflow = node.workflows?.[0]
  return <div style={{ width: node.width, height: node.height }} className={cn(
    "relative flex flex-col justify-center gap-0.5 rounded-lg border-2 bg-lw-surface px-2.5 font-lw text-lw-text shadow-sm transition-opacity",
    workflow && "pb-2",
    look.border, selected && "ring-2 ring-lw-accent-ring", data.dimmed && "opacity-30",
  )}>
    <Handles />
    <span className="flex items-center gap-1.5 text-[11px] leading-4">
      <span className={cn("font-lw-mono uppercase", look.text)}><span aria-hidden="true">{look.glyph}</span> {look.label}{time && ` ${time}`}</span>
      <span className="ml-auto truncate font-lw-mono text-lw-faint">{agent.task_id}</span>
    </span>
    <span className="truncate text-[13px] font-semibold leading-5" title={agent.task_title}>{agent.task_title || agent.task_id}</span>
    <span className="flex min-w-0 items-center gap-1 text-[10px] leading-4 text-lw-text-2">
      {stage && <span className="truncate">{stage}</span>}
      {node.badges?.map((badge) => <span key={badge.kind} title={badge.detail} className="shrink-0 rounded bg-lw-accent-soft px-1 text-lw-accent-text">{badge.label}</span>)}
      {workflow && <RunChip workflow={workflow} more={(node.workflows?.length ?? 1) - 1} />}
    </span>
    {workflow && <RunStrip workflow={workflow} />}
  </div>
}

// RunChip names the agent's step in its run, and how far through the run
// that step is; +N counts its other runs.
function RunChip({ workflow, more }: { workflow: NodeWorkflow; more: number }) {
  const words = workflowWords(workflow)
  const { tone } = runState(workflow.run)
  return <span title={`${workflow.run.workflow_title}: ${words.role}`} className={cn(
    "ml-auto flex min-w-0 shrink items-center gap-0.5 rounded px-1",
    tone === "needs" ? "bg-lw-needs-soft text-lw-needs" : tone === "error" ? "bg-lw-error-soft text-lw-error" : "bg-lw-live-soft text-lw-live",
  )}>
    <Workflow size={10} aria-hidden="true" className="shrink-0" />
    <span className="truncate">{words.title}</span>
    <span className="shrink-0 font-lw-mono">{words.number}/{words.total}</span>
    {more > 0 && <span className="shrink-0 font-lw-mono">+{more}</span>}
  </span>
}

// RunStrip is the run's steps along the foot of the node, as the runs list
// draws them.
function RunStrip({ workflow }: { workflow: NodeWorkflow }) {
  const { run } = workflow
  return <StepBar segments={stepSegments(run, run.steps)} label={runStripLabel(workflow)} className="absolute inset-x-2.5 bottom-1 [&>span]:h-1" />
}

function runStripLabel({ run }: NodeWorkflow): string {
  const position = stepPosition(run, run.steps)
  return `${run.workflow_title}: ${runState(run).label}${position ? `, step ${position.number} of ${position.total}` : ""}`
}

function SubagentNode({ data, selected }: NodeProps<FlowNode>) {
  const node = data.node!
  const look = subagentLook(node.state)
  return <div style={{ width: node.width, height: node.height }} className={cn(
    "flex items-center gap-1.5 rounded-full border-[1.5px] bg-lw-raised px-3 font-lw text-xs text-lw-text transition-opacity",
    look.border, selected && "ring-2 ring-lw-accent-ring", data.dimmed && "opacity-30",
  )}>
    <Handles />
    <span aria-hidden="true" className={look.text}>{look.glyph}</span>
    <span className="truncate" title={[node.job?.label, node.waitsOn].filter(Boolean).join(" · ")}>{node.job?.label || node.job?.id}</span>
  </div>
}

function FoldNode({ data }: NodeProps<FlowNode>) {
  const node = data.node!
  return <div style={{ width: node.width, height: node.height }} className={cn("flex items-center justify-center rounded-full border-[1.5px] border-dashed border-lw-line-strong bg-lw-ground font-lw text-xs text-lw-text-2", data.dimmed && "opacity-30")}>
    <Handles />
    +{node.hidden} more
  </div>
}

function ReeveNode({ data, selected }: NodeProps<FlowNode>) {
  const node = data.node!
  const look = agentLook(node.agent!)
  return <div style={{ width: node.width, height: node.height }} className={cn("flex items-center justify-center gap-1.5 rounded-full border-2 bg-lw-surface px-3 font-lw text-[13px] font-semibold text-lw-text", look.border, selected && "ring-2 ring-lw-accent-ring", data.dimmed && "opacity-30")}>
    <Handles />
    <span aria-hidden="true" className={look.text}>{look.glyph}</span>
    <span className="truncate">{node.agent?.task_title || "Reeve"}</span>
  </div>
}

// BoxNode is a repository's box, drawn under its agents.
function BoxNode({ data }: NodeProps<FlowNode>) {
  const box = data.box!
  const Chevron = box.collapsed ? ChevronRight : ChevronDown
  return <div style={{ width: box.width, height: box.height }} className="pointer-events-none rounded-xl border border-lw-line-strong bg-lw-surface/40 font-lw">
    <button
      type="button"
      aria-expanded={!box.collapsed}
      aria-label={`${box.collapsed ? "Open" : "Close"} ${box.label}`}
      onClick={(event) => { event.stopPropagation(); data.onToggleBox(box.id) }}
      className="nodrag nopan pointer-events-auto flex h-8 w-full items-center gap-1.5 px-3 text-left text-xs text-lw-text-2 hover:text-lw-text"
    >
      <Chevron size={14} aria-hidden="true" />
      <span className="truncate font-lw-mono font-medium text-lw-text">{box.label}</span>
      <span className="shrink-0">· {box.agents}</span>
    </button>
  </div>
}

const nodeTypes: NodeTypes = { agent: AgentNode, subagent: SubagentNode, fold: FoldNode, reeve: ReeveNode, box: BoxNode }

type EdgeData = { edge: GraphEdge; dimmed: boolean; lit: boolean }

// The Reeve's watch lines are faint until the Reeve or the agent it watches
// is selected (LEG-615): there are many, and they say less than the rest.
const QUIET = 0.35

// LinkEdge is a link between two agents, along the points the layout gave
// it: straight runs, around boxes and never through them, with rounded
// corners.
function LinkEdge({ id, data }: EdgeProps & { data?: EdgeData }) {
  const edge = data!.edge
  const style = linkStyles[edge.kind]
  const quiet = edge.kind === "reeve_watch" && !data?.lit
  const opacity = data?.dimmed ? 0.12 : quiet ? QUIET : edge.opacity
  // A run link's label is the run's title, which the nodes' chips and the
  // card already say.
  const label = edge.kind === "peer_message" ? `${edge.count ?? 1}×` : edge.kind === "workflow" ? undefined : edge.label
  return <>
    <BaseEdge id={id} path={roundedPath(edge.points)} markerEnd={style.arrow ? `url(#agent-graph-arrow-${edge.kind})` : undefined}
      style={{ stroke: style.stroke, strokeWidth: quiet ? Math.min(style.width, 1.5) : style.width, strokeDasharray: style.dash, strokeLinecap: "round", strokeLinejoin: "round", fill: "none", opacity }} />
    {label && !data?.dimmed && <EdgeLabelRenderer>
      <span style={{ transform: `translate(-50%, -50%) translate(${edge.labelAt.x}px, ${edge.labelAt.y}px)`, opacity }} className="nodrag nopan pointer-events-none absolute rounded border border-lw-line bg-lw-ground px-1 font-lw-mono text-[11px] leading-4 text-lw-text">{label}</span>
    </EdgeLabelRenderer>}
  </>
}

const edgeTypes: EdgeTypes = { link: LinkEdge }

// Arrow heads in each link's colour; React Flow's own markers take one
// colour per marker, and the theme's colours are variables.
function Arrows() {
  return <svg aria-hidden="true" style={{ position: "absolute", width: 0, height: 0 }}>
    <defs>
      {(Object.keys(linkStyles) as GraphEdgeKind[]).filter((kind) => linkStyles[kind].arrow).map((kind) => (
        <marker key={kind} id={`agent-graph-arrow-${kind}`} viewBox="0 0 10 10" refX="9" refY="5" markerWidth="7" markerHeight="7" orient="auto-start-reverse">
          <path d="M0,0 L10,5 L0,10 z" fill={linkStyles[kind].stroke} />
        </marker>
      ))}
    </defs>
  </svg>
}

// neighbours are the nodes linked to the selected one, itself included:
// what stays lit while the rest dims.
function neighbours(layout: AgentGraphLayout, selected: string | null): Set<string> | null {
  if (!selected) return null
  const lit = new Set([selected])
  // A sub-agent lights its whole agent's tree.
  const owner = layout.nodes.find((node) => node.id === selected)?.agentId
  for (const node of layout.nodes) if (owner && node.agentId === owner) lit.add(node.id)
  for (const edge of layout.edges) {
    if (lit.has(edge.source) || lit.has(edge.target)) {
      lit.add(edge.source)
      lit.add(edge.target)
    }
  }
  return lit
}

function Graph({ layout, direction, selectedId, onSelect, onOpen, onToggleFold, onToggleBox, focusOn, fit, faded }: AgentGraphCanvasProps) {
  const flow = useReactFlow()
  const reeve = useReeveName()
  const lit = useMemo(() => neighbours(layout, selectedId), [layout, selectedId])
  const nodes: FlowNode[] = useMemo(() => [
    ...layout.boxes.map((box): FlowNode => ({
      id: box.id, type: "box", position: { x: box.x, y: box.y }, width: box.width, height: box.height, zIndex: -1,
      data: { box, dimmed: false, direction, onToggleBox }, draggable: false, selectable: false, focusable: false, connectable: false, ariaRole: "group", ariaLabel: `${box.label}, ${box.agents} ${box.agents === 1 ? "agent" : "agents"}`,
    })),
    ...layout.nodes.map((node): FlowNode => ({
      id: node.id, type: node.kind, position: { x: node.x, y: node.y }, width: node.width, height: node.height,
      data: { node, dimmed: Boolean(lit && !lit.has(node.id)) || Boolean(faded?.has(node.agentId)), direction, onToggleBox },
      draggable: false, connectable: false, selected: node.id === selectedId, ariaRole: "button", ariaLabel: nodeName(node, reeve),
    })),
  ], [layout, lit, selectedId, direction, onToggleBox, faded, reeve])
  const edges: Edge[] = useMemo(() => {
    const agentOf = new Map(layout.nodes.map((node) => [node.id, node.agentId]))
    const fadedEnd = (id: string) => Boolean(faded?.has(agentOf.get(id) ?? id))
    return layout.edges.map((edge) => ({
      id: edge.id, source: edge.source, target: edge.target, type: "link", sourceHandle: "s", targetHandle: "t",
      data: {
        edge,
        dimmed: Boolean(lit && !(lit.has(edge.source) && lit.has(edge.target))) || fadedEnd(edge.source) || fadedEnd(edge.target),
        lit: Boolean(lit && lit.has(edge.source) && lit.has(edge.target)),
      },
      focusable: false, selectable: false, zIndex: edge.kind === "subagent" ? 0 : 1,
      ariaLabel: `${linkLabels[edge.kind]}: ${edge.source} to ${edge.target}`,
    }))
  }, [layout, lit, faded])

  const frame = useRef<HTMLDivElement>(null)
  // Enter or Space on a focused node selects it, as a click does.
  const onKeyDown = useCallback((event: React.KeyboardEvent) => {
    if (event.key !== "Enter" && event.key !== " ") return
    const id = (event.target as HTMLElement).closest<HTMLElement>(".react-flow__node")?.dataset.id
    const node = id ? layout.nodes.find((item) => item.id === id) : undefined
    if (!node) return
    event.preventDefault()
    if (node.kind === "fold") onToggleFold(node.id.slice(0, -2))
    else onSelect(node.id)
  }, [layout, onSelect, onToggleFold])
  // Left to right the whole graph is fitted. Top to bottom (a phone) its
  // width is, and the view starts at its top, to be scrolled down.
  const fitAll = useCallback(() => {
    if (direction === "LR") {
      void flow.fitView({ padding: 0.1, minZoom: 0.25, maxZoom: 1 })
      return
    }
    const width = frame.current?.clientWidth ?? 0
    const all = [...layout.boxes, ...layout.nodes]
    if (!width || all.length === 0) return
    const minX = Math.min(...all.map((item) => item.x)), maxX = Math.max(...all.map((item) => item.x + item.width))
    const minY = Math.min(...all.map((item) => item.y))
    const pad = 12
    const zoom = Math.min(1, Math.max(0.5, (width - 2 * pad) / (maxX - minX)))
    void flow.setViewport({ x: pad - minX * zoom, y: pad - minY * zoom, zoom })
  }, [flow, direction, layout])
  useEffect(() => {
    if (fit) fitAll()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [fit])
  useEffect(() => {
    if (!focusOn) return
    const node = layout.nodes.find((item) => item.id === focusOn.id)
    if (node) void flow.setCenter(node.x + node.width / 2, node.y + node.height / 2, { zoom: Math.max(flow.getZoom(), 0.9) })
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [focusOn?.n])

  return <div ref={frame} onKeyDown={onKeyDown} className="relative h-full min-h-[18rem] w-full" style={{ "--xy-background-color": "var(--color-lw-ground)", "--xy-controls-button-background-color": "var(--color-lw-surface)", "--xy-controls-button-color": "var(--color-lw-text)", "--xy-controls-button-border-color": "var(--color-lw-line)" } as React.CSSProperties}>
    <Arrows />
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
      onNodeClick={(_, node) => {
        if (node.type === "box") return
        if (node.type === "fold") { onToggleFold(node.id.slice(0, -2)); return }
        onSelect(node.id)
      }}
      onNodeDoubleClick={(_, node) => {
        const graphNode = (node.data as NodeData).node
        if (graphNode && graphNode.kind !== "fold") onOpen(graphNode.agentId)
      }}
      onPaneClick={() => onSelect(null)}
    >
      <Controls showInteractive={false} position="bottom-left" />
    </ReactFlow>
  </div>
}

// nodeName is how a node is read out: what it is and what state it is in.
function nodeName(node: GraphNode, reeve: ReeveName): string {
  switch (node.kind) {
    case "agent":
    case "reeve": {
      const name = `${node.agent?.task_id}, ${agentLook(node.agent!, reeve).label}${node.agent?.task_title ? `: ${node.agent.task_title}` : ""}`
      const workflow = node.workflows?.[0]
      if (!workflow) return name
      const words = workflowWords(workflow)
      return `${name}, run ${workflow.run.workflow_title}, step ${words.number} of ${words.total}: ${words.title}`
    }
    case "subagent":
      return `Sub-agent ${node.job?.label ?? node.job?.id}, ${node.waitsOn ?? subagentLook(node.state).label}`
    case "fold":
      return `${node.hidden} more sub-agents`
  }
}

export default function AgentGraphCanvas(props: AgentGraphCanvasProps) {
  return <ReactFlowProvider><Graph {...props} /></ReactFlowProvider>
}
