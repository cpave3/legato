// The workflow graph, drawn with React Flow and laid out by dagre. Loaded
// only through LazyWorkflowGraph, so neither library is in the bundle of any
// other page.
import { useCallback, useEffect, useMemo, useRef, useState } from "react"
import dagre from "@dagrejs/dagre"
import { Background, Controls, Handle, MiniMap, Position, ReactFlow, ReactFlowProvider, type Edge, type Node, type NodeProps, type NodeTypes, type OnSelectionChangeParams, type ReactFlowInstance } from "@xyflow/react"
import "@xyflow/react/dist/base.css"
import { ArrowRightLeft, BookOpen, Bot, CircleStop, Clock, Columns3, FileText, FileX, FolderSearch, FolderX, GitBranch, GitPullRequest, Hand, MessageSquareWarning, MessagesSquare, Repeat, Send, StickyNote, Tag, Trash2, Workflow, type LucideIcon } from "lucide-react"
import { keyValueText, nodeName, stepTypeLabel, triggerName, triggerOn, type GraphNode, type GraphNodeExtra, type WorkflowGraphModel } from "../../lib/workflowGraph"
import { cn } from "../../lib/utils"

export type WorkflowGraphProps = {
  graph: WorkflowGraphModel
  selectedId?: string | null
  onSelect?: (id: string | null) => void
  // direction is LR on wide screens, TB at phone width.
  direction?: "LR" | "TB"
  // extras is what the page knows of each node, by node ID.
  extras?: Record<string, GraphNodeExtra>
  className?: string
}

const stepIcons: Record<string, LucideIcon> = {
  "agent.run": Bot,
  "agent.dispatch": Send,
  "agent.interview": MessagesSquare,
  "agent.stop": CircleStop,
  "task.note": StickyNote,
  "task.move": ArrowRightLeft,
  "task.context": FileText,
  "task.metadata.set": Tag,
  "task.working_dir.resolve": FolderSearch,
  "github.pr.prepare": GitPullRequest,
  "github.pr.review_passes": Repeat,
  "repo.ensure": GitBranch,
  "worktree.remove": FolderX,
  "file.remove": FileX,
  "chimera.session.remove": Trash2,
  "learning.packet.prepare": BookOpen,
  "learning.proposals.submit": BookOpen,
}

const triggerIcons: Record<string, LucideIcon> = {
  manual: Hand,
  "task.entered_column": Columns3,
  schedule: Clock,
  "pr.changes_requested": MessageSquareWarning,
}

// Node sizes come from fixed classes, so the layout never waits for the
// nodes to be measured.
const sizes = { step: { width: 240, height: 76 }, trigger: { width: 220, height: 60 } } as const

type FlowData = { node: GraphNode; extra?: GraphNodeExtra; direction: "LR" | "TB" }
type FlowNode = Node<FlowData>

function Handles({ direction }: { direction: "LR" | "TB" }) {
  const [target, source] = direction === "LR" ? [Position.Left, Position.Right] : [Position.Top, Position.Bottom]
  return <>
    <Handle type="target" position={target} isConnectable={false} className="!h-1.5 !w-1.5 !border-0 !bg-lw-line-strong" />
    <Handle type="source" position={source} isConnectable={false} className="!h-1.5 !w-1.5 !border-0 !bg-lw-line-strong" />
  </>
}

function StepNode({ data, selected }: NodeProps<FlowNode>) {
  const step = data.node.step!
  const Icon = stepIcons[step.type] ?? Workflow
  return <div style={sizes.step} className={cn("flex flex-col justify-center gap-0.5 rounded-lg border bg-lw-surface px-3 py-2 text-left font-lw text-lw-text shadow-sm", selected ? "border-lw-accent ring-2 ring-lw-accent-ring" : "border-lw-line-strong")}>
    <Handles direction={data.direction} />
    <span className="flex items-center gap-1.5 text-[11px] font-medium uppercase tracking-wide text-lw-text-2">
      <Icon size={13} aria-hidden="true" />{stepTypeLabel(step.type)}
    </span>
    <span className="truncate text-[13px] font-semibold" title={step.title}>{step.title}</span>
    {step.key.value && <span className="truncate font-lw-mono text-[11px] text-lw-text-2" title={step.key.value}>{keyValueText(step.key.value)}</span>}
  </div>
}

function TriggerNode({ data, selected }: NodeProps<FlowNode>) {
  const trigger = data.node.trigger!
  const Icon = triggerIcons[trigger.kind] ?? Workflow
  const manual = trigger.kind === "manual"
  const on = triggerOn(trigger, data.extra)
  return <div style={sizes.trigger} className={cn("flex items-center gap-2.5 rounded-2xl border border-dashed bg-lw-raised px-3.5 font-lw text-[13px] text-lw-text", selected ? "border-lw-accent ring-2 ring-lw-accent-ring" : "border-lw-line-strong", !manual && !on && "opacity-70")}>
    <Handles direction={data.direction} />
    <Icon size={15} aria-hidden="true" className="shrink-0 text-lw-text-2" />
    <span className="flex min-w-0 flex-col">
      <span className="truncate font-medium" title={trigger.summary}>{trigger.summary || triggerName(trigger.kind)}</span>
      {!manual && <span className="flex items-center gap-1.5 text-[11px] text-lw-text-2">
        <span aria-hidden="true" className={cn("h-1.5 w-1.5 rounded-full", on ? "bg-lw-done" : "bg-lw-faint")} />
        <span className={on ? "text-lw-done" : undefined}>{on ? "on here" : "paused here"}</span>
        {trigger.max_runs_per_day ? <span title={`Loop stop: ${trigger.max_runs_per_day} runs per item per day`} className="rounded border border-lw-line px-1 font-lw-mono text-[10px]">{trigger.max_runs_per_day}/day</span> : null}
      </span>}
    </span>
  </div>
}

const nodeTypes: NodeTypes = { step: StepNode, trigger: TriggerNode }

// layout places the nodes once per graph. Only start and order edges shape
// it; the other kinds are drawn over the result.
function layout(graph: WorkflowGraphModel, direction: "LR" | "TB"): Omit<FlowNode, "ariaLabel">[] {
  const g = new dagre.graphlib.Graph()
  g.setGraph({ rankdir: direction, nodesep: 28, ranksep: 56, marginx: 8, marginy: 8 })
  g.setDefaultEdgeLabel(() => ({}))
  for (const node of graph.nodes) g.setNode(node.id, { ...sizes[node.kind] })
  for (const edge of graph.edges) if (edge.kind === "start" || edge.kind === "order") g.setEdge(edge.source, edge.target)
  dagre.layout(g)
  return graph.nodes.map(node => {
    const placed = g.node(node.id)
    const size = sizes[node.kind]
    return {
      id: node.id,
      type: node.kind,
      position: { x: placed.x - size.width / 2, y: placed.y - size.height / 2 },
      width: size.width,
      height: size.height,
      data: { node, direction },
      draggable: false,
      connectable: false,
      ariaRole: "button",
    }
  })
}

function edgesOf(graph: WorkflowGraphModel): Edge[] {
  return graph.edges.filter(edge => edge.kind === "start" || edge.kind === "order").map(edge => ({
    id: edge.id,
    source: edge.source,
    target: edge.target,
    focusable: false,
    selectable: false,
    style: edge.kind === "start" ? { strokeDasharray: "4 3" } : undefined,
  }))
}

function Graph({ graph, selectedId = null, onSelect, direction = "LR", extras, className }: WorkflowGraphProps) {
  // The layout is computed once per graph and direction; a new graph with
  // the same shape keeps its places.
  const shape = useMemo(() => JSON.stringify([direction, graph.nodes.map(node => [node.id, node.kind]), graph.edges.map(edge => edge.id)]), [graph, direction])
  // eslint-disable-next-line react-hooks/exhaustive-deps
  const placed = useMemo(() => layout(graph, direction), [shape])
  const [selected, setSelected] = useState<string | null>(selectedId)
  useEffect(() => { setSelected(selectedId) }, [selectedId])
  const nodes = useMemo(() => {
    const byId = new Map(graph.nodes.map(node => [node.id, node]))
    return placed.map(node => {
      const current = byId.get(node.id) ?? node.data.node
      const extra = extras?.[node.id]
      return { ...node, data: { ...node.data, node: current, extra }, ariaLabel: nodeName(current, extra), selected: node.id === selected }
    })
  }, [placed, graph, selected, extras])
  const edges = useMemo(() => edgesOf(graph), [graph])
  const frame = useRef<HTMLDivElement>(null)
  // startView fits the graph when it fits at a readable size, else shows it
  // from its start (the triggers) at that size, for the operator to pan.
  const startView = useCallback((flow: Pick<ReactFlowInstance, "setViewport">) => {
    const width = frame.current?.clientWidth ?? 0, height = frame.current?.clientHeight ?? 0
    const xs = placed.flatMap(node => [node.position.x, node.position.x + (node.width ?? 0)])
    const ys = placed.flatMap(node => [node.position.y, node.position.y + (node.height ?? 0)])
    const [minX, maxX, minY, maxY] = [Math.min(...xs), Math.max(...xs), Math.min(...ys), Math.max(...ys)]
    const pad = 24, boundsW = maxX - minX, boundsH = maxY - minY
    const zoom = Math.min(1, Math.max(0.75, Math.min((width - 2 * pad) / boundsW, (height - 2 * pad) / boundsH)))
    const x = boundsW * zoom <= width - 2 * pad ? (width - boundsW * zoom) / 2 - minX * zoom : pad - minX * zoom
    const y = boundsH * zoom <= height - 2 * pad ? (height - boundsH * zoom) / 2 - minY * zoom : pad - minY * zoom
    void flow.setViewport({ x, y, zoom })
  }, [placed])
  const choose = useCallback((id: string | null) => {
    setSelected(id)
    onSelect?.(id)
  }, [onSelect])
  const onSelectionChange = useCallback(({ nodes: chosen }: OnSelectionChangeParams) => {
    const id = chosen[0]?.id ?? null
    if (id !== selected) choose(id)
  }, [choose, selected])
  return <div ref={frame} className={cn("h-full min-h-[18rem] w-full", className)} style={{ "--xy-background-color": "var(--color-lw-ground)", "--xy-edge-stroke": "var(--color-lw-line-strong)", "--xy-minimap-background-color": "var(--color-lw-surface)", "--xy-minimap-mask-background-color": "color-mix(in srgb, var(--color-lw-ground) 70%, transparent)", "--xy-minimap-mask-stroke-color": "var(--color-lw-line-strong)", "--xy-minimap-node-background-color": "var(--color-lw-line-strong)", "--xy-controls-button-background-color": "var(--color-lw-surface)", "--xy-controls-button-color": "var(--color-lw-text)", "--xy-controls-button-border-color": "var(--color-lw-line)" } as React.CSSProperties}>
    <ReactFlow
      nodes={nodes}
      edges={edges}
      nodeTypes={nodeTypes}
      onInit={startView}
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
      onNodeClick={(_, node) => choose(node.id)}
      onPaneClick={() => choose(null)}
      onSelectionChange={onSelectionChange}
    >
      <Background gap={20} size={1} />
      <Controls showInteractive={false} />
      {direction === "LR" && <MiniMap pannable zoomable ariaLabel="Workflow graph overview" style={{ width: 160, height: 90 }} className="!rounded-md !border !border-lw-line" />}
    </ReactFlow>
  </div>
}

export default function WorkflowGraph(props: WorkflowGraphProps) {
  return <ReactFlowProvider><Graph {...props} /></ReactFlowProvider>
}
