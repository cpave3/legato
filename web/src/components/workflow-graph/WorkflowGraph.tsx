// The workflow graph, drawn with React Flow and laid out by dagre. Loaded
// only through LazyWorkflowGraph, so neither library is in the bundle of any
// other page.
import { useCallback, useEffect, useMemo, useState } from "react"
import dagre from "@dagrejs/dagre"
import { Background, Controls, Handle, MiniMap, Position, ReactFlow, ReactFlowProvider, type Edge, type Node, type NodeProps, type NodeTypes, type OnSelectionChangeParams } from "@xyflow/react"
import "@xyflow/react/dist/base.css"
import { ArrowRightLeft, BookOpen, Bot, CircleStop, Clock, Columns3, FileText, FileX, FolderSearch, FolderX, GitBranch, GitPullRequest, Hand, MessageSquareWarning, MessagesSquare, Repeat, Send, StickyNote, Tag, Trash2, Workflow, type LucideIcon } from "lucide-react"
import { nodeName, stepTypeLabel, triggerName, type GraphNode, type WorkflowGraphModel } from "../../lib/workflowGraph"
import { cn } from "../../lib/utils"

export type WorkflowGraphProps = {
  graph: WorkflowGraphModel
  selectedId?: string | null
  onSelect?: (id: string | null) => void
  // direction is LR on wide screens, TB at phone width.
  direction?: "LR" | "TB"
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
const sizes = { step: { width: 240, height: 76 }, trigger: { width: 200, height: 56 } } as const

type FlowData = { node: GraphNode; direction: "LR" | "TB" }
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
    {step.key.value && <span className="truncate font-lw-mono text-[11px] text-lw-text-2" title={step.key.value}>{step.key.value}</span>}
  </div>
}

function TriggerNode({ data, selected }: NodeProps<FlowNode>) {
  const trigger = data.node.trigger!
  const Icon = triggerIcons[trigger.kind] ?? Workflow
  return <div style={sizes.trigger} className={cn("flex items-center gap-2 rounded-full border border-dashed bg-lw-raised px-4 font-lw text-[13px] text-lw-text", selected ? "border-lw-accent ring-2 ring-lw-accent-ring" : "border-lw-line-strong")}>
    <Handles direction={data.direction} />
    <Icon size={14} aria-hidden="true" className="shrink-0 text-lw-text-2" />
    <span className="truncate">{triggerName(trigger.kind)}</span>
  </div>
}

const nodeTypes: NodeTypes = { step: StepNode, trigger: TriggerNode }

// layout places the nodes once per graph. Only start and order edges shape
// it; the other kinds are drawn over the result.
function layout(graph: WorkflowGraphModel, direction: "LR" | "TB"): FlowNode[] {
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
      ariaLabel: nodeName(node),
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

function Graph({ graph, selectedId = null, onSelect, direction = "LR", className }: WorkflowGraphProps) {
  // The layout is computed once per graph and direction; a new graph with
  // the same shape keeps its places.
  const shape = useMemo(() => JSON.stringify([direction, graph.nodes.map(node => [node.id, node.kind]), graph.edges.map(edge => edge.id)]), [graph, direction])
  // eslint-disable-next-line react-hooks/exhaustive-deps
  const placed = useMemo(() => layout(graph, direction), [shape])
  const [selected, setSelected] = useState<string | null>(selectedId)
  useEffect(() => { setSelected(selectedId) }, [selectedId])
  const nodes = useMemo(() => {
    const byId = new Map(graph.nodes.map(node => [node.id, node]))
    return placed.map(node => ({ ...node, data: { ...node.data, node: byId.get(node.id) ?? node.data.node }, selected: node.id === selected }))
  }, [placed, graph, selected])
  const edges = useMemo(() => edgesOf(graph), [graph])
  const choose = useCallback((id: string | null) => {
    setSelected(id)
    onSelect?.(id)
  }, [onSelect])
  const onSelectionChange = useCallback(({ nodes: chosen }: OnSelectionChangeParams) => {
    const id = chosen[0]?.id ?? null
    if (id !== selected) choose(id)
  }, [choose, selected])
  return <div className={cn("h-full min-h-[18rem] w-full", className)} style={{ "--xy-background-color": "var(--color-lw-ground)", "--xy-edge-stroke": "var(--color-lw-line-strong)", "--xy-minimap-background-color": "var(--color-lw-surface)", "--xy-controls-button-background-color": "var(--color-lw-surface)", "--xy-controls-button-color": "var(--color-lw-text)", "--xy-controls-button-border-color": "var(--color-lw-line)" } as React.CSSProperties}>
    <ReactFlow
      nodes={nodes}
      edges={edges}
      nodeTypes={nodeTypes}
      fitView
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
      {direction === "LR" && <MiniMap pannable zoomable ariaLabel="Workflow graph overview" />}
    </ReactFlow>
  </div>
}

export default function WorkflowGraph(props: WorkflowGraphProps) {
  return <ReactFlowProvider><Graph {...props} /></ReactFlowProvider>
}
